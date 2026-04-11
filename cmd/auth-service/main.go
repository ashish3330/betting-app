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

	"github.com/lotus-exchange/lotus-exchange/internal/auth"
	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/pkg/config"
	"github.com/lotus-exchange/lotus-exchange/pkg/logger"
	"github.com/lotus-exchange/lotus-exchange/pkg/service"
)

// statusRecorder wraps http.ResponseWriter to capture the status code so we
// can publish NATS events only for successful auth operations.
type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

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

	port := getEnv("AUTH_SERVICE_PORT", "8081")
	log.Info("starting auth service", "port", port, "env", cfg.Environment)

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

	nc, err := service.ConnectNATS(ctx, cfg.NatsURL, "auth-service", log)
	if err != nil {
		log.Error("nats", "err", err)
		os.Exit(1)
	}
	defer func() { _ = nc.Drain() }()

	// ── Auth Service ────────────────────────────────────────────
	authService, err := auth.NewService(
		db, rdb, log,
		cfg.AccessTokenExpiry, cfg.RefreshTokenExpiry,
		cfg.ED25519PrivateKeyHex, cfg.ED25519PublicKeyHex,
	)
	if err != nil {
		log.Error("failed to create auth service", "error", err)
		os.Exit(1)
	}

	// publishAuthEvent emits a NATS event with the caller's IP and a timestamp.
	publishAuthEvent := func(subject, ip string) {
		data, err := json.Marshal(map[string]string{
			"ip":        ip,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
		if err != nil {
			log.Error("failed to marshal nats event", "subject", subject, "error", err)
			return
		}
		if err := nc.Publish(subject, data); err != nil {
			log.Error("failed to publish nats event", "subject", subject, "error", err)
		}
	}

	// ── HTTP Handler ────────────────────────────────────────────
	handler := auth.NewHandler(authService)
	authMw := middleware.AuthMiddleware(authService)

	mux := http.NewServeMux()

	// Health check
	mux.Handle("GET /health", service.HealthHandler("auth-service", map[string]service.HealthCheck{
		"db":    func(ctx context.Context) error { return db.PingContext(ctx) },
		"redis": func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
		"nats": func(_ context.Context) error {
			if !nc.IsConnected() {
				return errors.New("disconnected")
			}
			return nil
		},
	}))

	// Public routes (no auth required). NATS events for successful calls are
	// emitted via statusRecorder.
	mux.HandleFunc("POST /api/v1/auth/register", func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, statusCode: 200}
		handler.Register(rec, r)
		if rec.statusCode >= 200 && rec.statusCode < 300 {
			publishAuthEvent("auth.register", r.RemoteAddr)
		}
	})
	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, statusCode: 200}
		handler.Login(rec, r)
		if rec.statusCode >= 200 && rec.statusCode < 300 {
			publishAuthEvent("auth.login", r.RemoteAddr)
		}
	})
	mux.HandleFunc("POST /api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, statusCode: 200}
		handler.Logout(rec, r)
		if rec.statusCode >= 200 && rec.statusCode < 300 {
			publishAuthEvent("auth.logout", r.RemoteAddr)
		}
	})
	mux.HandleFunc("POST /api/v1/auth/refresh", handler.Refresh)
	mux.HandleFunc("POST /api/v1/auth/otp/verify", handler.OTPVerify)
	// Public OTP resend — used by the pre-login OTP flow before a session
	// exists. The handler is deliberately non-enumerating: unknown user_ids
	// still return 200.
	mux.HandleFunc("POST /api/v1/auth/otp/resend", handler.OTPResend)

	// Protected routes (require JWT)
	protectedMux := http.NewServeMux()
	protectedMux.HandleFunc("POST /api/v1/auth/change-password", handler.ChangePassword)
	protectedMux.HandleFunc("POST /api/v1/auth/otp/generate", handler.OTPGenerate)
	protectedMux.HandleFunc("POST /api/v1/auth/otp/enable", handler.OTPEnable)
	protectedMux.HandleFunc("GET /api/v1/auth/sessions", handler.GetSessions)
	protectedMux.HandleFunc("DELETE /api/v1/auth/sessions", handler.LogoutAllSessions)
	protectedMux.HandleFunc("GET /api/v1/auth/login-history", handler.LoginHistory)

	mux.Handle("/api/v1/auth/change-password", authMw(protectedMux))
	mux.Handle("/api/v1/auth/otp/generate", authMw(protectedMux))
	mux.Handle("/api/v1/auth/otp/enable", authMw(protectedMux))
	mux.Handle("/api/v1/auth/sessions", authMw(protectedMux))
	mux.Handle("/api/v1/auth/login-history", authMw(protectedMux))

	// Prometheus scrape endpoint
	mux.Handle("GET /metrics", service.MetricsHandler())

	// ── Middleware chain ────────────────────────────────────────
	// DefaultMiddleware covers recovery, request ID, metrics, logging and
	// security headers. Auth-specific extras (body size, rate limit,
	// encryption) are added on top.
	base := service.DefaultMiddleware("auth-service", log)
	extra := middleware.ChainMiddleware(
		middleware.MaxBodySize(int64(cfg.MaxBodySizeMB)*1024*1024),
		middleware.PerIPRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst),
		middleware.EncryptionMiddleware,
	)

	runtimeCfg := service.Config{
		ServiceName: "auth-service",
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
