package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// HTTP metrics
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lotus_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	HTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "lotus_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"method", "path"},
	)

	// Business metrics
	BetsPlacedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lotus_bets_placed_total",
			Help: "Total number of bets placed",
		},
		[]string{"side", "sport", "status"},
	)

	BetStakeTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lotus_bet_stake_total",
			Help: "Total stake amount of bets placed",
		},
		[]string{"side", "sport"},
	)

	MatchingEngineDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "lotus_matching_engine_duration_seconds",
			Help:    "Time taken to match an order",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1},
		},
	)

	SettlementDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "lotus_settlement_duration_seconds",
			Help:    "Time taken to settle a market",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 10},
		},
	)

	ActiveWebSocketConnections = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "lotus_active_websocket_connections",
			Help: "Number of active WebSocket connections",
		},
	)

	WalletOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lotus_wallet_operations_total",
			Help: "Total wallet operations",
		},
		[]string{"type"}, // hold, release, deposit, withdrawal, settlement
	)

	FraudAlertsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lotus_fraud_alerts_total",
			Help: "Total fraud alerts generated",
		},
		[]string{"risk_level"},
	)

	ActiveUsersGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "lotus_active_users",
			Help: "Number of currently active users",
		},
	)

	DBConnectionPoolStats = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "lotus_db_pool_connections",
			Help: "Database connection pool statistics",
		},
		[]string{"state"}, // open, in_use, idle
	)

	RedisOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lotus_redis_operations_total",
			Help: "Total Redis operations",
		},
		[]string{"operation", "status"},
	)

	CashoutOffersTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "lotus_cashout_offers_total",
			Help: "Total cashout offers generated",
		},
	)

	DepositAmountTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lotus_deposit_amount_total",
			Help: "Total deposit amounts by method",
		},
		[]string{"method", "currency"},
	)

	WithdrawalAmountTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lotus_withdrawal_amount_total",
			Help: "Total withdrawal amounts by method",
		},
		[]string{"method", "currency"},
	)
)

func init() {
	prometheus.MustRegister(
		HTTPRequestsTotal,
		HTTPRequestDuration,
		BetsPlacedTotal,
		BetStakeTotal,
		MatchingEngineDuration,
		SettlementDuration,
		ActiveWebSocketConnections,
		WalletOperationsTotal,
		FraudAlertsTotal,
		ActiveUsersGauge,
		DBConnectionPoolStats,
		RedisOperationsTotal,
		CashoutOffersTotal,
		DepositAmountTotal,
		WithdrawalAmountTotal,
	)
}

// MetricsMiddleware records HTTP request metrics
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Skip metrics endpoint itself
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		duration := time.Since(start).Seconds()

		// Normalize path to avoid cardinality explosion
		path := normalizePath(r.URL.Path)

		HTTPRequestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(wrapped.status)).Inc()
		HTTPRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Handler returns the Prometheus metrics HTTP handler
func Handler() http.Handler {
	return promhttp.Handler()
}

// normalizePath replaces dynamic segments with placeholders to prevent metric cardinality explosion
func normalizePath(path string) string {
	parts := splitPath(path)
	for i, part := range parts {
		// Replace UUIDs and numeric IDs with placeholder
		if isID(part) {
			parts[i] = ":id"
		}
	}
	result := "/" + joinPath(parts)
	return result
}

func splitPath(path string) []string {
	var parts []string
	for _, p := range split(path, '/') {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func split(s string, sep byte) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			if i > start {
				parts = append(parts, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}
	return parts
}

func joinPath(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "/"
		}
		result += p
	}
	return result
}

func isID(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Check if it's a numeric ID
	allDigits := true
	for _, c := range s {
		if c < '0' || c > '9' {
			allDigits = false
			break
		}
	}
	if allDigits && len(s) > 0 {
		return true
	}
	// Check if it's a UUID (contains hyphens and hex chars, length 36)
	if len(s) == 36 && s[8] == '-' && s[13] == '-' {
		return true
	}
	// Check if it starts with tx_ or alert- (transaction IDs)
	if len(s) > 3 && (s[:3] == "tx_" || s[:6] == "alert-") {
		return true
	}
	return false
}
