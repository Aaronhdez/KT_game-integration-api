package actions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
	"github.com/aaronhdez/kt_game-integration-api/Domain/domaintest"
)

const (
	testProviderID               = "prov-tx-0001"
	anotherUserID  domain.UserID = 34673635133
)

var errDatabaseDown = errors.New("database down")

// Success

func TestWithdraw_WalletApplies_ReturnsTheCompletedTransaction(t *testing.T) {
	fixture := givenAWithdraw(t)

	result, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertResultTransaction(t, result, err, aCompletedWithdraw(t, "10.50", "4989.50"))
}

func TestWithdraw_WalletApplies_ReturnsBetStatusPlacedAndNotReplayed(t *testing.T) {
	fixture := givenAWithdraw(t)

	result, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertBetStatusAndReplayed(t, result, err, betStatus(domain.BetPlaced), false)
}

func TestWithdraw_WalletApplies_StoresTheTransactionAsCompleted(t *testing.T) {
	fixture := givenAWithdraw(t)

	_, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertStoredTransactions(t, fixture.store, err, aCompletedWithdraw(t, "10.50", "4989.50"))
}

func TestWithdraw_WalletApplies_CreatesAPlacedBetWithTheReservedID(t *testing.T) {
	fixture := givenAWithdraw(t)

	_, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertStoredBets(t, fixture.store, err, aPlacedBet(t, "10.50"))
}

func TestWithdraw_ReadsTheOldBalanceThenSendsTheBetIDAndTheTransactionIDAsReference(t *testing.T) {
	fixture := givenAWithdraw(t)

	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertWalletCalls(t, fixture.wallet, balanceCall(), withdrawCall(t, "10.50"))
}

// Intent first (§9.1): if the process died during the Wallet call, this row
// would still tell us that the call was made.
func TestWithdraw_StoresThePendingTransactionBeforeCallingTheWallet(t *testing.T) {
	fixture := givenAWithdraw(t)
	wallet := &storeObservingWallet{FakeWallet: fixture.wallet, store: fixture.store}

	fixture.withdrawWith(wallet).Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertTransactionsSeenByTheWallet(t, wallet, aPendingWithdraw(t, "10.50"))
}

func TestWithdraw_WalletApplies_LogsTheOutcomeWithTheProviderTransactionID(t *testing.T) {
	fixture := givenAWithdraw(t)

	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertLogContains(t, fixture.logs, `"provider_transaction_id":"prov-tx-0001"`, `"outcome":"completed"`)
}

// Applied, new balance unknown (e.g. a 2xx with an unreadable body)

func TestWithdraw_WalletAppliedButBalanceUnknown_CompletesWithOldBalanceMinusAmount(t *testing.T) {
	fixture := givenAWithdraw(t)
	wallet := balanceUnknownWallet{FakeWallet: fixture.wallet}

	result, err := fixture.withdrawWith(wallet).Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertResultTransaction(t, result, err, aCompletedWithdraw(t, "10.50", "4989.50"))
}

// Unknown outcome (§9.3): never FAILED

var unknownOutcomes = []struct {
	name      string
	behaviour domaintest.Behaviour
}{
	{"error after applying", domaintest.FailAfterApply},
	{"error before applying", domaintest.FailBeforeApply},
	{"timeout", domaintest.TimeOut},
}

func TestWithdraw_WalletOutcomeUnknown_ReturnsPendingWithoutError(t *testing.T) {
	for _, testCase := range unknownOutcomes {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenAWithdraw(t)
			fixture.wallet.Next(domaintest.Succeed, testCase.behaviour)

			result, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

			assertResultTransaction(t, result, err, aPendingWithdraw(t, "10.50"))
		})
	}
}

func TestWithdraw_WalletOutcomeUnknown_ReturnsNoBetStatus(t *testing.T) {
	for _, testCase := range unknownOutcomes {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenAWithdraw(t)
			fixture.wallet.Next(domaintest.Succeed, testCase.behaviour)

			result, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

			assertBetStatusAndReplayed(t, result, err, nil, false)
		})
	}
}

func TestWithdraw_WalletOutcomeUnknown_KeepsThePendingTransactionAndCreatesNoBet(t *testing.T) {
	for _, testCase := range unknownOutcomes {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenAWithdraw(t)
			fixture.wallet.Next(domaintest.Succeed, testCase.behaviour)

			fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

			assertStoredTransactionsAndNoBets(t, fixture.store, aPendingWithdraw(t, "10.50"))
		})
	}
}

