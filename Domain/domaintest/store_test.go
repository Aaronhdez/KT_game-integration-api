package domaintest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

const anotherUserID domain.UserID = 34673635133

var errRollback = errors.New("something failed inside the transaction")

// Atomic

func TestFakeStoreAtomic_FnSucceeds_KeepsTheWrites(t *testing.T) {
	store := NewFakeStore()
	withdraw := givenAPendingWithdraw(t, 1, testUserID, "prov-1")

	err := insertTransaction(store, withdraw)

	assertStoredTransactions(t, store, err, withdraw)
}

func TestFakeStoreAtomic_FnFails_DiscardsTheWrites(t *testing.T) {
	store := NewFakeStore()
	withdraw := givenAPendingWithdraw(t, 1, testUserID, "prov-1")

	err := store.Atomic(context.Background(), func(ctx context.Context, repos domain.Repositories) error {
		_ = repos.Transactions.Insert(ctx, withdraw)
		return errRollback
	})

	assertNoTransactions(t, store, err, errRollback)
}

func TestFakeStoreAtomic_ContextAlreadyDone_FailsWithoutRunningFn(t *testing.T) {
	store := NewFakeStore()
	withdraw := givenAPendingWithdraw(t, 1, testUserID, "prov-1")

	err := store.Atomic(givenACancelledContext(), func(ctx context.Context, repos domain.Repositories) error {
		return repos.Transactions.Insert(ctx, withdraw)
	})

	assertNoTransactions(t, store, err, context.Canceled)
}

// Transactions: unique rules

func TestFakeStoreTransactions_Insert_SameProviderIDForSamePlayer_ReturnsTransactionExists(t *testing.T) {
	store := NewFakeStore()
	store.PutTransaction(givenAPendingWithdraw(t, 1, testUserID, "prov-1"))

	err := insertTransaction(store, givenAPendingWithdraw(t, 2, testUserID, "prov-1"))

	assertErrorIs(t, err, domain.ErrTransactionExists)
}

func TestFakeStoreTransactions_Insert_SameProviderIDForAnotherPlayer_ReturnsTransactionExists(t *testing.T) {
	store := NewFakeStore()
	store.PutTransaction(givenAPendingWithdraw(t, 1, testUserID, "prov-1"))

	err := insertTransaction(store, givenAPendingWithdraw(t, 2, anotherUserID, "prov-1"))

	assertErrorIs(t, err, domain.ErrTransactionExists)
}

func TestFakeStoreTransactions_Insert_CancelWithTheProviderIDOfItsOriginal_IsStored(t *testing.T) {
	store := NewFakeStore()
	withdraw := givenACompletedWithdraw(t, 1, testUserID, "prov-1")
	store.PutTransaction(withdraw)
	cancel := givenACancelOf(t, 2, withdraw)

	err := insertTransaction(store, cancel)

	assertStoredTransactions(t, store, err, withdraw, cancel)
}

func TestFakeStoreTransactions_Insert_SecondActiveDepositOnSameBet_ReturnsBetAlreadySettled(t *testing.T) {
	store := NewFakeStore()
	store.PutTransaction(givenAPendingDeposit(t, 1, testUserID, "prov-1", 7))

	err := insertTransaction(store, givenAPendingDeposit(t, 2, testUserID, "prov-2", 7))

	assertErrorIs(t, err, domain.ErrBetAlreadySettled)
}

func TestFakeStoreTransactions_Insert_SecondCancelOfSameTransaction_ReturnsAlreadyCancelled(t *testing.T) {
	store := NewFakeStore()
	withdraw := givenACompletedWithdraw(t, 1, testUserID, "prov-1")
	store.PutTransaction(withdraw)
	store.PutTransaction(givenACancelOf(t, 2, withdraw))

	err := insertTransaction(store, givenACancelOf(t, 3, withdraw))

	assertErrorIs(t, err, domain.ErrAlreadyCancelled)
}

func TestFakeStoreTransactions_Insert_WithInsertTransactionErr_ReturnsIt(t *testing.T) {
	store := NewFakeStore()
	store.InsertTransactionErr = errDatabaseDown

	err := insertTransaction(store, givenAPendingWithdraw(t, 1, testUserID, "prov-1"))

	assertNoTransactions(t, store, err, errDatabaseDown)
}

// Transactions: reads scoped by player

