package middleware

import (
	"context"
	"hash/fnv"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/auth"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"golang.org/x/time/rate"
)

type contextKey string

// Per-field context keys. Kept for backwards compatibility with callers
// that inject claims directly without going through AuthMiddleware.
// Newer code paths use a single userCtx struct.
const (
	UserIDKey   contextKey = "user_id"
	UsernameKey contextKey = "username"
	RoleKey     contextKey = "role"
	PathKey     contextKey = "path"
)

// userCtx bundles per-request identity into a single struct so we pay for
// exactly one context.WithValue call per authenticated request instead of
// four. Each WithValue allocates a new context node, so collapsing the four
// Stores into one is a meaningful saving on the hot path.
type userCtx struct {
	UserID   int64
	Username string
	Role     models.Role
	Path     string
}

type userCtxKeyType struct{}

var userCtxKey = userCtxKeyType{}

// responseWriter wraps http.ResponseWriter to capture the status code for logging.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

// respWriterPool recycles responseWriter wrappers across requests so we avoid
// a heap allocation in RequestLogger on every call.
var respWriterPool = sync.Pool{
	New: func() interface{} { return new(responseWriter) },
}

func acquireResponseWriter(w http.ResponseWriter) *responseWriter {
	rw := respWriterPool.Get().(*responseWriter)
	rw.ResponseWriter = w
	rw.statusCode = http.StatusOK
	rw.written = false
	return rw
}

