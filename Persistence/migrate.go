package persistence

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// migrationFiles holds the SQL migrations, compiled into the binary: the
// service carries its own schema and needs no files next to it at runtime.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate applies every migration that the database does not have yet, in
// version order, and returns the versions it applied (empty if the schema
// was already up to date). goose records the applied versions in its own
// table, goose_db_version.
func Migrate(ctx context.Context, pool *pgxpool.Pool) ([]int64, error) {
	// goose reads the files from the root of the file system it receives.
	migrations, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("open migrations: %w", err)
	}

	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	if err != nil {
		return nil, fmt.Errorf("create migration provider: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	applied := make([]int64, 0, len(results))
	for _, result := range results {
		applied = append(applied, result.Source.Version)
	}
	return applied, nil
}
