//go:build integration

package persistence

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// FindPendingBefore reads across every player, so rows left by other tests
// (or earlier runs) would show up: each test starts from an empty
// transactions table. The tests never run in parallel.

func TestPendingTransactionFinder_ReturnsOnlyPendingTransactions(t *testing.T) {
	pool := migratedTestDatabaseWithoutTransactions(t)
	finder := NewPendingTransactionFinder(pool)
	cutoff := testNow()
	pending := insertWithdrawUpdatedAt(t, pool, eurPlayerID, cutoff.Add(-time.Minute))
	completed := newTestWithdrawUpdatedAt(t, pool, eurPlayerID, cutoff.Add(-time.Minute))
	completeTransaction(t, &completed, "89.50")
	insertTransactionRow(t, pool, completed)
	failed := newTestWithdrawUpdatedAt(t, pool, eurPlayerID, cutoff.Add(-time.Minute))
	failTransaction(t, &failed)
	insertTransactionRow(t, pool, failed)

	result, err := finder.FindPendingBefore(testContext(t), cutoff, 10)

	assertTransactionList(t, result, err, pending)
}

func TestPendingTransactionFinder_SkipsTransactionsUpdatedAfterTheCutoff(t *testing.T) {
	pool := migratedTestDatabaseWithoutTransactions(t)
	finder := NewPendingTransactionFinder(pool)
	cutoff := testNow()
	atTheCutoff := insertWithdrawUpdatedAt(t, pool, eurPlayerID, cutoff)
	insertWithdrawUpdatedAt(t, pool, eurPlayerID, cutoff.Add(time.Second))

	result, err := finder.FindPendingBefore(testContext(t), cutoff, 10)

	assertTransactionList(t, result, err, atTheCutoff)
}

func TestPendingTransactionFinder_ReturnsEveryPlayerOldestFirst(t *testing.T) {
	pool := migratedTestDatabaseWithoutTransactions(t)
	finder := NewPendingTransactionFinder(pool)
	cutoff := testNow()
	newer := insertWithdrawUpdatedAt(t, pool, eurPlayerID, cutoff.Add(-time.Minute))
	older := insertWithdrawUpdatedAt(t, pool, usdPlayerID, cutoff.Add(-2*time.Minute))

	result, err := finder.FindPendingBefore(testContext(t), cutoff, 10)

	assertTransactionList(t, result, err, older, newer)
}

func TestPendingTransactionFinder_ReturnsAtMostLimit(t *testing.T) {
	pool := migratedTestDatabaseWithoutTransactions(t)
	finder := NewPendingTransactionFinder(pool)
	cutoff := testNow()
	oldest := insertWithdrawUpdatedAt(t, pool, eurPlayerID, cutoff.Add(-3*time.Minute))
	middle := insertWithdrawUpdatedAt(t, pool, eurPlayerID, cutoff.Add(-2*time.Minute))
	insertWithdrawUpdatedAt(t, pool, eurPlayerID, cutoff.Add(-time.Minute))

	result, err := finder.FindPendingBefore(testContext(t), cutoff, 2)

	assertTransactionList(t, result, err, oldest, middle)
}

func TestPendingTransactionFinder_NothingPending_ReturnsAnEmptyList(t *testing.T) {
	pool := migratedTestDatabaseWithoutTransactions(t)
	finder := NewPendingTransactionFinder(pool)

	result, err := finder.FindPendingBefore(testContext(t), testNow(), 10)

	assertTransactionList(t, result, err)
}

/////////////////////////////////////////////////////////////////////////////////////

// migratedTestDatabaseWithoutTransactions deletes every transaction (bets and
// players stay). Deleting them in one statement satisfies the self-reference
// of a cancel to its original.
func migratedTestDatabaseWithoutTransactions(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := migratedTestDatabase(t)
	execSQL(t, pool, "DELETE FROM transactions")
	return pool
}

// newTestWithdrawUpdatedAt builds a PENDING withdraw whose last attempt
// started at updatedAt.
func newTestWithdrawUpdatedAt(t *testing.T, pool *pgxpool.Pool, userID domain.UserID, updatedAt time.Time) domain.Transaction {
	t.Helper()
	tx := newTestWithdraw(t, pool, userID)
	tx.CreatedAt = updatedAt
	tx.UpdatedAt = updatedAt
	return tx
}

func insertWithdrawUpdatedAt(t *testing.T, pool *pgxpool.Pool, userID domain.UserID, updatedAt time.Time) domain.Transaction {
	t.Helper()
	tx := newTestWithdrawUpdatedAt(t, pool, userID, updatedAt)
	insertTransactionRow(t, pool, tx)
	return tx
}

// assertTransactionList checks a list read from the database, in order.
func assertTransactionList(t *testing.T, result []domain.Transaction, err error, want ...domain.Transaction) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != len(want) {
		t.Fatalf("expected %d transactions, got %d: %+v", len(want), len(result), result)
	}
	for i := range want {
		assertTransaction(t, result[i], nil, want[i])
	}
}