func releaseResponseWriter(rw *responseWriter) {
	rw.ResponseWriter = nil
	respWriterPool.Put(rw)
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
// Token validation cache
// ---------------------------------------------------------------------------

// tokenCache memoizes JWT validation. JWT parsing is ~20-30µs per call; caching
// knocks that to a constant-time map lookup for the common case of a client
// that reuses the same token for many requests within its TTL.
//
// The cache is capped to avoid unbounded growth from token rotation.
type tokenCache struct {
	mu    sync.RWMutex
	items map[string]*auth.Claims
}

const tokenCacheCap = 10000

var jwtCache = &tokenCache{items: make(map[string]*auth.Claims, 1024)}

func (c *tokenCache) get(token string) (*auth.Claims, bool) {
	c.mu.RLock()
	claims, ok := c.items[token]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	// Honor JWT expiry — a cached entry past its TTL is useless.
	if claims.ExpiresAt != nil && time.Now().After(claims.ExpiresAt.Time) {
		c.mu.Lock()
		delete(c.items, token)
		c.mu.Unlock()
		return nil, false
	}
	return claims, true
}

func (c *tokenCache) put(token string, claims *auth.Claims) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= tokenCacheCap {
		// Simple bounded eviction: drop an arbitrary existing entry. This
		// keeps the cache from growing unbounded without the complexity of
		// full LRU bookkeeping on the hot path.
		for k := range c.items {
			delete(c.items, k)
			break
		}
	}
	c.items[token] = claims
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

			claims, ok := jwtCache.get(token)
			if !ok {
				var err error
				claims, err = authService.ValidateToken(token)
				if err != nil {
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
					return
				}
				jwtCache.put(token, claims)
			}

			// Collapse four WithValue calls into one by stashing a single
			// struct value. UserIDFromContext et al. read from this fast
			// path first and fall back to the legacy per-field keys for
			// callers that still set them individually.
			uc := &userCtx{
				UserID:   claims.UserID,
				Username: claims.Username,
				Role:     claims.Role,
				Path:     claims.Path,
			}
			ctx := context.WithValue(r.Context(), userCtxKey, uc)

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
			role := RoleFromContext(r.Context())
			if role == "" || !roleSet[role] {
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

// numRateLimiterShards controls how many independent mutex-guarded buckets
// the IP rate limiter splits its map across. At 32 shards, the probability
// of two random keys colliding on the same mutex is ~3%, which effectively
// eliminates the single-mutex contention we saw under load.
const numRateLimiterShards = 32

type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type rateLimiterShard struct {
	mu       sync.Mutex
	limiters map[string]*rateLimiterEntry
}

// IPRateLimiter implements per-key rate limiting (keyed by IP or user ID).
// The map of per-key rate.Limiter instances is sharded across multiple
// mutex-guarded buckets so that concurrent GetLimiter calls for unrelated
// keys rarely contend on the same lock.
type IPRateLimiter struct {
	shards [numRateLimiterShards]rateLimiterShard
	rps    rate.Limit
	burst  int
}

// NewIPRateLimiter creates a rate limiter with a background cleanup goroutine
// that is tied to the provided context. When the context is cancelled the
// cleanup goroutine exits, preventing goroutine leaks.
func NewIPRateLimiter(ctx context.Context, rps int, burst int) *IPRateLimiter {
	rl := &IPRateLimiter{
		rps:   rate.Limit(rps),
		burst: burst,
	}
	for i := 0; i < numRateLimiterShards; i++ {
		rl.shards[i].limiters = make(map[string]*rateLimiterEntry)
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

// shardFor picks the shard that owns the given key via an FNV-1a hash.
// FNV is fast, allocation-free, and gives us good distribution for
// IPv4/IPv6 strings and "user:<id>" keys.
func (rl *IPRateLimiter) shardFor(key string) *rateLimiterShard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &rl.shards[h.Sum32()%numRateLimiterShards]
}

func (rl *IPRateLimiter) getLimiter(key string) *rate.Limiter {
	shard := rl.shardFor(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	entry, exists := shard.limiters[key]
	if !exists {
		entry = &rateLimiterEntry{
			limiter:  rate.NewLimiter(rl.rps, rl.burst),
			lastSeen: time.Now(),
		}
		shard.limiters[key] = entry
		return entry.limiter
	}
	entry.lastSeen = time.Now()
	return entry.limiter
}

func (rl *IPRateLimiter) cleanup() {
	cutoff := time.Now().Add(-5 * time.Minute)
	// Iterate shards one at a time so we never hold more than one shard
	// lock simultaneously — ruling out any lock-ordering deadlocks.
	for i := 0; i < numRateLimiterShards; i++ {
		shard := &rl.shards[i]
		shard.mu.Lock()
		for k, entry := range shard.limiters {
			if entry.lastSeen.Before(cutoff) {
				delete(shard.limiters, k)
			}
		}
		shard.mu.Unlock()
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
			// strconv avoids the fmt.Sprintf reflection path — a noticeable
			// win on a per-request hot path.
			key := "user:" + strconv.FormatInt(userID, 10)
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
			// Skip observability and liveness endpoints entirely — they
			// fire every few seconds and would otherwise dominate the log
			// volume and the RequestLogger's per-request cost.
			if r.URL.Path == "/metrics" || r.URL.Path == "/health" || r.URL.Path == "/healthz" {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			rw := acquireResponseWriter(w)
			defer releaseResponseWriter(rw)

			next.ServeHTTP(rw, r)

			// Typed slog attributes avoid boxing ints/strings through the
			// variadic interface{} path, which allocates per call.
			logger.LogAttrs(r.Context(), slog.LevelInfo, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rw.statusCode),
				slog.Duration("duration", time.Since(start)),
				slog.String("request_id", RequestIDFromContext(r.Context())),
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
					logger.LogAttrs(r.Context(), slog.LevelError, "panic recovered",
						slog.Any("error", rec),
						slog.String("stack", stack),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
						slog.String("request_id", RequestIDFromContext(r.Context())),
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

// userCtxFromContext pulls the bundled userCtx out of the request context if
// AuthMiddleware stashed one. Returns nil when no struct was set so callers
// can fall back to the legacy per-field keys.
func userCtxFromContext(ctx context.Context) *userCtx {
	if v, ok := ctx.Value(userCtxKey).(*userCtx); ok {
		return v
	}
	return nil
}

func UserIDFromContext(ctx context.Context) int64 {
	if uc := userCtxFromContext(ctx); uc != nil {
		return uc.UserID
	}
	if v, ok := ctx.Value(UserIDKey).(int64); ok {
		return v
	}
	return 0
}

func UsernameFromContext(ctx context.Context) string {
	if uc := userCtxFromContext(ctx); uc != nil {
		return uc.Username
	}
	if v, ok := ctx.Value(UsernameKey).(string); ok {
		return v
	}
	return ""
}

func RoleFromContext(ctx context.Context) models.Role {
	if uc := userCtxFromContext(ctx); uc != nil {
		return uc.Role
	}
	if v, ok := ctx.Value(RoleKey).(models.Role); ok {
		return v
	}
	return ""
}

// PathFromContext returns the hierarchical path stored in the context (set by
// AuthMiddleware from JWT claims). Returns empty string if not present.
func PathFromContext(ctx context.Context) string {
	if uc := userCtxFromContext(ctx); uc != nil {
		return uc.Path
	}
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
