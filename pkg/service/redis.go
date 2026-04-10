package service

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// OpenRedis creates a go-redis client with the standard lotus-exchange pool
// configuration and verifies connectivity with a 5s ping. Callers retain
// ownership of the returned *redis.Client and must Close it.
func OpenRedis(ctx context.Context, addr, password string, poolSize int) (*redis.Client, error) {
	if addr == "" {
		return nil, fmt.Errorf("service: redis address is empty")
	}

	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		PoolSize: poolSize,
	})

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("service: redis ping: %w", err)
	}

	return client, nil
}
