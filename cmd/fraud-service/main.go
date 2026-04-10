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

	"github.com/nats-io/nats.go"

	"github.com/lotus-exchange/lotus-exchange/internal/auth"
	"github.com/lotus-exchange/lotus-exchange/internal/fraud"
	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
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

	port := getEnv("FRAUD_SERVICE_PORT", "8087")
	log.Info("starting fraud service", "port", port, "env", cfg.Environment)

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

	nc, err := service.ConnectNATS(ctx, cfg.NatsURL, "fraud-service", log)
	if err != nil {
		log.Error("nats", "err", err)
		os.Exit(1)
	}
	defer nc.Drain()

	// ── Fraud Service ───────────────────────────────────────────
	fraudService := fraud.NewService(db, rdb, log)
	handler := fraud.NewHandler(fraudService)

	// ── NATS Subscriptions ──────────────────────────────────────
	// Subscribe to bet placement events for fraud pattern analysis
	if _, err := nc.Subscribe("bets.placed", func(msg *nats.Msg) {
		var event struct {
			UserID    int64   `json:"user_id"`
			MarketID  string  `json:"market_id"`
			Stake     float64 `json:"stake"`
			Odds      float64 `json:"odds"`
			IP        string  `json:"ip"`
			Timestamp string  `json:"timestamp"`
		}
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			log.Error("failed to unmarshal bets.placed event", "error", err)
			return
		}

		pattern := &fraud.BetPattern{
			UserID:    event.UserID,
			MarketID:  event.MarketID,
			Stake:     event.Stake,
			Price:     event.Odds,
			IPAddress: event.IP,
		}

		if _, err := fraudService.AnalyzeBet(ctx, pattern); err != nil {
			log.Error("fraud analysis failed for bet", "user_id", event.UserID, "error", err)
		}
	}); err != nil {
		log.Error("failed to subscribe to bets.placed", "error", err)
		os.Exit(1)
	}
	log.Info("subscribed to bets.placed events")

	// Subscribe to login events for suspicious login detection
	if _, err := nc.Subscribe("auth.login", func(msg *nats.Msg) {
		var event struct {
			UserID    int64  `json:"user_id"`
			IP        string `json:"ip"`
			UserAgent string `json:"user_agent"`
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			log.Error("failed to unmarshal auth.login event", "error", err)
			return
		}
		log.Debug("analyzing login event", "user_id", event.UserID, "ip", event.IP)
	}); err != nil {
		log.Error("failed to subscribe to auth.login", "error", err)
		os.Exit(1)
	}
	log.Info("subscribed to auth.login events")

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

	mux.Handle("GET /health", service.HealthHandler("fraud-service", map[string]service.HealthCheck{
		"db":    func(ctx context.Context) error { return db.PingContext(ctx) },
		"redis": func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
		"nats": func(_ context.Context) error {
			if !nc.IsConnected() {
				return errors.New("disconnected")
			}
			return nil
		},
	}))

	// Admin-only fraud routes
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("GET /api/v1/fraud/alerts", handler.GetAlerts)
	adminMux.HandleFunc("POST /api/v1/fraud/alerts/{id}/resolve", handler.ResolveAlert)
	adminMux.HandleFunc("GET /api/v1/fraud/user/{id}/risk", handler.GetUserRisk)

	mux.Handle("/api/v1/fraud/", authMw(requireAdmin(adminMux)))

	// Prometheus scrape endpoint
	mux.Handle("GET /metrics", service.MetricsHandler())

	// ── Middleware chain ────────────────────────────────────────
	base := service.DefaultMiddleware("fraud-service", log)
	extra := middleware.ChainMiddleware(
		middleware.MaxBodySize(int64(cfg.MaxBodySizeMB)*1024*1024),
		middleware.PerIPRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst),
		middleware.EncryptionMiddleware,
	)

	runtimeCfg := service.Config{
		ServiceName: "fraud-service",
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
