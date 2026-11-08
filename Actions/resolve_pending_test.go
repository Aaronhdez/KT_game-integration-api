package actions

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
	"github.com/aaronhdez/kt_game-integration-api/Domain/domaintest"
)

// The resolver re-sends with each handler's resendPending, whose outcomes are
// tested with the provider's retries. These tests cover what the resolver
// adds: which transactions it picks, the dispatch by type, the batch and the
// summary.

func TestResolvePending_ResolvesADueTransactionOfEachType(t *testing.T) {
	testCases := []struct {
		name    string
		stored  storeContents
		balance string
		id      domain.TransactionID
	}{
		{"withdraw", storeContents{transactions: []domain.Transaction{aPendingWithdraw(t, "10.50")}}, "5000.00", domaintest.FakeTransactionID(1)},
		{"deposit", theArrangedStoreWith(t, aPendingDeposit(t, "25.00")), testBalanceAfterBet, domaintest.FakeTransactionID(1)},
		{"cancel", theArrangedStoreWith(t, aCancelOfTheWithdraw(t, domain.TransactionPending)), testBalanceAfterBet, domaintest.FakeTransactionID(50)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenAResolver(t, testCase.stored, testCase.balance)

			_, err := fixture.resolver.Execute(context.Background())

			assertStoredStatus(t, fixture.store, err, testCase.id, domain.TransactionCompleted)
		})
	}
}

func TestResolvePending_TransactionWhoseLastAttemptIsRecent_IsNotResent(t *testing.T) {
	recent := aPendingWithdrawNumber(t, 1, resendTime.Add(-resendDelay+time.Second))
	fixture := givenAResolver(t, storeContents{transactions: []domain.Transaction{recent}}, "5000.00")

	fixture.resolver.Execute(context.Background())

	assertWalletCalls(t, fixture.wallet)
}

func TestResolvePending_CountsEachOutcome(t *testing.T) {
	fixture := givenAResolver(t, storeContents{transactions: []domain.Transaction{
		aPendingWithdrawNumber(t, 1, testNow.Add(-3*time.Second)),
		aPendingWithdrawNumber(t, 2, testNow.Add(-2*time.Second)),
		aPendingWithdrawNumber(t, 3, testNow.Add(-time.Second)),
	}}, "5000.00")
	fixture.wallet.Next(domaintest.Succeed, domaintest.Reject, domaintest.TimeOut)

	result, err := fixture.resolver.Execute(context.Background())

	assertSummary(t, result, err, ResolveSummary{Checked: 3, Completed: 1, Failed: 1, StillPending: 1})
}

// A deposit whose bet is missing (inconsistent data) cannot be re-sent; the
// withdraw after it is still resolved.
func TestResolvePending_OneTransactionThatCannotBeResent_DoesNotStopTheOthers(t *testing.T) {
	broken := aPendingDeposit(t, "25.00")
	broken.ID = domaintest.FakeTransactionID(2)
	broken.UpdatedAt = testNow.Add(-time.Second)
	fixture := givenAResolver(t, storeContents{transactions: []domain.Transaction{broken, aPendingWithdraw(t, "10.50")}}, "5000.00")

	result, err := fixture.resolver.Execute(context.Background())

	assertSummary(t, result, err, ResolveSummary{Checked: 2, Completed: 1, Errors: 1})
}

func TestResolvePending_OneTransactionThatCannotBeResent_IsLoggedAsAnError(t *testing.T) {
	broken := aPendingDeposit(t, "25.00")
	fixture := givenAResolver(t, storeContents{transactions: []domain.Transaction{broken}}, "5000.00")

	fixture.resolver.Execute(context.Background())

	assertLogContains(t, fixture.logs, `"level":"ERROR"`, `"msg":"resolve pending: transaction not resolved"`, `"transaction_id":"`+string(broken.ID)+`"`)
}

// The scan runs without the lock: a retry may resolve the transaction before
// the resolver locks the player. It is read again and not re-sent.
func TestResolvePending_TransactionResolvedBetweenTheScanAndTheLock_IsNotResent(t *testing.T) {
	fixture := givenAResolver(t, storeContents{transactions: []domain.Transaction{aCompletedWithdraw(t, "10.50", "4989.50")}}, "5000.00")
	staleScan := staleFinder{transactions: []domain.Transaction{aPendingWithdraw(t, "10.50")}}

	fixture.resolverWith(staleScan).Execute(context.Background())

	assertWalletCalls(t, fixture.wallet)
}

func TestResolvePending_NothingPending_ReturnsAnEmptySummary(t *testing.T) {
	fixture := givenAResolver(t, storeContents{}, "5000.00")

	result, err := fixture.resolver.Execute(context.Background())

	assertSummary(t, result, err, ResolveSummary{})
}

func TestResolvePending_ScanFails_ReturnsItsError(t *testing.T) {
	fixture := givenAResolver(t, storeContents{}, "5000.00")

	_, err := fixture.resolverWith(failingFinder{}).Execute(context.Background())

	assertErrorIs(t, err, errDatabaseDown)
}

