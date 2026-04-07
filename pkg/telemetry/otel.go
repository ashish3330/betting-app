package telemetry

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// RequestLogger is HTTP middleware that logs request duration and status.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)
			logger.InfoContext(r.Context(), "request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.status,
				"duration_ms", duration.Milliseconds(),
				"remote", r.RemoteAddr,
			)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// HealthChecker aggregates health checks for all dependencies.
type HealthChecker struct {
	checks map[string]func(ctx context.Context) error
}

func NewHealthChecker() *HealthChecker {
	return &HealthChecker{checks: make(map[string]func(ctx context.Context) error)}
}

func (h *HealthChecker) Register(name string, check func(ctx context.Context) error) {
	h.checks[name] = check
}

func (h *HealthChecker) Check(ctx context.Context) map[string]string {
	results := make(map[string]string, len(h.checks))
	for name, check := range h.checks {
		if err := check(ctx); err != nil {
			results[name] = "unhealthy: " + err.Error()
		} else {
			results[name] = "healthy"
		}
	}
	return results
}

func (h *HealthChecker) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "alive"})
	}
}

func (h *HealthChecker) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		results := h.Check(ctx)
		allHealthy := true
		for _, v := range results {
			if v != "healthy" {
				allHealthy = false
				break
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if allHealthy {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     map[bool]string{true: "ready", false: "not_ready"}[allHealthy],
			"components": results,
		})
	}
}
