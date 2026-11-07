package persistence

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// unlockTimeout bounds the release of a lock. It uses its own context because
// the request's context may already be cancelled when the operation ends.
const unlockTimeout = 5 * time.Second

var (
	_ domain.PlayerLocker = (*PlayerLocker)(nil)
	_ domain.UnitOfWork   = (*unitOfWork)(nil)
)

// PlayerLocker serialises the operations of each player with a PostgreSQL
// session advisory lock keyed by the wallet user ID.
//
// The lock belongs to one database connection, and that same connection runs
// every query of the operation (see unitOfWork). This lets the lock outlive the
// commit of the PENDING transaction, and an operation never needs a second
// connection while holding one, so the pool cannot run dry with every
// connection waiting for another.
// If the process dies, PostgreSQL closes the connection and releases its locks.
type PlayerLocker struct {
	pool *pgxpool.Pool
}

func NewPlayerLocker(pool *pgxpool.Pool) *PlayerLocker {
	return &PlayerLocker{pool: pool}
}

// WithPlayerLock waits until no other operation holds the lock of userID (or
// until ctx is done), runs fn and then releases the lock, whatever fn returns.
func (l *PlayerLocker) WithPlayerLock(ctx context.Context, userID domain.UserID, fn func(ctx context.Context, uow domain.UnitOfWork) error) error {
	connection, err := l.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("lock player %d: acquire connection: %w", userID, err)
	}

	// If ctx is cancelled while waiting for the lock, the connection is released.
	if _, err := connection.Exec(ctx, "SELECT pg_advisory_lock($1)", int64(userID)); err != nil {
		destroy(connection)
		return fmt.Errorf("lock player %d: %w", userID, err)
	}
	defer unlock(connection, userID)

	return fn(ctx, &unitOfWork{connection: connection})
}

// unlock releases the lock and returns the connection to the pool.
// If the lock cannot be released, the connection is discarded instead:
// a connection still holding a lock must never be reused by another operation.
func unlock(connection *pgxpool.Conn, userID domain.UserID) {
	ctx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
	defer cancel()

	var released bool
	err := connection.QueryRow(ctx, "SELECT pg_advisory_unlock($1)", int64(userID)).Scan(&released)
	if err != nil || !released {
		destroy(connection)
		return
	}
	connection.Release()
}

// destroy closes the connection and gives it back to the pool, which drops
// closed connections instead of reusing them.
func destroy(connection *pgxpool.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
	defer cancel()

	_ = connection.Conn().Close(ctx)
	connection.Release()
}

// unitOfWork runs database transactions on the connection that holds the
// player's lock.
type unitOfWork struct {
	connection *pgxpool.Conn
}

// Atomic runs fn inside one database transaction, with repositories bound to it:
// committed if fn returns nil, rolled back otherwise.
func (u *unitOfWork) Atomic(ctx context.Context, fn func(ctx context.Context, repos domain.Repositories) error) error {
	tx, err := u.connection.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin database transaction: %w", err)
	}
	// Rolls back on every early return and on panic.
	// After a successful commit it does nothing.
	defer tx.Rollback(ctx)

	repos := domain.Repositories{
		Transactions: newTransactionRepository(tx),
		Bets:         newBetRepository(tx),
	}
	if err := fn(ctx, repos); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit database transaction: %w", err)
	}
	return nil
}