func TestFakeStoreTransactions_FindByID_ReturnsTheTransaction(t *testing.T) {
	store := NewFakeStore()
	withdraw := givenAPendingWithdraw(t, 1, testUserID, "prov-1")
	store.PutTransaction(withdraw)

	result, err := findTransactionByID(store, testUserID, withdraw.ID)

	assertTransactionID(t, result, err, withdraw.ID)
}

func TestFakeStoreTransactions_FindByID_AnotherPlayersTransaction_ReturnsTransactionNotFound(t *testing.T) {
	store := NewFakeStore()
	withdraw := givenAPendingWithdraw(t, 1, testUserID, "prov-1")
	store.PutTransaction(withdraw)

	_, err := findTransactionByID(store, anotherUserID, withdraw.ID)

	assertErrorIs(t, err, domain.ErrTransactionNotFound)
}

func TestFakeStoreTransactions_FindByProviderTransactionID_ReturnsTheOriginalNotItsCancel(t *testing.T) {
	store := NewFakeStore()
	withdraw := givenACompletedWithdraw(t, 1, testUserID, "prov-1")
	store.PutTransaction(withdraw)
	store.PutTransaction(givenACancelOf(t, 2, withdraw))

	result, err := findTransactionByProviderID(store, testUserID, "prov-1")

	assertTransactionID(t, result, err, withdraw.ID)
}

func TestFakeStoreTransactions_FindByProviderTransactionID_AnotherPlayersTransaction_ReturnsTransactionNotFound(t *testing.T) {
	store := NewFakeStore()
	store.PutTransaction(givenAPendingWithdraw(t, 1, testUserID, "prov-1"))

	_, err := findTransactionByProviderID(store, anotherUserID, "prov-1")

	assertErrorIs(t, err, domain.ErrTransactionNotFound)
}

func TestFakeStoreTransactions_FindCancelOf_ReturnsTheCancel(t *testing.T) {
	store := NewFakeStore()
	withdraw := givenACompletedWithdraw(t, 1, testUserID, "prov-1")
	cancel := givenACancelOf(t, 2, withdraw)
	store.PutTransaction(withdraw)
	store.PutTransaction(cancel)

	result, err := findCancelOf(store, testUserID, withdraw.ID)

	assertTransactionID(t, result, err, cancel.ID)
}

func TestFakeStoreTransactions_FindCancelOf_TransactionWithoutCancel_ReturnsTransactionNotFound(t *testing.T) {
	store := NewFakeStore()
	withdraw := givenACompletedWithdraw(t, 1, testUserID, "prov-1")
	store.PutTransaction(withdraw)

	_, err := findCancelOf(store, testUserID, withdraw.ID)

	assertErrorIs(t, err, domain.ErrTransactionNotFound)
}

// Transactions: Update

func TestFakeStoreTransactions_Update_StoresOnlyStatusNewBalanceAndUpdatedAt(t *testing.T) {
	store := NewFakeStore()
	pending := givenAPendingWithdraw(t, 1, testUserID, "prov-1")
	store.PutTransaction(pending)
	changed := givenACompletedWithdraw(t, 1, testUserID, "prov-1")
	changed.Amount = givenAParsedMoney(t, "99.99")
	want := givenACompletedWithdraw(t, 1, testUserID, "prov-1")

	err := updateTransaction(store, changed)

	assertStoredTransactions(t, store, err, want)
}

func TestFakeStoreTransactions_Update_AnotherPlayersTransaction_ReturnsTransactionNotFound(t *testing.T) {
	store := NewFakeStore()
	store.PutTransaction(givenAPendingWithdraw(t, 1, testUserID, "prov-1"))
	impostor := givenACompletedWithdraw(t, 1, anotherUserID, "prov-1")

	err := updateTransaction(store, impostor)

	assertErrorIs(t, err, domain.ErrTransactionNotFound)
}

func TestFakeStoreTransactions_Update_WithUpdateTransactionErr_ReturnsIt(t *testing.T) {
	store := NewFakeStore()
	store.PutTransaction(givenAPendingWithdraw(t, 1, testUserID, "prov-1"))
	store.UpdateTransactionErr = errDatabaseDown

	err := updateTransaction(store, givenACompletedWithdraw(t, 1, testUserID, "prov-1"))

	assertErrorIs(t, err, errDatabaseDown)
}

// Bets

