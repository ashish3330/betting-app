package main

// Comprehensive security hardening for the Lotus Exchange backend.
// Covers: input sanitization, request signing, IP blocking, session
// fingerprinting, anti-replay, data backup, and security headers.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ── Request Signing (anti-tampering) ───────────────────────────────────────

var requestSigningSecret string

func initSecurity() {
	requestSigningSecret = os.Getenv("SIGNING_SECRET")
	if requestSigningSecret == "" {
		fmt.Fprintln(os.Stderr, "FATAL: SIGNING_SECRET environment variable is required")
		os.Exit(1)
	}
}

// SignPayload generates HMAC-SHA256 signature for a payload.
func SignPayload(payload []byte) string {
	mac := hmac.New(sha256.New, []byte(requestSigningSecret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature checks if the X-Signature header matches the body.
func VerifySignature(payload []byte, signature string) bool {
	expected := SignPayload(payload)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// ── IP Blocking ────────────────────────────────────────────────────────────

type IPBlocker struct {
	mu       sync.RWMutex
	blocked  map[string]time.Time // IP -> blocked until
	strikes  map[string]int       // IP -> failed attempt count
	maxStrikes int
	blockDuration time.Duration
}

func NewIPBlocker(maxStrikes int, blockDuration time.Duration, stop <-chan struct{}) *IPBlocker {
	b := &IPBlocker{
		blocked:       make(map[string]time.Time),
		strikes:       make(map[string]int),
		maxStrikes:    maxStrikes,
		blockDuration: blockDuration,
	}
	// Cleanup expired blocks every 5 minutes
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				b.cleanup()
			case <-stop:
				return
			}
		}
	}()
	return b
}

func (b *IPBlocker) IsBlocked(ip string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	until, ok := b.blocked[ip]
	if !ok {
		return false
	}
	return time.Now().Before(until)
}

func (b *IPBlocker) RecordFailure(ip string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.strikes[ip]++
	if b.strikes[ip] >= b.maxStrikes {
		b.blocked[ip] = time.Now().Add(b.blockDuration)
		b.strikes[ip] = 0
	}
}

func (b *IPBlocker) RecordSuccess(ip string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.strikes, ip)
}

func (b *IPBlocker) cleanup() {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for ip, until := range b.blocked {
		if now.After(until) {
			delete(b.blocked, ip)
		}
	}
}

// ── Anti-Replay Protection ─────────────────────────────────────────────────

type ReplayGuard struct {
	mu    sync.Mutex
	seen  map[string]time.Time // nonce -> timestamp
	window time.Duration
}

func NewReplayGuard(window time.Duration, stop <-chan struct{}) *ReplayGuard {
	g := &ReplayGuard{
		seen:   make(map[string]time.Time),
		window: window,
	}
	go func() {
		ticker := time.NewTicker(window)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				g.cleanup()
			case <-stop:
				return
			}
		}
	}()
	return g
}

func (g *ReplayGuard) Check(nonce string) bool {
	if nonce == "" {
		slog.Warn("replay guard: request received without nonce, skipping check (backward compat)")
		return true // No nonce = skip check (backwards compatible)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, exists := g.seen[nonce]; exists {
		return false // Replay detected
	}
	g.seen[nonce] = time.Now()
	return true
}

func (g *ReplayGuard) cleanup() {
	g.mu.Lock()
	defer g.mu.Unlock()
	cutoff := time.Now().Add(-g.window)
	for nonce, ts := range g.seen {
		if ts.Before(cutoff) {
			delete(g.seen, nonce)
		}
	}
}

// ── Input Sanitization ─────────────────────────────────────────────────────

// SanitizeString removes potential injection characters.
func SanitizeString(s string) string {
	// Remove null bytes
	s = strings.ReplaceAll(s, "\x00", "")
	// Limit length
	if len(s) > 500 {
		s = s[:500]
	}
	return s
}

// ValidateUsername checks for valid username characters.
func ValidateUsername(username string) error {
	if len(username) < 3 || len(username) > 30 {
		return fmt.Errorf("username must be 3-30 characters")
	}
	for _, c := range username {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '.') {
			return fmt.Errorf("username can only contain lowercase letters, numbers, underscore, dot")
		}
	}
	return nil
}

