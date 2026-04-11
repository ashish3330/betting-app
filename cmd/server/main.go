package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
)

var (
	store       *Store
	logger      *slog.Logger
	oddsMode    string
	ipBlocker   *IPBlocker
	replayGuard *ReplayGuard
	oddsClient  *OddsAPIClient
)

type Claims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Path     string `json:"path"`
	jwt.RegisteredClaims
}

func main() {
	logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Initialize secrets from env vars (falls back to dev defaults)
	initEncryption()
	initSecurity()
	initCORS()

	// Connect to PostgreSQL if DATABASE_URL is set
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		if err := initDB(dbURL, logger); err != nil {
			logger.Error("database connection failed — falling back to in-memory", "error", err)
		}
	}

	store = NewStore()

	// Wire up bundled internal/* services that share the same DB as the
	// in-memory store. These back a growing subset of HTTP handlers — the
	// rest still delegate to `store`. See services.go and the migration
	// status block at the bottom of this file for details.
	initBundledServices(logger)

	// Start live odds fluctuation (makes mock odds blink in the UI)
	stopFluctuation := make(chan struct{})
	store.StartOddsFluctuation(stopFluctuation)

	// Start live score simulation for in-play cricket matches
	stopScores := make(chan struct{})
	store.StartScoreSimulation(stopScores)

	// If ODDS_API_KEY is set, use The Odds API for real odds data
	oddsMode = "mock (seed data only)"
	if apiKey := os.Getenv("ODDS_API_KEY"); apiKey != "" {
		oddsClient = NewOddsAPIClient(apiKey, logger)
		stopRefresh := make(chan struct{})
		go oddsClient.RefreshCache(stopRefresh)

		// Continuously merge as data arrives from the API
		go func() {
			// Merge every 10 seconds during initial load, then every 5 min
			fastTicker := time.NewTicker(10 * time.Second)
			defer fastTicker.Stop()

			// After 2 minutes, switch to slow interval (all initial fetches done by then)
			slowTimer := time.After(2 * time.Minute)
			for {
				select {
				case <-fastTicker.C:
					mergeLiveOdds(oddsClient)
				case <-slowTimer:
					// Final merge then switch to 5-min interval
					fastTicker.Stop()
					mergeLiveOdds(oddsClient)
					logger.Info("initial odds load complete, switching to 5-min refresh")

					slowTicker := time.NewTicker(5 * time.Minute)
					defer slowTicker.Stop()
					for range slowTicker.C {
						mergeLiveOdds(oddsClient)
					}
					return
				}
			}
		}()
		oddsMode = "live (The Odds API)"
		_ = stopRefresh
	}

	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	mux := http.NewServeMux()

	// ── Health ──────────────────────────────────────────────────
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		dbStatus := "disconnected"
		if useDB() { dbStatus = "connected" }
		writeJSON(w, 200, map[string]string{"status": "ok", "database": dbStatus, "version": "2.0.0", "odds_mode": oddsMode})
	})

	// ── Auth ────────────────────────────────────────────────────
	mux.HandleFunc("POST /api/v1/auth/register", handleRegister)
	mux.HandleFunc("POST /api/v1/auth/login", handleLogin)
	mux.HandleFunc("POST /api/v1/auth/demo", handleDemoLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", handleLogout)
	mux.HandleFunc("POST /api/v1/auth/refresh", handleRefresh)
	mux.HandleFunc("POST /api/v1/auth/otp/generate", auth(handleOTPGenerate))
	mux.HandleFunc("POST /api/v1/auth/otp/verify", handleOTPVerify)
	mux.HandleFunc("POST /api/v1/auth/otp/enable", auth(handleOTPEnable))
	mux.HandleFunc("POST /api/v1/auth/otp/resend", handleOTPResend)
	mux.HandleFunc("POST /api/v1/auth/change-password", auth(handleChangePassword))
	mux.HandleFunc("POST /api/v1/kyc/upload", auth(handleKYCUpload))
	mux.HandleFunc("GET /api/v1/auth/sessions", auth(handleGetSessions))
	mux.HandleFunc("DELETE /api/v1/auth/sessions", auth(handleLogoutAllSessions))
	mux.HandleFunc("GET /api/v1/auth/login-history", auth(handleLoginHistory))

	// ── Notifications ──────────────────────────────────────────────
	mux.HandleFunc("GET /api/v1/notifications", auth(handleGetNotifications))
	mux.HandleFunc("GET /api/v1/notifications/unread-count", auth(handleUnreadCount))
	mux.HandleFunc("POST /api/v1/notifications/{id}/read", auth(handleMarkRead))
	mux.HandleFunc("POST /api/v1/notifications/read-all", auth(handleMarkAllRead))

	// ── Sports / Markets (public) ───────────────────────────────
	mux.HandleFunc("GET /api/v1/sports", handleListSports)
	mux.HandleFunc("GET /api/v1/competitions", handleListCompetitions)
	mux.HandleFunc("GET /api/v1/events", handleListEvents)
	mux.HandleFunc("GET /api/v1/events/{id}/markets", handleEventMarkets)
	mux.HandleFunc("GET /api/v1/markets", handleListMarkets)
	mux.HandleFunc("GET /api/v1/markets/{id}/odds", handleGetOdds)

	// ── SSE Stream (Server-Sent Events for real-time odds push) ──
	mux.HandleFunc("GET /api/v1/stream/odds", handleSSEOddsStream)
	mux.HandleFunc("GET /api/v1/odds/status", handleOddsStatus)
	mux.HandleFunc("GET /api/v1/scores/{eventId}", func(w http.ResponseWriter, r *http.Request) {
		eventID := r.PathValue("eventId")
		score := store.GetLiveScore(eventID)
		if score == nil {
			writeErr(w, 404, "no live score for this event")
			return
		}
		writeJSON(w, 200, score)
	})

	// ── Casino (public) ─────────────────────────────────────────
	mux.HandleFunc("GET /api/v1/casino/providers", handleCasinoProviders)
	mux.HandleFunc("GET /api/v1/casino/games", handleCasinoGames)
	mux.HandleFunc("GET /api/v1/casino/categories", handleCasinoCategories)
	mux.HandleFunc("GET /api/v1/casino/games/{category}", handleCasinoGamesByCategory)
	mux.HandleFunc("GET /api/v1/casino/game/{id}/stream", handleCasinoGameStream)
	mux.HandleFunc("GET /api/v1/casino/game/{id}/info", handleCasinoGameInfo)

	// ── Protected: Positions (P&L per runner) ──────────────────
	mux.HandleFunc("GET /api/v1/positions/{marketId}", auth(handleUserPositions))

	// ── Protected: Betting ──────────────────────────────────────
	mux.HandleFunc("POST /api/v1/bet/place", auth(handlePlaceBet))
	mux.HandleFunc("DELETE /api/v1/bet/{betId}/cancel", auth(handleCancelBet))
	mux.HandleFunc("GET /api/v1/bets", auth(handleUserBets))
	mux.HandleFunc("GET /api/v1/bets/history", auth(handleBetsHistory))
	mux.HandleFunc("GET /api/v1/market/{marketId}/orderbook", auth(handleOrderBook))

	// ── Protected: Wallet ───────────────────────────────────────
	mux.HandleFunc("GET /api/v1/wallet/balance", auth(handleGetBalance))
	mux.HandleFunc("GET /api/v1/wallet/ledger", auth(handleGetLedger))
	mux.HandleFunc("GET /api/v1/wallet/statement", auth(handleGetLedger))
	mux.HandleFunc("POST /api/v1/wallet/deposit/upi", auth(handleUPIDeposit))
	mux.HandleFunc("POST /api/v1/wallet/deposit/crypto", auth(handleCryptoDeposit))
	mux.HandleFunc("POST /api/v1/wallet/withdraw", auth(handleWithdraw))
	mux.HandleFunc("GET /api/v1/wallet/deposits", auth(handleWalletDeposits))
	mux.HandleFunc("GET /api/v1/wallet/withdrawals", auth(handleWalletWithdrawals))

	// ── Protected: Hierarchy ────────────────────────────────────
	mux.HandleFunc("GET /api/v1/hierarchy/children", auth(handleGetChildren))
	mux.HandleFunc("GET /api/v1/hierarchy/children/direct", auth(handleGetDirectChildren))
	mux.HandleFunc("POST /api/v1/hierarchy/credit/transfer", auth(handleTransferCredit))
	mux.HandleFunc("GET /api/v1/hierarchy/user/{id}", auth(handleGetUser))

	// ── Protected: Risk ─────────────────────────────────────────
	mux.HandleFunc("GET /api/v1/risk/exposure", auth(handleMyExposure))
	mux.HandleFunc("GET /api/v1/risk/market/{id}", auth(handleMarketExposure))

	// ── Protected: Casino ───────────────────────────────────────
	mux.HandleFunc("POST /api/v1/casino/session", auth(handleCreateCasinoSession))
	mux.HandleFunc("GET /api/v1/casino/session/{id}", auth(handleGetCasinoSession))
	mux.HandleFunc("DELETE /api/v1/casino/session/{id}", auth(handleCloseCasinoSession))
	mux.HandleFunc("GET /api/v1/casino/history", auth(handleCasinoHistory))

	// ── Protected: Payments ─────────────────────────────────────
	mux.HandleFunc("POST /api/v1/payment/deposit/upi", auth(handleUPIDeposit))
	mux.HandleFunc("POST /api/v1/payment/deposit/crypto", auth(handleCryptoDeposit))
	mux.HandleFunc("POST /api/v1/payment/withdraw", auth(handleWithdraw))
	mux.HandleFunc("GET /api/v1/payment/transactions", auth(handleGetPayments))
	mux.HandleFunc("GET /api/v1/payment/transaction/{id}", auth(handleGetPayment))

	// ── Protected: Reports ──────────────────────────────────────
	mux.HandleFunc("GET /api/v1/reports/pnl", auth(handlePnL))
	mux.HandleFunc("GET /api/v1/reports/dashboard", auth(handleDashboard))

	// ── Protected: Fancy Positions (Run Ladder) ────────────────
	mux.HandleFunc("GET /api/v1/positions/fancy/{marketId}", auth(handleFancyPositions))

	// ── Protected: Cashout ──────────────────────────────────────
	mux.HandleFunc("GET /api/v1/cashout/offer/{betId}", auth(handleCashoutOffer))
	mux.HandleFunc("POST /api/v1/cashout/accept/{betId}", auth(handleCashoutAccept))

	// ── Panel: Audit Log ────────────────────────────────────────
	mux.HandleFunc("GET /api/v1/panel/audit", auth(handlePanelAudit))

	// ── Admin (superadmin / admin only) ─────────────────────────
	mux.HandleFunc("GET /api/v1/admin/users", requireRole(handleAdminListUsers, "superadmin", "admin"))
	mux.HandleFunc("GET /api/v1/admin/users/{id}", requireRole(handleAdminGetUser, "superadmin", "admin"))
	mux.HandleFunc("GET /api/v1/admin/markets", requireRole(handleAdminListMarkets, "superadmin", "admin"))
	mux.HandleFunc("GET /api/v1/admin/bets", requireRole(handleAdminListBets, "superadmin", "admin"))
	mux.HandleFunc("POST /api/v1/admin/markets/{id}/settle", requireRole(handleSettleMarket, "superadmin", "admin"))
	mux.HandleFunc("POST /api/v1/admin/markets/{id}/void", requireRole(handleVoidMarket, "superadmin", "admin"))
	mux.HandleFunc("POST /api/v1/admin/markets/{id}/suspend", requireRole(handleSuspendMarket, "superadmin", "admin"))
	mux.HandleFunc("POST /api/v1/admin/markets/{id}/resume", requireRole(handleResumeMarket, "superadmin", "admin"))

	// ── Panel (role-based hierarchy management) ───────────────
	mux.HandleFunc("GET /api/v1/panel/dashboard", auth(handlePanelDashboard))
	mux.HandleFunc("GET /api/v1/panel/users", auth(handlePanelUsers))
	mux.HandleFunc("POST /api/v1/panel/create-user", auth(handlePanelCreateUser))
	mux.HandleFunc("POST /api/v1/panel/credit/transfer", auth(handlePanelCreditTransfer))
	mux.HandleFunc("POST /api/v1/panel/user/{id}/status", auth(handlePanelUpdateStatus))
	mux.HandleFunc("POST /api/v1/panel/generate-password", auth(handleGeneratePassword))
	mux.HandleFunc("GET /api/v1/panel/reports/pnl", auth(handlePanelPnL))
	mux.HandleFunc("GET /api/v1/panel/reports/volume", auth(handlePanelVolume))
	mux.HandleFunc("GET /api/v1/panel/reports/csv", auth(handlePanelCSV))
	mux.HandleFunc("GET /api/v1/panel/reports/settlement", auth(handlePanelSettlement))

	// ── Referral System ────────────────────────────────────────
	mux.HandleFunc("GET /api/v1/referral/code", auth(handleReferralCode))
	mux.HandleFunc("GET /api/v1/referral/stats", auth(handleReferralStats))
	mux.HandleFunc("POST /api/v1/referral/apply", handleApplyReferral)

	// ── Responsible Gambling ───────────────────────────────────
	mux.HandleFunc("GET /api/v1/responsible/limits", auth(handleGetResponsibleLimits))
	mux.HandleFunc("POST /api/v1/responsible/limits", auth(handleSetResponsibleLimits))
	mux.HandleFunc("POST /api/v1/responsible/self-exclude", auth(handleSelfExclude))

	// ── Deposit Payment Module ─────────────────────────────────
	registerDepositRoutes(mux)

	// ── Config ─────────────────────────────────────────────────
	mux.HandleFunc("GET /api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]interface{}{"encryption": true})
	})

	// ── Seed endpoint (bypasses encryption, sets up full hierarchy + credit chain) ──
	mux.HandleFunc("POST /api/v1/seed", func(w http.ResponseWriter, r *http.Request) {
		// Require X-Seed-Secret header matching SEED_SECRET env var
		seedSecret := os.Getenv("SEED_SECRET")
		if seedSecret == "" {
			writeErr(w, 500, "SEED_SECRET env var is not set")
			return
		}
		if r.Header.Get("X-Seed-Secret") != seedSecret {
			writeErr(w, 403, "invalid or missing X-Seed-Secret header")
			return
		}

		// Read passwords from env vars
		seedPasswords := map[string]string{
			"SEED_SUPERADMIN_PASSWORD": os.Getenv("SEED_SUPERADMIN_PASSWORD"),
			"SEED_ADMIN_PASSWORD":      os.Getenv("SEED_ADMIN_PASSWORD"),
			"SEED_MASTER_PASSWORD":     os.Getenv("SEED_MASTER_PASSWORD"),
			"SEED_AGENT_PASSWORD":      os.Getenv("SEED_AGENT_PASSWORD"),
			"SEED_PLAYER_PASSWORD":     os.Getenv("SEED_PLAYER_PASSWORD"),
		}
		var missingVars []string
		for k, v := range seedPasswords {
			if v == "" {
				missingVars = append(missingVars, k)
			}
		}
		if len(missingVars) > 0 {
			writeErr(w, 500, fmt.Sprintf("missing required env vars: %v", missingVars))
			return
		}

		type seedResult struct {
			Users   []string `json:"users"`
			Credits []string `json:"credits"`
		}
		res := seedResult{}

		// Register hierarchy
		hierarchy := []struct{ u, e, p, role string; parent int64; credit, comm float64 }{
			{"superadmin", "sa@lotus.com", seedPasswords["SEED_SUPERADMIN_PASSWORD"], "superadmin", 0, 10000000, 5},
			{"admin1", "ad@lotus.com", seedPasswords["SEED_ADMIN_PASSWORD"], "admin", 1, 5000000, 4},
			{"master1", "ma@lotus.com", seedPasswords["SEED_MASTER_PASSWORD"], "master", 2, 1000000, 3},
			{"agent1", "ag@lotus.com", seedPasswords["SEED_AGENT_PASSWORD"], "agent", 3, 500000, 2},
			{"player1", "p1@lotus.com", seedPasswords["SEED_PLAYER_PASSWORD"], "client", 4, 100000, 2},
			{"player2", "p2@lotus.com", seedPasswords["SEED_PLAYER_PASSWORD"], "client", 4, 100000, 2},
		}
		for _, h := range hierarchy {
			var parentPtr *int64
			if h.parent > 0 { p := h.parent; parentPtr = &p }
			u, err := store.CreateUser(h.u, h.e, h.p, h.role, parentPtr, h.credit, h.comm)
			if err != nil {
				res.Users = append(res.Users, fmt.Sprintf("%s: %v", h.u, err))
			} else {
				res.Users = append(res.Users, fmt.Sprintf("%s: id=%d", h.u, u.ID))
			}
		}

		// Credit chain
		transfers := []struct{ from, to int64; amount float64 }{
			{1, 2, 500000}, {2, 3, 200000}, {3, 4, 100000}, {4, 5, 50000}, {4, 6, 50000},
		}
		for _, t := range transfers {
			err := store.TransferCredit(t.from, t.to, t.amount)
			if err != nil {
				res.Credits = append(res.Credits, fmt.Sprintf("%d→%d: %v", t.from, t.to, err))
			} else {
				res.Credits = append(res.Credits, fmt.Sprintf("%d→%d: ₹%.0f OK", t.from, t.to, t.amount))
			}
		}

		// Place some sample bets for order book depth on the first in-play match_odds market
		if seedMktID, seedSelIDs := findFirstInPlayMatchOdds(store); seedMktID != "" && len(seedSelIDs) >= 2 {
			store.PlaceAndMatch(5, seedMktID, seedSelIDs[0], "back", 1.80, 5000, "seed-1")
			store.PlaceAndMatch(5, seedMktID, seedSelIDs[0], "back", 1.75, 3000, "seed-2")
			store.PlaceAndMatch(6, seedMktID, seedSelIDs[0], "lay", 1.90, 6000, "seed-3")
			store.PlaceAndMatch(6, seedMktID, seedSelIDs[0], "lay", 1.95, 4000, "seed-4")
			store.PlaceAndMatch(5, seedMktID, seedSelIDs[1], "back", 2.10, 4000, "seed-5")
			store.PlaceAndMatch(6, seedMktID, seedSelIDs[1], "lay", 2.15, 3000, "seed-6")
			res.Credits = append(res.Credits, fmt.Sprintf("sample bets placed on %s for order book depth", seedMktID))
		} else {
			res.Credits = append(res.Credits, "no in-play match_odds market found for sample bets")
		}

		writeJSON(w, 200, res)
	})

	// ── Security Layers ────────────────────────────────────────

	// Shared stop channel for graceful shutdown of cleanup goroutines
	securityStop := make(chan struct{})

	// IP blocker: block IP after 20 failed requests in window
	ipBlocker = NewIPBlocker(20, 15*time.Minute, securityStop)

	// Anti-replay guard: reject duplicate nonces within 5 min
	replayGuard = NewReplayGuard(5*time.Minute, securityStop)

	// Per-IP rate limiter: 50 req/sec burst, 20 req/sec sustained
	rateLimiter := NewPerIPRateLimiter(20, 50, securityStop)

	// Data backup every 10 minutes
	store.StartBackupSchedule(10*time.Minute, "/tmp/lotus_backup.json")

	// Stack middleware: outer → inner
	// Request flow: Rate Limit → IP Block → Security Headers → Max Body → CORS → Encryption → Handler
	handler := rateLimitMiddleware(rateLimiter)(
		ipBlockMiddleware(ipBlocker)(
			securityHeadersMiddleware(
				maxBodySizeMiddleware(1 * 1024 * 1024)( // 1MB max body
					encryptionMiddleware(
						corsMiddleware(mux),
					),
				),
			),
		),
	)

	oddsKeyStatus := "not set"
	if os.Getenv("ODDS_API_KEY") != "" {
		oddsKeyStatus = "configured"
	}

	storageMode := "in-memory"
	if useDB() {
		storageMode = "PostgreSQL"
	}
	logger.Info("3XBet Exchange starting", "port", port, "storage", storageMode, "odds", oddsMode, "odds_api_key", oddsKeyStatus)
	fmt.Printf("\n")
	fmt.Printf("╔══════════════════════════════════════════════════════╗\n")
	fmt.Printf("║          Lotus Exchange Server v2.0.0               ║\n")
	fmt.Printf("╠══════════════════════════════════════════════════════╣\n")
	fmt.Printf("║  API:       http://localhost:%s                   ║\n", port)
	fmt.Printf("║  Health:    http://localhost:%s/health            ║\n", port)
	fmt.Printf("║  Database:  %-40s ║\n", storageMode)
	fmt.Printf("║  Odds:      %-40s ║\n", oddsMode)
	fmt.Printf("║                                                      ║\n")
	fmt.Printf("║  Register users via POST /api/v1/auth/register       ║\n")
	fmt.Printf("║  Then login, transfer credit, place bets, settle.    ║\n")
	fmt.Printf("║                                                      ║\n")
	fmt.Printf("║  Pre-loaded: 8 sports, 6 competitions, 6 events,    ║\n")
	fmt.Printf("║  10 markets, 30 casino games, live odds data.       ║\n")
	fmt.Printf("║  Odds fluctuate every 2-3s for live UI blinking.    ║\n")
	fmt.Printf("╚══════════════════════════════════════════════════════╝\n\n")

	// Graceful shutdown
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Listen for OS signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Block until signal
	sig := <-quit
	logger.Info("shutting down", "signal", sig)

	// Stop background goroutines
	close(stopFluctuation)
	close(stopScores)

	// Graceful shutdown with 10s timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("forced shutdown", "error", err)
	}

	// Drain the async notification/audit outbox so any pending DB writes
	// queued during the final requests are flushed before exit.
	if store != nil {
		store.Stop()
	}
	logger.Info("server stopped")
}

