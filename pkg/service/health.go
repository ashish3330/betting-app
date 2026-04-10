package service

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// HealthCheck is a single dependency probe: it should return nil if the
// dependency is healthy and a descriptive error otherwise. The supplied
// context carries the overall health-check deadline.
type HealthCheck func(ctx context.Context) error

// healthResponse is the JSON body returned by HealthHandler.
type healthResponse struct {
	Service string            `json:"service"`
	Status  string            `json:"status"`
	Checks  map[string]string `json:"checks"`
}

// HealthHandler returns an HTTP handler for GET /health. It runs every check
// in parallel with a 3 second deadline and reports:
//
//   - 200 OK with status "ok" when all checks pass.
//   - 503 Service Unavailable with status "degraded" if any check fails.
//
// The response body lists per-dependency state as "ok" or "error: <message>".
// Callers mount this under whatever route they prefer (typically
// "GET /health").
func HealthHandler(serviceName string, checks map[string]HealthCheck) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		results := make(map[string]string, len(checks))
		var mu sync.Mutex
		var wg sync.WaitGroup

		for name, check := range checks {
			wg.Add(1)
			go func(name string, check HealthCheck) {
				defer wg.Done()
				status := "ok"
				if err := check(ctx); err != nil {
					status = "error: " + err.Error()
				}
				mu.Lock()
				results[name] = status
				mu.Unlock()
			}(name, check)
		}
		wg.Wait()

		overall := "ok"
		code := http.StatusOK
		for _, v := range results {
			if v != "ok" {
				overall = "degraded"
				code = http.StatusServiceUnavailable
				break
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(healthResponse{
			Service: serviceName,
			Status:  overall,
			Checks:  results,
		})
	}
}
