package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/lotus-exchange/lotus-exchange/pkg/metrics"
)

// MetricsMiddleware records request count and latency by service, method,
// route pattern, and status. The route pattern (r.Pattern, populated by the
// Go 1.22+ ServeMux) is used instead of r.URL.Path to avoid a metric
// cardinality explosion for routes that embed ids or other variables.
//
// The /metrics endpoint itself is skipped so Prometheus scrapes do not
// pollute the metrics they publish.
func MetricsMiddleware(serviceName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			rec := &metricsRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			duration := time.Since(start).Seconds()

			// Use the route pattern set by net/http.ServeMux (Go 1.22+) to
			// keep label cardinality bounded. Fall back to "unknown" when
			// no route matched (e.g. 404s) so we still observe traffic.
			route := r.Pattern
			if route == "" {
				route = "unknown"
			}

			metrics.HTTPRequestsTotal.
				WithLabelValues(serviceName, r.Method, route, strconv.Itoa(rec.status)).
				Inc()
			metrics.HTTPRequestDuration.
				WithLabelValues(serviceName, r.Method, route).
				Observe(duration)
		})
	}
}

// metricsRecorder is a minimal http.ResponseWriter wrapper that captures the
// response status code so MetricsMiddleware can label the counter with it.
type metricsRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (m *metricsRecorder) WriteHeader(code int) {
	if m.wroteHeader {
		return
	}
	m.status = code
	m.wroteHeader = true
	m.ResponseWriter.WriteHeader(code)
}

func (m *metricsRecorder) Write(b []byte) (int, error) {
	if !m.wroteHeader {
		m.wroteHeader = true
	}
	return m.ResponseWriter.Write(b)
}
