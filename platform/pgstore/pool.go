package pgstore

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool creates a pgx connection pool from a database URL.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	// pgxpool requires postgres:// or postgresql:// scheme; convert pgx5:// if present.
	poolURL := databaseURL
	if len(poolURL) > 6 && poolURL[:6] == "pgx5:/" {
		poolURL = "postgres" + poolURL[4:]
	}
	pool, err := pgxpool.New(ctx, poolURL)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}
