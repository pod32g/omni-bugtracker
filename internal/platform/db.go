// Package platform holds infrastructure adapters shared by server and worker:
// the Postgres pool, Redis client, structured logger, and Prometheus registry.
package platform

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/omni/bugtracker/internal/config"
)

// NewDBPool builds a pgx connection pool used by both the API and the workers
// (River shares the same Postgres, so the pool is the single source of truth).
func NewDBPool(ctx context.Context, cfg config.Database) (*pgxpool.Pool, error) {
	pcfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	if cfg.MaxConns > 0 {
		pcfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		pcfg.MinConns = cfg.MinConns
	}
	pcfg.MaxConnLifetime = time.Hour
	pcfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}
