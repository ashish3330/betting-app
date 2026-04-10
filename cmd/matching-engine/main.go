package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/lotus-exchange/lotus-exchange/internal/auth"
	"github.com/lotus-exchange/lotus-exchange/internal/matching"
	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/wallet"
	"github.com/lotus-exchange/lotus-exchange/pkg/logger"
	"github.com/lotus-exchange/lotus-exchange/pkg/service"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	env := getEnv("ENVIRONMENT", "development")
	log := logger.New(env)
	slog.SetDefault(log)

	port := getEnv("MATCHING_SERVICE_PORT", "8083")
	log.Info("starting matching-engine service", "port", port, "env", env)

	db, err := service.OpenPostgres(ctx, getEnv("DATABASE_URL", ""), 25, 10, 5*time.Minute)
	if err != nil {
		log.Error("db", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	rdb, err := service.OpenRedis(ctx, getEnv("REDIS_URL", "localhost:6379"), getEnv("REDIS_PASSWORD", ""), 10)
	if err != nil {
		log.Error("redis", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	nc, err := service.ConnectNATS(ctx, getEnv("NATS_URL", nats.DefaultURL), "matching-engine", log)
	if err != nil {
		log.Error("nats", "err", err)
		os.Exit(1)
	}
	defer nc.Drain()

	// ── JetStream (for event publishing) ────────────────────────
	js, err := nc.JetStream()
	if err != nil {
		log.Error("failed to create JetStream context", "error", err)
		os.Exit(1)
	}
	if _, err := js.AddStream(&nats.StreamConfig{
		Name:     "BETS",
		Subjects: []string{"bets.>"},
		Storage:  nats.FileStorage,
	}); err != nil {
		log.Warn("JetStream stream setup (may already exist)", "error", err)
	}

	// ── Auth Service (for JWT validation in middleware) ──────────
	accessTTL, _ := time.ParseDuration(getEnv("ACCESS_TOKEN_EXPIRY", "15m"))
	refreshTTL, _ := time.ParseDuration(getEnv("REFRESH_TOKEN_EXPIRY", "168h"))

	authService, err := auth.NewService(
		db, rdb, log,
		accessTTL, refreshTTL,
		getEnv("ED25519_PRIVATE_KEY", ""),
		getEnv("ED25519_PUBLIC_KEY", ""),
	)
	if err != nil {
		log.Error("failed to create auth service", "error", err)
		os.Exit(1)
	}

	// ── Core Services ───────────────────────────────────────────
	walletService := wallet.NewService(db, rdb, log)
	matchingEngine := matching.NewEngine(rdb, log)

	// ── Order Book Recovery ─────────────────────────────────────
	if _, err := matchingEngine.RecoverOrders(ctx, db); err != nil {
		log.Error("matching engine recovery failed", "error", err)
		// Non-fatal: continue startup so we can still serve traffic.
	} else {
		log.Info("matching engine recovery complete")
	}

	handler := matching.NewHandler(matchingEngine, walletService, db, log)
	authMw := middleware.AuthMiddleware(authService)

	// publishEvent sends a JetStream event with a 5s publish timeout.
	publishEvent := func(subject string, payload interface{}) {
		data, err := json.Marshal(payload)
		if err != nil {
			log.Error("failed to marshal event", "subject", subject, "error", err)
			return
		}
		pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer pubCancel()
		if _, err := js.Publish(subject, data, nats.Context(pubCtx)); err != nil {
			log.Error("failed to publish event", "subject", subject, "error", err)
		}
	}

	// ── Router ──────────────────────────────────────────────────
	mux := http.NewServeMux()

	mux.Handle("GET /health", service.HealthHandler("matching-engine", map[string]service.HealthCheck{
		"db":    func(ctx context.Context) error { return db.PingContext(ctx) },
		"redis": func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
		"nats": func(_ context.Context) error {
			if !nc.IsConnected() {
				return errors.New("disconnected")
			}
			return nil
		},
	}))

	// Public route: order book does not require authentication
	mux.HandleFunc("GET /api/v1/market/{marketId}/orderbook", handler.GetOrderBook)

	// Protected routes: require JWT authentication
	mux.Handle("POST /api/v1/bet/place", authMw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.PlaceBet(w, r)
		publishEvent("bets.placed", map[string]interface{}{
			"timestamp": time.Now().UTC(),
			"path":      r.URL.Path,
		})
	})))

	mux.Handle("DELETE /api/v1/bet/{betId}/cancel", authMw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.CancelBet(w, r)
		publishEvent("bets.cancelled", map[string]interface{}{
			"bet_id":    r.PathValue("betId"),
			"timestamp": time.Now().UTC(),
		})
	})))

	mux.Handle("GET /api/v1/bets/history", authMw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromContext(r.Context())
		rows, err := db.QueryContext(r.Context(),
			`SELECT id, market_id, side, price, stake, matched_stake, status, created_at
			 FROM betting.orders
			 WHERE user_id = $1
			 ORDER BY created_at DESC
			 LIMIT 50`, userID)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "failed to fetch bet history"})
			return
		}
		defer rows.Close()

		type betRecord struct {
			ID           string  `json:"id"`
			MarketID     string  `json:"market_id"`
			Side         string  `json:"side"`
			Price        float64 `json:"price"`
			Stake        float64 `json:"stake"`
			MatchedStake float64 `json:"matched_stake"`
			Status       string  `json:"status"`
			CreatedAt    string  `json:"created_at"`
		}

		var bets []betRecord
		for rows.Next() {
			var b betRecord
			if err := rows.Scan(&b.ID, &b.MarketID, &b.Side, &b.Price, &b.Stake, &b.MatchedStake, &b.Status, &b.CreatedAt); err != nil {
				log.Error("failed to scan bet row", "error", err)
				continue
			}
			bets = append(bets, b)
		}
		if bets == nil {
			bets = []betRecord{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(bets)
	})))

	// Prometheus scrape endpoint
	mux.Handle("GET /metrics", service.MetricsHandler())

	runtimeCfg := service.Config{
		ServiceName: "matching-engine",
		Port:        port,
		Logger:      log,
	}
	if err := service.Run(ctx, runtimeCfg, service.DefaultMiddleware("matching-engine", log)(mux)); err != nil {
		log.Error("service failed", "err", err)
		os.Exit(1)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