func TestResolvePending_ContextDone_StopsBeforeResending(t *testing.T) {
	fixture := givenAResolver(t, storeContents{transactions: []domain.Transaction{aPendingWithdraw(t, "10.50")}}, "5000.00")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fixture.resolverWith(staleFinder{transactions: []domain.Transaction{aPendingWithdraw(t, "10.50")}}).Execute(ctx)

	assertWalletCalls(t, fixture.wallet)
}

// Both take the player's lock and re-read the transaction: whichever comes
// second finds the new attempt recorded and does not re-send.
func TestResolvePending_AndAProviderRetryAtTheSameTime_ResendOnlyOnce(t *testing.T) {
	fixture := givenAResolver(t, storeContents{transactions: []domain.Transaction{aPendingWithdraw(t, "10.50")}}, "5000.00")
	fixture.wallet.Next(domaintest.TimeOut)

	runConcurrently(
		func() { fixture.resolver.Execute(context.Background()) },
		func() { fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50")) },
	)

	assertWithdrawCalls(t, fixture.wallet, 1)
}

/////////////////////////////////////////////////////////////////////////////////////

type resolverFixture struct {
	resolver *ResolvePendingCommandHandler
	withdraw *WithdrawCommandHandler
	deposit  *DepositCommandHandler
	cancel   *CancelCommandHandler
	store    *domaintest.FakeStore
	wallet   *domaintest.FakeWallet
	logs     *bytes.Buffer
}

// givenAResolver runs at resendTime, so a transaction left PENDING at testNow
// is due. The store holds stored; the Wallet holds balance for the test player.
func givenAResolver(t *testing.T, stored storeContents, balance string) resolverFixture {
	t.Helper()
	players := domaintest.NewFakePlayerRepository(aUSDPlayer())
	store := domaintest.NewFakeStore()
	for _, tx := range stored.transactions {
		store.PutTransaction(tx)
	}
	for _, bet := range stored.bets {
		store.PutBet(bet)
	}
	wallet := domaintest.NewFakeWallet(map[domain.UserID]domain.WalletBalance{
		testUserID: {Amount: givenAParsedMoney(t, balance), Currency: domain.USD},
	})
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	clock := domaintest.FixedClock{Time: resendTime}
	ids := &domaintest.FakeIDGenerator{}

	fixture := resolverFixture{
		withdraw: NewWithdrawCommandHandler(players, store, wallet, ids, clock, logger),
		deposit:  NewDepositCommandHandler(players, store, wallet, ids, clock, logger),
		cancel:   NewCancelCommandHandler(players, store, wallet, ids, clock, logger),
		store:    store,
		wallet:   wallet,
		logs:     logs,
	}
	fixture.resolver = fixture.resolverWith(store)
	return fixture
}

// resolverWith builds the resolver with another scan of the pending transactions.
func (fixture resolverFixture) resolverWith(finder domain.PendingTransactionFinder) *ResolvePendingCommandHandler {
	return NewResolvePendingCommandHandler(
		finder,
		fixture.store,
		fixture.withdraw,
		fixture.deposit,
		fixture.cancel,
		domaintest.FixedClock{Time: resendTime},
		slog.New(slog.NewJSONHandler(fixture.logs, nil)),
	)
}

// staleFinder returns a fixed list, whatever the store holds now: the scan as
// it was a moment before.
type staleFinder struct {
	transactions []domain.Transaction
}

func (finder staleFinder) FindPendingBefore(context.Context, time.Time, int) ([]domain.Transaction, error) {
	return finder.transactions, nil
}

type failingFinder struct{}

func (failingFinder) FindPendingBefore(context.Context, time.Time, int) ([]domain.Transaction, error) {
	return nil, errDatabaseDown
}

// aPendingWithdrawNumber is a PENDING withdraw of 10.50 with its own IDs and
// bet, whose last attempt started at updatedAt.
func aPendingWithdrawNumber(t *testing.T, n int, updatedAt time.Time) domain.Transaction {
	t.Helper()
	tx := aPendingWithdraw(t, "10.50")
	tx.ID = domaintest.FakeTransactionID(n)
	tx.BetID = domain.BetID(n)
	tx.ProviderTransactionID = domain.ProviderTransactionID(fmt.Sprintf("prov-tx-resolve-%d", n))
	tx.CreatedAt = updatedAt
	tx.UpdatedAt = updatedAt
	return tx
}

// runConcurrently runs every function from its own goroutine at once and
// waits for all of them.
func runConcurrently(functions ...func()) {
	var wg sync.WaitGroup
	for _, function := range functions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			function()
		}()
	}
	wg.Wait()
}

func assertSummary(t *testing.T, result ResolveSummary, err error, want ResolveSummary) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != want {
		t.Errorf("expected summary %+v, got %+v", want, result)
	}
}

func assertStoredStatus(t *testing.T, store *domaintest.FakeStore, err error, id domain.TransactionID, want domain.TransactionStatus) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, tx := range store.Transactions() {
		if tx.ID == id {
			if tx.Status != want {
				t.Errorf("expected transaction %s to be %s, got %s", id, want, tx.Status)
			}
			return
		}
	}
	t.Fatalf("transaction %s not found in the store", id)
}