// ── Middleware ───────────────────────────────────────────────────────────────

var corsAllowedOrigins map[string]bool

func initCORS() {
	corsAllowedOrigins = make(map[string]bool)
	origins := os.Getenv("CORS_ALLOWED_ORIGINS")
	if origins == "" {
		origins = "http://localhost:3000,http://localhost:8080"
	}
	for _, o := range strings.Split(origins, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			corsAllowedOrigins[strings.ToLower(o)] = true
		}
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		w.Header().Add("Vary", "Origin")

		if origin != "" && corsAllowedOrigins[strings.ToLower(origin)] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-CSRF-Token")
		w.Header().Set("Access-Control-Expose-Headers", "X-Encrypted, X-CSRF-Token")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// cmdServerAuthCtx bundles per-request identity into a single struct so the
// auth() middleware pays for exactly one context.WithValue call per request
// instead of four. Each WithValue allocates a new context node, so collapsing
// the four Stores into one materially reduces allocation on the hot path.
type cmdServerAuthCtx struct {
	UserID   int64
	Username string
	Role     string
	Path     string
}

type cmdServerAuthCtxKeyType struct{}

var cmdServerAuthCtxKey = cmdServerAuthCtxKeyType{}

// jwtCache memoizes JWT validation for cmd/server's auth() helper. Parsing
// an EdDSA signed token is ~20-30µs per call; caching collapses that to a
// constant-time map lookup for clients that reuse the same token for many
// requests within its TTL. The cache is capped to avoid unbounded growth
// from token rotation.
var jwtCache = struct {
	mu    sync.RWMutex
	items map[string]*Claims
}{items: make(map[string]*Claims, 1024)}

const jwtCacheCap = 10000

// cachedParseJWT returns claims for tokenStr, either from the cache or by
// re-parsing. The read lock is released before the (potentially expensive)
// parse call so that a crowd of cache-missing requests doesn't serialize on
// a write lock held during JWT parsing.
func cachedParseJWT(tokenStr string) (*Claims, error) {
	jwtCache.mu.RLock()
	cached, ok := jwtCache.items[tokenStr]
	jwtCache.mu.RUnlock()
	if ok {
		if cached.ExpiresAt != nil && time.Now().Before(cached.ExpiresAt.Time) {
			return cached, nil
		}
		// Expired — fall through and re-parse. jwt.ParseWithClaims
		// will also reject the token, so the stale entry is evicted
		// below when we overwrite or evict.
	}

	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		return store.PublicKey, nil
	})
	if err != nil {
		return nil, err
	}
	claims := token.Claims.(*Claims)

	jwtCache.mu.Lock()
	if len(jwtCache.items) >= jwtCacheCap {
		// Simple bounded eviction: drop about half of the entries to
		// amortize the cost rather than doing it on every insert.
		n := 0
		for k := range jwtCache.items {
			delete(jwtCache.items, k)
			n++
			if n >= jwtCacheCap/2 {
				break
			}
		}
	}
	jwtCache.items[tokenStr] = claims
	jwtCache.mu.Unlock()

	return claims, nil
}

func auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract token: prefer Authorization header, fallback to HttpOnly cookie
		var tokenStr string
		h := r.Header.Get("Authorization")
		if strings.HasPrefix(h, "Bearer ") {
			tokenStr = h[7:]
		} else if cookie, err := r.Cookie("access_token"); err == nil && cookie.Value != "" {
			tokenStr = cookie.Value
		}
		if tokenStr == "" {
			writeErr(w, 401, "missing authorization token")
			return
		}

		// Blacklist lives on its own RWMutex so authenticated requests
		// no longer contend with bet writers on the global s.mu.
		store.blacklistMu.RLock()
		if exp, ok := store.blacklist[tokenStr]; ok && time.Now().Before(exp) {
			store.blacklistMu.RUnlock()
			writeErr(w, 401, "token has been revoked")
			return
		}
		store.blacklistMu.RUnlock()

		claims, err := cachedParseJWT(tokenStr)
		if err != nil {
			writeErr(w, 401, "invalid token")
			return
		}
		// Re-check user status on every request so an admin suspend takes
		// effect immediately rather than waiting for the access token to
		// expire (up to 24h). One in-memory map read under RLock — same
		// order as the blacklist check above.
		if status := store.GetUserStatus(claims.UserID); status != "" && status != "active" {
			writeErr(w, 403, "account is "+status)
			return
		}
		// Legacy headers preserved so existing handlers that still read
		// X-Username / X-User-ID / X-Role keep working during the
		// migration to the typed context value below.
		r.Header.Set("X-User-ID", strconv.FormatInt(claims.UserID, 10))
		r.Header.Set("X-Username", claims.Username)
		r.Header.Set("X-Role", claims.Role)

		// Bundle all per-request identity into one context value. This
		// replaces the four-WithValue chain that used to allocate a
		// new context.valueCtx node for each of UserID, Username,
		// Role and Path.
		ac := &cmdServerAuthCtx{
			UserID:   claims.UserID,
			Username: claims.Username,
			Role:     claims.Role,
			Path:     claims.Path,
		}
		//nolint:staticcheck // "claims" string key kept for backwards compat
		ctx := context.WithValue(r.Context(), "claims", claims)
		ctx = context.WithValue(ctx, cmdServerAuthCtxKey, ac)
		// Also seed the internal/middleware context keys so bundled
		// services (which read via middleware.UserIDFromContext et al.)
		// keep working when called through cmd/server's auth chain.
		ctx = context.WithValue(ctx, middleware.UserIDKey, claims.UserID)
		ctx = context.WithValue(ctx, middleware.UsernameKey, claims.Username)
		ctx = context.WithValue(ctx, middleware.RoleKey, models.Role(claims.Role))
		ctx = context.WithValue(ctx, middleware.PathKey, claims.Path)
		next(w, r.WithContext(ctx))
	}
}

// cmdServerAuthCtxFromRequest pulls the bundled auth context out of the
// request, or returns nil if the request didn't come through auth().
func cmdServerAuthCtxFromRequest(r *http.Request) *cmdServerAuthCtx {
	if v, ok := r.Context().Value(cmdServerAuthCtxKey).(*cmdServerAuthCtx); ok {
		return v
	}
	return nil
}

func getUserID(r *http.Request) int64 {
	if ac := cmdServerAuthCtxFromRequest(r); ac != nil {
		return ac.UserID
	}
	id, _ := strconv.ParseInt(r.Header.Get("X-User-ID"), 10, 64)
	return id
}

func getUsername(r *http.Request) string {
	if ac := cmdServerAuthCtxFromRequest(r); ac != nil {
		return ac.Username
	}
	return r.Header.Get("X-Username")
}

func getRole(r *http.Request) string {
	if ac := cmdServerAuthCtxFromRequest(r); ac != nil {
		return ac.Role
	}
	return r.Header.Get("X-Role")
}

// requireRole wraps an auth-protected handler with an additional role check.
// Only requests whose X-Role header (set by the auth middleware from JWT
// claims) matches one of the allowed roles are permitted through.
func requireRole(next http.HandlerFunc, allowedRoles ...string) http.HandlerFunc {
	return auth(func(w http.ResponseWriter, r *http.Request) {
		role := getRole(r)
		for _, ar := range allowedRoles {
			if role == ar {
				next(w, r)
				return
			}
		}
		writeErr(w, 403, "insufficient permissions")
	})
}