func TestFakeStoreBets_NextID_ReturnsIncreasingIDs(t *testing.T) {
	store := NewFakeStore()

	result := nextBetIDs(t, store, 3)

	assertBetIDs(t, result, []domain.BetID{1, 2, 3})
}

func TestFakeStoreBets_NextID_IsNotReusedAfterARollback(t *testing.T) {
	store := NewFakeStore()
	rollBackAfterNextBetID(store)

	result := nextBetIDs(t, store, 1)

	assertBetIDs(t, result, []domain.BetID{2})
}

func TestFakeStoreBets_Insert_KeepsTheBet(t *testing.T) {
	store := NewFakeStore()
	bet := givenAPlacedBet(t, 1, testUserID)

	err := insertBet(store, bet)

	assertStoredBets(t, store, err, bet)
}

func TestFakeStoreBets_FindByID_AnotherPlayersBet_ReturnsBetNotFound(t *testing.T) {
	store := NewFakeStore()
	store.PutBet(givenAPlacedBet(t, 1, testUserID))

	_, err := findBetByID(store, anotherUserID, 1)

	assertErrorIs(t, err, domain.ErrBetNotFound)
}

func TestFakeStoreBets_Update_StoresOnlyStatusAndUpdatedAt(t *testing.T) {
	store := NewFakeStore()
	store.PutBet(givenAPlacedBet(t, 1, testUserID))
	changed := givenAWonBet(t, 1, testUserID)
	changed.Stake = givenAParsedMoney(t, "99.99")
	want := givenAWonBet(t, 1, testUserID)

	err := updateBet(store, changed)

	assertStoredBets(t, store, err, want)
}

// Player lock

func TestFakeStoreWithPlayerLock_SamePlayer_WaitsUntilTheFirstReleases(t *testing.T) {
	store := NewFakeStore()
	release := holdPlayerLock(t, store, testUserID)

	entered := enterPlayerLock(store, context.Background(), testUserID)

	assertWaitsUntilReleased(t, entered, release)
}

func TestFakeStoreWithPlayerLock_AnotherPlayer_DoesNotWait(t *testing.T) {
	store := NewFakeStore()
	release := holdPlayerLock(t, store, testUserID)
	defer release()

	entered := enterPlayerLock(store, context.Background(), anotherUserID)

	assertEnters(t, entered)
}

func TestFakeStoreWithPlayerLock_ContextDoneWhileWaiting_ReturnsTheContextError(t *testing.T) {
	store := NewFakeStore()
	release := holdPlayerLock(t, store, testUserID)
	defer release()

	err := store.WithPlayerLock(givenACancelledContext(), testUserID, func(context.Context, domain.UnitOfWork) error {
		return nil
	})

	assertErrorIs(t, err, context.Canceled)
}

func TestFakeStoreWithPlayerLock_ReturnsTheErrorOfFn(t *testing.T) {
	store := NewFakeStore()

	err := store.WithPlayerLock(context.Background(), testUserID, func(context.Context, domain.UnitOfWork) error {
		return errRollback
	})

	assertErrorIs(t, err, errRollback)
}

/////////////////////////////////////////////////////////////////////////////////////

func givenACancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func givenAPendingWithdraw(t *testing.T, n int, userID domain.UserID, providerID string) domain.Transaction {
	t.Helper()
	return domain.Transaction{
		ID:                    FakeTransactionID(n),
		Type:                  domain.TransactionWithdraw,
		Status:                domain.TransactionPending,
		UserID:                userID,
		BetID:                 domain.BetID(n),
		ProviderTransactionID: domain.ProviderTransactionID(providerID),
		Amount:                givenAParsedMoney(t, "10.50"),
		Currency:              domain.USD,
		OldBalance:            givenAParsedMoney(t, "5000.00"),
		CreatedAt:             testNow,
		UpdatedAt:             testNow,
	}
}

func givenACompletedWithdraw(t *testing.T, n int, userID domain.UserID, providerID string) domain.Transaction {
	t.Helper()
	tx := givenAPendingWithdraw(t, n, userID, providerID)
	newBalance := givenAParsedMoney(t, "4989.50")
	tx.Status = domain.TransactionCompleted
	tx.NewBalance = &newBalance
	tx.UpdatedAt = testNow.Add(time.Second)
	return tx
}

