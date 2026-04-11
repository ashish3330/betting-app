package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/lotus-exchange/lotus-exchange/internal/auth"
	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/pkg/config"
	"github.com/lotus-exchange/lotus-exchange/pkg/logger"
	"github.com/lotus-exchange/lotus-exchange/pkg/service"
	"github.com/redis/go-redis/v9"
	"nhooyr.io/websocket"
)

// getEnv reads an env var or returns a fallback.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// appendSearchPath appends search_path to the DSN if not already present,
// ensuring every pooled connection uses the correct schema search order.
func appendSearchPath(dsn string) string {
	if strings.Contains(dsn, "search_path") {
		return dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "search_path=betting,auth,public"
}

// maxWSConnections caps the number of concurrent WebSocket connections.
const maxWSConnections = 10000

// activeWSConns tracks the number of active WebSocket connections.
var activeWSConns int64

func main() {
	cfg := config.Load()

	if err := cfg.Validate(); err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	log := logger.New(cfg.Environment)
	slog.SetDefault(log)

	log.Info("starting lotus exchange API gateway",
		"port", cfg.HTTPPort, "env", cfg.Environment)

	// ── Database & Redis (needed for auth service only) ─────────
	// Append search_path to the DSN so it is set on every connection
	// instead of using a one-shot db.Exec which only affects one conn.
	db, err := sql.Open("postgres", appendSearchPath(cfg.DatabaseURL))
	if err != nil {
		log.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	// Gateway only talks to the DB for auth token validation and session
	// lookups — everything else is proxied to downstream services. A
	// small pool avoids wasting connection slots that the wallet and
	// matching services need. Env-provided DB_MAX_*_CONNS still wins.
	gwMaxOpen := cfg.DBMaxOpenConns
	if os.Getenv("DB_MAX_OPEN_CONNS") == "" {
		gwMaxOpen = 10
	}
	gwMaxIdle := cfg.DBMaxIdleConns
	if os.Getenv("DB_MAX_IDLE_CONNS") == "" {
		gwMaxIdle = 5
	}
	db.SetMaxOpenConns(gwMaxOpen)
	db.SetMaxIdleConns(gwMaxIdle)
	db.SetConnMaxLifetime(cfg.DBConnMaxLifetime)

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisURL,
		Password: cfg.RedisPassword,
		PoolSize: cfg.RedisPoolSize,
	})
	defer rdb.Close()

	// ── Auth Service (gateway validates JWTs and injects headers) ─
	authService, err := auth.NewService(db, rdb, log,
		cfg.AccessTokenExpiry, cfg.RefreshTokenExpiry,
		cfg.ED25519PrivateKeyHex, cfg.ED25519PublicKeyHex)
	if err != nil {
		log.Error("failed to create auth service", "error", err)
		os.Exit(1)
	}

	// ── Service URLs from environment ──────────────────────────
	authURL := getEnv("AUTH_SERVICE_URL", "http://localhost:8081")
	walletURL := getEnv("WALLET_SERVICE_URL", "http://localhost:8082")
	matchingURL := getEnv("MATCHING_SERVICE_URL", "http://localhost:8083")
	paymentURL := getEnv("PAYMENT_SERVICE_URL", "http://localhost:8084")
	casinoURL := getEnv("CASINO_SERVICE_URL", "http://localhost:8085")
	oddsURL := getEnv("ODDS_SERVICE_URL", "http://localhost:8086")
	fraudURL := getEnv("FRAUD_SERVICE_URL", "http://localhost:8087")
	reportingURL := getEnv("REPORTING_SERVICE_URL", "http://localhost:8088")
	riskURL := getEnv("RISK_SERVICE_URL", "http://localhost:8089")
	hierarchyURL := getEnv("HIERARCHY_SERVICE_URL", "http://localhost:8090")
	notificationURL := getEnv("NOTIFICATION_SERVICE_URL", "http://localhost:8091")
	adminURL := getEnv("ADMIN_SERVICE_URL", "http://localhost:8092")

	// Map of service name → URL for health checking
	serviceMap := map[string]string{
		"auth":         authURL,
		"wallet":       walletURL,
		"matching":     matchingURL,
		"payment":      paymentURL,
		"casino":       casinoURL,
		"odds":         oddsURL,
		"fraud":        fraudURL,
		"reporting":    reportingURL,
		"risk":         riskURL,
		"hierarchy":    hierarchyURL,
		"notification": notificationURL,
		"admin":        adminURL,
	}

	// ── Custom transport for reverse proxies ───────────────────
	proxyTransport := &http.Transport{
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	// ── Build reverse proxies ──────────────────────────────────
	proxies := make(map[string]*httputil.ReverseProxy)
	for name, rawURL := range serviceMap {
		target, err := url.Parse(rawURL)
		if err != nil {
			log.Error("invalid service URL", "service", name, "url", rawURL, "error", err)
			os.Exit(1)
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.Transport = proxyTransport

		// Capture loop variable for the closure
		svcName := name
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			log.Error("proxy error", "service", svcName, "error", err, "path", r.URL.Path)
			http.Error(w, fmt.Sprintf(`{"error":"service unavailable: %s"}`, svcName), http.StatusBadGateway)
		}
		// Customize the Director to forward relevant headers
		originalDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			originalDirector(req)
			// Ensure X-Request-ID is forwarded (set by middleware)
			// Forward auth context headers
			// These are set by our auth middleware before proxying
		}
		proxies[name] = proxy
	}

	// ── Route table: path prefix → proxy name ──────────────────
	// Order matters: more specific prefixes must come first.
	type route struct {
		prefix       string
		proxyName    string
		requireAuth  bool
		requireAdmin bool
	}

	routes := []route{
		// Auth routes — no auth required (login, register, etc.)
		{prefix: "/api/v1/auth/", proxyName: "auth", requireAuth: false},

		// Payment webhooks — no auth (signature verified by service)
		{prefix: "/api/v1/payment/webhook/", proxyName: "payment", requireAuth: false},

		// Casino webhooks — no auth (provider-authenticated)
		{prefix: "/api/v1/casino/webhook/", proxyName: "casino", requireAuth: false},

		// Public casino catalog (match monolith behaviour)
		{prefix: "/api/v1/casino/providers", proxyName: "casino", requireAuth: false},
		{prefix: "/api/v1/casino/games", proxyName: "casino", requireAuth: false},
		{prefix: "/api/v1/casino/categories", proxyName: "casino", requireAuth: false},

		// Admin routes — require auth + admin role
		// /api/v1/admin/kyc/ is served by kyc handlers on the hierarchy service;
		// all other /api/v1/admin/* goes to the dedicated admin service.
		{prefix: "/api/v1/admin/kyc/", proxyName: "hierarchy", requireAuth: true, requireAdmin: true},
		{prefix: "/api/v1/admin/", proxyName: "admin", requireAuth: true, requireAdmin: true},
		{prefix: "/api/v1/fraud/", proxyName: "fraud", requireAuth: true, requireAdmin: true},

		// Protected routes — require auth
		{prefix: "/api/v1/wallet/", proxyName: "wallet", requireAuth: true},
		{prefix: "/api/v1/bet/", proxyName: "matching", requireAuth: true},
		{prefix: "/api/v1/bets", proxyName: "matching", requireAuth: true},
		{prefix: "/api/v1/positions/", proxyName: "matching", requireAuth: true},
		{prefix: "/api/v1/cashout/", proxyName: "matching", requireAuth: true},
		{prefix: "/api/v1/market/", proxyName: "matching", requireAuth: true},
		{prefix: "/api/v1/payment/", proxyName: "payment", requireAuth: true},
		{prefix: "/api/v1/casino/", proxyName: "casino", requireAuth: true},
		{prefix: "/api/v1/risk/", proxyName: "risk", requireAuth: true},
		{prefix: "/api/v1/reports/", proxyName: "reporting", requireAuth: true},
		{prefix: "/api/v1/panel/", proxyName: "admin", requireAuth: true},
		{prefix: "/api/v1/hierarchy/", proxyName: "hierarchy", requireAuth: true},
		{prefix: "/api/v1/responsible-gambling/", proxyName: "hierarchy", requireAuth: true},
		// Alias — the monolith/tests/frontend also use /responsible/; route to same service.
		{prefix: "/api/v1/responsible/", proxyName: "hierarchy", requireAuth: true},
		{prefix: "/api/v1/referral/", proxyName: "hierarchy", requireAuth: true},
		{prefix: "/api/v1/kyc/", proxyName: "hierarchy", requireAuth: true},
		{prefix: "/api/v1/notifications", proxyName: "notification", requireAuth: true},

		// Public odds/market routes (no trailing slash = exact or prefix match)
		{prefix: "/api/v1/sports", proxyName: "odds", requireAuth: false},
		{prefix: "/api/v1/competitions", proxyName: "odds", requireAuth: false},
		{prefix: "/api/v1/events/", proxyName: "odds", requireAuth: false},
		{prefix: "/api/v1/events", proxyName: "odds", requireAuth: false},
		{prefix: "/api/v1/markets", proxyName: "odds", requireAuth: false},
		{prefix: "/api/v1/scores/", proxyName: "odds", requireAuth: false},
		{prefix: "/api/v1/stream/", proxyName: "odds", requireAuth: false},
		{prefix: "/api/v1/odds/", proxyName: "odds", requireAuth: false},
		{prefix: "/api/v1/config", proxyName: "odds", requireAuth: false},

		// Seed endpoint — dev-only bootstrap served by admin-service
		{prefix: "/api/v1/seed", proxyName: "admin", requireAuth: false},
	}

	// ── Health-check HTTP client (created once at startup) ─────
	healthClient := &http.Client{Timeout: 5 * time.Second}

	// ── Router ─────────────────────────────────────────────────
	mux := http.NewServeMux()

	// Health endpoint: aggregates status from all downstream services
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		result := map[string]interface{}{
			"status":  "ok",
			"version": "2.0.0",
		}

		type svcResult struct {
			name   string
			status string
		}

		// Fan out health checks in parallel with a 5-second deadline
		healthCtx, healthCancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer healthCancel()

		resultsCh := make(chan svcResult, len(serviceMap))
		for name, svcURL := range serviceMap {
			go func(n, u string) {
				healthURL := u + "/health"
				req, err := http.NewRequestWithContext(healthCtx, http.MethodGet, healthURL, nil)
				if err != nil {
					resultsCh <- svcResult{name: n, status: "unavailable"}
					return
				}
				resp, err := healthClient.Do(req)
				if err != nil {
					resultsCh <- svcResult{name: n, status: "unavailable"}
					return
				}
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					resultsCh <- svcResult{name: n, status: "ok"}
				} else {
					resultsCh <- svcResult{name: n, status: "degraded"}
				}
			}(name, svcURL)
		}

		services := make(map[string]string)
		overallOK := true
		for range serviceMap {
			sr := <-resultsCh
			services[sr.name] = sr.status
			if sr.status != "ok" {
				overallOK = false
			}
		}

		// Also check gateway's own DB and Redis
		dbStatus := "ok"
		if err := db.PingContext(healthCtx); err != nil {
			dbStatus = "unavailable"
			overallOK = false
		}
		services["gateway_db"] = dbStatus

		redisStatus := "ok"
		if err := rdb.Ping(healthCtx).Err(); err != nil {
			redisStatus = "unavailable"
			overallOK = false
		}
		services["gateway_redis"] = redisStatus

		if !overallOK {
			result["status"] = "degraded"
		}
		result["services"] = services

		statusCode := http.StatusOK
		if !overallOK {
			statusCode = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(result)
	})

	// Prometheus scrape endpoint
	mux.Handle("GET /metrics", service.MetricsHandler())

	// WebSocket proxy to odds service
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		proxyWebSocket(w, r, oddsURL, authService, cfg.CORSOrigins, log)
	})

	// API route proxying
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		for _, rt := range routes {
			if !strings.HasPrefix(path, rt.prefix) {
				continue
			}

			proxy, ok := proxies[rt.proxyName]
			if !ok {
				http.Error(w, `{"error":"internal routing error"}`, http.StatusInternalServerError)
				return
			}

			// Auth validation at the gateway level
			if rt.requireAuth {
				token := extractBearerToken(r)
				if token == "" {
					http.Error(w, `{"error":"missing authorization token"}`, http.StatusUnauthorized)
					return
				}

				if authService.IsBlacklisted(r.Context(), token) {
					http.Error(w, `{"error":"token has been revoked"}`, http.StatusUnauthorized)
					return
				}

				claims, err := authService.ValidateToken(token)
				if err != nil {
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}

				// Inject identity headers for downstream services
				r.Header.Set("X-User-ID", fmt.Sprintf("%d", claims.UserID))
				r.Header.Set("X-Username", claims.Username)
				r.Header.Set("X-Role", string(claims.Role))

				// Admin role check
				if rt.requireAdmin {
					if claims.Role != "superadmin" && claims.Role != "admin" {
						http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
						return
					}
				}
			}

			proxy.ServeHTTP(w, r)
			return
		}

		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	})

	// ── Signal handling ────────────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// ── Global Middleware (chained) ────────────────────────────
	// Note: SecurityHeaders and EncryptionMiddleware are intentionally NOT
	// applied here. Every downstream microservice already runs them via
	// service.DefaultMiddleware + its own EncryptionMiddleware layer. Running
	// them again at the gateway would emit duplicate security headers and
	// double-encrypt response bodies, breaking clients.
	// RequestLogger is intentionally omitted here: downstream services already
	// log every request via pkg/service.DefaultMiddleware, and doubling up
	// wastes CPU and log volume on the hot path.
	chain := middleware.ChainMiddleware(
		middleware.RecoverPanic(log),
		middleware.CORSWithWhitelist(cfg.CORSOrigins),
		middleware.RequestID,
		middleware.MetricsMiddleware("gateway"),
		middleware.MaxBodySize(int64(cfg.MaxBodySizeMB)*1024*1024),
		middleware.PerIPRateLimiterWithContext(ctx, cfg.RateLimitRPS, cfg.RateLimitBurst),
	)
	handler := chain(mux)

	// ── HTTP Server ────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.HTTPPort,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Send server errors to a channel so the main goroutine can handle
	// graceful shutdown instead of calling os.Exit inside a goroutine.
	srvErr := make(chan error, 1)
	go func() {
		log.Info("API gateway starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			srvErr <- err
		}
	}()

	select {
	case err := <-srvErr:
		log.Error("server error", "error", err)
		stop()
	case <-ctx.Done():
	}
	log.Info("shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("server shutdown error", "error", err)
	}

	log.Info("gateway stopped")
}

