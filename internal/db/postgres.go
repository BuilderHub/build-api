package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps pgx pool for database access.
type Pool struct {
	*pgxpool.Pool
}

// NewPool creates a Postgres connection pool.
func NewPool(ctx context.Context, connString string) (*Pool, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Pool{Pool: pool}, nil
}