func TestWithdraw_WalletTooSlowForTheCaller_ReturnsPendingWithoutError(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.wallet.Next(domaintest.Succeed, domaintest.Slow)

	result, err := fixture.withdraw.Execute(givenACallerGivingUpAfter(t, 20*time.Millisecond), aWithdrawCommand(t, "10.50"))

	assertResultTransaction(t, result, err, aPendingWithdraw(t, "10.50"))
}

// Not applied (§8.3, §9.3): FAILED record kept, no bet

func TestWithdraw_InsufficientFunds_IsInsufficientFunds(t *testing.T) {
	fixture := givenAWithdraw(t)

	_, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "6000.00"))

	assertErrorIs(t, err, domain.ErrInsufficientFunds)
}

func TestWithdraw_InsufficientFunds_KeepsAFailedTransactionAndCreatesNoBet(t *testing.T) {
	fixture := givenAWithdraw(t)

	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "6000.00"))

	assertStoredTransactionsAndNoBets(t, fixture.store, aFailedWithdraw(t, "6000.00"))
}

func TestWithdraw_WalletRejects_IsWalletRejected(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.wallet.Next(domaintest.Succeed, domaintest.Reject)

	_, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertErrorIs(t, err, domain.ErrWalletRejected)
}

func TestWithdraw_WalletRejects_KeepsAFailedTransactionAndCreatesNoBet(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.wallet.Next(domaintest.Succeed, domaintest.Reject)

	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertStoredTransactionsAndNoBets(t, fixture.store, aFailedWithdraw(t, "10.50"))
}

// Validation (§7.3, §8.3): before any Wallet call

var invalidWithdrawCommands = []struct {
	name                  string
	providerTransactionID string
	amount                string
	currency              string
	want                  error
}{
	{"empty provider transaction id", "", "10.50", "USD", domain.ErrInvalidProviderTransactionID},
	{"blank provider transaction id", "   ", "10.50", "USD", domain.ErrInvalidProviderTransactionID},
	{"provider transaction id too long", strings.Repeat("x", 129), "10.50", "USD", domain.ErrInvalidProviderTransactionID},
	{"zero amount", testProviderID, "0", "USD", domain.ErrInvalidAmount},
	{"lowercase currency", testProviderID, "10.50", "usd", domain.ErrInvalidCurrency},
	{"unsupported currency", testProviderID, "10.50", "GBP", domain.ErrInvalidCurrency},
	{"empty currency", testProviderID, "10.50", "", domain.ErrInvalidCurrency},
	{"currency of another wallet", testProviderID, "10.50", "EUR", domain.ErrCurrencyMismatch},
}

func TestWithdraw_InvalidCommand_IsItsInvalidInputError(t *testing.T) {
	for _, testCase := range invalidWithdrawCommands {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenAWithdraw(t)
			command := aWithdrawCommandWith(t, testCase.providerTransactionID, testCase.amount, testCase.currency)

			_, err := fixture.withdraw.Execute(context.Background(), command)

			assertErrorIs(t, err, testCase.want)
		})
	}
}

func TestWithdraw_InvalidCommand_DoesNotCallTheWalletNorStoreAnything(t *testing.T) {
	for _, testCase := range invalidWithdrawCommands {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenAWithdraw(t)
			command := aWithdrawCommandWith(t, testCase.providerTransactionID, testCase.amount, testCase.currency)

			fixture.withdraw.Execute(context.Background(), command)

			assertNothingHappened(t, fixture)
		})
	}
}

// Player

func TestWithdraw_UnknownPlayer_IsPlayerNotFound(t *testing.T) {
	fixture := givenAWithdrawWithoutPlayers(t)

	_, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertErrorIs(t, err, domain.ErrPlayerNotFound)
}

func TestWithdraw_UnknownPlayer_DoesNotCallTheWalletNorStoreAnything(t *testing.T) {
	fixture := givenAWithdrawWithoutPlayers(t)

	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertNothingHappened(t, fixture)
}

func TestWithdraw_PlayerRepositoryFails_ReturnsItsError(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.players.Err = errDatabaseDown

	_, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertErrorIs(t, err, errDatabaseDown)
}

// Old balance read: nothing has been sent yet, so a failure is "not applied"
// (502) and never a timeout (§7.3: 504 is only for reads such as GET /player).

var balanceReadFailures = []struct {
	name      string
	behaviour domaintest.Behaviour
}{
	{"failure", domaintest.Reject},
	{"timeout", domaintest.TimeOut},
}

