package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/auth"
	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/risk"
	"github.com/lotus-exchange/lotus-exchange/pkg/logger"
	"github.com/lotus-exchange/lotus-exchange/pkg/service"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	log := logger.New(getEnv("ENVIRONMENT", "development"))
	slog.SetDefault(log)

	port := getEnv("RISK_SERVICE_PORT", "8089")
	log.Info("starting risk service", "port", port)

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

	rdb, err := service.OpenRedis(ctx, getEnv("REDIS_URL", "localhost:6379"), getEnv("REDIS_PASSWORD", ""), getIntEnv("REDIS_POOL_SIZE", 10))
	if err != nil {
		log.Error("redis", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	// ── Auth service (for JWT validation) ───────────────────────
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

	riskService := risk.NewService(db, rdb, log)
	handler := risk.NewHandler(riskService)
	authMw := middleware.AuthMiddleware(authService)

	mux := http.NewServeMux()

	mux.Handle("GET /health", service.HealthHandler("risk-service", map[string]service.HealthCheck{
		"db":    func(ctx context.Context) error { return db.PingContext(ctx) },
		"redis": func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
	}))

	// Protected risk routes
	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/v1/risk/market/{id}", handler.MarketExposure)
	protected.HandleFunc("GET /api/v1/risk/user/{id}", handler.UserExposure)
	mux.Handle("/api/v1/risk/", authMw(protected))

	// Prometheus scrape endpoint
	mux.Handle("GET /metrics", service.MetricsHandler())

	runtimeCfg := service.Config{
		ServiceName: "risk-service",
		Port:        port,
		Logger:      log,
	}
	if err := service.Run(ctx, runtimeCfg, service.DefaultMiddleware("risk-service", log)(mux)); err != nil {
		log.Error("service failed", "err", err)
		os.Exit(1)
	}
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

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