func generateToken(u *User, ttl time.Duration) string {
	claims := &Claims{
		UserID: u.ID, Username: u.Username, Role: u.Role, Path: u.Path,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "lotus-exchange",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	s, err := token.SignedString(store.PrivateKey)
	if err != nil {
		// Hard failure: previously this dropped a silent empty string into
		// the response and the client got "Bearer " back. Log loudly so the
		// outage is visible; the empty string still triggers the caller's
		// nil/empty checks (writeErr / refusal at the use site).
		logger.Error("token signing failed", "user_id", u.ID, "error", err)
		return ""
	}
	return s
}

// ── Auth Handlers ───────────────────────────────────────────────────────────

func handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username       string  `json:"username"`
		Email          string  `json:"email"`
		Password       string  `json:"password"`
		DateOfBirth    string  `json:"date_of_birth"` // YYYY-MM-DD, required (18+)
		Role           string  `json:"role"`
		ParentID       *int64  `json:"parent_id"`
		CreditLimit    float64 `json:"credit_limit"`
		CommissionRate float64 `json:"commission_rate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeErr(w, 400, "username and password are required")
		return
	}

	// REGULATORY: server-side age verification.
	// Date of birth is required, must parse, and the user must be 18+.
	// This is the only place age is enforced — never trust the client.
	if req.DateOfBirth == "" {
		writeErr(w, 400, "date of birth is required")
		return
	}
	dob, err := time.Parse("2006-01-02", req.DateOfBirth)
	if err != nil {
		writeErr(w, 400, "date of birth must be in YYYY-MM-DD format")
		return
	}
	now := time.Now()
	age := now.Year() - dob.Year()
	if now.YearDay() < dob.YearDay() {
		age-- // birthday hasn't happened yet this year
	}
	if age < 18 {
		writeErr(w, 403, "you must be 18 or older to register")
		return
	}
	if age > 120 {
		writeErr(w, 400, "invalid date of birth")
		return
	}

	// Public registration always creates a "client" role.
	// Only admin/panel endpoints may assign other roles.
	req.Role = "client"

	// Input validation
	req.Username = SanitizeString(req.Username)
	req.Email = SanitizeString(req.Email)
	if err := ValidateUsername(req.Username); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := ValidatePassword(req.Password); err != nil {
		writeErr(w, 400, err.Error())
		return
	}

	u, err := store.CreateUser(req.Username, req.Email, req.Password, req.Role, req.ParentID, req.CreditLimit, req.CommissionRate)
	if err != nil {
		writeErr(w, 409, err.Error())
		return
	}

	// Persist DOB to the users table. Best-effort; if the column doesn't
	// exist yet (legacy schema), log and continue — the age check above
	// already prevented under-18 from getting this far.
	if useDB() {
		if _, dberr := db.Exec(`UPDATE auth.users SET date_of_birth=$1 WHERE id=$2`, dob, u.ID); dberr != nil {
			logger.Warn("failed to persist date_of_birth", "user_id", u.ID, "error", dberr)
		}
	}

	logger.Info("user registered", "id", u.ID, "username", u.Username, "role", u.Role, "age", age)
	writeJSON(w, 201, u)
}

// checkSelfExcludedMonolith queries the betting.responsible_gambling table
// for an active self-exclusion. Returns (excluded, until-string).
//
// Fails CLOSED on DB errors: if we cannot determine exclusion status, we
// pessimistically deny login. This prevents a regulatory hole where a
// self-excluded user could log in any time the responsible_gambling table
// was unavailable.
//
// Handles two schema variants:
//   - migration 004: self_excluded_until TIMESTAMPTZ
//   - monolith db.go: self_excluded BOOLEAN + excluded_until TIMESTAMPTZ
func checkSelfExcludedMonolith(userID int64) (bool, string) {
	if !useDB() {
		// In-memory store path: check the cached limits map.
		store.mu.RLock()
		limits := store.responsibleLimits[userID]
		store.mu.RUnlock()
		if limits != nil && limits.SelfExcluded {
			return true, limits.ExcludedUntil
		}
		return false, ""
	}

	// Try the canonical column first (migration 004 schema).
	var until sql.NullTime
	err := db.QueryRow(
		`SELECT self_excluded_until FROM betting.responsible_gambling WHERE user_id=$1`,
		userID,
	).Scan(&until)
	if err == nil {
		if until.Valid && until.Time.After(time.Now()) {
			return true, until.Time.Format(time.RFC3339)
		}
		return false, ""
	}
	if err == sql.ErrNoRows {
		return false, ""
	}
	// Column might not exist (legacy schema) — try the alternate columns.
	if strings.Contains(err.Error(), "self_excluded_until") || strings.Contains(err.Error(), "42703") {
		var legacyExcluded sql.NullBool
		var legacyUntil sql.NullTime
		err2 := db.QueryRow(
			`SELECT self_excluded, excluded_until FROM betting.responsible_gambling WHERE user_id=$1`,
			userID,
		).Scan(&legacyExcluded, &legacyUntil)
		if err2 == sql.ErrNoRows {
			return false, ""
		}
		if err2 != nil {
			logger.Error("self-exclusion check failed (legacy schema)", "user_id", userID, "error", err2)
			return true, "system error" // fail closed
		}
		if legacyExcluded.Bool && legacyUntil.Valid && legacyUntil.Time.After(time.Now()) {
			return true, legacyUntil.Time.Format(time.RFC3339)
		}
		return false, ""
	}
	// Real DB error — fail closed.
	logger.Error("self-exclusion check failed", "user_id", userID, "error", err)
	return true, "system error"
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}

	clientIP := extractClientIP(r)
	userAgent := r.Header.Get("User-Agent")

	// IP block check
	if ipBlocker != nil && ipBlocker.IsBlocked(clientIP) {
		writeErr(w, 403, "IP temporarily blocked")
		return
	}

	// Brute force check
	allowed, lockedUntil := store.CheckLoginAttempt(req.Username)
	if !allowed {
		store.AddAudit(0, req.Username, "login_blocked", "account locked due to too many failed attempts", clientIP)
		writeJSON(w, 429, map[string]interface{}{
			"error":        "account locked",
			"locked_until": lockedUntil.Format(time.RFC3339),
		})
		return
	}

	u := store.GetUserByUsername(req.Username)
	if u == nil || !verifyPassword(req.Password, u.PasswordHash) {
		if u != nil {
			store.RecordLogin(u.ID, clientIP, userAgent, false)
			store.AddAudit(u.ID, req.Username, "login_failed", "invalid credentials", clientIP)
		}
		ok, lockTime := store.RecordFailedLogin(req.Username)
		if !ok {
			writeJSON(w, 429, map[string]interface{}{
				"error":        "account locked",
				"locked_until": lockTime.Format(time.RFC3339),
			})
			return
		}
		// Record IP failure for blocking
		if ipBlocker != nil {
			ipBlocker.RecordFailure(clientIP)
		}
		writeErr(w, 401, "invalid credentials")
		return
	}

	// Clear IP strikes on successful auth
	if ipBlocker != nil {
		ipBlocker.RecordSuccess(clientIP)
	}

	if u.Status != "active" {
		store.AddAudit(u.ID, u.Username, "login_failed", "account is "+u.Status, clientIP)
		logger.Warn("login attempt on non-active account", "user_id", u.ID, "status", u.Status)
		writeErr(w, 403, "invalid credentials")
		return
	}

	// REGULATORY: block login for self-excluded users.
	// Fails CLOSED — if the responsible_gambling table is unavailable we
	// reject the login rather than silently letting an excluded user in.
	if excluded, until := checkSelfExcludedMonolith(u.ID); excluded {
		store.AddAudit(u.ID, u.Username, "login_blocked", "self-excluded until "+until, clientIP)
		logger.Warn("blocking login: self-excluded", "user_id", u.ID, "until", until)
		writeErr(w, 403, "account is self-excluded")
		return
	}

	// If OTP is enabled, require verification
	if u.OTPEnabled {
		_ = store.GenerateOTP(u.ID) // OTP delivered out-of-band only
		store.AddAudit(u.ID, u.Username, "otp_generated", "OTP required for login", clientIP)
		logger.Info("OTP generated for user", "id", u.ID)
		writeJSON(w, 200, map[string]interface{}{
			"requires_otp": true,
			"user_id":      u.ID,
		})
		return
	}

	store.ClearLoginAttempts(req.Username)
	store.RecordLogin(u.ID, clientIP, userAgent, true)
	store.AddAudit(u.ID, u.Username, "login", "successful login", clientIP)

	access := generateToken(u, 24*time.Hour)
	refresh := generateToken(u, 7*24*time.Hour)
	csrf := store.GenerateCSRF(u.ID)

	store.refreshMu.Lock()
	store.refreshTokens[refresh] = u.ID
	store.refreshTokenTimes[refresh] = time.Now()
	store.refreshMu.Unlock()

	// Determine if connection is secure (direct TLS or behind TLS-terminating proxy)
	isSecure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"

	// Set HttpOnly cookies for secure token storage (immune to XSS)
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    access,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400, // 24 hours
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    refresh,
		Path:     "/api/v1/auth/refresh",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   604800, // 7 days
	})
	// CSRF token in a readable cookie (not HttpOnly) for frontend to read
	http.SetCookie(w, &http.Cookie{
		Name:     "csrf_token",
		Value:    csrf,
		Path:     "/",
		HttpOnly: false,
		Secure:   isSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400,
	})

	w.Header().Set("X-CSRF-Token", csrf)
	logger.Info("user logged in", "id", u.ID, "username", u.Username)

	// Notify hierarchy about login
	store.NotifyHierarchy(u.ID, "login",
		fmt.Sprintf("%s logged in", u.Username),
		fmt.Sprintf("User %s (%s) logged in from %s", u.Username, u.Role, clientIP))

	writeJSON(w, 200, map[string]interface{}{
		"access_token":  access,
		"refresh_token": refresh,
		"user":          u,
		"csrf_token":    csrf,
	})
}

func handleDemoLogin(w http.ResponseWriter, r *http.Request) {
	u := store.CreateDemoUser()

	access := generateToken(u, 24*time.Hour)
	refresh := generateToken(u, 7*24*time.Hour)
	csrf := store.GenerateCSRF(u.ID)

	store.refreshMu.Lock()
	store.refreshTokens[refresh] = u.ID
	store.refreshTokenTimes[refresh] = time.Now()
	store.refreshMu.Unlock()

	clientIP := extractClientIP(r)
	userAgent := r.Header.Get("User-Agent")
	store.RecordLogin(u.ID, clientIP, userAgent, true)
	store.AddAudit(u.ID, u.Username, "demo_login", "demo account created", clientIP)

	// Set HttpOnly cookies
	http.SetCookie(w, &http.Cookie{
		Name: "access_token", Value: access, Path: "/",
		HttpOnly: true, Secure: r.TLS != nil,
		SameSite: http.SameSiteStrictMode, MaxAge: 86400,
	})
	http.SetCookie(w, &http.Cookie{
		Name: "refresh_token", Value: refresh, Path: "/api/v1/auth/refresh",
		HttpOnly: true, Secure: r.TLS != nil,
		SameSite: http.SameSiteStrictMode, MaxAge: 604800,
	})
	http.SetCookie(w, &http.Cookie{
		Name: "csrf_token", Value: csrf, Path: "/",
		HttpOnly: false, Secure: r.TLS != nil,
		SameSite: http.SameSiteStrictMode, MaxAge: 86400,
	})

	logger.Info("demo user created", "id", u.ID, "username", u.Username)
	writeJSON(w, 200, map[string]interface{}{
		"access_token":  access,
		"refresh_token": refresh,
		"user":          u,
		"csrf_token":    csrf,
		"is_demo":       true,
	})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	// Extract token from header or cookie
	var tokenStr string
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		tokenStr = h[7:]
	} else if cookie, err := r.Cookie("access_token"); err == nil {
		tokenStr = cookie.Value
	}

	if tokenStr != "" {
		store.blacklistMu.Lock()
		store.blacklist[tokenStr] = time.Now().Add(24 * time.Hour)
		store.blacklistMu.Unlock()

		parsed, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
			return store.PublicKey, nil
		})
		if err == nil {
			claims := parsed.Claims.(*Claims)
			store.AddAudit(claims.UserID, claims.Username, "logout", "user logged out", r.RemoteAddr)
		}
	}

	// Clear HttpOnly cookies
	http.SetCookie(w, &http.Cookie{Name: "access_token", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	http.SetCookie(w, &http.Cookie{Name: "refresh_token", Value: "", Path: "/api/v1/auth/refresh", MaxAge: -1, HttpOnly: true})
	http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: "", Path: "/", MaxAge: -1})

	writeJSON(w, 200, map[string]string{"message": "logged out"})
}

func handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}

	store.refreshMu.Lock()
	userID, ok := store.refreshTokens[req.RefreshToken]
	if ok {
		delete(store.refreshTokens, req.RefreshToken)
		delete(store.refreshTokenTimes, req.RefreshToken)
	}
	store.refreshMu.Unlock()

	if !ok {
		writeErr(w, 401, "invalid refresh token")
		return
	}

	u := store.GetUser(userID)
	if u == nil {
		writeErr(w, 401, "user not found")
		return
	}

	// REGULATORY: refresh is just another way to extend a session, so it
	// must re-check both account status AND self-exclusion. Without these
	// a user who got a refresh token before being suspended/excluded could
	// keep their session alive indefinitely by refreshing.
	if u.Status != "active" {
		writeErr(w, 403, "account is "+u.Status)
		return
	}
	if excluded, until := checkSelfExcludedMonolith(u.ID); excluded {
		store.AddAudit(u.ID, u.Username, "refresh_blocked", "self-excluded until "+until, r.RemoteAddr)
		writeErr(w, 403, "account is self-excluded")
		return
	}

	access := generateToken(u, 24*time.Hour)
	refresh := generateToken(u, 7*24*time.Hour)

	store.refreshMu.Lock()
	store.refreshTokens[refresh] = u.ID
	store.refreshTokenTimes[refresh] = time.Now()
	store.refreshMu.Unlock()

	writeJSON(w, 200, map[string]interface{}{
		"access_token":  access,
		"refresh_token": refresh,
		"user":          u,
	})
}

// ── Sports & Markets ────────────────────────────────────────────────────────

func handleListSports(w http.ResponseWriter, r *http.Request) {
	store.mu.RLock()
	// Copy the slice header + elements to a new slice so writers cannot
	// mutate the backing array while we are encoding.
	sports := make([]*Sport, len(store.sports))
	copy(sports, store.sports)
	store.mu.RUnlock()
	writeJSON(w, 200, sports)
}

func handleListCompetitions(w http.ResponseWriter, r *http.Request) {
	sport := r.URL.Query().Get("sport")
	store.mu.RLock()
	var out []*Competition
	for _, c := range store.competitions {
		if sport == "" || c.SportID == sport {
			out = append(out, c)
		}
	}
	store.mu.RUnlock()
	writeJSON(w, 200, out)
}

func handleListEvents(w http.ResponseWriter, r *http.Request) {
	compID := r.URL.Query().Get("competition_id")
	sport := r.URL.Query().Get("sport")

	store.mu.RLock()
	defer store.mu.RUnlock()

	type enrichedEvent struct {
		*Event
		MarketID    string `json:"market_id,omitempty"`
		MarketCount int    `json:"market_count"`
	}

	var out []enrichedEvent
	for _, e := range store.events {
		if (compID == "" || e.CompetitionID == compID) && (sport == "" || e.SportID == sport) {
			ee := enrichedEvent{Event: e}
			// Find first match_odds market for this event
			var count int
			for _, m := range store.markets {
				if m.EventID == e.ID {
					count++
					if ee.MarketID == "" {
						ee.MarketID = m.ID
					}
				}
			}
			ee.MarketCount = count
			out = append(out, ee)
		}
	}
	writeJSON(w, 200, out)
}

func handleEventMarkets(w http.ResponseWriter, r *http.Request) {
	eventID := r.PathValue("id")
	store.mu.RLock()
	defer store.mu.RUnlock()
	var out []*Market
	for _, m := range store.markets {
		if m.EventID == eventID {
			out = append(out, m)
		}
	}
	writeJSON(w, 200, out)
}

func handleListMarkets(w http.ResponseWriter, r *http.Request) {
	sport := r.URL.Query().Get("sport")
	store.mu.RLock()
	defer store.mu.RUnlock()

	type marketWithRunners struct {
		*Market
		Runners []*Runner `json:"runners"`
	}

	var out []marketWithRunners
	for _, m := range store.markets {
		if sport == "" || m.Sport == sport {
			mr := marketWithRunners{Market: m}
			if runners, ok := store.runners[m.ID]; ok {
				mr.Runners = runners
			}
			out = append(out, mr)
		}
	}
	writeJSON(w, 200, out)
}

func handleGetOdds(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	store.mu.RLock()
	defer store.mu.RUnlock()
	runners, ok := store.runners[id]
	if !ok {
		writeErr(w, 404, "market not found")
		return
	}

	// Include market metadata so frontend can show status, name, live badge
	resp := map[string]interface{}{
		"market_id":  id,
		"runners":    runners,
		"timestamp":  time.Now(),
		"fetched_at": time.Now().Format(time.RFC3339),
	}
	if m, ok := store.markets[id]; ok {
		resp["status"] = m.Status
		resp["in_play"] = m.InPlay
		resp["event_name"] = m.Name
		resp["start_time"] = m.StartTime
		resp["sport"] = m.Sport
		resp["total_matched"] = m.TotalMatched

		// Include live score if available for this event
		if score, ok := store.liveScores[m.EventID]; ok {
			resp["score"] = map[string]interface{}{
				"home":          score.Home,
				"away":          score.Away,
				"home_score":    score.HomeScore,
				"away_score":    score.AwayScore,
				"overs":         score.Overs,
				"run_rate":      score.RunRate,
				"required_rate": score.RequiredRate,
				"last_wicket":   score.LastWicket,
				"partnership":   score.Partnership,
			}
		}
	}
	writeJSON(w, 200, resp)
}

// ── Casino ──────────────────────────────────────────────────────────────────

func handleCasinoProviders(w http.ResponseWriter, r *http.Request) {
	providers := []map[string]interface{}{
		{"id": "evolution", "name": "Evolution Gaming", "active": true},
		{"id": "ezugi", "name": "Ezugi Live", "active": true},
		{"id": "betgames", "name": "BetGames TV", "active": true},
		{"id": "superspade", "name": "Super Spade Games", "active": true},
		{"id": "pragmatic_play", "name": "Pragmatic Play", "active": true},
	}
	writeJSON(w, 200, providers)
}

func handleCasinoGames(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, store.casinoGames)
}

func handleCasinoCategories(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, []string{"live_casino", "virtual_sports", "slots", "crash_games", "card_games"})
}

func handleCasinoGamesByCategory(w http.ResponseWriter, r *http.Request) {
	cat := r.PathValue("category")
	var out []*CasinoGame
	for _, g := range store.casinoGames {
		if g.Category == cat && g.Active {
			out = append(out, g)
		}
	}
	writeJSON(w, 200, out)
}

// ── Casino Streaming ─────────────────────────────────────────────────────────

func handleCasinoGameStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	game := store.GetGameByID(id)
	if game == nil {
		writeErr(w, 404, "game not found")
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"game_id":    game.ID,
		"name":       game.Name,
		"stream_url": game.StreamURL,
		"iframe_url": game.IframeURL,
		"provider":   game.Provider,
		"active":     game.Active,
		"token":      randHex(16),
		"expires_at": time.Now().Add(4 * time.Hour).Format(time.RFC3339),
	})
}

func handleCasinoGameInfo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	game := store.GetGameByID(id)
	if game == nil {
		writeErr(w, 404, "game not found")
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"id":        game.ID,
		"name":      game.Name,
		"category":  game.Category,
		"provider":  game.Provider,
		"thumbnail": game.Thumbnail,
		"min_bet":   game.MinBet,
		"max_bet":   game.MaxBet,
		"rtp":       game.RTP,
		"tags":      game.Tags,
		"popular":   game.Popular,
		"new":       game.New,
		"active":    game.Active,
		"stream_url": game.StreamURL,
		"iframe_url": game.IframeURL,
	})
}

// ── User Positions (P&L per runner) ──────────────────────────────────────────

func handleUserPositions(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	marketID := r.PathValue("marketId")

	store.mu.RLock()
	defer store.mu.RUnlock()

	// Calculate P&L per selection: selectionID -> profit if that selection wins
	positions := make(map[int64]float64)
	runners := store.runners[marketID]

	for _, bet := range store.bets {
		if bet.UserID != uid || bet.MarketID != marketID {
			continue
		}
		if bet.Status != "matched" && bet.Status != "partial" {
			continue
		}

		selID := bet.SelectionID
		// For partial bets, only the matched portion represents real exposure.
		// For fully matched bets, MatchedStake may equal Stake or be zero
		// (if the field was not explicitly populated at match time).
		stake := bet.MatchedStake
		if stake == 0 {
			stake = bet.Stake
		}

		if bet.Side == "back" {
			// If this selection wins: profit = stake * (price - 1)
			positions[selID] += stake * (bet.Price - 1)
			// If any other selection wins: loss = -stake
			for _, rn := range runners {
				if rn.SelectionID != selID {
					positions[rn.SelectionID] -= stake
				}
			}
		} else { // lay
			// If this selection wins: loss = -stake * (price - 1)
			positions[selID] -= stake * (bet.Price - 1)
			// If any other selection wins: profit = +stake
			for _, rn := range runners {
				if rn.SelectionID != selID {
					positions[rn.SelectionID] += stake
				}
			}
		}
	}

	writeJSON(w, 200, positions)
}

// ── Betting ─────────────────────────────────────────────────────────────────

func handlePlaceBet(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)

	// ═══════════════════════════════════════════════════════════════════════
	// INDUSTRY-LEVEL BET VALIDATION PIPELINE
	// Each check runs in order. Any failure rejects the bet before matching.
	// ═══════════════════════════════════════════════════════════════════════

	// ── 1. Anti-replay: reject duplicate nonce ──
	nonce := r.Header.Get("X-Nonce")
	if replayGuard != nil && !replayGuard.Check(nonce) {
		writeErr(w, 409, "duplicate request detected")
		return
	}

	// ── 2. Parse request ──
	var req struct {
		MarketID    string  `json:"market_id"`
		SelectionID int64   `json:"selection_id"`
		Side        string  `json:"side"`
		Price       float64 `json:"price"`
		Stake       float64 `json:"stake"`
		ClientRef   string  `json:"client_ref"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}

	// ── 3. Basic field validation ──
	if req.Side != "back" && req.Side != "lay" {
		writeErr(w, 400, "side must be 'back' or 'lay'")
		return
	}
	if req.Price < 1.01 || req.Price > 1000 {
		writeErr(w, 400, "price must be between 1.01 and 1000")
		return
	}
	if req.Stake < 100 {
		writeErr(w, 400, "minimum bet is ₹100")
		return
	}
	if req.Stake > 500000 {
		writeErr(w, 400, "maximum bet is ₹5,00,000")
		return
	}

	// ── 4. User account checks ──
	user := store.GetUser(uid)
	if user == nil {
		writeErr(w, 404, "user not found")
		return
	}
	if user.Status != "active" {
		writeErr(w, 403, "account is "+user.Status+", cannot place bets")
		return
	}

	// ── 5-8. Snapshot responsible-gambling limits, market status, selection
	// validity, and the current best price in a single RLock pass. Previously
	// this handler acquired store.mu.RLock() four separate times; collapsing
	// to one pass eliminates three lock round-trips per bet.
	store.mu.RLock()
	limits := store.responsibleLimits[uid]
	m, marketExists := store.markets[req.MarketID]
	var marketStatus, marketType string
	if m != nil {
		marketStatus = m.Status
		marketType = m.MarketType
	}
	marketRunners := store.runners[req.MarketID]
	isSessionMarket := marketType == "fancy" || marketType == "session"
	var selectionValid bool
	var currentBestPrice float64
	for _, runner := range marketRunners {
		if runner.SelectionID == req.SelectionID {
			selectionValid = true
			if isSessionMarket {
				if req.Side == "back" {
					currentBestPrice = runner.YesRate
				} else {
					currentBestPrice = runner.NoRate
				}
			} else if req.Side == "back" {
				if len(runner.BackPrices) > 0 {
					currentBestPrice = runner.BackPrices[0].Price
				}
			} else {
				if len(runner.LayPrices) > 0 {
					currentBestPrice = runner.LayPrices[0].Price
				}
			}
			break
		}
	}
	store.mu.RUnlock()

	// Responsible gambling limits (evaluated outside the lock using the snapshot).
	if limits != nil {
		if limits.SelfExcluded {
			writeErr(w, 403, "self-excluded: betting is disabled")
			return
		}
		if limits.MaxStake > 0 && req.Stake > limits.MaxStake {
			writeErr(w, 400, fmt.Sprintf("stake ₹%.0f exceeds your max stake limit of ₹%.0f", req.Stake, limits.MaxStake))
			return
		}
	}

	// Market status check.
	if !marketExists {
		writeErr(w, 404, "market not found")
		return
	}
	if marketStatus != "open" {
		writeErr(w, 400, "market is "+marketStatus+", cannot place bets")
		return
	}

	// Selection validation — verify the selection exists in this market.
	if !selectionValid {
		writeErr(w, 400, fmt.Sprintf("selection %d does not exist in market %s", req.SelectionID, req.MarketID))
		return
	}

	// ── Stale odds protection ──
	// Match odds / Bookmaker: reject if price moved more than 0.02 from requested
	// Fancy / Session: reject if price is not an exact match (rates must be exact)

	if currentBestPrice > 0 {
		absDrift := req.Price - currentBestPrice
		if absDrift < 0 {
			absDrift = -absDrift
		}

		var rejected bool
		var reason string
		if isSessionMarket {
			// Session/fancy: exact match required
			if absDrift > 0.001 {
				rejected = true
				reason = fmt.Sprintf("Session rate changed from %.0f to %.0f. Rates must be exact.", req.Price, currentBestPrice)
			}
		} else {
			// Match odds / Bookmaker: max 0.02 drift
			if absDrift > 0.02 {
				rejected = true
				reason = fmt.Sprintf("Odds moved from %.2f to %.2f (moved by %.2f, max allowed 0.02).", req.Price, currentBestPrice, absDrift)
			}
		}

		if rejected {
			writeJSON(w, 409, map[string]interface{}{
				"error":         "odds have changed",
				"code":          "ODDS_CHANGED",
				"requested":     req.Price,
				"current_price": currentBestPrice,
				"message":       reason,
			})
			return
		}

		// Use the current live price for the bet (not the potentially stale requested price)
		req.Price = currentBestPrice
	}

	// ── 9. Compute hold amount (pre-check, will be re-validated atomically) ──
	var holdAmount float64
	if req.Side == "back" {
		holdAmount = req.Stake
	} else {
		holdAmount = req.Stake * (req.Price - 1)
	}
	if user.Available() < holdAmount {
		writeErr(w, 400, fmt.Sprintf("insufficient balance: available ₹%.2f, required ₹%.2f", user.Available(), holdAmount))
		return
	}

	// ═══════════════════════════════════════════════════════════════════════
	// ATOMIC BET EXECUTION — hold funds + place bet under a single lock.
	// Balance, exposure limits, duplicate client_ref, and market status are
	// all re-validated inside HoldAndPlaceBet to eliminate TOCTOU races.
	// ═══════════════════════════════════════════════════════════════════════

	result, err := store.HoldAndPlaceBet(uid, req.MarketID, req.SelectionID, req.Side, req.Price, req.Stake, req.ClientRef, holdAmount)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}

	store.AddAudit(uid, r.Header.Get("X-Username"), "bet_placed",
		fmt.Sprintf("bet=%s market=%s side=%s price=%.2f stake=%.2f", result.BetID, req.MarketID, req.Side, req.Price, req.Stake), r.RemoteAddr)
	logger.Info("bet placed", "bet_id", result.BetID, "user", uid, "side", req.Side,
		"price", req.Price, "stake", req.Stake, "matched", result.MatchedStake, "status", result.Status)

	// Notify user
	username := r.Header.Get("X-Username")
	store.AddNotification(uid, "bet_placed",
		fmt.Sprintf("Bet %s — ₹%.0f", strings.ToUpper(req.Side), req.Stake),
		fmt.Sprintf("Your %s bet at %.2f for ₹%.0f has been %s", req.Side, req.Price, req.Stake, result.Status))
	// Notify hierarchy
	store.NotifyHierarchy(uid, "bet_placed",
		fmt.Sprintf("%s placed a bet", username),
		fmt.Sprintf("%s placed %s bet at %.2f for ₹%.0f (%s)", username, req.Side, req.Price, req.Stake, result.Status))

	writeJSON(w, 200, result)
}