func TestWithdraw_BalanceReadFails_IsWalletFailureNeverTimeout(t *testing.T) {
	for _, testCase := range balanceReadFailures {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenAWithdraw(t)
			fixture.wallet.Next(testCase.behaviour)

			_, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

			assertWalletFailureNotTimeout(t, err)
		})
	}
}

func TestWithdraw_BalanceReadFails_DoesNotWithdrawNorStoreAnything(t *testing.T) {
	for _, testCase := range balanceReadFailures {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenAWithdraw(t)
			fixture.wallet.Next(testCase.behaviour)

			fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

			assertOnlyTheBalanceWasReadAndNothingStored(t, fixture)
		})
	}
}

func TestWithdraw_WalletBalanceInAnotherCurrency_IsWalletFailure(t *testing.T) {
	fixture := givenAWithdrawWithWalletCurrency(t, domain.EUR)

	_, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertErrorIs(t, err, domain.ErrWalletFailure)
}

func TestWithdraw_WalletBalanceInAnotherCurrency_DoesNotWithdrawNorStoreAnything(t *testing.T) {
	fixture := givenAWithdrawWithWalletCurrency(t, domain.EUR)

	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertOnlyTheBalanceWasReadAndNothingStored(t, fixture)
}

// Database failures

func TestWithdraw_PendingInsertFails_ReturnsItsError(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.store.InsertTransactionErr = errDatabaseDown

	_, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertErrorIs(t, err, errDatabaseDown)
}

// Without a stored PENDING row, a Wallet call could be applied with no trace
// of it in our database (§9.1).
func TestWithdraw_PendingInsertFails_DoesNotCallTheWalletWithdraw(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.store.InsertTransactionErr = errDatabaseDown

	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertWalletCalls(t, fixture.wallet, balanceCall())
}

// The Wallet applied the withdraw but the outcome could not be saved: the
// database still says PENDING, so that is what the API reports (202).
func TestWithdraw_OutcomeWriteFails_ReturnsPendingWithoutError(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.store.UpdateTransactionErr = errDatabaseDown

	result, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertResultTransaction(t, result, err, aPendingWithdraw(t, "10.50"))
}

func TestWithdraw_OutcomeWriteFails_KeepsThePendingTransactionAndCreatesNoBet(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.store.UpdateTransactionErr = errDatabaseDown

	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertStoredTransactionsAndNoBets(t, fixture.store, aPendingWithdraw(t, "10.50"))
}

// The provider may hang up while we wait for the Wallet; the Wallet may still
// have taken the money, so the outcome must be stored anyway.
func TestWithdraw_CallerLeavesDuringTheWalletCall_StillStoresTheOutcome(t *testing.T) {
	fixture := givenAWithdraw(t)
	ctx, leave := context.WithCancel(context.Background())
	defer leave()
	wallet := callerLeavingWallet{FakeWallet: fixture.wallet, leave: leave}

	fixture.withdrawWith(wallet).Execute(ctx, aWithdrawCommand(t, "10.50"))

	assertStoredTransactions(t, fixture.store, nil, aCompletedWithdraw(t, "10.50", "4989.50"))
}

// Idempotency (§7.1, §7.2, §9.2): a replay never reaches the Wallet

func TestWithdraw_SameRequestTwice_SecondIsAReplayOfTheCompletedTransaction(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	result, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertReplayOf(t, result, err, aCompletedWithdraw(t, "10.50", "4989.50"), betStatus(domain.BetPlaced))
}

func TestWithdraw_SameRequestTwice_CallsTheWalletOnlyForTheFirst(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertWalletCalls(t, fixture.wallet, balanceCall(), withdrawCall(t, "10.50"))
}

func TestWithdraw_SameRequestTwice_StoresOneTransactionAndOneBet(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	_, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertStoredTransactionAndBet(t, fixture.store, err, aCompletedWithdraw(t, "10.50", "4989.50"), aPlacedBet(t, "10.50"))
}

// §7.2: a replay "reflects the transaction's current state".
func TestWithdraw_ReplayOfACompletedWithdraw_ReturnsTheBetsCurrentStatus(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.store.PutTransaction(aCompletedWithdraw(t, "10.50", "4989.50"))
	fixture.store.PutBet(aBetWithStatus(t, "10.50", domain.BetWon))

	result, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertReplayOf(t, result, err, aCompletedWithdraw(t, "10.50", "4989.50"), betStatus(domain.BetWon))
}

