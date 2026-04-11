package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/lotus-exchange/lotus-exchange/internal/auth"
	"github.com/lotus-exchange/lotus-exchange/internal/hierarchy"
	"github.com/lotus-exchange/lotus-exchange/internal/kyc"
	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/lotus-exchange/lotus-exchange/internal/responsible"
	"github.com/lotus-exchange/lotus-exchange/pkg/config"
	"github.com/lotus-exchange/lotus-exchange/pkg/logger"
	"github.com/lotus-exchange/lotus-exchange/pkg/service"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	log := logger.New(cfg.Environment)
	slog.SetDefault(log)

	port := getEnv("HIERARCHY_SERVICE_PORT", "8090")
	log.Info("starting hierarchy service", "port", port, "env", cfg.Environment)

	db, err := service.OpenPostgres(ctx, cfg.DatabaseURL, cfg.DBMaxOpenConns, cfg.DBMaxIdleConns, cfg.DBConnMaxLifetime)
	if err != nil {
		log.Error("db", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	rdb, err := service.OpenRedis(ctx, cfg.RedisURL, cfg.RedisPassword, cfg.RedisPoolSize)
	if err != nil {
		log.Error("redis", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	nc, err := service.ConnectNATS(ctx, cfg.NatsURL, "hierarchy-service", log)
	if err != nil {
		log.Error("nats", "err", err)
		os.Exit(1)
	}
	defer func() { _ = nc.Drain() }()

	// ── Auth Service (for JWT validation) ───────────────────────
	authService, err := auth.NewService(
		db, rdb, log,
		cfg.AccessTokenExpiry, cfg.RefreshTokenExpiry,
		cfg.ED25519PrivateKeyHex, cfg.ED25519PublicKeyHex,
	)
	if err != nil {
		log.Error("failed to create auth service", "error", err)
		os.Exit(1)
	}

	// ── Domain Services ─────────────────────────────────────────
	hierarchyService := hierarchy.NewService(db, rdb, log)
	responsibleService := responsible.NewService(db, rdb, log)
	kycService := kyc.NewService(db, log)

	// ── HTTP Handlers ───────────────────────────────────────────
	hierarchyHandler := hierarchy.NewHandler(hierarchyService)
	responsibleHandler := responsible.NewHandler(responsibleService)
	kycHandler := kyc.NewHandler(kycService)

	// ── Router ──────────────────────────────────────────────────
	mux := http.NewServeMux()

	mux.Handle("GET /health", service.HealthHandler("hierarchy-service", map[string]service.HealthCheck{
		"db":    func(ctx context.Context) error { return db.PingContext(ctx) },
		"redis": func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
		"nats": func(_ context.Context) error {
			if !nc.IsConnected() {
				return errors.New("disconnected")
			}
			return nil
		},
	}))

	authMw := middleware.AuthMiddleware(authService)

	// ── Protected routes ────────────────────────────────────────
	protectedMux := http.NewServeMux()
	hierarchyHandler.RegisterRoutes(protectedMux)
	responsibleHandler.RegisterRoutes(protectedMux)
	kycHandler.RegisterRoutes(protectedMux)

	mux.Handle("/api/v1/hierarchy/", authMw(protectedMux))
	mux.Handle("/api/v1/responsible-gambling/", authMw(protectedMux))
	mux.Handle("/api/v1/responsible/", authMw(protectedMux))
	mux.Handle("/api/v1/referral/", authMw(protectedMux))
	mux.Handle("/api/v1/kyc/", authMw(protectedMux))

	// ── Admin routes (require auth + admin role) ────────────────
	adminMux := http.NewServeMux()
	kycHandler.RegisterAdminRoutes(adminMux)
	adminChain := middleware.RequireRole(models.RoleSuperAdmin, models.RoleAdmin)(adminMux)
	mux.Handle("/api/v1/admin/kyc/", authMw(adminChain))

	// Prometheus scrape endpoint
	mux.Handle("GET /metrics", service.MetricsHandler())

	// ── Middleware chain ────────────────────────────────────────
	base := service.DefaultMiddleware("hierarchy-service", log)
	extra := middleware.ChainMiddleware(
		middleware.MaxBodySize(int64(cfg.MaxBodySizeMB)*1024*1024),
		middleware.PerIPRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst),
		middleware.EncryptionMiddleware,
	)

	runtimeCfg := service.Config{
		ServiceName: "hierarchy-service",
		Port:        port,
		Logger:      log,
	}
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
