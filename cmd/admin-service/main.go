package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/admin"
	"github.com/lotus-exchange/lotus-exchange/internal/auth"
	"github.com/lotus-exchange/lotus-exchange/internal/fraud"
	"github.com/lotus-exchange/lotus-exchange/internal/market"
	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/lotus-exchange/lotus-exchange/internal/reporting"
	"github.com/lotus-exchange/lotus-exchange/internal/settlement"
	"github.com/lotus-exchange/lotus-exchange/internal/wallet"
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

	port := getEnv("ADMIN_SERVICE_PORT", "8092")
	log.Info("starting admin service", "port", port, "env", cfg.Environment)

	// ── Postgres ────────────────────────────────────────────────
	db, err := service.OpenPostgres(ctx, cfg.DatabaseURL, cfg.DBMaxOpenConns, cfg.DBMaxIdleConns, cfg.DBConnMaxLifetime)
	if err != nil {
		log.Error("db", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	// ── Redis ───────────────────────────────────────────────────
	rdb, err := service.OpenRedis(ctx, cfg.RedisURL, cfg.RedisPassword, cfg.RedisPoolSize)
	if err != nil {
		log.Error("redis", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	// ── ClickHouse (optional, used by reporting) ────────────────
	var clickhouseDB *sql.DB
	if chURL := os.Getenv("CLICKHOUSE_URL"); chURL != "" {
		chDB, err := sql.Open("clickhouse", chURL)
		if err != nil {
			log.Warn("failed to connect to clickhouse, falling back to postgres-only", "error", err)
		} else {
			chPingCtx, chPingCancel := context.WithTimeout(ctx, 5*time.Second)
			if err := chDB.PingContext(chPingCtx); err != nil {
				log.Warn("clickhouse ping failed, falling back to postgres-only", "error", err)
				chDB.Close()
			} else {
				clickhouseDB = chDB
				defer clickhouseDB.Close()
				log.Info("clickhouse connected")
			}
			chPingCancel()
		}
	} else {
		log.Info("CLICKHOUSE_URL not set, using postgres-only mode")
	}

	// ── Dependent services wired for the admin handler ─────────
	walletSvc := wallet.NewService(db, rdb, log)
	settlementSvc := settlement.NewService(db, walletSvc, log)
	marketSvc := market.NewService(db, log)
	reportingSvc := reporting.NewService(clickhouseDB, db, log)
	fraudSvc := fraud.NewService(db, rdb, log)

	adminHandler := admin.NewHandler(db, marketSvc, settlementSvc, reportingSvc, fraudSvc, log)

	// ── Auth Service for JWT validation ─────────────────────────
	authService, err := auth.NewService(
		db, rdb, log,
		cfg.AccessTokenExpiry, cfg.RefreshTokenExpiry,
		cfg.ED25519PrivateKeyHex, cfg.ED25519PublicKeyHex,
	)
	if err != nil {
		log.Error("failed to create auth service", "error", err)
		os.Exit(1)
	}
	authMw := middleware.AuthMiddleware(authService)

	// ── HTTP Router ─────────────────────────────────────────────
	mux := http.NewServeMux()

	checks := map[string]service.HealthCheck{
		"db":    func(ctx context.Context) error { return db.PingContext(ctx) },
		"redis": func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
	}
	if clickhouseDB != nil {
		checks["clickhouse"] = func(ctx context.Context) error { return clickhouseDB.PingContext(ctx) }
	}
	mux.Handle("GET /health", service.HealthHandler("admin-service", checks))

	// Prometheus scrape endpoint
	mux.Handle("GET /metrics", service.MetricsHandler())

	// Admin routes — all require auth + admin role
	adminMux := http.NewServeMux()
	adminHandler.RegisterRoutes(adminMux)
	mux.Handle("/api/v1/admin/", authMw(requireAdmin(adminMux)))

	// ── Middleware chain ────────────────────────────────────────
	base := service.DefaultMiddleware("admin-service", log)
	extra := middleware.ChainMiddleware(
		middleware.MaxBodySize(int64(cfg.MaxBodySizeMB)*1024*1024),
		middleware.PerIPRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst),
		middleware.EncryptionMiddleware,
	)

	runtimeCfg := service.Config{
		ServiceName: "admin-service",
		Port:        port,
		Logger:      log,
	}
	if err := service.Run(ctx, runtimeCfg, base(extra(mux))); err != nil {
		log.Error("service failed", "err", err)
		os.Exit(1)
	}
}

// requireAdmin wraps an http.Handler and rejects requests from non-admin users.
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := middleware.RoleFromContext(r.Context())
		if role != models.Role("superadmin") && role != models.Role("admin") {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