func TestWithdraw_ReplayOfACancelledWithdraw_ReturnsItsCurrentState(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.store.PutTransaction(aCancelledWithdraw(t, "10.50"))
	fixture.store.PutBet(aBetWithStatus(t, "10.50", domain.BetCancelled))

	result, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertReplayOf(t, result, err, aCancelledWithdraw(t, "10.50"), betStatus(domain.BetCancelled))
}

func TestWithdraw_ReplayOfAPendingWithdraw_ReturnsItPendingWithoutBetStatus(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.store.PutTransaction(aPendingWithdraw(t, "10.50"))

	result, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertReplayOf(t, result, err, aPendingWithdraw(t, "10.50"), nil)
}

// Resolving a PENDING withdraw (re-sending it to the Wallet) is Phase 11.
func TestWithdraw_ReplayOfAPendingWithdraw_DoesNotCallTheWallet(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.store.PutTransaction(aPendingWithdraw(t, "10.50"))

	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertWalletCalls(t, fixture.wallet)
}

func TestWithdraw_ReplayOfAFailedWithdraw_IsPreviousAttemptFailed(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.store.PutTransaction(aFailedWithdraw(t, "10.50"))

	_, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertErrorIs(t, err, domain.ErrPreviousAttemptFailed)
}

func TestWithdraw_ReplayOfAFailedWithdraw_DoesNotCallTheWallet(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.store.PutTransaction(aFailedWithdraw(t, "10.50"))

	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertWalletCalls(t, fixture.wallet)
}

func TestWithdraw_SameProviderIDWithAnotherAmount_IsIdempotencyConflict(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.store.PutTransaction(aCompletedWithdraw(t, "10.50", "4989.50"))

	_, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "11.00"))

	assertErrorIs(t, err, domain.ErrIdempotencyConflict)
}

func TestWithdraw_SameProviderIDWithAnotherAmount_DoesNotCallTheWallet(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.store.PutTransaction(aCompletedWithdraw(t, "10.50", "4989.50"))

	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "11.00"))

	assertWalletCalls(t, fixture.wallet)
}

// A deposit with the same provider ID, amount and currency is another request,
// not an earlier attempt of this withdraw.
func TestWithdraw_ProviderIDOfADeposit_IsIdempotencyConflict(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.store.PutTransaction(aDepositWithTheTestProviderID(t, "10.50"))
	fixture.store.PutBet(aBetWithStatus(t, "10.50", domain.BetWon))

	_, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertErrorIs(t, err, domain.ErrIdempotencyConflict)
}

func TestWithdraw_ProviderIDOfADeposit_DoesNotCallTheWallet(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.store.PutTransaction(aDepositWithTheTestProviderID(t, "10.50"))
	fixture.store.PutBet(aBetWithStatus(t, "10.50", domain.BetWon))

	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertWalletCalls(t, fixture.wallet)
}

// §6.1: provider transaction IDs are unique across the whole system. The
// other player's transaction is never returned, only "the ID is taken".
func TestWithdraw_ProviderIDOfAnotherPlayer_IsTransactionExists(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.store.PutTransaction(anotherPlayersWithdraw(t))

	_, err := fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertErrorIs(t, err, domain.ErrTransactionExists)
}

func TestWithdraw_ProviderIDOfAnotherPlayer_DoesNotCallTheWalletWithdraw(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.store.PutTransaction(anotherPlayersWithdraw(t))

	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertWalletCalls(t, fixture.wallet, balanceCall())
}

func TestWithdraw_ProviderIDOfAnotherPlayer_LeavesTheirTransactionUntouched(t *testing.T) {
	fixture := givenAWithdraw(t)
	fixture.store.PutTransaction(anotherPlayersWithdraw(t))

	fixture.withdraw.Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertStoredTransactionsAndNoBets(t, fixture.store, anotherPlayersWithdraw(t))
}

// The safety net: an identical request stored by someone else between our
// lookup and our insert (only possible if the lock failed, e.g. two API
// instances) is still a replay, thanks to the unique index.
func TestWithdraw_IdenticalRequestStoredMeanwhile_IsAReplayWithoutWithdrawing(t *testing.T) {
	fixture := givenAWithdraw(t)
	wallet := &racingWallet{FakeWallet: fixture.wallet, store: fixture.store, stored: aWithdrawStoredByAnotherInstance(t)}
	fixture.store.PutBet(aBetStoredByAnotherInstance(t))

	result, err := fixture.withdrawWith(wallet).Execute(context.Background(), aWithdrawCommand(t, "10.50"))

	assertReplayOf(t, result, err, aWithdrawStoredByAnotherInstance(t), betStatus(domain.BetPlaced))
}

