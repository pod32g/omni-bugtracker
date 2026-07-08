package platform

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/omni/bugtracker/internal/config"
)

// NewRedis builds the Redis client used for caching and rate limiting only.
// Durable jobs live in Postgres via River — Redis is never the source of truth.
func NewRedis(ctx context.Context, cfg config.Redis) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr: cfg.Addr,
		DB:   cfg.DB,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return client, nil
}