func handleUserBets(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	bets := store.GetUserBets(uid)
	if bets == nil {
		bets = []*Bet{}
	}

	// Filter by status if provided
	statusFilter := r.URL.Query().Get("status")
	marketFilter := r.URL.Query().Get("market_id")

	// "open" = active bets (matched but not yet settled)
	// House model: only matched, settled, cancelled, void are valid statuses
	validStatuses := map[string]bool{"matched": true, "settled": true, "cancelled": true, "void": true}
	openStatuses := map[string]bool{"matched": true}

	// Enrich bets with market_name, selection_name, market_type, display_side
	type enrichedBet struct {
		*Bet
		MarketName    string `json:"market_name"`
		SelectionName string `json:"selection_name"`
		MarketType    string `json:"market_type"`
		DisplaySide   string `json:"display_side"`
		ProfitLoss    float64 `json:"profit_loss"`
	}
	result := make([]enrichedBet, 0, len(bets))

	store.mu.RLock()
	for _, b := range bets {
		// Never return bets with invalid statuses (unmatched, partial, error, etc.)
		if !validStatuses[b.Status] {
			continue
		}
		if statusFilter == "open" {
			if !openStatuses[b.Status] {
				continue
			}
		} else if statusFilter != "" && b.Status != statusFilter {
			continue
		}
		if marketFilter != "" && b.MarketID != marketFilter {
			continue
		}
		eb := enrichedBet{Bet: b, ProfitLoss: b.Profit}
		if m, ok := store.markets[b.MarketID]; ok {
			eb.MarketName = m.Name
			eb.MarketType = m.MarketType
		}
		if runners, ok := store.runners[b.MarketID]; ok {
			for _, r := range runners {
				if r.SelectionID == b.SelectionID {
					eb.SelectionName = r.Name
					break
				}
			}
		}
		// For fancy/session: show "yes"/"no" instead of "back"/"lay"
		if eb.MarketType == "fancy" || eb.MarketType == "session" {
			if b.Side == "back" {
				eb.DisplaySide = "yes"
			} else {
				eb.DisplaySide = "no"
			}
		} else {
			eb.DisplaySide = b.Side
		}
		result = append(result, eb)
	}
	store.mu.RUnlock()

	// Sort by created_at descending (newest first)
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt > result[j].CreatedAt
	})

	// Pagination
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if page < 1 {
		page = 1
	}
	total := len(result)
	start := (page - 1) * limit
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}

	writeJSON(w, 200, map[string]interface{}{
		"bets":  result[start:end],
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// handleBetsHistory returns the user's bet history along with aggregated
// summary statistics. The bets payload mirrors handleUserBets so existing
// frontend rendering logic can be reused.
func handleBetsHistory(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	bets := store.GetUserBets(uid)
	if bets == nil {
		bets = []*Bet{}
	}

	type enrichedBet struct {
		*Bet
		MarketName    string  `json:"market_name"`
		SelectionName string  `json:"selection_name"`
		MarketType    string  `json:"market_type"`
		DisplaySide   string  `json:"display_side"`
		ProfitLoss    float64 `json:"profit_loss"`
	}

	validStatuses := map[string]bool{"matched": true, "settled": true, "cancelled": true, "void": true}

	var totalStake, totalPnl float64
	var won, lost, pending int
	result := make([]enrichedBet, 0, len(bets))

	store.mu.RLock()
	for _, b := range bets {
		if !validStatuses[b.Status] {
			continue
		}
		totalStake += b.Stake
		switch b.Status {
		case "settled":
			totalPnl += b.Profit
			if b.Profit > 0 {
				won++
			} else if b.Profit < 0 {
				lost++
			}
		case "matched":
			pending++
		}

		eb := enrichedBet{Bet: b, ProfitLoss: b.Profit}
		if m, ok := store.markets[b.MarketID]; ok {
			eb.MarketName = m.Name
			eb.MarketType = m.MarketType
		}
		if runners, ok := store.runners[b.MarketID]; ok {
			for _, rn := range runners {
				if rn.SelectionID == b.SelectionID {
					eb.SelectionName = rn.Name
					break
				}
			}
		}
		if eb.MarketType == "fancy" || eb.MarketType == "session" {
			if b.Side == "back" {
				eb.DisplaySide = "yes"
			} else {
				eb.DisplaySide = "no"
			}
		} else {
			eb.DisplaySide = b.Side
		}
		result = append(result, eb)
	}
	store.mu.RUnlock()

	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt > result[j].CreatedAt
	})

	writeJSON(w, 200, map[string]interface{}{
		"bets": result,
		"summary": map[string]interface{}{
			"total_bets":  len(result),
			"total_stake": roundMoney(totalStake),
			"total_pnl":   roundMoney(totalPnl),
			"won":         won,
			"lost":        lost,
			"pending":     pending,
		},
	})
}