// §9.2: "including when two identical requests arrive at the same time".
func TestWithdraw_ConcurrentIdenticalRequests_CallTheWalletWithdrawOnce(t *testing.T) {
	fixture := givenAWithdraw(t)

	executeConcurrently(t, fixture.withdraw, aWithdrawCommand(t, "10.50"), 10)

	assertWithdrawCalls(t, fixture.wallet, 1)
}

func TestWithdraw_ConcurrentIdenticalRequests_OneIsProcessedAndTheOthersAreReplays(t *testing.T) {
	fixture := givenAWithdraw(t)

	result := executeConcurrently(t, fixture.withdraw, aWithdrawCommand(t, "10.50"), 10)

	assertProcessedAndReplayed(t, result, 1, 9)
}

/////////////////////////////////////////////////////////////////////////////////////

// withdrawFixture keeps the fakes reachable, so a test can script or break one
// of them and inspect what it received or stored.
type withdrawFixture struct {
	withdraw *WithdrawCommandHandler
	players  *domaintest.FakePlayerRepository
	store    *domaintest.FakeStore
	wallet   *domaintest.FakeWallet
	logs     *bytes.Buffer
}

func givenAWithdraw(t *testing.T) withdrawFixture {
	t.Helper()
	return newWithdrawFixture(t, domaintest.NewFakePlayerRepository(aUSDPlayer()), domain.USD)
}

func givenAWithdrawWithoutPlayers(t *testing.T) withdrawFixture {
	t.Helper()
	return newWithdrawFixture(t, domaintest.NewFakePlayerRepository(), domain.USD)
}

func givenAWithdrawWithWalletCurrency(t *testing.T, walletCurrency domain.Currency) withdrawFixture {
	t.Helper()
	return newWithdrawFixture(t, domaintest.NewFakePlayerRepository(aUSDPlayer()), walletCurrency)
}

func newWithdrawFixture(t *testing.T, players *domaintest.FakePlayerRepository, walletCurrency domain.Currency) withdrawFixture {
	t.Helper()
	fixture := withdrawFixture{
		players: players,
		store:   domaintest.NewFakeStore(),
		wallet: domaintest.NewFakeWallet(map[domain.UserID]domain.WalletBalance{
			testUserID: {Amount: givenAParsedMoney(t, "5000.00"), Currency: walletCurrency},
		}),
		logs: &bytes.Buffer{},
	}
	fixture.withdraw = fixture.withdrawWith(fixture.wallet)
	return fixture
}

// withdrawWith builds the handler with another Wallet (a wrapper of the fake).
func (fixture withdrawFixture) withdrawWith(wallet domain.WalletClient) *WithdrawCommandHandler {
	return NewWithdrawCommandHandler(
		fixture.players,
		fixture.store,
		wallet,
		&domaintest.FakeIDGenerator{},
		domaintest.FixedClock{Time: testNow},
		slog.New(slog.NewJSONHandler(fixture.logs, nil)),
	)
}

func aUSDPlayer() domain.Player {
	return domain.Player{UserID: testUserID, Username: testUsername, Currency: domain.USD}
}

func aWithdrawCommand(t *testing.T, amount string) WithdrawCommand {
	t.Helper()
	return aWithdrawCommandWith(t, testProviderID, amount, "USD")
}

func aWithdrawCommandWith(t *testing.T, providerTransactionID, amount, currency string) WithdrawCommand {
	t.Helper()
	return WithdrawCommand{
		UserID:                testUserID,
		ProviderTransactionID: providerTransactionID,
		Amount:                givenAParsedMoney(t, amount),
		Currency:              currency,
	}
}

func givenAParsedMoney(t *testing.T, value string) domain.Money {
	t.Helper()
	money, err := domain.ParseMoney(value)
	if err != nil {
		t.Fatalf("invalid test amount %q: %v", value, err)
	}
	return money
}

func givenACallerGivingUpAfter(t *testing.T, timeout time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx
}

func betStatus(status domain.BetStatus) *domain.BetStatus {
	return &status
}

// Expected values: the first transaction of the FakeIDGenerator, the first bet
// ID of the FakeStore, an old balance of 5000.00 and the fixed clock.

func aPendingWithdraw(t *testing.T, amount string) domain.Transaction {
	t.Helper()
	return domain.Transaction{
		ID:                    domaintest.FakeTransactionID(1),
		Type:                  domain.TransactionWithdraw,
		Status:                domain.TransactionPending,
		UserID:                testUserID,
		BetID:                 1,
		ProviderTransactionID: testProviderID,
		Amount:                givenAParsedMoney(t, amount),
		Currency:              domain.USD,
		OldBalance:            givenAParsedMoney(t, "5000.00"),
		CreatedAt:             testNow,
		UpdatedAt:             testNow,
	}
}

