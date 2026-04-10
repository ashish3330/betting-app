package service

import (
	"log/slog"
	"net/http"

	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
	"github.com/lotus-exchange/lotus-exchange/pkg/metrics"
)

// DefaultMiddleware returns the standard chain that every Lotus Exchange
// HTTP service wraps its mux with: panic recovery, request ID propagation,
// Prometheus request metrics, request logging, and security headers.
//
// The serviceName label is used for the Prometheus metrics so dashboards can
// slice traffic per service. Service-specific middleware (rate limiting,
// encryption, CORS, body size) should be composed on top of this base chain
// in the individual main.go.
func DefaultMiddleware(serviceName string, log *slog.Logger) func(http.Handler) http.Handler {
	return middleware.ChainMiddleware(
		middleware.RecoverPanic(log),
		middleware.RequestID,
		middleware.MetricsMiddleware(serviceName),
		middleware.RequestLogger(log),
		middleware.SecurityHeaders,
	)
}

// MetricsHandler returns the Prometheus scrape endpoint. Services should mount
// it at "GET /metrics" on their mux so Prometheus can collect the metrics
// recorded by MetricsMiddleware and any business counters registered in
// pkg/metrics.
func MetricsHandler() http.Handler {
	return metrics.Handler()
}