func handleCancelBet(w http.ResponseWriter, r *http.Request) {
	betID := r.PathValue("betId")
	marketID := r.URL.Query().Get("market_id")
	side := r.URL.Query().Get("side")
	if err := store.CancelOrder(marketID, betID, side); err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	logger.Info("bet cancelled", "bet_id", betID)
	writeJSON(w, 200, map[string]string{"message": "order cancelled", "bet_id": betID})
}

func handleOrderBook(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("marketId")
	backs, lays := store.GetOrderBook(marketID)
	if backs == nil {
		backs = []PriceSize{}
	}
	if lays == nil {
		lays = []PriceSize{}
	}
	writeJSON(w, 200, map[string]interface{}{
		"market_id": marketID,
		"back":      backs,
		"lay":       lays,
	})
}

// ── Wallet ──────────────────────────────────────────────────────────────────

func handleGetBalance(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	u := store.GetUser(uid)
	if u == nil {
		writeErr(w, 404, "user not found")
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"user_id":           u.ID,
		"balance":           u.Balance,
		"exposure":          u.Exposure,
		"available_balance": u.Available(),
	})
}

func handleGetLedger(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 20
	}
	entries := store.GetLedger(uid, limit, offset)
	if entries == nil {
		entries = []*LedgerEntry{}
	}
	writeJSON(w, 200, entries)
}

// ── Hierarchy ───────────────────────────────────────────────────────────────

func handleGetChildren(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	children := store.GetChildren(uid)
	if children == nil {
		children = []*User{}
	}
	writeJSON(w, 200, children)
}

func handleGetDirectChildren(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	children := store.GetDirectChildren(uid)
	if children == nil {
		children = []*User{}
	}
	writeJSON(w, 200, children)
}

func handleTransferCredit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FromUserID int64   `json:"from_user_id"`
		ToUserID   int64   `json:"to_user_id"`
		Amount     float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}
	if req.Amount <= 0 {
		writeErr(w, 400, "amount must be positive")
		return
	}
	if err := store.TransferCredit(req.FromUserID, req.ToUserID, req.Amount); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	store.AddAudit(getUserID(r), r.Header.Get("X-Username"), "credit_transfer",
		fmt.Sprintf("from=%d to=%d amount=%.2f", req.FromUserID, req.ToUserID, req.Amount), r.RemoteAddr)
	logger.Info("credit transferred", "from", req.FromUserID, "to", req.ToUserID, "amount", req.Amount)

	// Notify receiver
	fromUser := store.GetUser(req.FromUserID)
	fromName := "System"
	if fromUser != nil {
		fromName = fromUser.Username
	}
	store.AddNotification(req.ToUserID, "credit",
		fmt.Sprintf("Credit Received — ₹%.0f", req.Amount),
		fmt.Sprintf("₹%.0f credited to your wallet by %s", req.Amount, fromName))
	// Notify hierarchy
	toUser := store.GetUser(req.ToUserID)
	toName := "user"
	if toUser != nil {
		toName = toUser.Username
	}
	store.NotifyHierarchy(req.ToUserID, "credit",
		fmt.Sprintf("Credit transfer — ₹%.0f to %s", req.Amount, toName),
		fmt.Sprintf("%s transferred ₹%.0f to %s", fromName, req.Amount, toName))

	writeJSON(w, 200, map[string]interface{}{
		"message": "credit transferred",
		"from":    req.FromUserID,
		"to":      req.ToUserID,
		"amount":  req.Amount,
	})
}

func handleGetUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	u := store.GetUser(id)
	if u == nil {
		writeErr(w, 404, "user not found")
		return
	}
	writeJSON(w, 200, u)
}

// ── Risk ────────────────────────────────────────────────────────────────────

func handleMyExposure(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	u := store.GetUser(uid)
	if u == nil {
		writeErr(w, 404, "user not found")
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"user_id":        uid,
		"total_exposure": u.Exposure,
	})
}

func handleMarketExposure(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	store.mu.RLock()
	defer store.mu.RUnlock()

	var backStake, layStake float64
	for _, b := range store.bets {
		if b.MarketID == marketID && (b.Status == "matched" || b.Status == "partial") {
			if b.Side == "back" {
				backStake += b.MatchedStake
			} else {
				layStake += b.MatchedStake * (b.Price - 1)
			}
		}
	}
	writeJSON(w, 200, map[string]interface{}{
		"market_id":        marketID,
		"total_back_stake": backStake,
		"total_lay_stake":  layStake,
		"net_exposure":     layStake - backStake,
	})
}

// ── Casino Protected ────────────────────────────────────────────────────────