func aCompletedWithdraw(t *testing.T, amount, newBalance string) domain.Transaction {
	t.Helper()
	tx := aPendingWithdraw(t, amount)
	balance := givenAParsedMoney(t, newBalance)
	tx.Status = domain.TransactionCompleted
	tx.NewBalance = &balance
	return tx
}

func aFailedWithdraw(t *testing.T, amount string) domain.Transaction {
	t.Helper()
	tx := aPendingWithdraw(t, amount)
	tx.Status = domain.TransactionFailed
	return tx
}

func aCancelledWithdraw(t *testing.T, amount string) domain.Transaction {
	t.Helper()
	tx := aCompletedWithdraw(t, amount, "4989.50")
	tx.Status = domain.TransactionCancelled
	return tx
}

// anotherPlayersWithdraw uses the same provider transaction ID as the test
// commands, for another player.
func anotherPlayersWithdraw(t *testing.T) domain.Transaction {
	t.Helper()
	tx := aCompletedWithdraw(t, "10.50", "31404.75")
	tx.ID = domaintest.FakeTransactionID(99)
	tx.UserID = anotherUserID
	tx.BetID = 99
	tx.OldBalance = givenAParsedMoney(t, "31415.25")
	return tx
}

// aDepositWithTheTestProviderID is a completed deposit of the test player that
// uses the provider transaction ID of the test withdraw commands.
func aDepositWithTheTestProviderID(t *testing.T, amount string) domain.Transaction {
	t.Helper()
	tx := aCompletedWithdraw(t, amount, "5010.50")
	tx.ID = domaintest.FakeTransactionID(70)
	tx.Type = domain.TransactionDeposit
	return tx
}

// aWithdrawStoredByAnotherInstance is the same request processed elsewhere:
// same player, provider ID and amount, but its own transaction and bet IDs.
func aWithdrawStoredByAnotherInstance(t *testing.T) domain.Transaction {
	t.Helper()
	tx := aCompletedWithdraw(t, "10.50", "4989.50")
	tx.ID = domaintest.FakeTransactionID(50)
	tx.BetID = 50
	return tx
}

func aBetStoredByAnotherInstance(t *testing.T) domain.Bet {
	t.Helper()
	bet := aPlacedBet(t, "10.50")
	bet.ID = 50
	return bet
}

func aPlacedBet(t *testing.T, stake string) domain.Bet {
	t.Helper()
	return domain.NewPlacedBet(1, testUserID, domain.USD, givenAParsedMoney(t, stake), testNow)
}

func aBetWithStatus(t *testing.T, stake string, status domain.BetStatus) domain.Bet {
	t.Helper()
	bet := aPlacedBet(t, stake)
	bet.Status = status
	return bet
}

// executeConcurrently runs the same command from count goroutines at once and
// returns every result. Each one must succeed.
func executeConcurrently(t *testing.T, withdraw *WithdrawCommandHandler, command WithdrawCommand, count int) []TransactionResult {
	t.Helper()
	results := make([]TransactionResult, count)
	errs := make([]error, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = withdraw.Execute(context.Background(), command)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
	}
	return results
}

func balanceCall() domaintest.Call {
	return domaintest.Call{Method: "Balance", UserID: testUserID}
}

func withdrawCall(t *testing.T, amount string) domaintest.Call {
	t.Helper()
	return domaintest.Call{
		Method: "Withdraw",
		UserID: testUserID,
		Operation: domain.WalletOperation{
			UserID:    testUserID,
			Currency:  domain.USD,
			Amount:    givenAParsedMoney(t, amount),
			BetID:     1,
			Reference: domaintest.FakeTransactionID(1),
		},
	}
}

// Wallet wrappers: each one changes a single thing of the FakeWallet.

// storeObservingWallet records what the store held when Withdraw or Deposit
// was called.
type storeObservingWallet struct {
	*domaintest.FakeWallet
	store *domaintest.FakeStore
	seen  []domain.Transaction
}

func (wallet *storeObservingWallet) Withdraw(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	wallet.seen = wallet.store.Transactions()
	return wallet.FakeWallet.Withdraw(ctx, operation)
}

func (wallet *storeObservingWallet) Deposit(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	wallet.seen = wallet.store.Transactions()
	return wallet.FakeWallet.Deposit(ctx, operation)
}

