package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/lotus-exchange/lotus-exchange/internal/auth"
	"github.com/lotus-exchange/lotus-exchange/internal/market"
	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/odds"
	"github.com/lotus-exchange/lotus-exchange/pkg/logger"
	natsclient "github.com/lotus-exchange/lotus-exchange/pkg/nats"
	"github.com/lotus-exchange/lotus-exchange/pkg/service"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	log := logger.New(getEnv("ENVIRONMENT", "development"))
	slog.SetDefault(log)

	port := getEnv("ODDS_SERVICE_PORT", "8086")
	log.Info("starting odds service", "port", port)

	db, err := service.OpenPostgres(
		ctx,
		getEnv("DATABASE_URL", ""),
		getIntEnv("DB_MAX_OPEN_CONNS", 25),
		getIntEnv("DB_MAX_IDLE_CONNS", 10),
		getDurationEnv("DB_CONN_MAX_LIFETIME", 5*time.Minute),
	)
	if err != nil {
		log.Error("db", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	// Redis intentionally uses raw client here because the odds service needs
	// the *redis.Client pointer for the odds service constructor; service.OpenRedis
	// returns the same type.
	var rdb *redis.Client
	rdb, err = service.OpenRedis(ctx, getEnv("REDIS_URL", "localhost:6379"), getEnv("REDIS_PASSWORD", ""), getIntEnv("REDIS_POOL_SIZE", 10))
	if err != nil {
		log.Error("redis", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	// Odds service uses the JetStream-backed wrapper from pkg/nats to manage
	// streams and publish odds updates, so we keep that client rather than
	// the core nats.Conn.
	nc, err := natsclient.NewClient(getEnv("NATS_URL", "nats://localhost:4222"), log)
	if err != nil {
		log.Error("failed to connect to NATS", "error", err)
		os.Exit(1)
	}
	defer nc.Close()

	// Create NATS stream for odds updates
	if _, err := nc.CreateStream(ctx, "ODDS", []string{"odds.update.>"}); err != nil {
		log.Error("failed to create NATS stream", "error", err)
		os.Exit(1)
	}

	// ── Auth service (for JWT validation on WebSocket) ──────────
	authService, err := auth.NewService(
		db, rdb, log,
		getDurationEnv("ACCESS_TOKEN_EXPIRY", 15*time.Minute),
		getDurationEnv("REFRESH_TOKEN_EXPIRY", 7*24*time.Hour),
		getEnv("ED25519_PRIVATE_KEY", ""),
		getEnv("ED25519_PUBLIC_KEY", ""),
	)
	if err != nil {
		log.Error("failed to create auth service", "error", err)
		os.Exit(1)
	}

	// ── Odds provider (pluggable) ───────────────────────────────
	var oddsProvider odds.OddsProvider
	switch getEnv("ODDS_PROVIDER", "mock") {
	case "entity_sports":
		oddsProvider = odds.NewEntitySportsProvider(
			getEnv("ENTITY_SPORTS_API_KEY", ""),
			getEnv("ENTITY_SPORTS_URL", "https://rest.entitysport.com/v2"),
			log,
		)
	default:
		oddsProvider = odds.NewMockProvider(
			getFloatEnv("MOCK_VOLATILITY", 0.05),
			getDurationEnv("MOCK_UPDATE_INTERVAL", 2*time.Second),
		)
	}
	defer oddsProvider.Close()

	// ── Services ────────────────────────────────────────────────
	oddsService := odds.NewService(oddsProvider, rdb, log)
	oddsService.SetNATS(nc) // publish odds updates to NATS once in the service layer
	marketService := market.NewService(db, log)

	// ── Mock market seed ────────────────────────────────────────
	//
	// The integration test suite (scripts/api-test) expects mock markets
	// like "mock-ipl-match-001" to be queryable immediately after the
	// odds-service comes up — both their /markets/{id}/odds view and any
	// /bet/place calls against them. The mock provider already knows the
	// full catalogue; we just push it into Redis at boot so the HTTP
	// handlers have something to serve on a cold cache.
	//
	// We only seed for the mock provider. entity_sports is a real upstream
	// and is expected to push real updates via Subscribe.
	if oddsProvider.Name() == "mock" {
		seedCtx, seedCancel := context.WithTimeout(ctx, 10*time.Second)
		if mkts, runners, seedErr := oddsService.SeedMockMarkets(seedCtx); seedErr != nil {
			log.Warn("mock market seed failed", "error", seedErr)
		} else {
			log.Info("mock markets seeded", "markets", mkts, "runners", runners)
		}
		seedCancel()

		// Background re-seed loop. The mock cache uses a 5-minute Redis
		// TTL so the entries don't pile up forever, but in dev / CI
		// that means the catalogue disappears between an integration
		// suite restart and the next run. Re-seed every 60s — well
		// below the TTL — so the cache is always populated.
		go func() {
			tick := time.NewTicker(60 * time.Second)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					rsCtx, rsCancel := context.WithTimeout(ctx, 10*time.Second)
					if _, _, err := oddsService.SeedMockMarkets(rsCtx); err != nil {
						log.Warn("mock market re-seed failed", "error", err)
					}
					rsCancel()
				}
			}
		}()
	}

	// ── WebSocket state ─────────────────────────────────────────
	wsConns := &sync.Map{}
	corsOrigins := getStringSliceEnv("CORS_ORIGINS", []string{"http://localhost:3000"})

	// ── Router ──────────────────────────────────────────────────
	mux := http.NewServeMux()

	// Prometheus scrape endpoint
	mux.Handle("GET /metrics", service.MetricsHandler())

	mux.Handle("GET /health", service.HealthHandler("odds-service", map[string]service.HealthCheck{
		"db":    func(ctx context.Context) error { return db.PingContext(ctx) },
		"redis": func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
		"nats": func(_ context.Context) error {
			if !nc.Conn().IsConnected() {
				return errors.New("disconnected")
			}
			return nil
		},
	}))

	// ── Public market/sports routes ─────────────────────────────
	mux.HandleFunc("GET /api/v1/sports", func(w http.ResponseWriter, r *http.Request) {
		sports, err := marketService.ListSports(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sports)
	})

	mux.HandleFunc("GET /api/v1/competitions", func(w http.ResponseWriter, r *http.Request) {
		sport := r.URL.Query().Get("sport")
		competitions, err := marketService.ListCompetitions(r.Context(), sport)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(competitions)
	})

	mux.HandleFunc("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		competitionID := r.URL.Query().Get("competition_id")
		sport := r.URL.Query().Get("sport")
		events, err := marketService.ListEvents(r.Context(), competitionID, sport)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(events)
	})

	mux.HandleFunc("GET /api/v1/events/{id}/markets", func(w http.ResponseWriter, r *http.Request) {
		eventID := r.PathValue("id")
		markets, err := marketService.ListEventMarkets(r.Context(), eventID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(markets)
	})

	// ── Public odds routes ──────────────────────────────────────
	mux.HandleFunc("GET /api/v1/markets", func(w http.ResponseWriter, r *http.Request) {
		sport := r.URL.Query().Get("sport")
		markets, err := oddsService.FetchMarkets(r.Context(), sport)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		marketService.SyncFromProvider(r.Context(), markets)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(markets)
	})

	mux.HandleFunc("GET /api/v1/markets/{id}/odds", func(w http.ResponseWriter, r *http.Request) {
		marketID := r.PathValue("id")
		update, err := oddsService.GetLatestOdds(r.Context(), marketID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(update)
	})

	// /odds/status is the cheap health/overview endpoint consumed by the
	// integration test suite and ops dashboards. It reports which provider
	// is active, how many markets/runners are currently warm in the Redis
	// cache and when the cache was last refreshed. The counters are kept
	// in-memory on the odds Service and updated by SeedMockMarkets at
	// startup (and, in the future, by any background refresh job).
	mux.HandleFunc("GET /api/v1/odds/status", func(w http.ResponseWriter, r *http.Request) {
		status := oddsService.GetStatus(r.Context())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	// wsWrite writes a JSON message to the WebSocket with a 5-second deadline.
	wsWrite := func(c *websocket.Conn, v interface{}) error {
		writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer writeCancel()
		return wsjson.Write(writeCtx, c, v)
	}

	// ── WebSocket handler ───────────────────────────────────────
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		// Validate JWT from query param
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, `{"error":"missing token query parameter"}`, http.StatusUnauthorized)
			return
		}
		claims, err := authService.ValidateToken(token)
		if err != nil {
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}

		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: corsOrigins,
		})
		if err != nil {
			log.Error("websocket accept error", "error", err)
			return
		}
		defer c.CloseNow()

		connID := uuid.New().String()
		wsConns.Store(connID, c)
		defer wsConns.Delete(connID)

		wsCtx := r.Context()

		wsWrite(c, map[string]string{"type": "authenticated", "user": claims.Username})

		heartbeatDone := make(chan struct{})
		var heartbeatOnce sync.Once
		closeHeartbeat := func() { heartbeatOnce.Do(func() { close(heartbeatDone) }) }

		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-wsCtx.Done():
					closeHeartbeat()
					return
				case <-ticker.C:
					if err := c.Ping(wsCtx); err != nil {
						closeHeartbeat()
						return
					}
				}
			}
		}()

		var subCancel context.CancelFunc
		cancelSubscription := func() {
			if subCancel != nil {
				subCancel()
				subCancel = nil
			}
		}
		defer cancelSubscription()

		for {
			var msg struct {
				Type    string `json:"type"`
				Payload struct {
					MarketIDs []string `json:"market_ids"`
				} `json:"payload"`
			}

			if err := wsjson.Read(wsCtx, c, &msg); err != nil {
				break
			}

			switch msg.Type {
			case "subscribe":
				if len(msg.Payload.MarketIDs) == 0 {
					continue
				}
				cancelSubscription()
				subCtx, cancel := context.WithCancel(wsCtx)
				subCancel = cancel

				updates, err := oddsService.StartSubscription(subCtx, msg.Payload.MarketIDs)
				if err != nil {
					wsWrite(c, map[string]string{"type": "error", "message": err.Error()})
					cancel()
					subCancel = nil
					continue
				}

				go func() {
					for update := range updates {
						if writeErr := wsWrite(c, map[string]interface{}{
							"type":    "odds_update",
							"payload": update,
						}); writeErr != nil {
							return
						}
					}
				}()

			case "unsubscribe":
				cancelSubscription()
				wsWrite(c, map[string]string{
					"type":    "unsubscribed",
					"message": "unsubscribe acknowledged",
				})

			case "ping":
				wsWrite(c, map[string]string{"type": "pong"})
			}
		}

		<-heartbeatDone
	})

	// Odds service has specialised HTTP server timeouts (ReadTimeout=0 for
	// long-lived WebSocket connections) so it doesn't use service.Run. The
	// shared package intentionally defines sane defaults for standard APIs;
	// long-poll/WebSocket services own their server lifecycle.
	//
	// We still wrap the mux with a lightweight chain so that request IDs,
	// request logs and Prometheus metrics are emitted consistently with the
	// rest of the services.
	handler := middleware.ChainMiddleware(
		middleware.RecoverPanic(log),
		middleware.RequestID,
		middleware.MetricsMiddleware("odds-service"),
		middleware.RequestLogger(log),
		middleware.SecurityHeaders,
		middleware.EncryptionMiddleware,
	)(mux)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadTimeout:       0, // must be 0 for long-lived WebSocket connections
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      0, // WebSocket writes use per-message timeouts
		IdleTimeout:       120 * time.Second,
	}

	srvErr := make(chan error, 1)
	go func() {
		log.Info("odds service starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErr <- err
		}
	}()

	select {
	case err := <-srvErr:
		log.Error("server error", "error", err)
		stop()
	case <-ctx.Done():
	}
	log.Info("shutting down odds service...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("server shutdown error", "error", err)
	}

	// Close all WebSocket connections
	wsConns.Range(func(key, value interface{}) bool {
		if conn, ok := value.(*websocket.Conn); ok {
			conn.Close(websocket.StatusGoingAway, "server shutting down")
		}
		return true
	})

	log.Info("odds service stopped")
}

// ── Env helpers ─────────────────────────────────────────────────

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getFloatEnv(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func getStringSliceEnv(key string, fallback []string) []string {
	if v := os.Getenv(key); v != "" {
		parts := strings.Split(v, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return fallback
}
