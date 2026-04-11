package main

// Per-user token bucket rate limiter. The existing rate limiter in
// internal/middleware/security.go is per-IP, which lets a NAT'd group
// of users share a bucket and lets a single attacker who rotates IPs
// bypass it. Per-user limits sit in front of the most-abusable
// endpoints (login attempt, bet placement, withdrawal request) so a
// compromised account can't burn through the bucket.

import (
	"net/http"
	"sync"
	"time"
)

type userBucket struct {
	tokens     float64
	lastRefill time.Time
}

type userRateLimiter struct {
	mu       sync.Mutex
	buckets  map[int64]*userBucket
	rate     float64 // tokens per second
	capacity float64
}

func newUserRateLimiter(perSecond, burst float64) *userRateLimiter {
	return &userRateLimiter{
		buckets:  make(map[int64]*userBucket),
		rate:     perSecond,
		capacity: burst,
	}
}

// Allow returns true if the user has a token to spend, false otherwise.
// Refills the bucket at the configured rate up to capacity.
func (l *userRateLimiter) Allow(userID int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[userID]
	if !ok {
		b = &userBucket{tokens: l.capacity, lastRefill: now}
		l.buckets[userID] = b
	}

	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > l.capacity {
		b.tokens = l.capacity
	}
	b.lastRefill = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// cleanup removes buckets idle for more than 1 hour to keep the map
// from growing forever. Called from startCleanupLoop.
func (l *userRateLimiter) cleanup() {
	cutoff := time.Now().Add(-1 * time.Hour)
	l.mu.Lock()
	defer l.mu.Unlock()
	for uid, b := range l.buckets {
		if b.lastRefill.Before(cutoff) && b.tokens >= l.capacity {
			delete(l.buckets, uid)
		}
	}
}

// Pre-configured limiters for the most abusable endpoints. Numbers
// chosen to be generous enough that real users never hit them but
// tight enough to slow down a scripted attacker. Adjust via env vars
// in production.
var (
	betPlaceLimiter   = newUserRateLimiter(5, 10)  // 5 bets/sec, burst 10
	withdrawLimiter   = newUserRateLimiter(0.05, 3) // 1 every 20s, burst 3
	depositLimiter    = newUserRateLimiter(0.2, 5)  // 1 every 5s, burst 5
	passwordOpLimiter = newUserRateLimiter(0.1, 3)  // 1 every 10s, burst 3
)

// rateLimitUser wraps a handler in a per-user rate-limit check. Reads
// the user ID from the auth context (set by auth() middleware) and
// rejects with 429 if the user is over their bucket.
func rateLimitUser(limiter *userRateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := getUserID(r)
		if uid == 0 {
			// Not authenticated yet — let the auth middleware reject.
			next(w, r)
			return
		}
		if !limiter.Allow(uid) {
			w.Header().Set("Retry-After", "5")
			writeErr(w, 429, "too many requests — slow down")
			return
		}
		next(w, r)
	}
}