// balanceUnknownWallet applies the operation but reports that the new balance
// is unknown, as the real client does for a 2xx with an unusable body.
type balanceUnknownWallet struct {
	*domaintest.FakeWallet
}

func (wallet balanceUnknownWallet) Withdraw(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	if _, err := wallet.FakeWallet.Withdraw(ctx, operation); err != nil {
		return domain.Money{}, err
	}
	return domain.Money{}, fmt.Errorf("test wallet: %w", domain.ErrWalletAppliedBalanceUnknown)
}

func (wallet balanceUnknownWallet) Deposit(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	if _, err := wallet.FakeWallet.Deposit(ctx, operation); err != nil {
		return domain.Money{}, err
	}
	return domain.Money{}, fmt.Errorf("test wallet: %w", domain.ErrWalletAppliedBalanceUnknown)
}

// callerLeavingWallet cancels the caller's context while the Wallet applies
// the operation, as when the provider closes the connection.
type callerLeavingWallet struct {
	*domaintest.FakeWallet
	leave context.CancelFunc
}

func (wallet callerLeavingWallet) Withdraw(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	wallet.leave()
	return wallet.FakeWallet.Withdraw(ctx, operation)
}

func (wallet callerLeavingWallet) Deposit(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	wallet.leave()
	return wallet.FakeWallet.Deposit(ctx, operation)
}

// racingWallet stores an identical withdraw when the old balance is read,
// i.e. after our idempotency lookup and before our insert, as if another
// API instance had processed the same request at the same time.
type racingWallet struct {
	*domaintest.FakeWallet
	store  *domaintest.FakeStore
	stored domain.Transaction
}

func (wallet *racingWallet) Balance(ctx context.Context, userID domain.UserID) (domain.WalletBalance, error) {
	wallet.store.PutTransaction(wallet.stored)
	return wallet.FakeWallet.Balance(ctx, userID)
}

// Assertions

func assertReplayOf(t *testing.T, result TransactionResult, err error, want domain.Transaction, wantBetStatus *domain.BetStatus) {
	t.Helper()
	assertResultTransaction(t, result, err, want)
	assertBetStatusAndReplayed(t, result, err, wantBetStatus, true)
}

func assertStoredTransactionAndBet(t *testing.T, store *domaintest.FakeStore, err error, wantTransaction domain.Transaction, wantBet domain.Bet) {
	t.Helper()
	assertStoredTransactions(t, store, err, wantTransaction)
	assertStoredBets(t, store, err, wantBet)
}

func assertWithdrawCalls(t *testing.T, wallet *domaintest.FakeWallet, want int) {
	t.Helper()
	withdraws := 0
	for _, call := range wallet.Calls() {
		if call.Method == "Withdraw" {
			withdraws++
		}
	}
	if withdraws != want {
		t.Errorf("expected %d Wallet withdraw(s), got %d", want, withdraws)
	}
}

func assertProcessedAndReplayed(t *testing.T, result []TransactionResult, wantProcessed, wantReplayed int) {
	t.Helper()
	processed, replayed := 0, 0
	for _, r := range result {
		if r.Replayed {
			replayed++
		} else {
			processed++
		}
	}
	if processed != wantProcessed || replayed != wantReplayed {
		t.Errorf("expected %d processed and %d replayed, got %d and %d", wantProcessed, wantReplayed, processed, replayed)
	}
}

func assertResultTransaction(t *testing.T, result TransactionResult, err error, want domain.Transaction) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if describeTransaction(result.Transaction) != describeTransaction(want) {
		t.Errorf("expected transaction\n  %s\ngot\n  %s", describeTransaction(want), describeTransaction(result.Transaction))
	}
}

func assertBetStatusAndReplayed(t *testing.T, result TransactionResult, err error, wantBetStatus *domain.BetStatus, wantReplayed bool) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if describeBetStatus(result.BetStatus) != describeBetStatus(wantBetStatus) {
		t.Errorf("expected bet status %s, got %s", describeBetStatus(wantBetStatus), describeBetStatus(result.BetStatus))
	}
	if result.Replayed != wantReplayed {
		t.Errorf("expected replayed %t, got %t", wantReplayed, result.Replayed)
	}
}

func assertStoredTransactions(t *testing.T, store *domaintest.FakeStore, err error, want ...domain.Transaction) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertTransactions(t, store.Transactions(), want)
}

func assertStoredTransactionsAndNoBets(t *testing.T, store *domaintest.FakeStore, want ...domain.Transaction) {
	t.Helper()
	assertTransactions(t, store.Transactions(), want)
	if bets := store.Bets(); len(bets) != 0 {
		t.Errorf("expected no bets, got %d", len(bets))
	}
}