// extractBearerToken extracts the JWT from the Authorization header.
func extractBearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return h[7:]
	}
	return ""
}

// proxyWebSocket proxies a WebSocket connection to the odds service.
// It validates the JWT from the query parameter before establishing the
// connection, then bidirectionally copies frames between client and backend.
func proxyWebSocket(w http.ResponseWriter, r *http.Request, oddsServiceURL string, authService *auth.Service, corsOrigins []string, log *slog.Logger) {
	// Validate auth token before accepting the WebSocket
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, `{"error":"missing token query parameter"}`, http.StatusUnauthorized)
		return
	}

	// Check token against blacklist before accepting the WebSocket
	if authService.IsBlacklisted(r.Context(), token) {
		http.Error(w, `{"error":"token has been revoked"}`, http.StatusUnauthorized)
		return
	}

	claims, err := authService.ValidateToken(token)
	if err != nil {
		http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
		return
	}

	// Enforce WebSocket connection limit
	if atomic.AddInt64(&activeWSConns, 1) > maxWSConnections {
		atomic.AddInt64(&activeWSConns, -1)
		http.Error(w, `{"error":"too many websocket connections"}`, http.StatusServiceUnavailable)
		return
	}
	defer atomic.AddInt64(&activeWSConns, -1)

	// Accept WebSocket from client
	clientConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: corsOrigins,
	})
	if err != nil {
		log.Error("websocket accept error", "error", err)
		return
	}
	defer clientConn.CloseNow()

	// Build the backend WebSocket URL
	backendURL := strings.Replace(oddsServiceURL, "http://", "ws://", 1)
	backendURL = strings.Replace(backendURL, "https://", "wss://", 1)
	backendURL = backendURL + "/ws?token=" + url.QueryEscape(token)

	// Connect to backend odds service
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	backendHeaders := http.Header{}
	backendHeaders.Set("X-User-ID", fmt.Sprintf("%d", claims.UserID))
	backendHeaders.Set("X-Username", claims.Username)
	backendHeaders.Set("X-Role", string(claims.Role))

	backendConn, _, err := websocket.Dial(ctx, backendURL, &websocket.DialOptions{
		HTTPHeader: backendHeaders,
	})
	if err != nil {
		log.Error("failed to connect to backend websocket", "error", err, "url", backendURL)
		clientConn.Close(websocket.StatusInternalError, "backend unavailable")
		return
	}
	defer backendConn.CloseNow()

	// Start ping/pong keepalive for the client connection
	go wsPingLoop(ctx, clientConn, 30*time.Second)

	// Bidirectional proxy with proper cancellation
	errc := make(chan error, 2)

	// Client → Backend
	go func() {
		errc <- copyWS(ctx, backendConn, clientConn)
	}()

	// Backend → Client
	go func() {
		errc <- copyWS(ctx, clientConn, backendConn)
	}()

	// Wait for either direction to finish, then cancel the other
	err = <-errc
	cancel()

	// Graceful close
	closeMsg := "connection closed"
	if err != nil {
		closeMsg = err.Error()
	}
	clientConn.Close(websocket.StatusNormalClosure, closeMsg)
	backendConn.Close(websocket.StatusNormalClosure, closeMsg)
}

// wsPingLoop sends periodic pings to detect dead WebSocket connections.
func wsPingLoop(ctx context.Context, conn *websocket.Conn, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := conn.Ping(ctx); err != nil {
				return
			}
		}
	}
}

// copyWS copies WebSocket messages from src to dst until an error or context cancellation.
func copyWS(ctx context.Context, dst, src *websocket.Conn) error {
	for {
		msgType, data, err := src.Read(ctx)
		if err != nil {
			return err
		}
		err = dst.Write(ctx, msgType, data)
		if err != nil {
			return err
		}
	}
}

// wsConnectionCount returns the current active WebSocket connection count.
// Exported for testing / metrics.
func wsConnectionCount() int64 {
	return atomic.LoadInt64(&activeWSConns)
}
