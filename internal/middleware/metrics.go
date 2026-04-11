package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/lotus-exchange/lotus-exchange/pkg/metrics"
)

// metricsRecorderPool recycles metricsRecorder wrappers so MetricsMiddleware
// doesn't allocate a fresh struct for every request.
var metricsRecorderPool = sync.Pool{
	New: func() interface{} { return new(metricsRecorder) },
}

func acquireMetricsRecorder(w http.ResponseWriter) *metricsRecorder {
	rec := metricsRecorderPool.Get().(*metricsRecorder)
	rec.ResponseWriter = w
	rec.status = http.StatusOK
	rec.wroteHeader = false
	return rec
}

func releaseMetricsRecorder(rec *metricsRecorder) {
	rec.ResponseWriter = nil
	metricsRecorderPool.Put(rec)
}

// routeObserver caches the per-(service, method, route) Prometheus vector
// children so the hot path does no hash lookups at all — just a map lookup
// keyed on an interned (method, route) tuple.
type routeObserver struct {
	requests  prometheus.Counter
	duration  prometheus.Observer
	errors4xx prometheus.Counter
	errors5xx prometheus.Counter
}

type routeKey struct {
	method string
	route  string
}

// MetricsMiddleware records request count and latency by service, method,
// and route pattern. The route pattern (r.Pattern, populated by the Go 1.22+
// ServeMux) is used instead of r.URL.Path to avoid a metric cardinality
// explosion for routes that embed ids or other variables.
//
// The /metrics endpoint itself is skipped so Prometheus scrapes do not
// pollute the metrics they publish.
func MetricsMiddleware(serviceName string) func(http.Handler) http.Handler {
	var (
		mu    sync.RWMutex
		cache = make(map[routeKey]*routeObserver)
	)

	getObserver := func(method, route string) *routeObserver {
		key := routeKey{method: method, route: route}
		mu.RLock()
		obs, ok := cache[key]
		mu.RUnlock()
		if ok {
			return obs
		}
		mu.Lock()
		defer mu.Unlock()
		if obs, ok = cache[key]; ok {
			return obs
		}
		obs = &routeObserver{
			requests:  metrics.HTTPRequestsTotal.WithLabelValues(serviceName, method, route),
			duration:  metrics.HTTPRequestDuration.WithLabelValues(serviceName, method, route),
			errors4xx: metrics.HTTPErrorsTotal.WithLabelValues(serviceName, method, route, "4xx"),
			errors5xx: metrics.HTTPErrorsTotal.WithLabelValues(serviceName, method, route, "5xx"),
		}
		cache[key] = obs
		return obs
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			rec := acquireMetricsRecorder(w)
			defer releaseMetricsRecorder(rec)

			next.ServeHTTP(rec, r)
			duration := time.Since(start).Seconds()

			// Use the route pattern set by net/http.ServeMux (Go 1.22+) to
			// keep label cardinality bounded. Fall back to "unknown" when
			// no route matched (e.g. 404s) so we still observe traffic.
			route := r.Pattern
			if route == "" {
				route = "unknown"
			}

			obs := getObserver(r.Method, route)
			obs.requests.Inc()
			obs.duration.Observe(duration)
			if rec.status >= 500 {
				obs.errors5xx.Inc()
			} else if rec.status >= 400 {
				obs.errors4xx.Inc()
			}
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

// Unwrap exposes the underlying writer for http.ResponseController users.
func (m *metricsRecorder) Unwrap() http.ResponseWriter {
	return m.ResponseWriter
}
