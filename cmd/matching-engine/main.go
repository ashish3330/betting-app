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

	// Matching engine drives the busiest DB workload in the platform
	// (bet placement, settlement, ledger writes) so the pool defaults
	// are bumped unless DB_MAX_*_CONNS is explicitly set in the env.
	maxOpen := getIntEnv("DB_MAX_OPEN_CONNS", 50)
	maxIdle := getIntEnv("DB_MAX_IDLE_CONNS", 20)
	db, err := service.OpenPostgres(ctx, getEnv("DATABASE_URL", ""), maxOpen, maxIdle, 5*time.Minute)
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

	// User-facing bet read endpoints. These used to be served by the
	// monolith (cmd/server/main.go:handleUserBets / handleBetsHistory);
	// the matching-engine now owns them because it already owns the
	// betting.bets table. The previous inline /bets/history handler
	// queried a non-existent betting.orders table — see
	// scripts/api-test/main.go's skip list for the bug report.
	mux.Handle("GET /api/v1/bets", authMw(http.HandlerFunc(handler.UserBets)))
	mux.Handle("GET /api/v1/bets/history", authMw(http.HandlerFunc(handler.BetsHistoryHandler)))
	mux.Handle("GET /api/v1/positions/{marketId}", authMw(http.HandlerFunc(handler.GetPositions)))

	// Prometheus scrape endpoint
	mux.Handle("GET /metrics", service.MetricsHandler())

	runtimeCfg := service.Config{
		ServiceName: "matching-engine",
		Port:        port,
		Logger:      log,
	}
	// ── Middleware chain ────────────────────────────────────────
	// EncryptionMiddleware unwraps the AES-GCM envelope on POST/PUT
	// bodies. Without it the matching-engine sees opaque ciphertext
	// when the gateway forwards bet placements from the encryption-aware
	// frontend, and req.Validate() reports every field missing.
	// MaxBodySize + PerIPRateLimiter are the same cap every other
	// canonical service applies on top of DefaultMiddleware.
	base := service.DefaultMiddleware("matching-engine", log)
	extra := middleware.ChainMiddleware(
		middleware.MaxBodySize(int64(getIntEnv("MAX_BODY_SIZE_MB", 1))*1024*1024),
		middleware.PerIPRateLimiter(getIntEnv("RATE_LIMIT_RPS", 100), getIntEnv("RATE_LIMIT_BURST", 200)),
		middleware.EncryptionMiddleware,
	)
	if err := service.Run(ctx, runtimeCfg, base(extra(mux))); err != nil {
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

func getIntEnv(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