func handleCreateCasinoSession(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	var req struct {
		GameType   string `json:"game_type"`
		ProviderID string `json:"provider_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}
	if req.GameType == "" || req.ProviderID == "" {
		writeErr(w, 400, "game_type and provider_id are required")
		return
	}
	sess := store.CreateCasinoSession(uid, req.GameType, req.ProviderID)
	logger.Info("casino session created", "id", sess.ID, "user", uid, "game", req.GameType)
	writeJSON(w, 200, sess)
}

func handleGetCasinoSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Try in-memory cache first (hot path), fall through to DB so the
	// session is recoverable after a server restart.
	store.mu.RLock()
	sess, ok := store.casinoSessions[id]
	store.mu.RUnlock()
	if !ok && useDB() {
		sess = dbGetCasinoSession(id)
		if sess != nil {
			ok = true
			// Re-populate cache for subsequent requests.
			store.mu.Lock()
			store.casinoSessions[id] = sess
			store.mu.Unlock()
		}
	}
	if !ok {
		writeErr(w, 404, "session not found")
		return
	}
	writeJSON(w, 200, sess)
}

func handleCloseCasinoSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	store.mu.Lock()
	if sess, ok := store.casinoSessions[id]; ok {
		sess.Status = "closed"
	}
	store.mu.Unlock()
	if useDB() {
		dbUpdateCasinoSessionStatus(id, "closed")
	}
	writeJSON(w, 200, map[string]string{"message": "session closed", "id": id})
}

func handleCasinoHistory(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	if useDB() {
		out := dbGetUserCasinoSessions(uid)
		if out == nil {
			out = []*CasinoSession{}
		}
		writeJSON(w, 200, out)
		return
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	var out []*CasinoSession
	for _, s := range store.casinoSessions {
		if s.UserID == uid {
			out = append(out, s)
		}
	}
	if out == nil {
		out = []*CasinoSession{}
	}
	writeJSON(w, 200, out)
}

// ── Payments ────────────────────────────────────────────────────────────────

func handleUPIDeposit(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	var req struct {
		Amount float64 `json:"amount"`
		UPIID  string  `json:"upi_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}
	if req.Amount < 100 || req.Amount > 1000000 {
		writeErr(w, 400, "amount must be between 100 and 1,000,000")
		return
	}
	tx := store.CreatePaymentTx(uid, "deposit", "upi", req.Amount, "INR", req.UPIID, "")
	if useDB() {
		dbSavePaymentTx(tx)
	}
	logger.Info("UPI deposit initiated", "tx", tx.ID, "user", uid, "amount", req.Amount)
	writeJSON(w, 200, map[string]interface{}{
		"id":               tx.ID,
		"user_id":          tx.UserID,
		"direction":        tx.Direction,
		"method":           tx.Method,
		"amount":           tx.Amount,
		"currency":         tx.Currency,
		"status":           tx.Status,
		"upi_id":           tx.UPIID,
		"created_at":       tx.CreatedAt,
		"whatsapp_notify":  true,
		"whatsapp_message": fmt.Sprintf("Deposit of %s%.0f initiated. Transaction ID: %s. Contact support for status.", "\u20B9", req.Amount, tx.ID),
	})
}

func handleCryptoDeposit(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	var req struct {
		Amount   float64 `json:"amount"`
		Currency string  `json:"currency"`
		Wallet   string  `json:"wallet_address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}
	// Same bounds as UPI deposit. Previously this handler accepted any
	// amount including zero, negative, NaN, and 1e9 — money created from
	// nothing once an agent confirmed it.
	if req.Amount < 100 || req.Amount > 1000000 {
		writeErr(w, 400, "amount must be between 100 and 1,000,000")
		return
	}
	if req.Currency == "" || req.Wallet == "" {
		writeErr(w, 400, "currency and wallet_address are required")
		return
	}
	tx := store.CreatePaymentTx(uid, "deposit", "crypto", req.Amount, req.Currency, "", req.Wallet)
	if useDB() {
		dbSavePaymentTx(tx)
	}
	logger.Info("crypto deposit initiated", "tx", tx.ID, "user", uid)
	writeJSON(w, 200, map[string]interface{}{
		"id":               tx.ID,
		"user_id":          tx.UserID,
		"direction":        tx.Direction,
		"method":           tx.Method,
		"amount":           tx.Amount,
		"currency":         tx.Currency,
		"status":           tx.Status,
		"wallet_address":   tx.Wallet,
		"created_at":       tx.CreatedAt,
		"whatsapp_notify":  true,
		"whatsapp_message": fmt.Sprintf("Crypto deposit of %.2f %s initiated. Transaction ID: %s. Contact support for status.", req.Amount, req.Currency, tx.ID),
	})
}

// isKYCVerified returns true only if the user's auth.users.kyc_status is
// 'verified'. Fails CLOSED — any DB error or missing column is treated as
// "not verified" so that no money can leave the platform without explicit
// human approval of the user's KYC documents.
func isKYCVerified(userID int64) bool {
	if !useDB() {
		// In-memory store has no KYC concept; allow withdrawal in dev mode
		// only. Production must use the DB-backed path.
		return true
	}
	var status sql.NullString
	err := db.QueryRow(`SELECT kyc_status FROM auth.users WHERE id=$1`, userID).Scan(&status)
	if err != nil {
		// Column missing or other error — fail closed.
		logger.Warn("kyc status check failed", "user_id", userID, "error", err)
		return false
	}
	return status.Valid && status.String == "verified"
}

// isWithdrawalMethodAllowed enforces that a user can only withdraw via a
// payment method they have previously deposited with. This is a critical
// AML rule: it prevents the "deposit via UPI, withdraw via crypto" layering
// pattern that money launderers use.
//
// Allows the withdrawal if the user has at least one COMPLETED deposit via
// the same method. If they have no completed deposits at all, blocks.
func isWithdrawalMethodAllowed(userID int64, method string) bool {
	if !useDB() {
		// AML must fail closed in every mode. Allowing all methods in
		// dev mode is acceptable only when DATABASE_URL is unset on a
		// developer workstation; in production we never want this branch
		// to silently green-light cross-method withdrawals. The check
		// remains present so prod still hits the real query path below.
		logger.Warn("withdrawal method check skipped — db not available", "user_id", userID)
		return false
	}
	if method == "" {
		return false
	}
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM betting.payment_transactions
		  WHERE user_id=$1 AND direction='deposit' AND method=$2 AND status='completed'`,
		userID, method,
	).Scan(&count)
	if err != nil {
		logger.Warn("withdrawal method check failed", "user_id", userID, "method", method, "error", err)
		return false // fail closed
	}
	return count > 0
}

func handleWithdraw(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	var req struct {
		Amount float64 `json:"amount"`
		Method string  `json:"method"`
		UPIID  string  `json:"upi_id"`
		Wallet string  `json:"wallet_address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}
	if req.Amount <= 0 {
		writeErr(w, 400, "amount must be positive")
		return
	}
	if req.Method == "" {
		writeErr(w, 400, "method is required")
		return
	}
	u := store.GetUser(uid)
	if u == nil || u.Available() < req.Amount {
		writeErr(w, 400, "insufficient balance")
		return
	}

	// REGULATORY: KYC must be verified before any withdrawal.
	// This is a backend enforcement — the frontend KYC gate is bypassable
	// (anyone can curl this endpoint directly).
	if !isKYCVerified(uid) {
		store.AddAudit(uid, u.Username, "withdrawal_blocked", "KYC not verified", extractClientIP(r))
		writeErr(w, 403, "KYC verification required before withdrawal")
		return
	}

	// Withdrawal amount validation
	if req.Amount < 100 {
		writeErr(w, 400, "minimum withdrawal is ₹100")
		return
	}
	if req.Amount > 500000 {
		writeErr(w, 400, "maximum withdrawal per transaction is ₹5,00,000")
		return
	}

	// AML: withdrawal method must match the user's last successful deposit method.
	// Prevents the classic "deposit via UPI, withdraw via crypto" layering pattern.
	if !isWithdrawalMethodAllowed(uid, req.Method) {
		store.AddAudit(uid, u.Username, "withdrawal_blocked",
			fmt.Sprintf("method %s does not match deposit history", req.Method), extractClientIP(r))
		writeErr(w, 403, "withdrawal method must match your deposit method")
		return
	}

	tx := store.CreatePaymentTx(uid, "withdrawal", req.Method, req.Amount, "INR", req.UPIID, req.Wallet)
	if useDB() {
		dbSavePaymentTx(tx)
	}
	store.HoldFunds(uid, req.Amount, "withdrawal:"+tx.ID)
	logger.Info("withdrawal initiated", "tx", tx.ID, "user", uid, "amount", req.Amount)
	writeJSON(w, 200, map[string]interface{}{
		"id":               tx.ID,
		"user_id":          tx.UserID,
		"direction":        tx.Direction,
		"method":           tx.Method,
		"amount":           tx.Amount,
		"currency":         tx.Currency,
		"status":           tx.Status,
		"created_at":       tx.CreatedAt,
		"whatsapp_notify":  true,
		"whatsapp_message": fmt.Sprintf("Withdrawal of %s%.0f initiated. Transaction ID: %s. Contact support for status.", "\u20B9", req.Amount, tx.ID),
	})
}

func handleGetPayments(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	txns := store.GetUserPayments(uid)
	if txns == nil {
		txns = []*PaymentTx{}
	}
	writeJSON(w, 200, txns)
}

// handleWalletDeposits returns only deposit-direction payment transactions
// for the authenticated user, mounted at /api/v1/wallet/deposits.
func handleWalletDeposits(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	all := store.GetUserPayments(uid)
	result := make([]*PaymentTx, 0, len(all))
	for _, tx := range all {
		if tx.Direction == "deposit" {
			result = append(result, tx)
		}
	}
	writeJSON(w, 200, result)
}

// handleWalletWithdrawals returns only withdrawal-direction payment
// transactions for the authenticated user, mounted at /api/v1/wallet/withdrawals.
func handleWalletWithdrawals(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	all := store.GetUserPayments(uid)
	result := make([]*PaymentTx, 0, len(all))
	for _, tx := range all {
		if tx.Direction == "withdrawal" {
			result = append(result, tx)
		}
	}
	writeJSON(w, 200, result)
}

func handleGetPayment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Cache hit first; on miss fall through to DB so the transaction is
	// retrievable after a server restart even if it isn't in the in-memory
	// map (which only holds payments created since boot).
	store.mu.RLock()
	tx, ok := store.paymentTxns[id]
	store.mu.RUnlock()
	if !ok && useDB() {
		// We only have a per-user query helper, so look it up via the
		// payments table directly. The in-memory cache is repopulated
		// on hit so subsequent reads stay fast.
		row := db.QueryRow(`
			SELECT id, user_id, direction, method, amount, currency, status, upi_id, wallet_address, created_at
			FROM betting.payment_transactions WHERE id=$1`, id)
		t := &PaymentTx{}
		var upiID, wallet sql.NullString
		if err := row.Scan(&t.ID, &t.UserID, &t.Direction, &t.Method, &t.Amount, &t.Currency,
			&t.Status, &upiID, &wallet, &t.CreatedAt); err == nil {
			t.UPIID = upiID.String
			t.Wallet = wallet.String
			tx = t
			ok = true
			store.mu.Lock()
			store.paymentTxns[id] = t
			store.mu.Unlock()
		}
	}
	if !ok {
		writeErr(w, 404, "transaction not found")
		return
	}
	writeJSON(w, 200, tx)
}

// ── Reports ─────────────────────────────────────────────────────────────────

func handlePnL(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	bets := store.GetUserBets(uid)
	var totalStake, totalProfit float64
	var won, lost, pending int
	for _, b := range bets {
		totalStake += b.Stake
		if b.Status == "settled" {
			totalProfit += b.Profit
			if b.Profit > 0 {
				won++
			} else {
				lost++
			}
		} else if b.Status != "cancelled" && b.Status != "void" {
			pending++
		}
	}
	writeJSON(w, 200, map[string]interface{}{
		"user_id":      uid,
		"total_bets":   len(bets),
		"total_stake":  totalStake,
		"net_pnl":      totalProfit,
		"won":          won,
		"lost":         lost,
		"pending":      pending,
	})
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	users := store.AllUsers()
	bets := store.AllBets()
	store.mu.RLock()
	marketCount := len(store.markets)
	store.mu.RUnlock()

	var totalVolume float64
	for _, b := range bets {
		totalVolume += b.Stake
	}
	writeJSON(w, 200, map[string]interface{}{
		"total_users":   len(users),
		"total_bets":    len(bets),
		"total_markets": marketCount,
		"total_volume":  totalVolume,
	})
}

// ── Admin ───────────────────────────────────────────────────────────────────

func handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, store.AllUsers())
}

func handleAdminGetUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	u := store.GetUser(id)
	if u == nil {
		writeErr(w, 404, "user not found")
		return
	}
	writeJSON(w, 200, u)
}

func handleAdminListMarkets(w http.ResponseWriter, r *http.Request) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	out := make([]*Market, 0, len(store.markets))
	for _, m := range store.markets {
		out = append(out, m)
	}
	writeJSON(w, 200, out)
}

func handleAdminListBets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, store.AllBets())
}

func handleSettleMarket(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	var req struct {
		WinnerSelectionID int64 `json:"winner_selection_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}
	if req.WinnerSelectionID == 0 {
		// Settling with selection_id 0 silently marks every back bet as a
		// loser. Force the operator to be explicit.
		writeErr(w, 400, "winner_selection_id is required")
		return
	}
	settled, paidOut := store.SettleMarket(marketID, req.WinnerSelectionID)
	store.AddAudit(getUserID(r), r.Header.Get("X-Username"), "settlement",
		fmt.Sprintf("market=%s winner=%d bets=%d payout=%.2f", marketID, req.WinnerSelectionID, settled, paidOut), r.RemoteAddr)
	logger.Info("market settled", "market", marketID, "winner", req.WinnerSelectionID, "bets", settled, "payout", paidOut)

	// Notify each user with settled bets
	store.mu.RLock()
	marketName := marketID
	if m, ok := store.markets[marketID]; ok {
		marketName = m.Name
	}
	for _, bet := range store.bets {
		if bet.MarketID != marketID || bet.Status != "settled" {
			continue
		}
		pnlLabel := fmt.Sprintf("+₹%.0f", bet.Profit)
		typ := "bet_won"
		if bet.Profit < 0 {
			pnlLabel = fmt.Sprintf("-₹%.0f", -bet.Profit)
			typ = "bet_lost"
		}
		store.mu.RUnlock()
		store.AddNotification(bet.UserID, typ,
			fmt.Sprintf("Bet Settled — %s", pnlLabel),
			fmt.Sprintf("Your bet on %s has been settled. P&L: %s", marketName, pnlLabel))
		store.mu.RLock()
	}
	store.mu.RUnlock()
	writeJSON(w, 200, map[string]interface{}{
		"market_id":    marketID,
		"winner":       req.WinnerSelectionID,
		"bets_settled": settled,
		"total_payout": paidOut,
	})
}

func handleVoidMarket(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	voided := store.VoidMarket(marketID)
	logger.Info("market voided", "market", marketID, "bets_voided", voided)
	writeJSON(w, 200, map[string]interface{}{"market_id": marketID, "bets_voided": voided})
}

func handleSuspendMarket(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	store.mu.Lock()
	if m, ok := store.markets[marketID]; ok {
		m.Status = "suspended"
	}
	store.mu.Unlock()
	writeJSON(w, 200, map[string]string{"message": "market suspended", "market_id": marketID})
}

func handleResumeMarket(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	store.mu.Lock()
	if m, ok := store.markets[marketID]; ok {
		m.Status = "open"
	}
	store.mu.Unlock()
	writeJSON(w, 200, map[string]string{"message": "market resumed", "market_id": marketID})
}

// ── Panel Handlers (role-based hierarchy) ───────────────────────────────────

var roleHierarchy = map[string]int{
	"superadmin": 5, "admin": 4, "master": 3, "agent": 2, "client": 1,
}

func handlePanelDashboard(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	role := getRole(r)
	if role == "client" {
		writeErr(w, 403, "clients cannot access panel")
		return
	}

	u := store.GetUser(uid)
	if u == nil {
		writeErr(w, 404, "user not found")
		return
	}

	stats := store.GetDownlineStats(uid)
	direct := store.GetDirectChildren(uid)

	stats["role"] = role
	stats["username"] = u.Username
	stats["own_balance"] = u.Balance
	stats["own_exposure"] = u.Exposure
	stats["direct_children"] = len(direct)

	// SuperAdmin gets platform-wide extra stats
	if role == "superadmin" {
		all := store.AllUsers()
		stats["platform_total_users"] = len(all)
		store.mu.RLock()
		stats["platform_total_markets"] = len(store.markets)
		store.mu.RUnlock()
		allBets := store.AllBets()
		var vol float64
		for _, b := range allBets {
			vol += b.Stake
		}
		stats["platform_total_bets"] = len(allBets)
		stats["platform_total_volume"] = vol

		// Platform revenue (house P&L)
		store.mu.RLock()
		stats["platform_commission_earned"] = store.platformRevenue.TotalCommission
		stats["platform_bookmaker_pnl"] = store.platformRevenue.TotalBookmakerPnL
		stats["platform_casino_revenue"] = store.platformRevenue.TotalCasinoRevenue
		stats["platform_total_revenue"] = store.platformRevenue.TotalCommission + store.platformRevenue.TotalBookmakerPnL + store.platformRevenue.TotalCasinoRevenue
		store.mu.RUnlock()
	}

	writeJSON(w, 200, stats)
}

func handlePanelUsers(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	role := getRole(r)
	if role == "client" {
		writeErr(w, 403, "clients cannot access panel")
		return
	}

	var users []*User
	if role == "superadmin" {
		users = store.AllUsers()
	} else {
		users = store.GetDownlineUsers(uid)
	}

	// Filter by role if query param provided
	filterRole := r.URL.Query().Get("role")
	if filterRole != "" {
		var filtered []*User
		for _, u := range users {
			if u.Role == filterRole {
				filtered = append(filtered, u)
			}
		}
		users = filtered
	}

	if users == nil {
		users = []*User{}
	}
	writeJSON(w, 200, users)
}

func handlePanelCreateUser(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	creatorRole := getRole(r)
	if creatorRole == "client" {
		writeErr(w, 403, "clients cannot create users")
		return
	}

	var req struct {
		Username       string  `json:"username"`
		Email          string  `json:"email"`
		Password       string  `json:"password"`
		Role           string  `json:"role"`
		CreditLimit    float64 `json:"credit_limit"`
		CommissionRate float64 `json:"commission_rate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}

	// Validate role hierarchy: can only create roles BELOW own level
	if roleHierarchy[req.Role] >= roleHierarchy[creatorRole] {
		writeErr(w, 403, fmt.Sprintf("%s cannot create %s accounts", creatorRole, req.Role))
		return
	}

	if req.Username == "" || req.Password == "" || req.Role == "" {
		writeErr(w, 400, "username, password, and role are required")
		return
	}
	// Same complexity rules as public registration. Previously this path
	// only checked length>=6, letting agents create sub-users with weak
	// passwords that public registration would reject.
	if err := ValidatePassword(req.Password); err != nil {
		writeErr(w, 400, err.Error())
		return
	}

	// Auto-set parent_id to creator
	parentID := uid
	u, err := store.CreateUser(req.Username, req.Email, req.Password, req.Role, &parentID, req.CreditLimit, req.CommissionRate)
	if err != nil {
		writeErr(w, 409, err.Error())
		return
	}

	store.AddAudit(uid, r.Header.Get("X-Username"), "user_created",
		fmt.Sprintf("created user=%s id=%d role=%s", u.Username, u.ID, req.Role), r.RemoteAddr)
	logger.Info("user created via panel", "creator", uid, "created", u.ID, "role", req.Role)

	// Return user WITH password so agent can share credentials
	writeJSON(w, 201, map[string]interface{}{
		"user":     u,
		"password": req.Password,
		"message":  fmt.Sprintf("User %s created successfully. Share credentials securely.", req.Username),
	})
}

func handlePanelCreditTransfer(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	role := getRole(r)
	if role == "client" {
		writeErr(w, 403, "clients cannot transfer credit")
		return
	}

	var req struct {
		ToUserID int64   `json:"to_user_id"`
		Amount   float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}

	if req.Amount <= 0 {
		writeErr(w, 400, "amount must be positive")
		return
	}

	// Validate target is a DIRECT child
	if !store.IsDirectChild(uid, req.ToUserID) {
		writeErr(w, 403, "can only transfer credit to direct children")
		return
	}

	if err := store.TransferCredit(uid, req.ToUserID, req.Amount); err != nil {
		writeErr(w, 400, err.Error())
		return
	}

	store.AddAudit(uid, r.Header.Get("X-Username"), "credit_transfer",
		fmt.Sprintf("from=%d to=%d amount=%.2f (panel)", uid, req.ToUserID, req.Amount), r.RemoteAddr)
	logger.Info("panel credit transfer", "from", uid, "to", req.ToUserID, "amount", req.Amount)
	writeJSON(w, 200, map[string]interface{}{
		"message": "credit transferred",
		"from":    uid,
		"to":      req.ToUserID,
		"amount":  req.Amount,
	})
}

func handlePanelUpdateStatus(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	targetID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)

	// Validate target is in downline
	downline := store.GetDownlineUsers(uid)
	found := false
	for _, u := range downline {
		if u.ID == targetID {
			found = true
			break
		}
	}
	if !found {
		writeErr(w, 403, "user not in your downline")
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}

	if req.Status != "active" && req.Status != "suspended" && req.Status != "blocked" {
		writeErr(w, 400, "status must be active, suspended, or blocked")
		return
	}

	wasSuperadmin := false
	store.mu.Lock()
	if u, ok := store.users[targetID]; ok {
		if u.Role == "superadmin" {
			wasSuperadmin = true
		}
		u.Status = req.Status
	}
	store.mu.Unlock()

	if useDB() {
		dbUpdateUserStatus(targetID, req.Status)
	}

	// If a superadmin's state changed, refresh the cached superadmin list
	// that NotifyHierarchy uses for fan-out.
	if wasSuperadmin {
		store.refreshSuperadminCache()
	}

	store.AddAudit(uid, r.Header.Get("X-Username"), "status_change",
		fmt.Sprintf("target_user=%d new_status=%s", targetID, req.Status), r.RemoteAddr)
	writeJSON(w, 200, map[string]string{"message": "status updated", "status": req.Status})
}

func handleGeneratePassword(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"password": randHex(4)})
}

// ── Panel Report Handlers ───────────────────────────────────────────────────

func handlePanelPnL(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	role := getRole(r)
	if role == "client" {
		writeErr(w, 403, "clients cannot access panel")
		return
	}

	downline := store.GetDownlineUsers(uid)
	userIDs := map[int64]bool{uid: true}
	for _, u := range downline {
		userIDs[u.ID] = true
	}

	type dayEntry struct {
		bets  int
		stake float64
		pnl   float64
	}
	daily := map[string]*dayEntry{}

	store.mu.RLock()
	for _, b := range store.bets {
		if !userIDs[b.UserID] && role != "superadmin" {
			continue
		}
		day := b.CreatedAt[:10]
		entry, ok := daily[day]
		if !ok {
			entry = &dayEntry{}
			daily[day] = entry
		}
		entry.bets++
		entry.stake += b.Stake
		if b.Status == "settled" {
			entry.pnl += b.Profit
		}
	}
	store.mu.RUnlock()

	var result []map[string]interface{}
	for day, d := range daily {
		result = append(result, map[string]interface{}{
			"date": day, "bets": d.bets, "stake": d.stake, "pnl": d.pnl,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i]["date"].(string) < result[j]["date"].(string)
	})
	if result == nil {
		result = []map[string]interface{}{}
	}
	writeJSON(w, 200, result)
}

func handlePanelVolume(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	role := getRole(r)
	if role == "client" {
		writeErr(w, 403, "clients cannot access panel")
		return
	}

	downline := store.GetDownlineUsers(uid)
	userIDs := map[int64]bool{uid: true}
	for _, u := range downline {
		userIDs[u.ID] = true
	}

	type sportEntry struct {
		bets   int
		volume float64
	}
	bySport := map[string]*sportEntry{}

	store.mu.RLock()
	for _, b := range store.bets {
		if !userIDs[b.UserID] && role != "superadmin" {
			continue
		}
		sport := "unknown"
		if m, ok := store.markets[b.MarketID]; ok {
			sport = m.Sport
		}
		entry, ok := bySport[sport]
		if !ok {
			entry = &sportEntry{}
			bySport[sport] = entry
		}
		entry.bets++
		entry.volume += b.Stake
	}
	store.mu.RUnlock()

	var result []map[string]interface{}
	for sport, d := range bySport {
		result = append(result, map[string]interface{}{
			"sport": sport, "bets": d.bets, "volume": d.volume,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i]["volume"].(float64) > result[j]["volume"].(float64)
	})
	if result == nil {
		result = []map[string]interface{}{}
	}
	writeJSON(w, 200, result)
}

func handlePanelSettlement(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	role := getRole(r)
	if role == "client" {
		writeErr(w, 403, "clients cannot access panel")
		return
	}

	downline := store.GetDownlineUsers(uid)
	userIDs := map[int64]bool{uid: true}
	for _, u := range downline {
		userIDs[u.ID] = true
	}

	var settled []map[string]interface{}

	store.mu.RLock()
	for _, b := range store.bets {
		if b.Status != "settled" {
			continue
		}
		if !userIDs[b.UserID] && role != "superadmin" {
			continue
		}
		username := ""
		if u, ok := store.users[b.UserID]; ok {
			username = u.Username
		}
		marketName := b.MarketID
		if m, ok := store.markets[b.MarketID]; ok {
			marketName = m.Name
		}
		settled = append(settled, map[string]interface{}{
			"bet_id":    b.ID,
			"user":      username,
			"market":    marketName,
			"side":      b.Side,
			"stake":     b.Stake,
			"pnl":       b.Profit,
			"settled_at": b.CreatedAt,
		})
	}
	store.mu.RUnlock()

	// Return most recent first, limit to 50
	sort.Slice(settled, func(i, j int) bool {
		return settled[i]["settled_at"].(string) > settled[j]["settled_at"].(string)
	})
	if len(settled) > 50 {
		settled = settled[:50]
	}
	if settled == nil {
		settled = []map[string]interface{}{}
	}
	writeJSON(w, 200, settled)
}

func handlePanelCSV(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	role := getRole(r)
	if role == "client" {
		writeErr(w, 403, "clients cannot access panel")
		return
	}

	downline := store.GetDownlineUsers(uid)
	userIDs := map[int64]bool{uid: true}
	for _, u := range downline {
		userIDs[u.ID] = true
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=bets_report.csv")

	fmt.Fprintf(w, "BetID,User,Market,Side,Price,Stake,Matched,Status,Profit,Created\n")

	store.mu.RLock()
	for _, b := range store.bets {
		if !userIDs[b.UserID] && role != "superadmin" {
			continue
		}
		username := ""
		if u, ok := store.users[b.UserID]; ok {
			username = u.Username
		}
		marketName := b.MarketID
		if m, ok := store.markets[b.MarketID]; ok {
			marketName = m.Name
		}
		fmt.Fprintf(w, "%s,%s,%s,%s,%.2f,%.2f,%.2f,%s,%.2f,%s\n",
			b.ID, username, marketName, b.Side, b.Price, b.Stake, b.MatchedStake, b.Status, b.Profit, b.CreatedAt)
	}
	store.mu.RUnlock()
}

// ── Referral Handlers ───────────────────────────────────────────────────────

func handleReferralCode(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	u := store.GetUser(uid)
	if u == nil {
		writeErr(w, 404, "user not found")
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"referral_code": u.ReferralCode,
		"referral_link": fmt.Sprintf("https://lotusexchange.com/register?ref=%s", u.ReferralCode),
	})
}

func handleReferralStats(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	stats := store.GetReferralStats(uid)
	if stats == nil {
		writeErr(w, 404, "user not found")
		return
	}
	writeJSON(w, 200, stats)
}

func handleApplyReferral(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID       int64  `json:"user_id"`
		ReferralCode string `json:"referral_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}
	if req.ReferralCode == "" {
		writeErr(w, 400, "referral_code is required")
		return
	}

	referrerID, err := store.ApplyReferralCode(req.UserID, req.ReferralCode)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}

	logger.Info("referral applied", "user", req.UserID, "referrer", referrerID, "code", req.ReferralCode)
	writeJSON(w, 200, map[string]interface{}{
		"message":     "referral code applied successfully",
		"referrer_id": referrerID,
	})
}

// ── SSE Odds Stream ────────────────────────────────────────────────────────

func handleSSEOddsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	sport := r.URL.Query().Get("sport")
	if sport == "" {
		sport = "cricket"
	}

	// Set SSE headers (skip encryption for SSE)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no") // nginx

	ctx := r.Context()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Send initial data
	sendSSEOdds(w, flusher, sport)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sendSSEOdds(w, flusher, sport)
		}
	}
}