func assertTransactionsSeenByTheWallet(t *testing.T, wallet *storeObservingWallet, want ...domain.Transaction) {
	t.Helper()
	assertTransactions(t, wallet.seen, want)
}

func assertTransactions(t *testing.T, result, want []domain.Transaction) {
	t.Helper()
	if len(result) != len(want) {
		t.Fatalf("expected %d transaction(s), got %d: %v", len(want), len(result), describeTransactions(result))
	}
	for i := range want {
		if describeTransaction(result[i]) != describeTransaction(want[i]) {
			t.Errorf("expected transaction\n  %s\ngot\n  %s", describeTransaction(want[i]), describeTransaction(result[i]))
		}
	}
}

func assertStoredBets(t *testing.T, store *domaintest.FakeStore, err error, want ...domain.Bet) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := store.Bets()
	if len(result) != len(want) {
		t.Fatalf("expected %d bet(s), got %d", len(want), len(result))
	}
	for i := range want {
		if describeBet(result[i]) != describeBet(want[i]) {
			t.Errorf("expected bet\n  %s\ngot\n  %s", describeBet(want[i]), describeBet(result[i]))
		}
	}
}

func assertWalletCalls(t *testing.T, wallet *domaintest.FakeWallet, want ...domaintest.Call) {
	t.Helper()
	result := wallet.Calls()
	if len(result) != len(want) {
		t.Fatalf("expected %d Wallet call(s), got %d: %v", len(want), len(result), describeCalls(result))
	}
	for i := range want {
		if describeCall(result[i]) != describeCall(want[i]) {
			t.Errorf("Wallet call %d: expected %s, got %s", i, describeCall(want[i]), describeCall(result[i]))
		}
	}
}

func assertNothingHappened(t *testing.T, fixture withdrawFixture) {
	t.Helper()
	if calls := fixture.wallet.Calls(); len(calls) != 0 {
		t.Errorf("expected no Wallet calls, got %v", describeCalls(calls))
	}
	if transactions := fixture.store.Transactions(); len(transactions) != 0 {
		t.Errorf("expected nothing stored, got %v", describeTransactions(transactions))
	}
}

func assertOnlyTheBalanceWasReadAndNothingStored(t *testing.T, fixture withdrawFixture) {
	t.Helper()
	assertWalletCalls(t, fixture.wallet, balanceCall())
	if transactions := fixture.store.Transactions(); len(transactions) != 0 {
		t.Errorf("expected nothing stored, got %v", describeTransactions(transactions))
	}
}

func assertWalletFailureNotTimeout(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, domain.ErrWalletFailure) || errors.Is(err, domain.ErrWalletTimeout) {
		t.Errorf("expected %v to be %q and not %q", err, domain.ErrWalletFailure, domain.ErrWalletTimeout)
	}
}

func assertLogContains(t *testing.T, logs *bytes.Buffer, want ...string) {
	t.Helper()
	for _, fragment := range want {
		if !strings.Contains(logs.String(), fragment) {
			t.Errorf("expected the log to contain %s, got:\n%s", fragment, logs.String())
		}
	}
}

// Descriptions: every field as text, amounts included, so values holding
// Money can be compared with == and a difference is easy to read.

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

func describeTransactions(transactions []domain.Transaction) []string {
	descriptions := make([]string, 0, len(transactions))
	for _, tx := range transactions {
		descriptions = append(descriptions, describeTransaction(tx))
	}
	return descriptions
}

func describeBet(bet domain.Bet) string {
	return fmt.Sprintf("%d user=%d %s stake=%s %s created=%s updated=%s",
		bet.ID, bet.UserID, bet.Currency, bet.Stake, bet.Status,
		bet.CreatedAt.Format(time.RFC3339Nano), bet.UpdatedAt.Format(time.RFC3339Nano))
}

func describeBetStatus(status *domain.BetStatus) string {
	if status == nil {
		return "nil"
	}
	return string(*status)
}

func describeCall(call domaintest.Call) string {
	operation := call.Operation
	return fmt.Sprintf("%s user=%d op{user=%d %s amount=%s bet=%d reference=%s}",
		call.Method, call.UserID, operation.UserID, operation.Currency, operation.Amount, operation.BetID, operation.Reference)
}

func describeCalls(calls []domaintest.Call) []string {
	descriptions := make([]string, 0, len(calls))
	for _, call := range calls {
		descriptions = append(descriptions, describeCall(call))
	}
	return descriptions
}