// ValidatePassword checks password strength.
func ValidatePassword(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if len(password) > 128 {
		return fmt.Errorf("password too long")
	}
	var hasUpper, hasLower, hasDigit bool
	for _, c := range password {
		if c >= 'A' && c <= 'Z' {
			hasUpper = true
		}
		if c >= 'a' && c <= 'z' {
			hasLower = true
		}
		if c >= '0' && c <= '9' {
			hasDigit = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit {
		return fmt.Errorf("password must contain uppercase, lowercase, and a digit")
	}
	return nil
}

// ── Security Headers Middleware ─────────────────────────────────────────────

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "DENY")
		// XSS protection
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		// Strict transport (HTTPS)
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		// Referrer policy
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// Content Security Policy
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; connect-src 'self' ws: wss:; font-src 'self' https://fonts.gstatic.com; frame-ancestors 'none'; base-uri 'self'")
		// Permissions policy
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// Cache control for API responses
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("Pragma", "no-cache")

		next.ServeHTTP(w, r)
	})
}

// ── IP Extraction ──────────────────────────────────────────────────────────

// trustedProxies is the set of proxy IPs allowed to set X-Forwarded-For.
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

func extractClientIP(r *http.Request) string {
	// Determine the direct-connection IP first.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	// Only trust proxy headers when the direct connection is from a trusted proxy.
	if trustedProxies[host] {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.SplitN(xff, ",", 2)
			ip := strings.TrimSpace(parts[0])
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			if net.ParseIP(xri) != nil {
				return xri
			}
		}
	}

	return host
}

// ── IP Block Middleware ─────────────────────────────────────────────────────

func ipBlockMiddleware(blocker *IPBlocker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractClientIP(r)
			if blocker.IsBlocked(ip) {
				http.Error(w, `{"error":"IP temporarily blocked due to suspicious activity"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ── Request Size Limiter ───────────────────────────────────────────────────

func maxBodySizeMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxBytes {
				http.Error(w, `{"error":"request body too large"}`, http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// ── Per-IP Rate Limiter ────────────────────────────────────────────────────

type rateLimiterEntry struct {
	tokens    float64
	lastCheck time.Time
}

type PerIPRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rateLimiterEntry
	rate     float64 // tokens per second
	burst    float64 // max tokens
}

func NewPerIPRateLimiter(rps, burst float64, stop <-chan struct{}) *PerIPRateLimiter {
	rl := &PerIPRateLimiter{
		limiters: make(map[string]*rateLimiterEntry),
		rate:     rps,
		burst:    burst,
	}
	// Cleanup every 3 minutes
	go func() {
		ticker := time.NewTicker(3 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rl.mu.Lock()
				cutoff := time.Now().Add(-5 * time.Minute)
				for ip, e := range rl.limiters {
					if e.lastCheck.Before(cutoff) {
						delete(rl.limiters, ip)
					}
				}
				rl.mu.Unlock()
			case <-stop:
				return
			}
		}
	}()
	return rl
}

func (rl *PerIPRateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	e, exists := rl.limiters[ip]
	if !exists {
		rl.limiters[ip] = &rateLimiterEntry{tokens: rl.burst - 1, lastCheck: now}
		return true
	}

	// Refill tokens
	elapsed := now.Sub(e.lastCheck).Seconds()
	e.tokens += elapsed * rl.rate
	if e.tokens > rl.burst {
		e.tokens = rl.burst
	}
	e.lastCheck = now

	if e.tokens < 1 {
		return false
	}
	e.tokens--
	return true
}

func rateLimitMiddleware(rl *PerIPRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractClientIP(r)
			if !rl.Allow(ip) {
				w.Header().Set("Retry-After", "1")
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ── Data Backup (periodic snapshot to JSON file) ───────────────────────────

func (s *Store) BackupToFile(path string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"users":     len(s.users),
		"bets":      len(s.bets),
		"markets":   len(s.markets),
		"ledger":    len(s.ledger),
		"audit":     len(s.auditLog),
	}

	// In production this would serialize the full store to disk/S3
	// For mock server, we just log the counts
	logger.Info("backup snapshot",
		"users", data["users"],
		"bets", data["bets"],
		"markets", data["markets"],
		"ledger", data["ledger"],
		"audit", data["audit"],
	)
	return nil
}

// StartBackupSchedule runs periodic backups every interval.
func (s *Store) StartBackupSchedule(interval time.Duration, path string) {
	go func() {
		for {
			time.Sleep(interval)
			if err := s.BackupToFile(path); err != nil {
				logger.Error("backup failed", "error", err)
			}
		}
	}()
}

// ── Session Fingerprinting ─────────────────────────────────────────────────

// GenerateFingerprint creates a device fingerprint from request headers.
func GenerateFingerprint(r *http.Request) string {
	raw := r.Header.Get("User-Agent") + "|" +
		r.Header.Get("Accept-Language") + "|" +
		r.Header.Get("Accept-Encoding")
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:8]) // Short fingerprint
}
