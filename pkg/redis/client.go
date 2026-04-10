package redis

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker/v2"
)

// Client wraps a redis.Client with a circuit breaker for resilience.
type Client struct {
	rdb     *redis.Client
	breaker *gobreaker.CircuitBreaker[interface{}]
	logger  *slog.Logger
}

// NewClient creates a new Redis client with the given pool size. If poolSize
// is 0 a default of 100 is used.
func NewClient(addr, password string, poolSize int, logger *slog.Logger) (*Client, error) {
	if poolSize <= 0 {
		poolSize = 100
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           0,
		PoolSize:     poolSize,
		MinIdleConns: 10,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		DialTimeout:  5 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	cb := gobreaker.NewCircuitBreaker[interface{}](gobreaker.Settings{
		Name:        "redis",
		MaxRequests: 5,
		Interval:    10 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 10 && failureRatio >= 0.5
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			logger.Warn("redis circuit breaker state change",
				"name", name, "from", from, "to", to)
		},
	})

	logger.Info("redis connected", "addr", addr)
	return &Client{rdb: rdb, breaker: cb, logger: logger}, nil
}

// Do executes fn through the circuit breaker. If the breaker is open, the
// call is rejected immediately without hitting Redis.
func (c *Client) Do(ctx context.Context, fn func(ctx context.Context, rdb *redis.Client) error) error {
	_, err := c.breaker.Execute(func() (interface{}, error) {
		err := fn(ctx, c.rdb)
		return nil, err
	})
	return err
}

// Raw returns the underlying go-redis client for operations that need direct
// access (e.g. pipelines, pub/sub). Callers should prefer Do for ordinary
// commands so that the circuit breaker can track failures.
func (c *Client) Raw() *redis.Client { return c.rdb }

func (c *Client) Close() error {
	return c.rdb.Close()
}

func (c *Client) HealthCheck(ctx context.Context) error {
	return c.Do(ctx, func(ctx context.Context, rdb *redis.Client) error {
		return rdb.Ping(ctx).Err()
	})
}
