//go:build integration

package persistence

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// How long a test waits for something that must happen, and for something
// that must not happen (long enough to be meaningful, short enough to keep
// the suite fast).
const (
	eventuallyTimeout = 5 * time.Second
	blockedFor        = 300 * time.Millisecond
)

var errTestFailure = errors.New("test failure")

func TestUnitOfWork_Atomic_FnSucceeds_CommitsTheWrites(t *testing.T) {
	pool := migratedTestDatabase(t)
	locker := NewPlayerLocker(pool)
	bet := newTestBet(t, pool, eurPlayerID, "10.50")

	err := locker.WithPlayerLock(testContext(t), eurPlayerID, func(ctx context.Context, uow domain.UnitOfWork) error {
		return uow.Atomic(ctx, insertBetWith(bet, nil))
	})

	assertStoredBet(t, pool, err, bet)
}

func TestUnitOfWork_Atomic_FnFails_RollsBackTheWrites(t *testing.T) {
	pool := migratedTestDatabase(t)
	locker := NewPlayerLocker(pool)
	bet := newTestBet(t, pool, eurPlayerID, "10.50")

	_ = locker.WithPlayerLock(testContext(t), eurPlayerID, func(ctx context.Context, uow domain.UnitOfWork) error {
		return uow.Atomic(ctx, insertBetWith(bet, errTestFailure))
	})

	assertBetNotStored(t, pool, bet.ID)
}

func TestUnitOfWork_Atomic_FnFails_ReturnsItsError(t *testing.T) {
	pool := migratedTestDatabase(t)
	locker := NewPlayerLocker(pool)
	bet := newTestBet(t, pool, eurPlayerID, "10.50")

	err := locker.WithPlayerLock(testContext(t), eurPlayerID, func(ctx context.Context, uow domain.UnitOfWork) error {
		return uow.Atomic(ctx, insertBetWith(bet, errTestFailure))
	})

	assertErrorIs(t, err, errTestFailure)
}

// The intent-first property: what the first Atomic committed (e.g. the
// PENDING transaction) survives whatever happens afterwards.
func TestUnitOfWork_Atomic_SecondBlockFails_KeepsWhatTheFirstCommitted(t *testing.T) {
	pool := migratedTestDatabase(t)
	locker := NewPlayerLocker(pool)
	first := newTestBet(t, pool, eurPlayerID, "10.50")
	second := newTestBet(t, pool, eurPlayerID, "20.00")

	err := locker.WithPlayerLock(testContext(t), eurPlayerID, func(ctx context.Context, uow domain.UnitOfWork) error {
		if err := uow.Atomic(ctx, insertBetWith(first, nil)); err != nil {
			return err
		}
		return uow.Atomic(ctx, insertBetWith(second, errTestFailure))
	})

	assertStoredBet(t, pool, ignoreError(err, errTestFailure), first)
}

func TestPlayerLocker_SamePlayer_SecondOperationWaitsWhileTheFirstHoldsTheLock(t *testing.T) {
	locker := NewPlayerLocker(migratedTestDatabase(t))
	holdLock(t, locker, eurPlayerID)

	second := runWithLock(t, locker, eurPlayerID)

	assertNotSignalledWithin(t, second, blockedFor)
}

func TestPlayerLocker_SamePlayer_SecondOperationRunsOnceTheFirstReleasesTheLock(t *testing.T) {
	locker := NewPlayerLocker(migratedTestDatabase(t))
	release := holdLock(t, locker, eurPlayerID)
	second := runWithLock(t, locker, eurPlayerID)

	release()

	assertSignalledWithin(t, second, eventuallyTimeout)
}

func TestPlayerLocker_DifferentPlayer_DoesNotWait(t *testing.T) {
	locker := NewPlayerLocker(migratedTestDatabase(t))
	holdLock(t, locker, eurPlayerID)

	other := runWithLock(t, locker, usdPlayerID)

	assertSignalledWithin(t, other, eventuallyTimeout)
}

func TestPlayerLocker_FnFails_ReleasesTheLock(t *testing.T) {
	locker := NewPlayerLocker(migratedTestDatabase(t))
	_ = locker.WithPlayerLock(testContext(t), eurPlayerID, func(context.Context, domain.UnitOfWork) error {
		return errTestFailure
	})

	next := runWithLock(t, locker, eurPlayerID)

	assertSignalledWithin(t, next, eventuallyTimeout)
}

