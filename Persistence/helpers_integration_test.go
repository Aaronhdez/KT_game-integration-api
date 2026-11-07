//go:build integration

package persistence

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Integration tests need a real PostgreSQL. They only compile with
// `-tags=integration`, so `go test ./...` never needs Docker. Running them
// without TEST_DATABASE_URL is a mistake, so it fails instead of skipping.
//
// The tests share one database and must not run in parallel (no t.Parallel).

const testTimeout = 10 * time.Second

// testPlayerID is a player inserted by the tests themselves, outside the seeded IDs.
const testPlayerID = 1

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	t.Cleanup(cancel)
	return ctx
}

// connectTestDatabase returns a pool to TEST_DATABASE_URL, closed when the test ends.
func connectTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("TEST_DATABASE_URL is not set (see README, 'Running the tests')")
	}
	pool, err := Connect(testContext(t), databaseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// migratedTestDatabase returns a pool to a database with every migration applied.
func migratedTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := connectTestDatabase(t)
	if _, err := Migrate(testContext(t), pool); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return pool
}

// dropAllTables leaves the database empty, as if just created. Other tests
// migrate it again through migratedTestDatabase.
func dropAllTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	execSQL(t, pool, "DROP SCHEMA public CASCADE")
	execSQL(t, pool, "CREATE SCHEMA public")
}

// insertTestPlayer makes sure testPlayerID exists, for rows that reference a player.
func insertTestPlayer(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	execSQL(t, pool, `
		INSERT INTO players (wallet_user_id, username, password_hash, currency)
		VALUES ($1, 'test_player', 'not-a-real-hash', 'USD')
		ON CONFLICT (wallet_user_id) DO NOTHING`, testPlayerID)
}

func execSQL(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(testContext(t), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// assertCheckViolation checks that PostgreSQL rejected a row because of a CHECK constraint.
func assertCheckViolation(t *testing.T, err error) {
	t.Helper()
	const checkViolation = "23514" // SQLSTATE code
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected a PostgreSQL error, got %v", err)
	}
	if pgErr.Code != checkViolation {
		t.Errorf("expected check violation (%s), got %s: %s", checkViolation, pgErr.Code, pgErr.Message)
	}
}