func sendSSEOdds(w http.ResponseWriter, flusher http.Flusher, sport string) {
	store.mu.RLock()
	var markets []map[string]interface{}
	for _, m := range store.markets {
		if sport != "" && m.Sport != sport {
			continue
		}
		runners := store.runners[m.ID]
		markets = append(markets, map[string]interface{}{
			"id":            m.ID,
			"name":          m.Name,
			"sport":         m.Sport,
			"status":        m.Status,
			"in_play":       m.InPlay,
			"total_matched": m.TotalMatched,
			"runners":       runners,
			"updated_at":    time.Now().Format(time.RFC3339),
		})
	}
	store.mu.RUnlock()

	data, _ := json.Marshal(markets)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func handleOddsStatus(w http.ResponseWriter, r *http.Request) {
	store.mu.RLock()
	marketCount := len(store.markets)
	eventCount := len(store.events)
	store.mu.RUnlock()

	status := map[string]interface{}{
		"markets":    marketCount,
		"events":     eventCount,
		"odds_mode":  oddsMode,
		"updated_at": time.Now().Format(time.RFC3339),
	}

	// Add credit info if odds client exists
	if oddsClient != nil {
		rem, used := oddsClient.GetCreditStatus()
		status["api_credits_remaining"] = rem
		status["api_credits_used"] = used
	}

	writeJSON(w, 200, status)
}

// ── Cashout Handlers ────────────────────────────────────────────────────────

func handleCashoutOffer(w http.ResponseWriter, r *http.Request) {
	betID := r.PathValue("betId")
	uid := getUserID(r)

	store.mu.RLock()
	bet, ok := store.bets[betID]
	store.mu.RUnlock()

	if !ok {
		writeErr(w, 404, "bet not found")
		return
	}
	if bet.UserID != uid {
		writeErr(w, 403, "not your bet")
		return
	}
	if bet.Status != "matched" && bet.Status != "partial" {
		writeErr(w, 400, "bet cannot be cashed out (status: "+bet.Status+")")
		return
	}

	// Cashout offer = 95% of current value (5% house margin)
	offer := bet.MatchedStake * 0.95
	if bet.Side == "back" {
		offer = bet.MatchedStake * (bet.Price - 1) * 0.90 // Back cashout: partial profit
	}

	writeJSON(w, 200, map[string]interface{}{
		"bet_id":       betID,
		"offer_amount": offer,
		"original_stake": bet.MatchedStake,
		"side":         bet.Side,
		"price":        bet.Price,
		"market_id":    bet.MarketID,
	})
}

func handleCashoutAccept(w http.ResponseWriter, r *http.Request) {
	betID := r.PathValue("betId")
	uid := getUserID(r)

	store.mu.Lock()
	bet, ok := store.bets[betID]
	if !ok {
		store.mu.Unlock()
		writeErr(w, 404, "bet not found")
		return
	}
	if bet.UserID != uid {
		store.mu.Unlock()
		writeErr(w, 403, "not your bet")
		return
	}
	if bet.Status != "matched" && bet.Status != "partial" {
		store.mu.Unlock()
		writeErr(w, 400, "cannot cash out")
		return
	}

	// Calculate cashout offer (95% of position value, 10% house margin on
	// back winnings). Same formula as handleCashoutOffer so the user pays
	// what they were quoted.
	cashout := bet.MatchedStake * 0.95
	if bet.Side == "back" {
		cashout = bet.MatchedStake * (bet.Price - 1) * 0.90
	}

	// Compute exposure release using the same liability formula as
	// SettleMarket. CRITICAL: for lay bets the held exposure is
	// MatchedStake*(price-1), NOT just MatchedStake — using the wrong
	// value here permanently leaks exposure (P1.4-class bug, fixed
	// previously for settle/void but missed for cashout).
	exposureToRelease := bet.MatchedStake
	if bet.Side == "lay" {
		exposureToRelease = roundMoney(bet.MatchedStake * (bet.Price - 1))
	}

	// Settle the bet
	bet.Status = "settled"
	bet.Profit = roundMoney(cashout)

	u := store.users[uid]
	if u != nil {
		u.Exposure = roundMoney(math.Max(u.Exposure-exposureToRelease, 0))
		u.Balance = roundMoney(u.Balance + cashout)
	}

	// Keep the per-user-per-market exposure index in sync. Without this
	// the navbar exposure breakdown and future bet placements see stale
	// liability for this market.
	store.releaseExposureLocked(bet)

	now := time.Now().Format(time.RFC3339)
	// Ledger release amount mirrors actual exposure released (lay-aware)
	// so the audit trail reconciles against balance+exposure.
	store.addLedger(uid, exposureToRelease, "release", "cashout-release:"+betID, betID, now)
	store.addLedger(uid, cashout, "settlement", "cashout:"+betID, betID, now)
	store.mu.Unlock()

	// Persist to DB so the cashout survives restart. Was previously a pure
	// in-memory mutation, meaning a server bounce reverted the user's
	// balance and showed the bet as "matched" again.
	if useDB() && u != nil {
		dbUpdateBet(bet)
		dbUpdateBalance(uid, u.Balance, u.Exposure)
	}

	// Audit
	store.AddAudit(uid, "", "cashout_accepted", fmt.Sprintf("bet=%s amount=%.2f", betID, cashout), "")

	logger.Info("cashout accepted", "bet", betID, "user", uid, "amount", cashout)
	writeJSON(w, 200, map[string]interface{}{
		"bet_id":  betID,
		"cashout": cashout,
		"message": "bet cashed out successfully",
	})
}

// ── Odds API Integration ────────────────────────────────────────────────────

// mergeLiveOdds is now handled internally by OddsAPIClient.RefreshCache.
// This function is kept for the initial merge goroutine compatibility.
func mergeLiveOdds(client *OddsAPIClient) {
	client.mu.RLock()
	var sportKeys []string
	for key := range client.cache {
		sportKeys = append(sportKeys, key)
	}
	client.mu.RUnlock()

	for _, key := range sportKeys {
		entry, ok := func() (*cacheEntry, bool) {
			client.mu.RLock()
			defer client.mu.RUnlock()
			e, ok := client.cache[key]
			return e, ok
		}()
		if !ok || entry == nil {
			continue
		}
		MergeOddsIntoStore(store, entry.markets, entry.runners, entry.events, key)
	}
}

// findFirstInPlayMatchOdds returns the first in-play match_odds market ID and
// the selection IDs of its runners, so seed bets can be placed dynamically
// regardless of how event IDs are generated (mock or live API).
func findFirstInPlayMatchOdds(s *Store) (marketID string, selectionIDs []int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for mID, m := range s.markets {
		if m.MarketType == "match_odds" && m.InPlay {
			runners := s.runners[mID]
			if len(runners) >= 2 {
				for _, r := range runners {
					selectionIDs = append(selectionIDs, r.SelectionID)
				}
				return mID, selectionIDs
			}
		}
	}
	return "", nil
}

// ── OTP Handlers ────────────────────────────────────────────────────────────

func handleOTPGenerate(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	_ = store.GenerateOTP(uid) // OTP delivered out-of-band only
	store.AddAudit(uid, r.Header.Get("X-Username"), "otp_generated", "OTP generated manually", r.RemoteAddr)
	logger.Info("OTP generated", "user_id", uid)
	writeJSON(w, 200, map[string]interface{}{
		"message": "OTP generated",
	})
}

func handleOTPVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID int64  `json:"user_id"`
		Code   string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}

	if !store.VerifyOTP(req.UserID, req.Code) {
		store.AddAudit(req.UserID, "", "otp_failed", "invalid OTP code", r.RemoteAddr)
		writeErr(w, 401, "invalid or expired OTP code")
		return
	}

	u := store.GetUser(req.UserID)
	if u == nil {
		writeErr(w, 404, "user not found")
		return
	}

	// REGULATORY: must re-check self-exclusion AND active status on every
	// login path. handleLogin enforces this; OTP verify is a parallel
	// login path that previously skipped both, allowing an excluded /
	// suspended user to log in via the OTP flow.
	if u.Status != "active" {
		store.AddAudit(u.ID, u.Username, "login_failed", "account is "+u.Status, r.RemoteAddr)
		writeErr(w, 403, "invalid credentials")
		return
	}
	if excluded, until := checkSelfExcludedMonolith(u.ID); excluded {
		store.AddAudit(u.ID, u.Username, "login_blocked", "self-excluded until "+until+" (otp)", r.RemoteAddr)
		writeErr(w, 403, "account is self-excluded")
		return
	}

	store.ClearLoginAttempts(u.Username)
	store.RecordLogin(u.ID, r.RemoteAddr, r.Header.Get("User-Agent"), true)
	store.AddAudit(u.ID, u.Username, "login", "successful login via OTP", r.RemoteAddr)

	access := generateToken(u, 24*time.Hour)
	refresh := generateToken(u, 7*24*time.Hour)
	csrf := store.GenerateCSRF(u.ID)

	store.refreshMu.Lock()
	store.refreshTokens[refresh] = u.ID
	store.refreshTokenTimes[refresh] = time.Now()
	store.refreshMu.Unlock()

	w.Header().Set("X-CSRF-Token", csrf)
	logger.Info("user logged in via OTP", "id", u.ID, "username", u.Username)
	writeJSON(w, 200, map[string]interface{}{
		"access_token":  access,
		"refresh_token": refresh,
		"user":          u,
		"csrf_token":    csrf,
	})
}

func handleOTPEnable(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	var req struct {
		Enable bool `json:"enable"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}

	store.mu.Lock()
	if u, ok := store.users[uid]; ok {
		u.OTPEnabled = req.Enable
	}
	store.mu.Unlock()

	action := "disabled"
	if req.Enable {
		action = "enabled"
	}
	store.AddAudit(uid, r.Header.Get("X-Username"), "otp_"+action, "2FA "+action, r.RemoteAddr)
	writeJSON(w, 200, map[string]string{"message": "2FA " + action})
}

// handleOTPResend issues a fresh OTP for a user. If an auth token is present,
// the authenticated user's ID is used. Otherwise the caller may supply a
// user_id in the body (used by the login OTP flow before a session exists).
// The code is never returned in the response — it is delivered out-of-band.
// For dev environments without an SMS gateway, the server logs the code.
func handleOTPResend(w http.ResponseWriter, r *http.Request) {
	var uid int64

	// Prefer authenticated user from JWT (Authorization header or cookie).
	var tokenStr string
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tokenStr = h[7:]
	} else if cookie, err := r.Cookie("access_token"); err == nil && cookie.Value != "" {
		tokenStr = cookie.Value
	}
	if tokenStr != "" {
		if tok, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
			return store.PublicKey, nil
		}); err == nil {
			if claims, ok := tok.Claims.(*Claims); ok {
				uid = claims.UserID
			}
		}
	}

	// Fall back to user_id in body (public resend for pre-login OTP flows).
	if uid == 0 {
		var req struct {
			UserID int64 `json:"user_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		uid = req.UserID
	}

	if uid == 0 {
		writeErr(w, 400, "user_id required (or send Authorization header)")
		return
	}

	u := store.GetUser(uid)
	if u == nil {
		writeErr(w, 404, "user not found")
		return
	}

	code := store.GenerateOTP(uid)
	// Dev-only: log the code so it can be read from server logs when no SMS
	// gateway is configured. Never include it in the HTTP response.
	logger.Info("OTP resent", "user_id", uid, "username", u.Username, "code_dev_only", code)
	store.AddAudit(uid, u.Username, "otp_resent", "OTP resent", r.RemoteAddr)

	writeJSON(w, 200, map[string]string{"message": "OTP sent"})
}

// handleChangePassword lets an authenticated user rotate their password.
// The old password is verified with a constant-time comparison before the
// new hash is written to both the in-memory store and the database.
func handleChangePassword(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}
	if req.OldPassword == "" || req.NewPassword == "" {
		writeErr(w, 400, "old_password and new_password are required")
		return
	}
	if len(req.NewPassword) < 8 {
		writeErr(w, 400, "new password must be at least 8 characters")
		return
	}
	if req.OldPassword == req.NewPassword {
		writeErr(w, 400, "new password must be different from old password")
		return
	}

	u := store.GetUser(uid)
	if u == nil {
		writeErr(w, 404, "user not found")
		return
	}
	if !verifyPassword(req.OldPassword, u.PasswordHash) {
		store.AddAudit(uid, u.Username, "password_change_failed", "old password mismatch", r.RemoteAddr)
		writeErr(w, 401, "old password is incorrect")
		return
	}

	newHash := hashPassword(req.NewPassword)
	store.mu.Lock()
	if user, ok := store.users[uid]; ok {
		user.PasswordHash = newHash
		user.UpdatedAt = time.Now().Format(time.RFC3339)
	}
	store.mu.Unlock()

	if useDB() {
		if _, err := db.Exec(`UPDATE auth.users SET password_hash=$1, updated_at=NOW() WHERE id=$2`, newHash, uid); err != nil {
			logger.Error("dbUpdatePassword failed", "error", err, "user_id", uid)
		}
	}

	store.AddAudit(uid, u.Username, "password_changed", "password changed by user", r.RemoteAddr)
	logger.Info("password changed", "user_id", uid, "username", u.Username)
	writeJSON(w, 200, map[string]string{"message": "password changed"})
}