func givenAPendingDeposit(t *testing.T, n int, userID domain.UserID, providerID string, betID domain.BetID) domain.Transaction {
	t.Helper()
	tx := givenAPendingWithdraw(t, n, userID, providerID)
	tx.Type = domain.TransactionDeposit
	tx.BetID = betID
	return tx
}

func givenACancelOf(t *testing.T, n int, original domain.Transaction) domain.Transaction {
	t.Helper()
	return domain.NewCancel(FakeTransactionID(n), original, givenAParsedMoney(t, "4989.50"), testNow)
}

func givenAPlacedBet(t *testing.T, id domain.BetID, userID domain.UserID) domain.Bet {
	t.Helper()
	return domain.NewPlacedBet(id, userID, domain.USD, givenAParsedMoney(t, "10.50"), testNow)
}

func givenAWonBet(t *testing.T, id domain.BetID, userID domain.UserID) domain.Bet {
	t.Helper()
	bet := givenAPlacedBet(t, id, userID)
	bet.Status = domain.BetWon
	bet.UpdatedAt = testNow.Add(time.Second)
	return bet
}

// The helpers below run one repository call inside its own Atomic call.

func insertTransaction(store *FakeStore, tx domain.Transaction) error {
	return store.Atomic(context.Background(), func(ctx context.Context, repos domain.Repositories) error {
		return repos.Transactions.Insert(ctx, tx)
	})
}

func updateTransaction(store *FakeStore, tx domain.Transaction) error {
	return store.Atomic(context.Background(), func(ctx context.Context, repos domain.Repositories) error {
		return repos.Transactions.Update(ctx, tx)
	})
}

func findTransactionByID(store *FakeStore, userID domain.UserID, id domain.TransactionID) (domain.Transaction, error) {
	var result domain.Transaction
	err := store.Atomic(context.Background(), func(ctx context.Context, repos domain.Repositories) error {
		var err error
		result, err = repos.Transactions.FindByID(ctx, userID, id)
		return err
	})
	return result, err
}

func findTransactionByProviderID(store *FakeStore, userID domain.UserID, providerID string) (domain.Transaction, error) {
	var result domain.Transaction
	err := store.Atomic(context.Background(), func(ctx context.Context, repos domain.Repositories) error {
		var err error
		result, err = repos.Transactions.FindByProviderTransactionID(ctx, userID, domain.ProviderTransactionID(providerID))
		return err
	})
	return result, err
}

func findCancelOf(store *FakeStore, userID domain.UserID, originalID domain.TransactionID) (domain.Transaction, error) {
	var result domain.Transaction
	err := store.Atomic(context.Background(), func(ctx context.Context, repos domain.Repositories) error {
		var err error
		result, err = repos.Transactions.FindCancelOf(ctx, userID, originalID)
		return err
	})
	return result, err
}

func insertBet(store *FakeStore, bet domain.Bet) error {
	return store.Atomic(context.Background(), func(ctx context.Context, repos domain.Repositories) error {
		return repos.Bets.Insert(ctx, bet)
	})
}

func updateBet(store *FakeStore, bet domain.Bet) error {
	return store.Atomic(context.Background(), func(ctx context.Context, repos domain.Repositories) error {
		return repos.Bets.Update(ctx, bet)
	})
}

func findBetByID(store *FakeStore, userID domain.UserID, id domain.BetID) (domain.Bet, error) {
	var result domain.Bet
	err := store.Atomic(context.Background(), func(ctx context.Context, repos domain.Repositories) error {
		var err error
		result, err = repos.Bets.FindByID(ctx, userID, id)
		return err
	})
	return result, err
}

