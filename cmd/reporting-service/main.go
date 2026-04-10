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

	"github.com/lotus-exchange/lotus-exchange/internal/auth"
	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/lotus-exchange/lotus-exchange/internal/reporting"
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

	port := getEnv("REPORTING_SERVICE_PORT", "8088")
	log.Info("starting reporting service", "port", port, "env", cfg.Environment)

	db, err := service.OpenPostgres(ctx, cfg.DatabaseURL, cfg.DBMaxOpenConns, cfg.DBMaxIdleConns, cfg.DBConnMaxLifetime)
	if err != nil {
		log.Error("db", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	// ── ClickHouse (optional) ───────────────────────────────────
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

	rdb, err := service.OpenRedis(ctx, cfg.RedisURL, cfg.RedisPassword, cfg.RedisPoolSize)
	if err != nil {
		log.Error("redis", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	// ── Reporting Service ───────────────────────────────────────
	reportingService := reporting.NewService(clickhouseDB, db, log)
	handler := reporting.NewHandler(reportingService)

	// ── Auth Service for middleware ─────────────────────────────
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
	mux.Handle("GET /health", service.HealthHandler("reporting-service", checks))

	// Admin-only reporting routes
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("GET /api/v1/reports/dashboard", handler.GetDashboard)
	adminMux.HandleFunc("GET /api/v1/reports/volume", handler.GetBetVolume)
	adminMux.HandleFunc("GET /api/v1/reports/pnl", handler.GetPnL)
	adminMux.HandleFunc("GET /api/v1/reports/market/{id}", handler.GetMarketReport)
	adminMux.HandleFunc("GET /api/v1/reports/hierarchy/pnl", handler.GetHierarchyPnL)

	mux.Handle("/api/v1/reports/", authMw(requireAdmin(adminMux)))

	// Prometheus scrape endpoint
	mux.Handle("GET /metrics", service.MetricsHandler())

	// ── Middleware chain ────────────────────────────────────────
	base := service.DefaultMiddleware("reporting-service", log)
	extra := middleware.ChainMiddleware(
		middleware.MaxBodySize(int64(cfg.MaxBodySizeMB)*1024*1024),
		middleware.PerIPRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst),
		middleware.EncryptionMiddleware,
	)

	runtimeCfg := service.Config{
		ServiceName: "reporting-service",
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
