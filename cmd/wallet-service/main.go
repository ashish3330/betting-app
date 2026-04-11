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
	"strings"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/lotus-exchange/lotus-exchange/internal/auth"
	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/wallet"
	"github.com/lotus-exchange/lotus-exchange/pkg/logger"
	"github.com/lotus-exchange/lotus-exchange/pkg/service"
)

// ---------------------------------------------------------------------------
// NATS request/response types
// ---------------------------------------------------------------------------

// BalanceRequest is sent by other services to query a user's wallet balance.
type BalanceRequest struct {
	UserID int64 `json:"user_id"`
}

// BalanceResponse is the reply to a balance query.
type BalanceResponse struct {
	Success          bool    `json:"success"`
	Balance          float64 `json:"balance"`
	Exposure         float64 `json:"exposure"`
	AvailableBalance float64 `json:"available_balance"`
	Error            string  `json:"error,omitempty"`
}

// HoldRequest is sent by the matching engine to hold funds for a bet.
type HoldRequest struct {
	UserID int64   `json:"user_id"`
	Amount float64 `json:"amount"`
	BetID  string  `json:"bet_id"`
}

// HoldResponse is the reply to a hold request.
type HoldResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// ReleaseRequest is sent to release previously held funds.
type ReleaseRequest struct {
	UserID int64   `json:"user_id"`
	Amount float64 `json:"amount"`
	BetID  string  `json:"bet_id"`
}

// ReleaseResponse is the reply to a release request.
type ReleaseResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// DepositRequest is sent by the payment service to credit a deposit.
type DepositRequest struct {
	UserID    int64   `json:"user_id"`
	Amount    float64 `json:"amount"`
	Reference string  `json:"reference"`
}

// DepositResponse is the reply to a deposit request.
type DepositResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// SettleRequest is sent by the settlement service to apply P&L.
type SettleRequest struct {
	UserID     int64   `json:"user_id"`
	BetID      string  `json:"bet_id"`
	PnL        float64 `json:"pnl"`
	Commission float64 `json:"commission"`
}

// SettleResponse is the reply to a settle request.
type SettleResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

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

func errStr(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}

// isSerializationError returns true when the error indicates a PostgreSQL
// serialization failure (SQLSTATE 40001).
func isSerializationError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "40001") || strings.Contains(msg, "serialization_failure")
}

