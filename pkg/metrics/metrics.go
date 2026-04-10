package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// HTTP metrics. The service label identifies which Lotus Exchange service
	// produced the metric; the path label should be a low-cardinality route
	// pattern (e.g. "/api/v1/events/{id}") rather than the raw request path.
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lotus_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"service", "method", "path", "status"},
	)

	HTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "lotus_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"service", "method", "path"},
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

// Handler returns the Prometheus metrics HTTP handler. Mount it at "/metrics"
// so Prometheus can scrape the registered metrics.
func Handler() http.Handler {
	return promhttp.Handler()
}
