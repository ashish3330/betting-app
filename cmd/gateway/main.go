package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/lotus-exchange/lotus-exchange/internal/admin"
	"github.com/lotus-exchange/lotus-exchange/internal/auth"
	"github.com/lotus-exchange/lotus-exchange/internal/casino"
	"github.com/lotus-exchange/lotus-exchange/internal/fraud"
	"github.com/lotus-exchange/lotus-exchange/internal/hierarchy"
	"github.com/lotus-exchange/lotus-exchange/internal/market"
	"github.com/lotus-exchange/lotus-exchange/internal/matching"
	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/odds"
	"github.com/lotus-exchange/lotus-exchange/internal/payment"
	"github.com/lotus-exchange/lotus-exchange/internal/reporting"
	"github.com/lotus-exchange/lotus-exchange/internal/risk"
	"github.com/lotus-exchange/lotus-exchange/internal/settlement"
	"github.com/lotus-exchange/lotus-exchange/internal/wallet"
	"github.com/lotus-exchange/lotus-exchange/pkg/config"
	"github.com/lotus-exchange/lotus-exchange/pkg/logger"
	"github.com/redis/go-redis/v9"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

func main() {
	cfg := config.Load()

	// ── Config validation ──────────────────────────────────────
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	log := logger.New(cfg.Environment)
	slog.SetDefault(log)

	log.Info("starting lotus exchange gateway",
		"port", cfg.HTTPPort, "ws_port", cfg.WSPort, "env", cfg.Environment)

	// ── Database ────────────────────────────────────────────────
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	db.SetMaxOpenConns(cfg.DBMaxOpenConns)
	db.SetMaxIdleConns(cfg.DBMaxIdleConns)
	db.SetConnMaxLifetime(cfg.DBConnMaxLifetime)
	db.Exec("SET search_path TO betting, auth, public")

	// ── Redis ───────────────────────────────────────────────────
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisURL,
		Password: cfg.RedisPassword,
		PoolSize: cfg.RedisPoolSize,
	})
	defer rdb.Close()

	// ── Core Services (Phase 1) ─────────────────────────────────
	authService, err := auth.NewService(db, rdb, log, cfg.AccessTokenExpiry, cfg.RefreshTokenExpiry, cfg.ED25519PrivateKeyHex, cfg.ED25519PublicKeyHex)
	if err != nil {
		log.Error("failed to create auth service", "error", err)
		os.Exit(1)
	}

	hierarchyService := hierarchy.NewService(db, rdb, log)
	walletService := wallet.NewService(db, rdb, log)
	matchingEngine := matching.NewEngine(rdb, log)
	marketService := market.NewService(db, log)
	riskService := risk.NewService(db, rdb, log)
	settlementService := settlement.NewService(db, walletService, log)

	// Odds provider (pluggable)
	var oddsProvider odds.OddsProvider
	switch cfg.OddsProvider {
	case "entity_sports":
		oddsProvider = odds.NewEntitySportsProvider(cfg.EntitySportsAPIKey, cfg.EntitySportsURL, log)
	default:
		oddsProvider = odds.NewMockProvider(cfg.MockVolatility, cfg.MockUpdateInterval)
	}
	oddsService := odds.NewService(oddsProvider, rdb, log)

	// ── Phase 2 Services ────────────────────────────────────────
	casinoService := casino.NewService(db, rdb, walletService, log)
	paymentService := payment.NewService(db, walletService, log, os.Getenv("RAZORPAY_SECRET"), os.Getenv("CRYPTO_WEBHOOK_KEY"))
	fraudService := fraud.NewService(db, rdb, log)
	reportingService := reporting.NewService(nil, db, log) // nil = ClickHouse (optional for dev)

	// ── Matching Engine Recovery ────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if _, err := matchingEngine.RecoverOrders(ctx, db); err != nil {
		log.Error("matching engine recovery failed", "error", err)
		// Non-fatal: continue startup so we can still serve traffic.
	} else {
		log.Info("matching engine recovery complete")
	}

	// ── Settlement Outbox Processor ─────────────────────────────
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := settlementService.ProcessOutbox(ctx); err != nil {
					log.Error("settlement outbox processing error", "error", err)
				}
			}
		}
	}()

	// ── HTTP Handlers ───────────────────────────────────────────
	authHandler := auth.NewHandler(authService)
	hierarchyHandler := hierarchy.NewHandler(hierarchyService)
	walletHandler := wallet.NewHandler(walletService)
	matchingHandler := matching.NewHandler(matchingEngine, walletService)
	marketHandler := market.NewHandler(marketService)
	riskHandler := risk.NewHandler(riskService)
	casinoHandler := casino.NewHandler(casinoService)
	paymentHandler := payment.NewHandler(paymentService)
	reportingHandler := reporting.NewHandler(reportingService)
	fraudHandler := fraud.NewHandler(fraudService)
	adminHandler := admin.NewHandler(db, marketService, settlementService, reportingService, fraudService, log)

	// ── Router ──────────────────────────────────────────────────
	mux := http.NewServeMux()

	// Public routes (no auth)
	authHandler.RegisterRoutes(mux)

	// Health check (includes db + redis ping)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		dbStatus := "ok"
		if err := db.PingContext(r.Context()); err != nil {
			dbStatus = "error: " + err.Error()
		}
		redisStatus := "ok"
		if err := rdb.Ping(r.Context()).Err(); err != nil {
			redisStatus = "error: " + err.Error()
		}

		status := "ok"
		statusCode := http.StatusOK
		if dbStatus != "ok" || redisStatus != "ok" {
			status = "degraded"
			statusCode = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(map[string]string{
			"status":   status,
			"database": dbStatus,
			"redis":    redisStatus,
			"provider": oddsService.Provider().Name(),
			"version":  "2.0.0",
		})
	})

	// ── Public market/odds routes ───────────────────────────────
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

	// ── Multi-sport routes (public) ─────────────────────────────
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
		events, err := marketService.ListEvents(r.Context(), competitionID)
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

	// ── Casino routes (public) ──────────────────────────────────
	mux.HandleFunc("GET /api/v1/casino/providers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(casinoService.ListProviders())
	})
	mux.HandleFunc("GET /api/v1/casino/games", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(casinoService.ListGames())
	})
	mux.HandleFunc("GET /api/v1/casino/categories", func(w http.ResponseWriter, r *http.Request) {
		_ = r.Context()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(casinoService.ListCategories())
	})
	mux.HandleFunc("GET /api/v1/casino/games/{category}", func(w http.ResponseWriter, r *http.Request) {
		category := r.PathValue("category")
		games := casinoService.ListGamesByCategory(casino.GameCategory(category))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(games)
	})

	// Payment webhooks (no auth - signature verified internally)
	mux.HandleFunc("POST /api/v1/payment/webhook/razorpay", paymentHandler.RazorpayWebhook)
	mux.HandleFunc("POST /api/v1/payment/webhook/crypto", paymentHandler.CryptoWebhook)

	// Casino settlement webhook (provider-authenticated)
	mux.HandleFunc("POST /api/v1/casino/webhook/settlement", casinoHandler.SettlementWebhook)

	// ── Protected routes (require JWT auth) ─────────────────────
	protected := http.NewServeMux()
	hierarchyHandler.RegisterRoutes(protected)
	walletHandler.RegisterRoutes(protected)
	matchingHandler.RegisterRoutes(protected)
	marketHandler.RegisterRoutes(protected)
	riskHandler.RegisterRoutes(protected)
	casinoHandler.RegisterRoutes(protected)
	paymentHandler.RegisterRoutes(protected)
	reportingHandler.RegisterRoutes(protected)

	// Placeholder route groups for new feature areas
	protected.HandleFunc("/api/v1/responsible/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "coming_soon", "module": "responsible_gambling"})
	})
	protected.HandleFunc("/api/v1/notifications/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "coming_soon", "module": "notifications"})
	})
	protected.HandleFunc("/api/v1/kyc/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "coming_soon", "module": "kyc"})
	})

	authMw := middleware.AuthMiddleware(authService)
	mux.Handle("/api/v1/hierarchy/", authMw(protected))
	mux.Handle("/api/v1/wallet/", authMw(protected))
	mux.Handle("/api/v1/bet/", authMw(protected))
	mux.Handle("/api/v1/market/", authMw(protected))
	mux.Handle("/api/v1/risk/", authMw(protected))
	mux.Handle("/api/v1/casino/", authMw(protected))
	mux.Handle("/api/v1/payment/", authMw(protected))
	mux.Handle("/api/v1/reports/", authMw(protected))
	mux.Handle("/api/v1/markets/", authMw(protected))
	mux.Handle("/api/v1/responsible/", authMw(protected))
	mux.Handle("/api/v1/notifications/", authMw(protected))
	mux.Handle("/api/v1/kyc/", authMw(protected))

	// ── Admin routes (require auth + admin role) ────────────────
	adminMux := http.NewServeMux()
	adminHandler.RegisterRoutes(adminMux)
	fraudHandler.RegisterRoutes(adminMux)

	adminChain := middleware.RequireRole("superadmin", "admin")(adminMux)
	mux.Handle("/api/v1/admin/", authMw(adminChain))
	mux.Handle("/api/v1/fraud/", authMw(adminChain))

	// ── WebSocket Gateway ───────────────────────────────────────
	wsConns := &sync.Map{}

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		// Check auth token BEFORE accepting the WebSocket connection
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
			OriginPatterns: cfg.CORSOrigins,
		})
		if err != nil {
			log.Error("websocket accept error", "error", err)
			return
		}
		defer c.CloseNow()

		wsConns.Store(claims.UserID, c)
		defer wsConns.Delete(claims.UserID)

		wsCtx := r.Context()

		// Send initial auth confirmation
		wsjson.Write(wsCtx, c, map[string]string{"type": "authenticated", "user": claims.Username})

		// Heartbeat: server sends ping every 30 seconds
		heartbeatDone := make(chan struct{})
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-wsCtx.Done():
					close(heartbeatDone)
					return
				case <-ticker.C:
					if err := c.Ping(wsCtx); err != nil {
						close(heartbeatDone)
						return
					}
				}
			}
		}()

		for {
			var msg struct {
				Type    string `json:"type"`
				Payload struct {
					MarketIDs []string `json:"market_ids"`
				} `json:"payload"`
			}

			err := wsjson.Read(wsCtx, c, &msg)
			if err != nil {
				break
			}

			switch msg.Type {
			case "subscribe":
				if len(msg.Payload.MarketIDs) == 0 {
					continue
				}
				updates, err := oddsService.StartSubscription(wsCtx, msg.Payload.MarketIDs)
				if err != nil {
					wsjson.Write(wsCtx, c, map[string]string{"type": "error", "message": err.Error()})
					continue
				}
				go func() {
					for update := range updates {
						if writeErr := wsjson.Write(wsCtx, c, map[string]interface{}{
							"type":    "odds_update",
							"payload": update,
						}); writeErr != nil {
							return
						}
					}
				}()

			case "ping":
				wsjson.Write(wsCtx, c, map[string]string{"type": "pong"})
			}
		}

		<-heartbeatDone
	})

	// ── Global Middleware (chained) ─────────────────────────────
	chain := middleware.ChainMiddleware(
		middleware.RecoverPanic(log),
		middleware.CORSWithWhitelist(cfg.CORSOrigins),
		middleware.SecurityHeaders,
		middleware.RequestID,
		middleware.MaxBodySize(int64(cfg.MaxBodySizeMB)*1024*1024),
		middleware.RequestLogger(log),
		middleware.PerIPRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst),
		middleware.EncryptionMiddleware,
	)
	handler := chain(mux)

	// ── HTTP Server ─────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.HTTPPort,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// ── Start Server ────────────────────────────────────────────
	go func() {
		log.Info("HTTP server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("server shutdown error", "error", err)
	}

	wsConns.Range(func(key, value interface{}) bool {
		if c, ok := value.(*websocket.Conn); ok {
			c.Close(websocket.StatusGoingAway, "server shutting down")
		}
		return true
	})

	oddsProvider.Close()
	log.Info("server stopped")
}