func TestPlayerLocker_ContextCancelledWhileWaiting_ReturnsError(t *testing.T) {
	locker := NewPlayerLocker(migratedTestDatabase(t))
	holdLock(t, locker, eurPlayerID)
	ctx, cancel := context.WithTimeout(testContext(t), blockedFor)
	defer cancel()

	err := locker.WithPlayerLock(ctx, eurPlayerID, func(context.Context, domain.UnitOfWork) error {
		return nil
	})

	assertError(t, err)
}

func TestPlayerLocker_ContextCancelledWhileWaiting_DoesNotKeepTheLock(t *testing.T) {
	locker := NewPlayerLocker(migratedTestDatabase(t))
	release := holdLock(t, locker, eurPlayerID)
	ctx, cancel := context.WithTimeout(testContext(t), blockedFor)
	defer cancel()
	_ = locker.WithPlayerLock(ctx, eurPlayerID, func(context.Context, domain.UnitOfWork) error {
		return nil
	})
	release()

	next := runWithLock(t, locker, eurPlayerID)

	assertSignalledWithin(t, next, eventuallyTimeout)
}

/////////////////////////////////////////////////////////////////////////////////////

// insertBetWith returns an Atomic function that inserts bet and then returns
// result: nil to commit, an error to roll back.
func insertBetWith(bet domain.Bet, result error) func(context.Context, domain.Repositories) error {
	return func(ctx context.Context, repos domain.Repositories) error {
		if err := repos.Bets.Insert(ctx, bet); err != nil {
			return err
		}
		return result
	}
}

// assertBetNotStored checks with plain SQL that no row exists for id.
func assertBetNotStored(t *testing.T, pool *pgxpool.Pool, id domain.BetID) {
	t.Helper()
	var count int
	if err := pool.QueryRow(testContext(t), "SELECT count(*) FROM bets WHERE id = $1", int64(id)).Scan(&count); err != nil {
		t.Fatalf("count bets: %v", err)
	}
	if count != 0 {
		t.Errorf("expected bet %d not to be stored, found %d rows", id, count)
	}
}

// ignoreError returns nil if err is expected, so a test can tolerate a
// failure it caused on purpose; any other error is returned unchanged.
func ignoreError(err, expected error) error {
	if errors.Is(err, expected) {
		return nil
	}
	return err
}

// holdLock takes userID's lock in a goroutine and keeps it until the returned
// function is called. It is also called when the test ends, so a failing test
// never leaves the lock (and its connection) taken.
func holdLock(t *testing.T, locker *PlayerLocker, userID domain.UserID) (release func()) {
	t.Helper()
	acquired := make(chan struct{})
	releaseRequested := make(chan struct{})
	finished := make(chan struct{})

	go func() {
		defer close(finished)
		_ = locker.WithPlayerLock(context.Background(), userID, func(context.Context, domain.UnitOfWork) error {
			close(acquired)
			<-releaseRequested
			return nil
		})
	}()
	assertSignalledWithin(t, acquired, eventuallyTimeout)

	var once sync.Once
	release = func() {
		once.Do(func() {
			close(releaseRequested)
			<-finished
		})
	}
	t.Cleanup(release)
	return release
}

// runWithLock starts an operation on userID's lock in a goroutine and returns
// a channel that is closed when the operation gets the lock. It gives up when
// the test's context ends.
func runWithLock(t *testing.T, locker *PlayerLocker, userID domain.UserID) <-chan struct{} {
	t.Helper()
	acquired := make(chan struct{})
	ctx := testContext(t)
	go func() {
		_ = locker.WithPlayerLock(ctx, userID, func(context.Context, domain.UnitOfWork) error {
			close(acquired)
			return nil
		})
	}()
	return acquired
}

func assertSignalledWithin(t *testing.T, signal <-chan struct{}, timeout time.Duration) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(timeout):
		t.Fatalf("expected the operation to get the lock within %v", timeout)
	}
}

func assertNotSignalledWithin(t *testing.T, signal <-chan struct{}, duration time.Duration) {
	t.Helper()
	select {
	case <-signal:
		t.Errorf("expected the operation to wait for the lock, but it got it")
	case <-time.After(duration):
	}
}
