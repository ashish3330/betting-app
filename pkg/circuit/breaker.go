// Package circuit provides thin wrappers around sony/gobreaker/v2 for guarding
// outbound calls to flaky external dependencies (third-party APIs, webhooks).
//
// Circuit breakers are intentionally NOT used for local infrastructure such as
// Postgres or Redis: those should fail fast so operators notice, and a tripped
// breaker on a local dependency usually masks real problems rather than solving
// them. Use this package for things like odds feed HTTP calls, payment gateway
// callbacks, or any outbound integration where sustained failure should shed
// load instead of retrying forever.
package circuit

import (
	"log/slog"
	"time"

	"github.com/sony/gobreaker/v2"
)

// Settings controls the thresholds for a breaker. Zero values fall back to
// sensible defaults tuned for outbound HTTP calls.
type Settings struct {
	// Name identifies the breaker in logs and metrics.
	Name string
	// MaxRequests is the number of probe requests allowed in half-open state.
	MaxRequests uint32
	// Interval is the rolling window over which counts reset in closed state.
	Interval time.Duration
	// Timeout is how long the breaker stays open before transitioning to
	// half-open.
	Timeout time.Duration
	// ConsecutiveFailuresToTrip trips the breaker once this many consecutive
	// failures are observed. Defaults to 5.
	ConsecutiveFailuresToTrip uint32
}

func (s Settings) withDefaults() Settings {
	if s.Name == "" {
		s.Name = "circuit"
	}
	if s.MaxRequests == 0 {
		s.MaxRequests = 5
	}
	if s.Interval == 0 {
		s.Interval = 60 * time.Second
	}
	if s.Timeout == 0 {
		s.Timeout = 30 * time.Second
	}
	if s.ConsecutiveFailuresToTrip == 0 {
		s.ConsecutiveFailuresToTrip = 5
	}
	return s
}

// New constructs a CircuitBreaker parameterized by the result type T. State
// changes are logged at warn level on the supplied logger.
func New[T any](s Settings, log *slog.Logger) *gobreaker.CircuitBreaker[T] {
	s = s.withDefaults()
	trip := s.ConsecutiveFailuresToTrip
	return gobreaker.NewCircuitBreaker[T](gobreaker.Settings{
		Name:        s.Name,
		MaxRequests: s.MaxRequests,
		Interval:    s.Interval,
		Timeout:     s.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures > trip
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			if log != nil {
				log.Warn("circuit breaker state change",
					"name", name,
					"from", from.String(),
					"to", to.String())
			}
		},
	})
}

// NewBytes is a convenience wrapper for breakers that return raw HTTP bodies.
// It is equivalent to New[[]byte](s, log).
func NewBytes(s Settings, log *slog.Logger) *gobreaker.CircuitBreaker[[]byte] {
	return New[[]byte](s, log)
}
