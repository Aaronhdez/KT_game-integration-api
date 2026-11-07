// Package persistence implements the repository ports of the domain on PostgreSQL.
package persistence

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect creates a connection pool for databaseURL and checks that the
// database answers. The caller owns the pool and must Close it.
// Pool settings such as pool_max_conns can be given in the URL.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		// pgx removes the password from this error message.
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	// The pool opens connections lazily: without a ping, a wrong host or
	// password would only show up on the first request.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}
