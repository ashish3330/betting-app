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
	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/notification"
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

	port := getEnv("NOTIFICATION_SERVICE_PORT", "8091")
	log.Info("starting notification service", "port", port, "env", cfg.Environment)

	// Notification service mostly reads outbox rows and writes a small
	// amount of delivery state — it does not need a big pool. Shrink the
	// defaults so wallet/matching get the headroom. Env-provided
	// DB_MAX_*_CONNS still wins.
	notifMaxOpen := cfg.DBMaxOpenConns
	if os.Getenv("DB_MAX_OPEN_CONNS") == "" {
		notifMaxOpen = 10
	}
	notifMaxIdle := cfg.DBMaxIdleConns
	if os.Getenv("DB_MAX_IDLE_CONNS") == "" {
		notifMaxIdle = 5
	}
	db, err := service.OpenPostgres(ctx, cfg.DatabaseURL, notifMaxOpen, notifMaxIdle, cfg.DBConnMaxLifetime)
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

	nc, err := service.ConnectNATS(ctx, cfg.NatsURL, "notification-service", log)
	if err != nil {
		log.Error("nats", "err", err)
		os.Exit(1)
	}
	defer nc.Drain()

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

	// ── Notification Service ────────────────────────────────────
	notifService := notification.NewService(db, log)
	handler := notification.NewHandler(notifService)

	// ── NATS Subscriptions (auto-create notifications) ──────────
	subscribeEvent := func(subject, title string, notifType notification.NotificationType) {
		_, err := nc.Subscribe(subject, func(msg *nats.Msg) {
			var payload struct {
				UserID  int64  `json:"user_id"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(msg.Data, &payload); err != nil {
				log.Error("failed to unmarshal nats event",
					"subject", subject, "error", err)
				return
			}
			if payload.UserID == 0 {
				log.Warn("nats event missing user_id", "subject", subject)
				return
			}

			message := payload.Message
			if message == "" {
				message = title
			}

			if err := notifService.Send(
				context.Background(),
				payload.UserID,
				notifType,
				title,
				message,
				nil,
				"",
			); err != nil {
				log.Error("failed to create notification from nats event",
					"subject", subject,
					"user_id", payload.UserID,
					"error", err)
			}
		})
		if err != nil {
			log.Error("failed to subscribe to nats subject",
				"subject", subject, "error", err)
		} else {
			log.Info("subscribed to nats subject", "subject", subject)
		}
	}

	subscribeEvent("bets.settled", "Bet Settled", notification.NotifBetSettled)
	subscribeEvent("payment.deposit.completed", "Deposit Completed", notification.NotifDepositComplete)
	subscribeEvent("auth.login", "New Login Detected", notification.NotifSystem)

	// ── Router ──────────────────────────────────────────────────
	mux := http.NewServeMux()

	mux.Handle("GET /health", service.HealthHandler("notification-service", map[string]service.HealthCheck{
		"db": func(ctx context.Context) error { return db.PingContext(ctx) },
		"nats": func(_ context.Context) error {
			if !nc.IsConnected() {
				return errors.New("disconnected")
			}
			return nil
		},
	}))

	// Auth middleware
	authMw := middleware.AuthMiddleware(authService)

	// ── Protected routes ────────────────────────────────────────
	protectedMux := http.NewServeMux()
	handler.RegisterRoutes(protectedMux)

	mux.Handle("/api/v1/notifications", authMw(protectedMux))
	mux.Handle("/api/v1/notifications/", authMw(protectedMux))

	// Prometheus scrape endpoint
	mux.Handle("GET /metrics", service.MetricsHandler())

	// ── Middleware chain ────────────────────────────────────────
	base := service.DefaultMiddleware("notification-service", log)
	extra := middleware.ChainMiddleware(
		middleware.MaxBodySize(int64(cfg.MaxBodySizeMB)*1024*1024),
		middleware.PerIPRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst),
		middleware.EncryptionMiddleware,
	)

	runtimeCfg := service.Config{
		ServiceName: "notification-service",
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