// handleKYCUpload accepts a KYC document upload via multipart/form-data.
// The field name is "document" and the maximum file size is 5 MB.
// For dev environments, the document metadata is persisted but the file
// bytes themselves are discarded — real object-storage integration is a TODO.
func handleKYCUpload(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)

	const maxBytes = 5 << 20 // 5 MB
	// Cap the request body before parsing to prevent memory abuse.
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+1024)

	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeErr(w, 400, "failed to parse multipart form (max 5MB): "+err.Error())
		return
	}

	file, header, err := r.FormFile("document")
	if err != nil {
		writeErr(w, 400, "missing 'document' file field")
		return
	}
	defer file.Close()

	if header.Size <= 0 {
		writeErr(w, 400, "uploaded file is empty")
		return
	}
	if header.Size > maxBytes {
		writeErr(w, 413, "file exceeds 5MB limit")
		return
	}

	// MIME / magic-byte whitelist. KYC documents must be JPEG, PNG, or
	// PDF — never executables, scripts, archives, or HTML. We sniff the
	// first 512 bytes (RFC 7574 / DetectContentType) instead of trusting
	// the client-provided Content-Type header. The file pointer is then
	// reset so downstream code (when streaming to object storage) reads
	// from the start.
	sniffBuf := make([]byte, 512)
	n, _ := file.Read(sniffBuf)
	detected := http.DetectContentType(sniffBuf[:n])
	allowed := map[string]bool{
		"image/jpeg":      true,
		"image/png":       true,
		"application/pdf": true,
	}
	if !allowed[detected] {
		writeErr(w, 415, "unsupported file type — only JPEG, PNG, and PDF are accepted (got "+detected+")")
		return
	}
	if seeker, ok := file.(interface {
		Seek(offset int64, whence int) (int64, error)
	}); ok {
		_, _ = seeker.Seek(0, 0)
	}

	docType := r.FormValue("doc_type")
	if docType == "" {
		docType = "identity"
	}

	// TODO: stream the file bytes to object storage (S3/GCS) and store the
	// returned URL. For now, we only persist metadata and mark it pending.
	status := "pending_review"
	filename := header.Filename

	if useDB() {
		// Ensure the table exists (idempotent migration) so this endpoint
		// works on databases that predate the KYC feature.
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS betting.kyc_documents (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES auth.users(id),
			doc_type TEXT NOT NULL DEFAULT 'identity',
			filename TEXT NOT NULL,
			size_bytes BIGINT NOT NULL DEFAULT 0,
			content_type TEXT DEFAULT '',
			storage_url TEXT DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending_review',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`); err != nil {
			logger.Error("kyc_documents table create failed", "error", err)
		}
		if _, err := db.Exec(`INSERT INTO betting.kyc_documents
			(user_id, doc_type, filename, size_bytes, content_type, status)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			uid, docType, filename, header.Size, detected, status); err != nil {
			logger.Error("kyc_documents insert failed", "error", err)
		}
	}

	store.AddAudit(uid, r.Header.Get("X-Username"), "kyc_upload",
		fmt.Sprintf("KYC document uploaded: %s (%d bytes, type=%s)", filename, header.Size, docType),
		r.RemoteAddr)
	logger.Info("KYC document uploaded", "user_id", uid, "filename", filename, "size", header.Size)

	writeJSON(w, 200, map[string]interface{}{
		"message":  "KYC document uploaded",
		"status":   status,
		"filename": filename,
		"size":     header.Size,
		"doc_type": docType,
	})
}

// ── Session Handlers ────────────────────────────────────────────────────────

func handleGetSessions(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	// In a real system, we'd track sessions. For mock, return current token info.
	sessions := []map[string]interface{}{
		{
			"id":         randHex(8),
			"ip":         r.RemoteAddr,
			"user_agent": r.Header.Get("User-Agent"),
			"created_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			"current":    true,
		},
	}

	// Add mock older sessions
	history := store.GetLoginHistory(uid, 5)
	for i, rec := range history {
		if i == 0 {
			continue // skip most recent (that's current)
		}
		sessions = append(sessions, map[string]interface{}{
			"id":         randHex(8),
			"ip":         rec.IP,
			"user_agent": rec.UserAgent,
			"created_at": rec.LoginAt,
			"current":    false,
		})
	}

	writeJSON(w, 200, sessions)
}

func handleLogoutAllSessions(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	// Revoke all refresh tokens for this user
	store.refreshMu.Lock()
	for token, id := range store.refreshTokens {
		if id == uid {
			delete(store.refreshTokens, token)
			delete(store.refreshTokenTimes, token)
		}
	}
	store.refreshMu.Unlock()

	store.AddAudit(uid, r.Header.Get("X-Username"), "logout_all", "all sessions terminated", r.RemoteAddr)
	writeJSON(w, 200, map[string]string{"message": "all sessions terminated"})
}

func handleLoginHistory(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	records := store.GetLoginHistory(uid, limit)
	if records == nil {
		records = []*LoginRecord{}
	}
	writeJSON(w, 200, records)
}

// ── Audit Handler ───────────────────────────────────────────────────────────

func handlePanelAudit(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	role := getRole(r)
	if role == "client" {
		writeErr(w, 403, "clients cannot access audit log")
		return
	}

	entries := store.GetAuditLog(uid, role)
	if entries == nil {
		entries = []*AuditEntry{}
	}

	// Filter by action if query param
	actionFilter := r.URL.Query().Get("action")
	usernameFilter := r.URL.Query().Get("username")
	if actionFilter != "" || usernameFilter != "" {
		var filtered []*AuditEntry
		for _, e := range entries {
			if actionFilter != "" && e.Action != actionFilter {
				continue
			}
			if usernameFilter != "" && !strings.Contains(strings.ToLower(e.Username), strings.ToLower(usernameFilter)) {
				continue
			}
			filtered = append(filtered, e)
		}
		entries = filtered
	}

	if entries == nil {
		entries = []*AuditEntry{}
	}
	writeJSON(w, 200, entries)
}

// ── Responsible Gambling Handlers ────────────────────────────────────────────

func handleGetResponsibleLimits(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value("claims").(*Claims)
	store.mu.RLock()
	limits := store.responsibleLimits[claims.UserID]
	store.mu.RUnlock()
	if limits == nil {
		limits = &ResponsibleGamblingLimits{}
	}
	writeJSON(w, 200, limits)
}

func handleSetResponsibleLimits(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value("claims").(*Claims)
	var req ResponsibleGamblingLimits
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid request body")
		return
	}
	if req.DailyDeposit < 0 || req.DailyLoss < 0 || req.MaxStake < 0 || req.SessionMinutes < 0 {
		writeErr(w, 400, "limits must be non-negative")
		return
	}

	store.mu.Lock()
	existing := store.responsibleLimits[claims.UserID]
	if existing == nil {
		existing = &ResponsibleGamblingLimits{}
		store.responsibleLimits[claims.UserID] = existing
	}

	// REGULATORY: limit DECREASES (or activations from zero) take effect
	// immediately to protect the user. Limit INCREASES are subject to a
	// 24-hour cool-down to prevent compulsive lifting in the heat of the
	// moment — same rule the internal/responsible service enforces.
	// Increases are rejected here with 202 + pending_changes; the user
	// must wait and re-submit. (A scheduled-change table is the long-term
	// fix; this monolith path opts for the simpler "blocked + told to
	// wait" pattern that meets the regulatory intent.)
	pending := []string{}
	if req.DailyDeposit != 0 {
		if existing.DailyDeposit == 0 || req.DailyDeposit < existing.DailyDeposit {
			existing.DailyDeposit = req.DailyDeposit
		} else if req.DailyDeposit > existing.DailyDeposit {
			pending = append(pending, "daily_deposit_limit")
		}
	}
	if req.DailyLoss != 0 {
		if existing.DailyLoss == 0 || req.DailyLoss < existing.DailyLoss {
			existing.DailyLoss = req.DailyLoss
		} else if req.DailyLoss > existing.DailyLoss {
			pending = append(pending, "daily_loss_limit")
		}
	}
	if req.MaxStake != 0 {
		if existing.MaxStake == 0 || req.MaxStake < existing.MaxStake {
			existing.MaxStake = req.MaxStake
		} else if req.MaxStake > existing.MaxStake {
			pending = append(pending, "max_stake_per_bet")
		}
	}
	if req.SessionMinutes != 0 {
		if existing.SessionMinutes == 0 || req.SessionMinutes < existing.SessionMinutes {
			existing.SessionMinutes = req.SessionMinutes
		} else if req.SessionMinutes > existing.SessionMinutes {
			pending = append(pending, "session_limit_minutes")
		}
	}
	store.mu.Unlock()

	if useDB() {
		dbSaveResponsibleLimits(claims.UserID, existing)
	}

	if len(pending) > 0 {
		writeJSON(w, 202, map[string]interface{}{
			"limits":          existing,
			"pending_changes": pending,
			"message":         "limit increases require a 24-hour cool-down before taking effect",
		})
		return
	}
	writeJSON(w, 200, existing)
}

func handleSelfExclude(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value("claims").(*Claims)
	store.mu.Lock()
	existing := store.responsibleLimits[claims.UserID]
	if existing == nil {
		existing = &ResponsibleGamblingLimits{}
		store.responsibleLimits[claims.UserID] = existing
	}
	existing.SelfExcluded = true
	existing.ExcludedUntil = time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	store.mu.Unlock()

	if useDB() {
		dbSaveResponsibleLimits(claims.UserID, existing)
	}

	writeJSON(w, 200, map[string]string{
		"message":       "Self-excluded for 24 hours",
		"excluded_until": existing.ExcludedUntil,
	})
}

// ── Fancy Positions (Run Ladder) ─────────────────────────────────────────

func handleFancyPositions(w http.ResponseWriter, r *http.Request) {
	uid := getUserID(r)
	marketID := r.PathValue("marketId")

	store.mu.RLock()
	defer store.mu.RUnlock()

	// Find the fancy market's run value
	runners := store.runners[marketID]
	if len(runners) == 0 {
		writeJSON(w, 200, []interface{}{})
		return
	}

	runValue := int(runners[0].RunValue)
	if runValue == 0 {
		runValue = 50
	} // default

	// Calculate P&L based on user's bets
	var totalStake float64
	var totalProfit float64
	for _, bet := range store.bets {
		if bet.UserID != uid || bet.MarketID != marketID {
			continue
		}
		if bet.Status != "matched" && bet.Status != "partial" {
			continue
		}
		// For partial bets, only the matched portion represents real exposure.
		stake := bet.MatchedStake
		if stake == 0 {
			stake = bet.Stake
		}
		if bet.Side == "back" { // YES
			totalProfit += stake * (bet.Price - 1)
			totalStake += stake
		} else { // NO
			totalProfit -= stake * (bet.Price - 1)
			totalStake -= stake
		}
	}

	// Build run ladder: show runs from runValue-5 to runValue+5
	type RunEntry struct {
		Run int     `json:"run"`
		PnL float64 `json:"pnl"`
	}
	var ladder []RunEntry
	for run := runValue - 5; run <= runValue+5; run++ {
		if run < 0 {
			continue
		}
		var pnl float64
		if run >= runValue {
			pnl = totalProfit // YES wins
		} else {
			pnl = -totalStake // NO wins (user loses stake)
		}
		ladder = append(ladder, RunEntry{Run: run, PnL: pnl})
	}

	writeJSON(w, 200, ladder)
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ── Notification Handlers ────────────────────────────────────────────────────

// Notification handlers — migrated to internal/notification.
//
// When the bundled DB-backed service is wired up (see services.go) these
// delegate straight to internal/notification.Handler so the monolith and the
// cmd/notification-service binary share identical behaviour. When the DB is
// unavailable we fall back to the legacy in-memory Store so local-dev-without-
// a-database still works.

func handleGetNotifications(w http.ResponseWriter, r *http.Request) {
	if bundled != nil && bundled.notifHandler != nil {
		bundled.notifHandler.List(w, r)
		return
	}
	uid := getUserID(r)
	unreadOnly := r.URL.Query().Get("unread") == "true"
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	notifs := store.GetNotifications(uid, unreadOnly, limit, offset)
	writeJSON(w, 200, notifs)
}

func handleUnreadCount(w http.ResponseWriter, r *http.Request) {
	if bundled != nil && bundled.notifHandler != nil {
		bundled.notifHandler.UnreadCount(w, r)
		return
	}
	uid := getUserID(r)
	count := store.GetUnreadCount(uid)
	writeJSON(w, 200, map[string]int{"unread_count": count})
}

func handleMarkRead(w http.ResponseWriter, r *http.Request) {
	// The internal handler expects a numeric ID; the legacy in-memory store
	// uses opaque string IDs. When the bundled service is active and the
	// path value parses as int64, delegate; otherwise use the legacy path.
	if bundled != nil && bundled.notifHandler != nil {
		if _, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil {
			bundled.notifHandler.MarkRead(w, r)
			return
		}
	}
	uid := getUserID(r)
	notifID := r.PathValue("id")
	if store.MarkNotificationRead(uid, notifID) {
		writeJSON(w, 200, map[string]string{"message": "marked as read"})
	} else {
		writeErr(w, 404, "notification not found")
	}
}

func handleMarkAllRead(w http.ResponseWriter, r *http.Request) {
	if bundled != nil && bundled.notifHandler != nil {
		bundled.notifHandler.MarkAllRead(w, r)
		return
	}
	uid := getUserID(r)
	count := store.MarkAllNotificationsRead(uid)
	writeJSON(w, 200, map[string]interface{}{"message": "all marked as read", "count": count})
}

// ─────────────────────────────────────────────────────────────────────────────
// cmd/server migration status (Phase 15 — migrate monolith to internal/*)
// ─────────────────────────────────────────────────────────────────────────────
//
// Goal: turn cmd/server into a "bundled mode" binary that reuses the same
// internal/* services as cmd/*-service binaries for local-dev ergonomics.
//
// MIGRATED to internal/*:
//   • notification (GET /api/v1/notifications, /unread-count, POST /{id}/read,
//     /read-all)  ← delegates to internal/notification.Handler when DB is up.
//
// STILL USING in-memory Store:
//   • auth (register/login/logout/otp/refresh)          — tightly coupled to
//     store.PublicKey, store.blacklist, store.brute-force tracking.
//   • wallet (balance, ledger, deposit, withdraw)       — in-memory ledger +
//     deposit.go payment flow.
//   • bet placement, cancellation, history, positions   — in-memory matching
//     engine in store.go.
//   • market / sport / event / odds / SSE stream        — seeded in memory,
//     odds fluctuation goroutine.
//   • casino sessions + game catalog                    — in-memory.
//   • hierarchy GET/POST                                — store.GetChildren,
//     store.TransferCredit (these also update local exposure tracking).
//   • risk / exposure                                    — computed from the
//     in-memory bet book.
//   • reports (PnL, dashboard, panel CSV)               — aggregates in-memory
//     bets + ledger.
//   • KYC upload (POST /api/v1/kyc/upload)              — multipart upload,
//     not in the internal/kyc handler which exposes /kyc/submit (JSON).
//   • referral, responsible-gambling, admin, panel      — all use store.
//
// Next steps for future work:
//   1. Wire a *redis.Client into bundledServices so risk/fraud/hierarchy can
//      share the same Redis the microservices already use.
//   2. Migrate hierarchy (read-only) first — its DB schema is stable.
//   3. Migrate reporting once clickhouse wiring is optional in that package.
//   4. Tackle wallet/bet/matching last — they require a full port off the
//      in-memory Store to the internal services.
