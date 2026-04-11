package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/auth"
	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/payment"
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

	port := getEnv("PAYMENT_SERVICE_PORT", "8084")
	log.Info("starting payment service", "port", port, "env", env)

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

	// ── Auth Service (for JWT validation) ───────────────────────
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

	// ── Services ────────────────────────────────────────────────
	walletSvc := wallet.NewService(db, rdb, log)
	paymentSvc := payment.NewService(
		db,
		walletSvc,
		log,
		getEnv("RAZORPAY_SECRET", ""),
		getEnv("CRYPTO_WEBHOOK_KEY", ""),
	)
	handler := payment.NewHandler(paymentSvc)

	// Auth middleware wrapper for per-route use
	authMwFactory := middleware.AuthMiddleware(authService)
	authMw := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			authMwFactory(h).ServeHTTP(w, r)
		}
	}

	// ── API router (protected + infra; gets the full extra chain) ──
	// Everything mounted on apiMux is wrapped in EncryptionMiddleware,
	// body-size cap and per-IP rate limiting. Webhook endpoints are
	// deliberately NOT on apiMux so that provider payloads (Razorpay,
	// crypto processors) are not wrapped in the AES-GCM envelope.
	apiMux := http.NewServeMux()

	apiMux.Handle("GET /health", service.HealthHandler("payment-service", map[string]service.HealthCheck{
		"db":    func(ctx context.Context) error { return db.PingContext(ctx) },
		"redis": func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
	}))

	// Prometheus scrape endpoint
	apiMux.Handle("GET /metrics", service.MetricsHandler())

	// Protected routes
	apiMux.HandleFunc("POST /api/v1/payment/deposit/upi", authMw(handler.UPIDeposit))
	apiMux.HandleFunc("POST /api/v1/payment/deposit/crypto", authMw(handler.CryptoDeposit))
	apiMux.HandleFunc("POST /api/v1/payment/withdraw", authMw(handler.Withdraw))
	apiMux.HandleFunc("GET /api/v1/payment/transactions", authMw(handler.GetTransactions))
	apiMux.HandleFunc("GET /api/v1/payment/transaction/{id}", authMw(handler.GetTransaction))

	// ── Webhook router (public; bypasses encryption + auth) ────────
	// Razorpay and crypto processors POST cleartext JSON signed with an
	// HMAC over the raw body. Running EncryptionMiddleware on these
	// would reject every webhook with "invalid ciphertext", and auth is
	// intentionally skipped because the handler verifies the provider
	// signature instead.
	webhookMux := http.NewServeMux()
	webhookMux.HandleFunc("POST /api/v1/payment/webhook/razorpay", handler.RazorpayWebhook)
	webhookMux.HandleFunc("POST /api/v1/payment/webhook/crypto", handler.CryptoWebhook)

	// ── Middleware chain ────────────────────────────────────────
	base := service.DefaultMiddleware("payment-service", log)
	extra := middleware.ChainMiddleware(
		middleware.CORSWithWhitelist(getStringSliceEnv("CORS_ORIGINS", []string{"http://localhost:3000"})),
		middleware.MaxBodySize(int64(getIntEnv("MAX_BODY_SIZE_MB", 1))*1024*1024),
		middleware.PerIPRateLimiter(getIntEnv("RATE_LIMIT_RPS", 100), getIntEnv("RATE_LIMIT_BURST", 200)),
		middleware.EncryptionMiddleware,
	)

	// Compose: webhooks go through the base chain only (recover, request id,
	// metrics, logging, security headers) but skip the encryption + rate
	// limit layer. All other routes go through base + extra.
	apiHandler := base(extra(apiMux))
	webhookHandler := base(webhookMux)

	rootMux := http.NewServeMux()
	rootMux.Handle("/api/v1/payment/webhook/", webhookHandler)
	rootMux.Handle("/", apiHandler)

	runtimeCfg := service.Config{
		ServiceName: "payment-service",
		Port:        port,
		Logger:      log,
	}
	if err := service.Run(ctx, runtimeCfg, rootMux); err != nil {
		log.Error("service failed", "err", err)
		os.Exit(1)
	}
}

// ── Environment Helpers ─────────────────────────────────────────

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

func getStringSliceEnv(key string, fallback []string) []string {
	if v := os.Getenv(key); v != "" {
		parts := strings.Split(v, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return fallback
}