// withRetry executes fn up to maxAttempts times, retrying only on
// serialization failures with exponential backoff.
func withRetry(ctx context.Context, maxAttempts int, fn func(ctx context.Context) error) error {
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = fn(ctx)
		if err == nil || !isSerializationError(err) {
			return err
		}
		backoff := time.Duration(50<<uint(attempt)) * time.Millisecond // 50ms, 100ms, 200ms
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	return err
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	env := getEnv("ENVIRONMENT", "development")
	log := logger.New(env)
	slog.SetDefault(log)

	port := getEnv("WALLET_SERVICE_PORT", "8082")
	log.Info("starting wallet service", "port", port, "env", env)

	// Wallet service handles the money-path: ledger reads/writes, balance
	// checks and exposure updates. Bump the pool defaults to match the
	// real concurrency; DB_MAX_*_CONNS in the environment still wins.
	walletMaxOpen := getIntEnv("DB_MAX_OPEN_CONNS", 50)
	walletMaxIdle := getIntEnv("DB_MAX_IDLE_CONNS", 20)
	db, err := service.OpenPostgres(ctx, getEnv("DATABASE_URL", ""), walletMaxOpen, walletMaxIdle, 5*time.Minute)
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

	nc, err := service.ConnectNATS(ctx, getEnv("NATS_URL", nats.DefaultURL), "wallet-service", log)
	if err != nil {
		log.Error("nats", "err", err)
		os.Exit(1)
	}
	defer nc.Drain()

	// ── Auth Service (for JWT validation in HTTP middleware) ────
	authService, err := auth.NewService(
		db, rdb, log,
		15*time.Minute, // accessTTL — only used for token generation, not validation
		24*time.Hour,   // refreshTTL
		getEnv("ED25519_PRIVATE_KEY", ""),
		getEnv("ED25519_PUBLIC_KEY", ""),
	)
	if err != nil {
		log.Error("failed to create auth service for middleware", "error", err)
		os.Exit(1)
	}
	authMw := middleware.AuthMiddleware(authService)

	// ── Wallet Service & Handler ────────────────────────────────
	walletSvc := wallet.NewService(db, rdb, log)
	walletHandler := wallet.NewHandler(walletSvc)

	// ── NATS Subscriptions (request/reply) ──────────────────────

	// natsTimeout is the maximum time a NATS handler may spend processing a
	// request before the context is cancelled.
	const natsTimeout = 10 * time.Second
	const maxRetries = 3

	// Pending limits on subscriptions guard against unbounded memory growth if
	// the wallet service momentarily falls behind producers.
	const (
		natsPendingMsgLimit   = 65536            // 64k pending messages
		natsPendingBytesLimit = 64 * 1024 * 1024 // 64 MiB pending bytes
	)

	applyPendingLimits := func(sub *nats.Subscription, subject string) {
		if err := sub.SetPendingLimits(natsPendingMsgLimit, natsPendingBytesLimit); err != nil {
			log.Warn("failed to set nats pending limits", "subject", subject, "error", err)
		}
	}

	// subscribe registers a NATS request handler with pending limits applied.
	// On subscribe failure the process exits — these subjects are load-bearing.
	subscribe := func(subject string, handler nats.MsgHandler) {
		sub, err := nc.Subscribe(subject, handler)
		if err != nil {
			log.Error("failed to subscribe", "subject", subject, "error", err)
			os.Exit(1)
		}
		applyPendingLimits(sub, subject)
	}

	// wallet.balance — other services query a user's balance
	subscribe("wallet.balance", func(msg *nats.Msg) {
		var req BalanceRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			data, _ := json.Marshal(BalanceResponse{Success: false, Error: "invalid request: " + err.Error()})
			msg.Respond(data)
			return
		}
		cctx, cancel := context.WithTimeout(context.Background(), natsTimeout)
		defer cancel()
		summary, err := walletSvc.GetBalance(cctx, req.UserID)
		if err != nil {
			data, _ := json.Marshal(BalanceResponse{Success: false, Error: errStr(err)})
			msg.Respond(data)
			return
		}
		data, _ := json.Marshal(BalanceResponse{
			Success:          true,
			Balance:          summary.Balance,
			Exposure:         summary.Exposure,
			AvailableBalance: summary.AvailableBalance,
		})
		msg.Respond(data)
	})

	// wallet.hold — matching engine holds funds for a bet
	subscribe("wallet.hold", func(msg *nats.Msg) {
		var req HoldRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			data, _ := json.Marshal(HoldResponse{Success: false, Error: "invalid request: " + err.Error()})
			msg.Respond(data)
			return
		}
		cctx, cancel := context.WithTimeout(context.Background(), natsTimeout)
		defer cancel()
		err := withRetry(cctx, maxRetries, func(ctx context.Context) error {
			return walletSvc.HoldFunds(ctx, req.UserID, req.Amount, req.BetID)
		})
		data, _ := json.Marshal(HoldResponse{Success: err == nil, Error: errStr(err)})
		msg.Respond(data)
	})

	// wallet.release — release previously held funds
	subscribe("wallet.release", func(msg *nats.Msg) {
		var req ReleaseRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			data, _ := json.Marshal(ReleaseResponse{Success: false, Error: "invalid request: " + err.Error()})
			msg.Respond(data)
			return
		}
		cctx, cancel := context.WithTimeout(context.Background(), natsTimeout)
		defer cancel()
		err := withRetry(cctx, maxRetries, func(ctx context.Context) error {
			return walletSvc.ReleaseFunds(ctx, req.UserID, req.Amount, req.BetID)
		})
		data, _ := json.Marshal(ReleaseResponse{Success: err == nil, Error: errStr(err)})
		msg.Respond(data)
	})

	// wallet.deposit — payment service credits deposits
	subscribe("wallet.deposit", func(msg *nats.Msg) {
		var req DepositRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			data, _ := json.Marshal(DepositResponse{Success: false, Error: "invalid request: " + err.Error()})
			msg.Respond(data)
			return
		}
		cctx, cancel := context.WithTimeout(context.Background(), natsTimeout)
		defer cancel()
		err := withRetry(cctx, maxRetries, func(ctx context.Context) error {
			return walletSvc.Deposit(ctx, req.UserID, req.Amount, req.Reference)
		})
		data, _ := json.Marshal(DepositResponse{Success: err == nil, Error: errStr(err)})
		msg.Respond(data)
	})

	// wallet.settle — settlement service applies P&L
	subscribe("wallet.settle", func(msg *nats.Msg) {
		var req SettleRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			data, _ := json.Marshal(SettleResponse{Success: false, Error: "invalid request: " + err.Error()})
			msg.Respond(data)
			return
		}
		cctx, cancel := context.WithTimeout(context.Background(), natsTimeout)
		defer cancel()
		err := withRetry(cctx, maxRetries, func(ctx context.Context) error {
			return walletSvc.SettleBet(ctx, req.UserID, req.BetID, req.PnL, req.Commission)
		})
		data, _ := json.Marshal(SettleResponse{Success: err == nil, Error: errStr(err)})
		msg.Respond(data)
	})

	log.Info("nats subscriptions active",
		"subjects", []string{"wallet.balance", "wallet.hold", "wallet.release", "wallet.deposit", "wallet.settle"})

	// ── HTTP Router ─────────────────────────────────────────────
	mux := http.NewServeMux()

	mux.Handle("GET /health", service.HealthHandler("wallet-service", map[string]service.HealthCheck{
		"db":    func(ctx context.Context) error { return db.PingContext(ctx) },
		"redis": func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
		"nats": func(_ context.Context) error {
			if !nc.IsConnected() {
				return errors.New("disconnected")
			}
			return nil
		},
	}))

	// Wallet HTTP routes (protected by auth middleware)
	protectedMux := http.NewServeMux()
	protectedMux.HandleFunc("GET /api/v1/wallet/balance", walletHandler.GetBalance)
	protectedMux.HandleFunc("GET /api/v1/wallet/ledger", walletHandler.GetLedger)
	protectedMux.HandleFunc("GET /api/v1/wallet/statement", walletHandler.GetStatement)
	protectedMux.HandleFunc("GET /api/v1/wallet/deposits", walletHandler.GetDeposits)
	protectedMux.HandleFunc("GET /api/v1/wallet/withdrawals", walletHandler.GetWithdrawals)
	protectedMux.HandleFunc("POST /api/v1/wallet/deposit", walletHandler.Deposit)

	mux.Handle("/api/v1/wallet/", authMw(protectedMux))

	// Prometheus scrape endpoint
	mux.Handle("GET /metrics", service.MetricsHandler())

	// ── Middleware chain ────────────────────────────────────────
	base := service.DefaultMiddleware("wallet-service", log)
	extra := middleware.ChainMiddleware(
		middleware.MaxBodySize(int64(getIntEnv("MAX_BODY_SIZE_MB", 1))*1024*1024),
		middleware.PerIPRateLimiter(getIntEnv("RATE_LIMIT_RPS", 100), getIntEnv("RATE_LIMIT_BURST", 200)),
		middleware.EncryptionMiddleware,
	)

	runtimeCfg := service.Config{
		ServiceName: "wallet-service",
		Port:        port,
		Logger:      log,
	}
	if err := service.Run(ctx, runtimeCfg, base(extra(mux))); err != nil {
		log.Error("service failed", "err", err)
		os.Exit(1)
	}
}
