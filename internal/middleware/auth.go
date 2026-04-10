package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/auth"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"golang.org/x/time/rate"
)

type contextKey string

const (
	UserIDKey   contextKey = "user_id"
	UsernameKey contextKey = "username"
	RoleKey     contextKey = "role"
	PathKey     contextKey = "path"
)

// responseWriter wraps http.ResponseWriter to capture the status code for logging.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

// Unwrap supports http.ResponseController and middleware that check for
// wrapped writers (e.g. http.Flusher).
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// ---------------------------------------------------------------------------
// Auth middleware
// ---------------------------------------------------------------------------

func AuthMiddleware(authService *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

			ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
			ctx = context.WithValue(ctx, UsernameKey, claims.Username)
			ctx = context.WithValue(ctx, RoleKey, claims.Role)
			ctx = context.WithValue(ctx, PathKey, claims.Path)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireRole(roles ...models.Role) func(http.Handler) http.Handler {
	roleSet := make(map[models.Role]bool, len(roles))
	for _, r := range roles {
		roleSet[r] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, ok := r.Context().Value(RoleKey).(models.Role)
			if !ok || !roleSet[role] {
				http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ---------------------------------------------------------------------------
// Rate limiting
// ---------------------------------------------------------------------------

// RateLimiter applies per-IP rate limiting by default. It delegates to
// PerIPRateLimiter so that each source IP gets its own token bucket.
func RateLimiter(rps int, burst int) func(http.Handler) http.Handler {
	return PerIPRateLimiter(rps, burst)
}

// IPRateLimiter implements per-key rate limiting (keyed by IP or user ID).
type IPRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rateLimiterEntry
	rps      rate.Limit
	burst    int
}

type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewIPRateLimiter creates a rate limiter with a background cleanup goroutine
// that is tied to the provided context. When the context is cancelled the
// cleanup goroutine exits, preventing goroutine leaks.
func NewIPRateLimiter(ctx context.Context, rps int, burst int) *IPRateLimiter {
	rl := &IPRateLimiter{
		limiters: make(map[string]*rateLimiterEntry),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
	// Cleanup stale entries every 3 minutes; stops when ctx is cancelled.
	go func() {
		ticker := time.NewTicker(3 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rl.cleanup()
			}
		}
	}()
	return rl
}

func (rl *IPRateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	entry, exists := rl.limiters[ip]
	if !exists {
		entry = &rateLimiterEntry{
			limiter:  rate.NewLimiter(rl.rps, rl.burst),
			lastSeen: time.Now(),
		}
		rl.limiters[ip] = entry
		return entry.limiter
	}
	entry.lastSeen = time.Now()
	return entry.limiter
}

func (rl *IPRateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-5 * time.Minute)
	for ip, entry := range rl.limiters {
		if entry.lastSeen.Before(cutoff) {
			delete(rl.limiters, ip)
		}
	}
}

// PerIPRateLimiter creates a per-IP rate limiter whose cleanup goroutine
// runs indefinitely. Use PerIPRateLimiterWithContext for controllable lifetime.
func PerIPRateLimiter(rps int, burst int) func(http.Handler) http.Handler {
	rl := NewIPRateLimiter(context.Background(), rps, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)
			if !rl.getLimiter(ip).Allow() {
				w.Header().Set("Retry-After", "1")
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// PerIPRateLimiterWithContext is like PerIPRateLimiter but accepts a context
// to control the lifetime of the background cleanup goroutine.
func PerIPRateLimiterWithContext(ctx context.Context, rps int, burst int) func(http.Handler) http.Handler {
	rl := NewIPRateLimiter(ctx, rps, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)
			if !rl.getLimiter(ip).Allow() {
				w.Header().Set("Retry-After", "1")
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func PerUserRateLimiter(rps int, burst int) func(http.Handler) http.Handler {
	rl := NewIPRateLimiter(context.Background(), rps, burst) // reuse same structure, keyed by user ID
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := UserIDFromContext(r.Context())
			key := fmt.Sprintf("user:%d", userID)
			if !rl.getLimiter(key).Allow() {
				w.Header().Set("Retry-After", "1")
				http.Error(w, `{"error":"rate limit exceeded, slow down"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ---------------------------------------------------------------------------
// Request logging
// ---------------------------------------------------------------------------

// RequestLogger logs every request with method, path, status code, duration,
// and request_id (if present in context via the RequestID middleware).
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := newResponseWriter(w)

			next.ServeHTTP(rw, r)

			logger.InfoContext(r.Context(), "http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.statusCode,
				"duration", time.Since(start).String(),
				"request_id", RequestIDFromContext(r.Context()),
			)
		})
	}
}

// ---------------------------------------------------------------------------
// Panic recovery
// ---------------------------------------------------------------------------

// RecoverPanic catches panics in downstream handlers, logs the stack trace,
// and returns a 500 Internal Server Error to the client.
func RecoverPanic(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					stack := string(debug.Stack())
					logger.ErrorContext(r.Context(), "panic recovered",
						"error", fmt.Sprintf("%v", rec),
						"stack", stack,
						"method", r.Method,
						"path", r.URL.Path,
						"request_id", RequestIDFromContext(r.Context()),
					)
					http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// ---------------------------------------------------------------------------
// Chain helper
// ---------------------------------------------------------------------------

// ChainMiddleware composes multiple middleware functions into a single
// middleware. Middlewares are applied in the order provided: the first
// middleware in the list wraps the outermost layer.
func ChainMiddleware(middlewares ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(final http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}

// ---------------------------------------------------------------------------
// Context helpers
// ---------------------------------------------------------------------------

func extractBearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return h[7:]
	}
	return ""
}

func UserIDFromContext(ctx context.Context) int64 {
	if v, ok := ctx.Value(UserIDKey).(int64); ok {
		return v
	}
	return 0
}

func RoleFromContext(ctx context.Context) models.Role {
	if v, ok := ctx.Value(RoleKey).(models.Role); ok {
		return v
	}
	return ""
}

// PathFromContext returns the hierarchical path stored in the context (set by
// AuthMiddleware from JWT claims). Returns empty string if not present.
func PathFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(PathKey).(string); ok {
		return v
	}
	return ""
}

// ---------------------------------------------------------------------------
// IP extraction
// ---------------------------------------------------------------------------

// trustedProxies is the set of proxy IPs allowed to set X-Forwarded-For.
// Populated from TRUSTED_PROXIES env var (comma-separated), defaults to
// loopback addresses.
var trustedProxies = func() map[string]bool {
	raw := os.Getenv("TRUSTED_PROXIES")
	if raw == "" {
		raw = "127.0.0.1,::1"
	}
	m := make(map[string]bool)
	for _, p := range strings.Split(raw, ",") {
		m[strings.TrimSpace(p)] = true
	}
	return m
}()

func extractIP(r *http.Request) string {
	// Determine the direct-connection IP first.
	remoteIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remoteIP); err == nil {
		remoteIP = host
	}

	// Only trust proxy headers when the direct connection is from a trusted proxy.
	if trustedProxies[remoteIP] {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.SplitN(xff, ",", 2)
			return strings.TrimSpace(parts[0])
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return xri
		}
	}

	return remoteIP
}