func nextBetIDs(t *testing.T, store *FakeStore, count int) []domain.BetID {
	t.Helper()
	var ids []domain.BetID
	err := store.Atomic(context.Background(), func(ctx context.Context, repos domain.Repositories) error {
		for range count {
			id, err := repos.Bets.NextID(ctx)
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return ids
}

func rollBackAfterNextBetID(store *FakeStore) {
	_ = store.Atomic(context.Background(), func(ctx context.Context, repos domain.Repositories) error {
		_, _ = repos.Bets.NextID(ctx)
		return errRollback
	})
}

// holdPlayerLock takes the lock of userID in another goroutine and keeps it
// until the returned function is called.
func holdPlayerLock(t *testing.T, store *FakeStore, userID domain.UserID) (release func()) {
	t.Helper()
	locked := make(chan struct{})
	done := make(chan struct{})
	go func() {
		_ = store.WithPlayerLock(context.Background(), userID, func(context.Context, domain.UnitOfWork) error {
			close(locked)
			<-done
			return nil
		})
	}()
	<-locked
	return func() { close(done) }
}

// enterPlayerLock tries to take the lock of userID in another goroutine; the
// returned channel is closed once it has it.
func enterPlayerLock(store *FakeStore, ctx context.Context, userID domain.UserID) <-chan struct{} {
	entered := make(chan struct{})
	go func() {
		_ = store.WithPlayerLock(ctx, userID, func(context.Context, domain.UnitOfWork) error {
			close(entered)
			return nil
		})
	}()
	return entered
}

// How long a goroutine is given to (not) take a lock. Long enough on a slow
// machine; a wrong "it waited" could only make a test pass, never fail.
const lockWait = 50 * time.Millisecond

func assertWaitsUntilReleased(t *testing.T, entered <-chan struct{}, release func()) {
	t.Helper()
	select {
	case <-entered:
		t.Fatal("expected the second operation to wait for the lock, but it entered")
	case <-time.After(lockWait):
	}
	release()
	assertEnters(t, entered)
}

func assertEnters(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("expected the operation to take the lock, but it is still waiting")
	}
}

func assertNoTransactions(t *testing.T, store *FakeStore, err, wantErr error) {
	t.Helper()
	assertErrorIs(t, err, wantErr)
	if result := store.Transactions(); len(result) != 0 {
		t.Errorf("expected no transactions, got %+v", result)
	}
}

func assertStoredTransactions(t *testing.T, store *FakeStore, err error, want ...domain.Transaction) {
	t.Helper()
	assertNoError(t, err)
	result := store.Transactions()
	if len(result) != len(want) {
		t.Fatalf("expected %d transactions, got %d: %+v", len(want), len(result), result)
	}
	for i := range want {
		if describeTransaction(result[i]) != describeTransaction(want[i]) {
			t.Errorf("expected transaction\n  %s\ngot\n  %s", describeTransaction(want[i]), describeTransaction(result[i]))
		}
	}
}

// describeTransaction writes every field as text, amounts included, so two
// transactions can be compared with == and a difference is easy to read.
func describeTransaction(tx domain.Transaction) string {
	newBalance := "nil"
	if tx.NewBalance != nil {
		newBalance = tx.NewBalance.String()
	}
	return fmt.Sprintf("%s %s %s user=%d bet=%d provider=%s cancels=%q amount=%s %s old=%s new=%s created=%s updated=%s",
		tx.ID, tx.Type, tx.Status, tx.UserID, tx.BetID, tx.ProviderTransactionID, tx.CancelledTransactionID,
		tx.Amount, tx.Currency, tx.OldBalance, newBalance,
		tx.CreatedAt.Format(time.RFC3339Nano), tx.UpdatedAt.Format(time.RFC3339Nano))
}

func describeBet(bet domain.Bet) string {
	return fmt.Sprintf("%d user=%d %s stake=%s %s created=%s updated=%s",
		bet.ID, bet.UserID, bet.Currency, bet.Stake, bet.Status,
		bet.CreatedAt.Format(time.RFC3339Nano), bet.UpdatedAt.Format(time.RFC3339Nano))
}

func assertTransactionID(t *testing.T, result domain.Transaction, err error, want domain.TransactionID) {
	t.Helper()
	assertNoError(t, err)
	if result.ID != want {
		t.Errorf("expected transaction %s, got %s", want, result.ID)
	}
}

func assertStoredBets(t *testing.T, store *FakeStore, err error, want ...domain.Bet) {
	t.Helper()
	assertNoError(t, err)
	result := store.Bets()
	if len(result) != len(want) {
		t.Fatalf("expected %d bets, got %d: %+v", len(want), len(result), result)
	}
	for i := range want {
		if describeBet(result[i]) != describeBet(want[i]) {
			t.Errorf("expected bet\n  %s\ngot\n  %s", describeBet(want[i]), describeBet(result[i]))
		}
	}
}

func assertBetIDs(t *testing.T, result, want []domain.BetID) {
	t.Helper()
	if len(result) != len(want) {
		t.Fatalf("expected bet IDs %v, got %v", want, result)
	}
	for i := range want {
		if result[i] != want[i] {
			t.Errorf("expected bet IDs %v, got %v", want, result)
		}
	}
}
