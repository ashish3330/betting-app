package redis

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker/v2"
)

type Client struct {
	rdb     *redis.Client
	breaker *gobreaker.CircuitBreaker[interface{}]
	logger  *slog.Logger
}

func NewClient(addr, password string, logger *slog.Logger) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           0,
		PoolSize:     100,
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

func (c *Client) Raw() *redis.Client { return c.rdb }

func (c *Client) Close() error {
	return c.rdb.Close()
}

func (c *Client) HealthCheck(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}
