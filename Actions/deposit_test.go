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

// The deposit tests settle the bet of one stored withdraw: provider ID
// testProviderID (shared with the withdraw tests), bet testBetID.
const (
	testDepositProviderID              = "prov-tx-0002"
	testBetID             domain.BetID = 7
	testWithdrawNumber                 = 100 // its transaction ID is FakeTransactionID(100)
	unknownProviderID                  = "prov-tx-unknown"
	testWithdrawnStake                 = "10.50"
	testBalanceAfterBet                = "4989.50"
)

// Success: the Wallet pays, the deposit completes and the bet is WON

func TestDeposit_WalletApplies_ReturnsTheCompletedDeposit(t *testing.T) {
	fixture := givenADeposit(t)

	result, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertResultTransaction(t, result, err, aCompletedDeposit(t, "25.00", "5014.50"))
}

func TestDeposit_WalletApplies_ReturnsBetStatusWonAndNotReplayed(t *testing.T) {
	fixture := givenADeposit(t)

	result, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertBetStatusAndReplayed(t, result, err, betStatus(domain.BetWon), false)
}

func TestDeposit_WalletApplies_StoresTheDepositAsCompletedAndTheBetAsWon(t *testing.T) {
	fixture := givenADeposit(t)

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertStoreHolds(t, fixture.store, storeContents{
		transactions: []domain.Transaction{aCompletedDeposit(t, "25.00", "5014.50"), theWithdraw(t, domain.TransactionCompleted)},
		bets:         []domain.Bet{theBet(t, domain.BetWon)},
	})
}

func TestDeposit_ReadsTheOldBalanceThenSendsTheWithdrawsBetIDAndTheDepositIDAsReference(t *testing.T) {
	fixture := givenADeposit(t)

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertWalletCalls(t, fixture.wallet, balanceCall(), depositCall(t, "25.00"))
}

// Intent first.
func TestDeposit_StoresThePendingDepositBeforeCallingTheWallet(t *testing.T) {
	fixture := givenADeposit(t)
	wallet := &storeObservingWallet{FakeWallet: fixture.wallet, store: fixture.store}

	fixture.depositWith(wallet).Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertTransactionsSeenByTheWallet(t, wallet, aPendingDeposit(t, "25.00"), theWithdraw(t, domain.TransactionCompleted))
}

func TestDeposit_WalletApplies_LogsTheOutcomeWithTheProviderTransactionID(t *testing.T) {
	fixture := givenADeposit(t)

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertLogContains(t, fixture.logs, `"msg":"deposit"`, `"provider_transaction_id":"prov-tx-0002"`, `"bet_id":7`, `"outcome":"completed"`)
}

// Applied, new balance unknown (e.g. a 2xx with an unreadable body)

func TestDeposit_WalletAppliedButBalanceUnknown_CompletesWithOldBalancePlusAmount(t *testing.T) {
	fixture := givenADeposit(t)
	wallet := balanceUnknownWallet{FakeWallet: fixture.wallet}

	result, err := fixture.depositWith(wallet).Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertResultTransaction(t, result, err, aCompletedDeposit(t, "25.00", "5014.50"))
}

// Unknown outcome: never FAILED, the bet stays PLACED

func TestDeposit_WalletOutcomeUnknown_ReturnsPendingWithoutError(t *testing.T) {
	for _, testCase := range unknownOutcomes {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenADeposit(t)
			fixture.wallet.Next(domaintest.Succeed, testCase.behaviour)

			result, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

			assertResultTransaction(t, result, err, aPendingDeposit(t, "25.00"))
		})
	}
}

// betStatus is null only while a withdraw is PENDING; here the bet exists.
func TestDeposit_WalletOutcomeUnknown_ReturnsBetStatusPlaced(t *testing.T) {
	for _, testCase := range unknownOutcomes {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenADeposit(t)
			fixture.wallet.Next(domaintest.Succeed, testCase.behaviour)

			result, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

			assertBetStatusAndReplayed(t, result, err, betStatus(domain.BetPlaced), false)
		})
	}
}

func TestDeposit_WalletOutcomeUnknown_KeepsThePendingDepositAndTheBetPlaced(t *testing.T) {
	for _, testCase := range unknownOutcomes {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenADeposit(t)
			fixture.wallet.Next(domaintest.Succeed, testCase.behaviour)

			fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

			assertStoreHolds(t, fixture.store, theArrangedStoreWith(t, aPendingDeposit(t, "25.00")))
		})
	}
}

func TestDeposit_WalletTooSlowForTheCaller_ReturnsPendingWithoutError(t *testing.T) {
	fixture := givenADeposit(t)
	fixture.wallet.Next(domaintest.Succeed, domaintest.Slow)

	result, err := fixture.deposit.Execute(givenACallerGivingUpAfter(t, 20*time.Millisecond), aDepositCommand(t, "25.00"))

	assertResultTransaction(t, result, err, aPendingDeposit(t, "25.00"))
}

// Not applied: FAILED record kept, the bet stays PLACED

func TestDeposit_WalletRejects_IsWalletRejected(t *testing.T) {
	fixture := givenADeposit(t)
	fixture.wallet.Next(domaintest.Succeed, domaintest.Reject)

	_, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertErrorIs(t, err, domain.ErrWalletRejected)
}

func TestDeposit_WalletRejects_KeepsAFailedDepositAndTheBetPlaced(t *testing.T) {
	fixture := givenADeposit(t)
	fixture.wallet.Next(domaintest.Succeed, domaintest.Reject)

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertStoreHolds(t, fixture.store, theArrangedStoreWith(t, aFailedDeposit(t, "25.00", domain.FailureWalletRejected)))
}

// Database failures around the Wallet call

func TestDeposit_PendingInsertFails_ReturnsItsError(t *testing.T) {
	fixture := givenADeposit(t)
	fixture.store.InsertTransactionErr = errDatabaseDown

	_, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertErrorIs(t, err, errDatabaseDown)
}

// Without a stored PENDING row, the Wallet could pay with no trace of it in
// our database.
func TestDeposit_PendingInsertFails_DoesNotCallTheWalletDeposit(t *testing.T) {
	fixture := givenADeposit(t)
	fixture.store.InsertTransactionErr = errDatabaseDown

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertWalletCalls(t, fixture.wallet, balanceCall())
}

// The Wallet paid but the outcome could not be saved: the database still
// says PENDING, so that is what the API reports (202).
func TestDeposit_OutcomeWriteFails_ReturnsPendingWithoutError(t *testing.T) {
	fixture := givenADeposit(t)
	fixture.store.UpdateTransactionErr = errDatabaseDown

	result, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertResultTransaction(t, result, err, aPendingDeposit(t, "25.00"))
}

func TestDeposit_OutcomeWriteFails_KeepsThePendingDepositAndTheBetPlaced(t *testing.T) {
	fixture := givenADeposit(t)
	fixture.store.UpdateTransactionErr = errDatabaseDown

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertStoreHolds(t, fixture.store, theArrangedStoreWith(t, aPendingDeposit(t, "25.00")))
}

func TestDeposit_CallerLeavesDuringTheWalletCall_StillStoresTheOutcome(t *testing.T) {
	fixture := givenADeposit(t)
	ctx, leave := context.WithCancel(context.Background())
	defer leave()
	wallet := callerLeavingWallet{FakeWallet: fixture.wallet, leave: leave}

	fixture.depositWith(wallet).Execute(ctx, aDepositCommand(t, "25.00"))

	assertStoreHolds(t, fixture.store, storeContents{
		transactions: []domain.Transaction{aCompletedDeposit(t, "25.00", "5014.50"), theWithdraw(t, domain.TransactionCompleted)},
		bets:         []domain.Bet{theBet(t, domain.BetWon)},
	})
}

// One active settlement per bet: while a deposit is PENDING the bet is
// still PLACED, so only the unique index sees the earlier deposit.

func TestDeposit_BetWithAPendingDeposit_IsBetAlreadySettled(t *testing.T) {
	fixture := givenADepositWithStore(t, theArrangedStoreWith(t, anEarlierDeposit(t, domain.TransactionPending)))

	_, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertErrorIs(t, err, domain.ErrBetAlreadySettled)
}

func TestDeposit_BetWithAPendingDeposit_DoesNotCallTheWalletDeposit(t *testing.T) {
	fixture := givenADepositWithStore(t, theArrangedStoreWith(t, anEarlierDeposit(t, domain.TransactionPending)))

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertWalletCalls(t, fixture.wallet, balanceCall())
}

// A FAILED deposit was never applied: the bet can still be settled.
func TestDeposit_BetWithAFailedDeposit_CompletesTheNewDeposit(t *testing.T) {
	fixture := givenADepositWithStore(t, theArrangedStoreWith(t, anEarlierDeposit(t, domain.TransactionFailed)))

	result, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertResultTransaction(t, result, err, aCompletedDeposit(t, "25.00", "5014.50"))
}

func TestDeposit_WithdrawWithAPendingCancel_IsTransactionPending(t *testing.T) {
	fixture := givenADepositWithStore(t, theArrangedStoreWith(t, aCancelOfTheWithdraw(t, domain.TransactionPending)))

	_, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertErrorIs(t, err, domain.ErrTransactionPending)
}

func TestDeposit_WithdrawWithAPendingCancel_DoesNotCallTheWalletNorStoreAnything(t *testing.T) {
	stored := theArrangedStoreWith(t, aCancelOfTheWithdraw(t, domain.TransactionPending))
	fixture := givenADepositWithStore(t, stored)

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertNoWalletCallsAndStoreHolds(t, fixture, stored)
}

// Once the cancel is COMPLETED it is no longer "in progress": the bet is
// CANCELLED, which is the reason given.
func TestDeposit_WithdrawCancelledByACompletedCancel_IsBetAlreadySettled(t *testing.T) {
	fixture := givenADepositWithStore(t, storeContents{
		transactions: []domain.Transaction{aCancelOfTheWithdraw(t, domain.TransactionCompleted), theWithdraw(t, domain.TransactionCancelled)},
		bets:         []domain.Bet{theBet(t, domain.BetCancelled)},
	})

	_, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertErrorIs(t, err, domain.ErrBetAlreadySettled)
}

// A FAILED cancel was never applied: the bet can still be settled.
func TestDeposit_WithdrawWithAFailedCancel_CompletesTheNewDeposit(t *testing.T) {
	fixture := givenADepositWithStore(t, theArrangedStoreWith(t, aCancelOfTheWithdraw(t, domain.TransactionFailed)))

	result, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertResultTransaction(t, result, err, aCompletedDeposit(t, "25.00", "5014.50"))
}

// Zero amount: the bet is LOST, no money moves, so the
// deposit is not forwarded to the Wallet

func TestDeposit_ZeroAmount_ReturnsACompletedDepositWithTheOldBalanceAsNewBalance(t *testing.T) {
	fixture := givenADeposit(t)

	result, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "0.00"))

	assertResultTransaction(t, result, err, aCompletedDeposit(t, "0.00", testBalanceAfterBet))
}

func TestDeposit_ZeroAmount_ReturnsBetStatusLostAndNotReplayed(t *testing.T) {
	fixture := givenADeposit(t)

	result, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "0.00"))

	assertBetStatusAndReplayed(t, result, err, betStatus(domain.BetLost), false)
}

func TestDeposit_ZeroAmount_StoresTheDepositAsCompletedAndTheBetAsLost(t *testing.T) {
	fixture := givenADeposit(t)

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "0.00"))

	assertStoreHolds(t, fixture.store, storeContents{
		transactions: []domain.Transaction{aCompletedDeposit(t, "0.00", testBalanceAfterBet), theWithdraw(t, domain.TransactionCompleted)},
		bets:         []domain.Bet{theBet(t, domain.BetLost)},
	})
}

func TestDeposit_ZeroAmount_OnlyReadsTheBalance(t *testing.T) {
	fixture := givenADeposit(t)

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "0.00"))

	assertWalletCalls(t, fixture.wallet, balanceCall())
}

func TestDeposit_ZeroAmount_LogsThatTheWalletWasNotCalled(t *testing.T) {
	fixture := givenADeposit(t)

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "0.00"))

	assertLogContains(t, fixture.logs, `"msg":"deposit"`, `"provider_transaction_id":"prov-tx-0002"`, `"outcome":"completed_without_wallet"`)
}

// oldBalance must be one we observed (§7.4): without it nothing is stored, and
// the provider can retry.
func TestDeposit_ZeroAmountAndBalanceReadFails_IsWalletFailureNeverTimeout(t *testing.T) {
	for _, testCase := range balanceReadFailures {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenADeposit(t)
			fixture.wallet.Next(testCase.behaviour)

			_, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "0.00"))

			assertWalletFailureNotTimeout(t, err)
		})
	}
}

func TestDeposit_ZeroAmountAndBalanceReadFails_StoresNothing(t *testing.T) {
	for _, testCase := range balanceReadFailures {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenADeposit(t)
			fixture.wallet.Next(testCase.behaviour)

			fixture.deposit.Execute(context.Background(), aDepositCommand(t, "0.00"))

			assertStoreHolds(t, fixture.store, theArrangedStore(t))
		})
	}
}

func TestDeposit_ZeroAmountOnABetWithAPendingDeposit_IsBetAlreadySettled(t *testing.T) {
	fixture := givenADepositWithStore(t, theArrangedStoreWith(t, anEarlierDeposit(t, domain.TransactionPending)))

	_, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "0.00"))

	assertErrorIs(t, err, domain.ErrBetAlreadySettled)
}

func TestDeposit_ZeroAmountOnABetWithAPendingDeposit_LeavesTheBetPlaced(t *testing.T) {
	fixture := givenADepositWithStore(t, theArrangedStoreWith(t, anEarlierDeposit(t, domain.TransactionPending)))

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "0.00"))

	assertStoreHolds(t, fixture.store, theArrangedStoreWith(t, anEarlierDeposit(t, domain.TransactionPending)))
}

func TestDeposit_ZeroAmountAndStoreFails_ReturnsItsError(t *testing.T) {
	fixture := givenADeposit(t)
	fixture.store.InsertTransactionErr = errDatabaseDown

	_, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "0.00"))

	assertErrorIs(t, err, errDatabaseDown)
}

// The deposit and the bet are written together: neither is stored alone.
func TestDeposit_ZeroAmountAndStoreFails_LeavesTheBetPlaced(t *testing.T) {
	fixture := givenADeposit(t)
	fixture.store.InsertTransactionErr = errDatabaseDown

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "0.00"))

	assertStoreHolds(t, fixture.store, theArrangedStore(t))
}

// Idempotency (§7.1, §7.2, §9.2): a replay never reaches the Wallet

func TestDeposit_SameRequestTwice_SecondIsAReplayOfTheCompletedDeposit(t *testing.T) {
	fixture := givenADeposit(t)
	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	result, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertReplayOf(t, result, err, aCompletedDeposit(t, "25.00", "5014.50"), betStatus(domain.BetWon))
}

func TestDeposit_SameRequestTwice_CallsTheWalletOnlyForTheFirst(t *testing.T) {
	fixture := givenADeposit(t)
	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertWalletCalls(t, fixture.wallet, balanceCall(), depositCall(t, "25.00"))
}

func TestDeposit_SameRequestTwice_StoresOneDeposit(t *testing.T) {
	fixture := givenADeposit(t)
	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertStoreHolds(t, fixture.store, storeContents{
		transactions: []domain.Transaction{aCompletedDeposit(t, "25.00", "5014.50"), theWithdraw(t, domain.TransactionCompleted)},
		bets:         []domain.Bet{theBet(t, domain.BetWon)},
	})
}

func TestDeposit_ZeroAmountTwice_SecondIsAReplayWithoutWalletCalls(t *testing.T) {
	fixture := givenADeposit(t)
	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "0.00"))

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "0.00"))

	assertWalletCalls(t, fixture.wallet, balanceCall())
}

func TestDeposit_ReplayOfAPendingDeposit_ReturnsItPendingWithBetStatusPlaced(t *testing.T) {
	fixture := givenADepositWithStore(t, theArrangedStoreWith(t, aPendingDeposit(t, "25.00")))

	result, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertReplayOf(t, result, err, aPendingDeposit(t, "25.00"), betStatus(domain.BetPlaced))
}

// Resolving a PENDING deposit (re-sending it to the Wallet) is Phase 11.
func TestDeposit_ReplayOfAPendingDeposit_DoesNotCallTheWallet(t *testing.T) {
	fixture := givenADepositWithStore(t, theArrangedStoreWith(t, aPendingDeposit(t, "25.00")))

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertWalletCalls(t, fixture.wallet)
}

func TestDeposit_ReplayOfAFailedDeposit_IsPreviousAttemptFailed(t *testing.T) {
	fixture := givenADepositWithStore(t, theArrangedStoreWith(t, aFailedDeposit(t, "25.00", domain.FailureWalletRejected)))

	_, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertErrorIs(t, err, domain.ErrPreviousAttemptFailed)
}

func TestDeposit_ReplayOfAFailedDeposit_DoesNotCallTheWallet(t *testing.T) {
	fixture := givenADepositWithStore(t, theArrangedStoreWith(t, aFailedDeposit(t, "25.00", domain.FailureWalletRejected)))

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertWalletCalls(t, fixture.wallet)
}

// §7.2: a replay "reflects the transaction's current state", even after the
// deposit was cancelled and the bet returned to PLACED (§8.5).
func TestDeposit_ReplayOfACancelledDeposit_ReturnsItsCurrentState(t *testing.T) {
	fixture := givenADepositWithStore(t, theArrangedStoreWith(t, aCancelledDeposit(t, "25.00")))

	result, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertReplayOf(t, result, err, aCancelledDeposit(t, "25.00"), betStatus(domain.BetPlaced))
}

type depositConflictCase struct {
	name    string
	stored  storeContents
	command DepositCommand
}

// depositConflicts: the provider ID is already used by a request that is not
// this one.
func depositConflicts(t *testing.T) []depositConflictCase {
	t.Helper()
	completed := theArrangedStoreWith(t, aCompletedDeposit(t, "25.00", "5014.50"))
	return []depositConflictCase{
		{"another amount", completed, aDepositCommand(t, "26.00")},
		{"another currency", theArrangedStoreWith(t, aDepositInCurrency(t, domain.EUR)), aDepositCommand(t, "25.00")},
		{"another withdraw", withAnotherWithdraw(t, completed),
			aDepositCommandWith(t, testDepositProviderID, anotherWithdrawProviderID, "25.00", "USD")},
		{"an unknown withdraw", completed,
			aDepositCommandWith(t, testDepositProviderID, unknownProviderID, "25.00", "USD")},
		{"the provider id of a withdraw", theArrangedStore(t),
			aDepositCommandWith(t, testProviderID, testProviderID, "10.50", "USD")},
	}
}

func TestDeposit_SameProviderIDWithAnotherPayload_IsIdempotencyConflict(t *testing.T) {
	for _, testCase := range depositConflicts(t) {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenADepositWithStore(t, testCase.stored)

			_, err := fixture.deposit.Execute(context.Background(), testCase.command)

			assertErrorIs(t, err, domain.ErrIdempotencyConflict)
		})
	}
}

func TestDeposit_SameProviderIDWithAnotherPayload_DoesNotCallTheWalletNorStoreAnything(t *testing.T) {
	for _, testCase := range depositConflicts(t) {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenADepositWithStore(t, testCase.stored)

			fixture.deposit.Execute(context.Background(), testCase.command)

			assertNoWalletCallsAndStoreHolds(t, fixture, testCase.stored)
		})
	}
}

// §6.1: provider transaction IDs are unique across the whole system. The
// other player's transaction is never returned, only "the ID is taken".

var depositAmounts = []struct {
	name   string
	amount string
}{
	{"positive amount", "25.00"},
	{"zero amount", "0.00"},
}

func TestDeposit_ProviderIDOfAnotherPlayer_IsTransactionExists(t *testing.T) {
	for _, testCase := range depositAmounts {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenADepositWithStore(t, theArrangedStoreWith(t, anotherPlayersDeposit(t)))

			_, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, testCase.amount))

			assertErrorIs(t, err, domain.ErrTransactionExists)
		})
	}
}

func TestDeposit_ProviderIDOfAnotherPlayer_DoesNotCallTheWalletDepositNorStoreAnything(t *testing.T) {
	for _, testCase := range depositAmounts {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenADepositWithStore(t, theArrangedStoreWith(t, anotherPlayersDeposit(t)))

			fixture.deposit.Execute(context.Background(), aDepositCommand(t, testCase.amount))

			assertOnlyTheBalanceWasReadAndStoreHolds(t, fixture, theArrangedStoreWith(t, anotherPlayersDeposit(t)))
		})
	}
}

// The safety net: an identical request stored by someone else between our
// lookup and our insert (only possible if the lock failed, e.g. two API
// instances) is still a replay, thanks to the unique indexes.
func TestDeposit_IdenticalRequestStoredMeanwhile_IsAReplayWithoutDepositing(t *testing.T) {
	fixture := givenADeposit(t)
	wallet := &racingWallet{FakeWallet: fixture.wallet, store: fixture.store, stored: aDepositStoredByAnotherInstance(t)}

	result, err := fixture.depositWith(wallet).Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertReplayOf(t, result, err, aDepositStoredByAnotherInstance(t), betStatus(domain.BetPlaced))
}

// §9.2: "including when two identical requests arrive at the same time".
func TestDeposit_ConcurrentIdenticalRequests_CallTheWalletDepositOnce(t *testing.T) {
	fixture := givenADeposit(t)

	executeDepositsConcurrently(t, fixture.deposit, identicalDepositCommands(t, 10))

	assertDepositCalls(t, fixture.wallet, 1)
}

func TestDeposit_ConcurrentIdenticalRequests_OneIsProcessedAndTheOthersAreReplays(t *testing.T) {
	fixture := givenADeposit(t)

	result, errs := executeDepositsConcurrently(t, fixture.deposit, identicalDepositCommands(t, 10))

	assertProcessedAndReplayedWithoutErrors(t, result, errs, 1, 9)
}

// One active settlement per bet (§8.4), also for simultaneous requests.
func TestDeposit_ConcurrentDepositsOnOneBet_CallTheWalletDepositOnce(t *testing.T) {
	fixture := givenADeposit(t)

	executeDepositsConcurrently(t, fixture.deposit, competingDepositCommands(t, 10))

	assertDepositCalls(t, fixture.wallet, 1)
}

func TestDeposit_ConcurrentDepositsOnOneBet_OneCompletesAndTheOthersAreBetAlreadySettled(t *testing.T) {
	fixture := givenADeposit(t)

	_, errs := executeDepositsConcurrently(t, fixture.deposit, competingDepositCommands(t, 10))

	assertOneSucceededAndTheOthersFailedWith(t, errs, domain.ErrBetAlreadySettled)
}

// Validation: before the lock and any Wallet call

var invalidDepositCommands = []struct {
	name        string
	providerID  string
	withdrawnID string
	currency    string
	want        error
}{
	{"empty provider transaction id", "", testProviderID, "USD", domain.ErrInvalidProviderTransactionID},
	{"blank provider transaction id", "   ", testProviderID, "USD", domain.ErrInvalidProviderTransactionID},
	{"provider transaction id too long", strings.Repeat("x", 129), testProviderID, "USD", domain.ErrInvalidProviderTransactionID},
	{"empty provider withdrawn transaction id", testDepositProviderID, "", "USD", domain.ErrInvalidProviderTransactionID},
	{"provider withdrawn transaction id too long", testDepositProviderID, strings.Repeat("x", 129), "USD", domain.ErrInvalidProviderTransactionID},
	{"lowercase currency", testDepositProviderID, testProviderID, "usd", domain.ErrInvalidCurrency},
	{"unsupported currency", testDepositProviderID, testProviderID, "GBP", domain.ErrInvalidCurrency},
	{"empty currency", testDepositProviderID, testProviderID, "", domain.ErrInvalidCurrency},
	{"currency of another wallet", testDepositProviderID, testProviderID, "EUR", domain.ErrCurrencyMismatch},
}

func TestDeposit_InvalidCommand_IsItsInvalidInputError(t *testing.T) {
	for _, testCase := range invalidDepositCommands {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenADeposit(t)
			command := aDepositCommandWith(t, testCase.providerID, testCase.withdrawnID, "25.00", testCase.currency)

			_, err := fixture.deposit.Execute(context.Background(), command)

			assertErrorIs(t, err, testCase.want)
		})
	}
}

func TestDeposit_InvalidCommand_DoesNotCallTheWalletNorStoreAnything(t *testing.T) {
	for _, testCase := range invalidDepositCommands {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenADeposit(t)
			command := aDepositCommandWith(t, testCase.providerID, testCase.withdrawnID, "25.00", testCase.currency)

			fixture.deposit.Execute(context.Background(), command)

			assertNoWalletCallsAndStoreHolds(t, fixture, theArrangedStore(t))
		})
	}
}

// Player

func TestDeposit_UnknownPlayer_IsPlayerNotFound(t *testing.T) {
	fixture := givenADepositWithoutPlayers(t)

	_, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertErrorIs(t, err, domain.ErrPlayerNotFound)
}

func TestDeposit_UnknownPlayer_DoesNotCallTheWalletNorStoreAnything(t *testing.T) {
	fixture := givenADepositWithoutPlayers(t)

	fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertNoWalletCallsAndStoreHolds(t, fixture, theArrangedStore(t))
}

func TestDeposit_PlayerRepositoryFails_ReturnsItsError(t *testing.T) {
	fixture := givenADeposit(t)
	fixture.players.Err = errDatabaseDown

	_, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

	assertErrorIs(t, err, errDatabaseDown)
}

// Referenced withdraw: a COMPLETED withdraw of the same player, in the
// deposit's currency, whose bet is still PLACED. Checked before any Wallet call.

type referencedWithdrawCase struct {
	name   string
	stored storeContents
	want   error
}

func referencedWithdrawProblems(t *testing.T) []referencedWithdrawCase {
	t.Helper()
	return []referencedWithdrawCase{
		{"no transaction with that id", storeContents{}, domain.ErrTransactionNotFound},
		{"withdraw of another player", storeContents{
			transactions: []domain.Transaction{theWithdrawOfAnotherPlayer(t)},
		}, domain.ErrTransactionNotFound},
		{"it is a deposit", storeContents{
			transactions: []domain.Transaction{aDepositWithTheWithdrawnProviderID(t)},
		}, domain.ErrTransactionNotFound},
		{"withdraw in another currency", storeContents{
			transactions: []domain.Transaction{theWithdrawInCurrency(t, domain.EUR)},
			bets:         []domain.Bet{theBet(t, domain.BetPlaced)},
		}, domain.ErrCurrencyMismatch},
		{"pending withdraw", storeContents{
			transactions: []domain.Transaction{theWithdraw(t, domain.TransactionPending)},
		}, domain.ErrWithdrawNotCompleted},
		{"failed withdraw", storeContents{
			transactions: []domain.Transaction{theWithdraw(t, domain.TransactionFailed)},
		}, domain.ErrWithdrawNotCompleted},
		{"bet already won", storeContents{
			transactions: []domain.Transaction{theWithdraw(t, domain.TransactionCompleted)},
			bets:         []domain.Bet{theBet(t, domain.BetWon)},
		}, domain.ErrBetAlreadySettled},
		{"bet already lost", storeContents{
			transactions: []domain.Transaction{theWithdraw(t, domain.TransactionCompleted)},
			bets:         []domain.Bet{theBet(t, domain.BetLost)},
		}, domain.ErrBetAlreadySettled},
		{"withdraw cancelled", storeContents{
			transactions: []domain.Transaction{theWithdraw(t, domain.TransactionCancelled)},
			bets:         []domain.Bet{theBet(t, domain.BetCancelled)},
		}, domain.ErrBetAlreadySettled},
	}
}

func TestDeposit_ReferencedWithdrawCannotBeSettled_IsItsError(t *testing.T) {
	for _, testCase := range referencedWithdrawProblems(t) {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenADepositWithStore(t, testCase.stored)

			_, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

			assertErrorIs(t, err, testCase.want)
		})
	}
}

func TestDeposit_ReferencedWithdrawCannotBeSettled_DoesNotCallTheWalletNorStoreAnything(t *testing.T) {
	for _, testCase := range referencedWithdrawProblems(t) {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenADepositWithStore(t, testCase.stored)

			fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

			assertNoWalletCallsAndStoreHolds(t, fixture, testCase.stored)
		})
	}
}

func TestDeposit_UnknownProviderWithdrawnTransactionID_IsTransactionNotFound(t *testing.T) {
	fixture := givenADeposit(t)
	command := aDepositCommandWith(t, testDepositProviderID, unknownProviderID, "25.00", "USD")

	_, err := fixture.deposit.Execute(context.Background(), command)

	assertErrorIs(t, err, domain.ErrTransactionNotFound)
}

// Old balance read: nothing has been sent yet,
// so a failure is "not applied" (502) and never a timeout.

func TestDeposit_BalanceReadFails_IsWalletFailureNeverTimeout(t *testing.T) {
	for _, testCase := range balanceReadFailures {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenADeposit(t)
			fixture.wallet.Next(testCase.behaviour)

			_, err := fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

			assertWalletFailureNotTimeout(t, err)
		})
	}
}

func TestDeposit_BalanceReadFails_DoesNotDepositNorStoreAnything(t *testing.T) {
	for _, testCase := range balanceReadFailures {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenADeposit(t)
			fixture.wallet.Next(testCase.behaviour)

			fixture.deposit.Execute(context.Background(), aDepositCommand(t, "25.00"))

			assertOnlyTheBalanceWasReadAndStoreHolds(t, fixture, theArrangedStore(t))
		})
	}
}

/////////////////////////////////////////////////////////////////////////////////////

// depositFixture keeps the fakes reachable, so a test can script or break one
// of them and inspect what it received or stored.
type depositFixture struct {
	deposit *DepositCommandHandler
	players *domaintest.FakePlayerRepository
	store   *domaintest.FakeStore
	wallet  *domaintest.FakeWallet
	logs    *bytes.Buffer
}

// storeContents is what a test puts in the FakeStore before acting, and what
// it expects to find there afterwards.
type storeContents struct {
	transactions []domain.Transaction
	bets         []domain.Bet
}

// givenADeposit has a completed withdraw of 10.50 whose bet is still PLACED,
// and a Wallet balance of 4989.50 (5000.00 minus the stake).
func givenADeposit(t *testing.T) depositFixture {
	t.Helper()
	return newDepositFixture(t, domaintest.NewFakePlayerRepository(aUSDPlayer()), theArrangedStore(t))
}

func givenADepositWithoutPlayers(t *testing.T) depositFixture {
	t.Helper()
	return newDepositFixture(t, domaintest.NewFakePlayerRepository(), theArrangedStore(t))
}

func givenADepositWithStore(t *testing.T, stored storeContents) depositFixture {
	t.Helper()
	return newDepositFixture(t, domaintest.NewFakePlayerRepository(aUSDPlayer()), stored)
}

func newDepositFixture(t *testing.T, players *domaintest.FakePlayerRepository, stored storeContents) depositFixture {
	t.Helper()
	fixture := depositFixture{
		players: players,
		store:   domaintest.NewFakeStore(),
		wallet: domaintest.NewFakeWallet(map[domain.UserID]domain.WalletBalance{
			testUserID: {Amount: givenAParsedMoney(t, testBalanceAfterBet), Currency: domain.USD},
		}),
		logs: &bytes.Buffer{},
	}
	for _, tx := range stored.transactions {
		fixture.store.PutTransaction(tx)
	}
	for _, bet := range stored.bets {
		fixture.store.PutBet(bet)
	}
	fixture.deposit = fixture.depositWith(fixture.wallet)
	return fixture
}

// depositWith builds the handler with another Wallet (a wrapper of the fake).
func (fixture depositFixture) depositWith(wallet domain.WalletClient) *DepositCommandHandler {
	return NewDepositCommandHandler(
		fixture.players,
		fixture.store,
		wallet,
		&domaintest.FakeIDGenerator{},
		domaintest.FixedClock{Time: testNow},
		slog.New(slog.NewJSONHandler(fixture.logs, nil)),
	)
}

func theArrangedStore(t *testing.T) storeContents {
	t.Helper()
	return storeContents{
		transactions: []domain.Transaction{theWithdraw(t, domain.TransactionCompleted)},
		bets:         []domain.Bet{theBet(t, domain.BetPlaced)},
	}
}

// theArrangedStoreWith is theArrangedStore plus one more transaction, which
// sorts before the withdraw: the store returns them by ID.
func theArrangedStoreWith(t *testing.T, tx domain.Transaction) storeContents {
	t.Helper()
	stored := theArrangedStore(t)
	stored.transactions = append([]domain.Transaction{tx}, stored.transactions...)
	return stored
}

func aDepositCommand(t *testing.T, amount string) DepositCommand {
	t.Helper()
	return aDepositCommandWith(t, testDepositProviderID, testProviderID, amount, "USD")
}

func aDepositCommandWith(t *testing.T, providerID, withdrawnID, amount, currency string) DepositCommand {
	t.Helper()
	return DepositCommand{
		UserID:                         testUserID,
		ProviderTransactionID:          providerID,
		ProviderWithdrawnTransactionID: withdrawnID,
		Amount:                         givenAParsedMoney(t, amount),
		Currency:                       currency,
	}
}

// theWithdraw is the withdraw the test deposits refer to, in the given status.
func theWithdraw(t *testing.T, status domain.TransactionStatus) domain.Transaction {
	t.Helper()
	tx := domain.Transaction{
		ID:                    domaintest.FakeTransactionID(testWithdrawNumber),
		Type:                  domain.TransactionWithdraw,
		Status:                status,
		UserID:                testUserID,
		BetID:                 testBetID,
		ProviderTransactionID: testProviderID,
		Amount:                givenAParsedMoney(t, testWithdrawnStake),
		Currency:              domain.USD,
		OldBalance:            givenAParsedMoney(t, "5000.00"),
		CreatedAt:             testNow,
		UpdatedAt:             testNow,
	}
	if status == domain.TransactionCompleted || status == domain.TransactionCancelled {
		newBalance := givenAParsedMoney(t, testBalanceAfterBet)
		tx.NewBalance = &newBalance
	}
	return tx
}

// theWithdrawOfAnotherPlayer is a completed withdraw with the provider ID
// the test deposits refer to, for another player.
func theWithdrawOfAnotherPlayer(t *testing.T) domain.Transaction {
	t.Helper()
	tx := theWithdraw(t, domain.TransactionCompleted)
	tx.ID = domaintest.FakeTransactionID(99)
	tx.UserID = anotherUserID
	tx.BetID = 99
	return tx
}

// aDepositWithTheWithdrawnProviderID is a completed deposit of the test
// player whose provider ID is the one the test deposits refer to as withdraw.
func aDepositWithTheWithdrawnProviderID(t *testing.T) domain.Transaction {
	t.Helper()
	tx := aCompletedDeposit(t, "25.00", "5014.50")
	tx.ID = domaintest.FakeTransactionID(70)
	tx.ProviderTransactionID = testProviderID
	return tx
}

// theWithdrawInCurrency is a completed withdraw in another currency than its
// player's: impossible through the API, but the rule is checked against it.
func theWithdrawInCurrency(t *testing.T, currency domain.Currency) domain.Transaction {
	t.Helper()
	tx := theWithdraw(t, domain.TransactionCompleted)
	tx.Currency = currency
	return tx
}

func theBet(t *testing.T, status domain.BetStatus) domain.Bet {
	t.Helper()
	bet := domain.NewPlacedBet(testBetID, testUserID, domain.USD, givenAParsedMoney(t, testWithdrawnStake), testNow)
	bet.Status = status
	return bet
}

func aPendingDeposit(t *testing.T, amount string) domain.Transaction {
	t.Helper()
	return domain.Transaction{
		ID:                    domaintest.FakeTransactionID(1),
		Type:                  domain.TransactionDeposit,
		Status:                domain.TransactionPending,
		UserID:                testUserID,
		BetID:                 testBetID,
		ProviderTransactionID: testDepositProviderID,
		Amount:                givenAParsedMoney(t, amount),
		Currency:              domain.USD,
		OldBalance:            givenAParsedMoney(t, testBalanceAfterBet),
		CreatedAt:             testNow,
		UpdatedAt:             testNow,
	}
}

func aCompletedDeposit(t *testing.T, amount, newBalance string) domain.Transaction {
	t.Helper()
	tx := aPendingDeposit(t, amount)
	balance := givenAParsedMoney(t, newBalance)
	tx.Status = domain.TransactionCompleted
	tx.NewBalance = &balance
	return tx
}

func aFailedDeposit(t *testing.T, amount string, reason domain.FailureReason) domain.Transaction {
	t.Helper()
	tx := aPendingDeposit(t, amount)
	tx.Status = domain.TransactionFailed
	tx.FailureReason = reason
	return tx
}

// anEarlierDeposit is another deposit on the same bet, with its own provider
// and transaction IDs.
func anEarlierDeposit(t *testing.T, status domain.TransactionStatus) domain.Transaction {
	t.Helper()
	tx := aPendingDeposit(t, "30.00")
	tx.ID = domaintest.FakeTransactionID(60)
	tx.ProviderTransactionID = "prov-tx-0003"
	tx.Status = status
	return tx
}

// aCancelOfTheWithdraw is a cancel of theWithdraw in the given status. Its
// ID sorts before the withdraw's: the store returns transactions by ID.
func aCancelOfTheWithdraw(t *testing.T, status domain.TransactionStatus) domain.Transaction {
	t.Helper()
	tx := domain.NewCancel(domaintest.FakeTransactionID(50), theWithdraw(t, domain.TransactionCompleted), givenAParsedMoney(t, testBalanceAfterBet), testNow)
	tx.Status = status
	return tx
}

func aCancelledDeposit(t *testing.T, amount string) domain.Transaction {
	t.Helper()
	tx := aCompletedDeposit(t, amount, "5014.50")
	tx.Status = domain.TransactionCancelled
	return tx
}

// aDepositInCurrency is a completed deposit with the test provider ID in
// another currency.
func aDepositInCurrency(t *testing.T, currency domain.Currency) domain.Transaction {
	t.Helper()
	tx := aCompletedDeposit(t, "25.00", "5014.50")
	tx.Currency = currency
	return tx
}

// anotherPlayersDeposit uses the provider ID of the test deposit commands,
// for another player and another bet.
func anotherPlayersDeposit(t *testing.T) domain.Transaction {
	t.Helper()
	tx := aCompletedDeposit(t, "25.00", "31440.25")
	tx.ID = domaintest.FakeTransactionID(98)
	tx.UserID = anotherUserID
	tx.BetID = 98
	tx.OldBalance = givenAParsedMoney(t, "31415.25")
	return tx
}

// aDepositStoredByAnotherInstance is the same request, still PENDING in
// another API instance: same player, provider ID, amount and bet, but its own
// transaction ID.
func aDepositStoredByAnotherInstance(t *testing.T) domain.Transaction {
	t.Helper()
	tx := aPendingDeposit(t, "25.00")
	tx.ID = domaintest.FakeTransactionID(50)
	return tx
}

const anotherWithdrawProviderID = "prov-tx-0009"

// withAnotherWithdraw adds a second completed withdraw of the test player,
// with its own PLACED bet (8), to stored.
func withAnotherWithdraw(t *testing.T, stored storeContents) storeContents {
	t.Helper()
	withdraw := theWithdraw(t, domain.TransactionCompleted)
	withdraw.ID = domaintest.FakeTransactionID(101)
	withdraw.ProviderTransactionID = anotherWithdrawProviderID
	withdraw.BetID = 8
	bet := theBet(t, domain.BetPlaced)
	bet.ID = 8
	return storeContents{
		transactions: append(append([]domain.Transaction{}, stored.transactions...), withdraw),
		bets:         append(append([]domain.Bet{}, stored.bets...), bet),
	}
}

func identicalDepositCommands(t *testing.T, count int) []DepositCommand {
	t.Helper()
	commands := make([]DepositCommand, count)
	for i := range commands {
		commands[i] = aDepositCommand(t, "25.00")
	}
	return commands
}

// competingDepositCommands settle the same bet with different provider IDs.
func competingDepositCommands(t *testing.T, count int) []DepositCommand {
	t.Helper()
	commands := make([]DepositCommand, count)
	for i := range commands {
		commands[i] = aDepositCommandWith(t, fmt.Sprintf("prov-tx-competing-%d", i), testProviderID, "25.00", "USD")
	}
	return commands
}

// executeDepositsConcurrently runs every command from its own goroutine at
// once and returns every result and error, in the order of commands.
func executeDepositsConcurrently(t *testing.T, deposit *DepositCommandHandler, commands []DepositCommand) ([]TransactionResult, []error) {
	t.Helper()
	results := make([]TransactionResult, len(commands))
	errs := make([]error, len(commands))
	var wg sync.WaitGroup
	for i, command := range commands {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = deposit.Execute(context.Background(), command)
		}()
	}
	wg.Wait()
	return results, errs
}

func depositCall(t *testing.T, amount string) domaintest.Call {
	t.Helper()
	return domaintest.Call{
		Method: "Deposit",
		UserID: testUserID,
		Operation: domain.WalletOperation{
			UserID:    testUserID,
			Currency:  domain.USD,
			Amount:    givenAParsedMoney(t, amount),
			BetID:     testBetID,
			Reference: domaintest.FakeTransactionID(1),
		},
	}
}

// Assertions

func assertDepositCalls(t *testing.T, wallet *domaintest.FakeWallet, want int) {
	t.Helper()
	deposits := 0
	for _, call := range wallet.Calls() {
		if call.Method == "Deposit" {
			deposits++
		}
	}
	if deposits != want {
		t.Errorf("expected %d Wallet deposit(s), got %d", want, deposits)
	}
}

func assertProcessedAndReplayedWithoutErrors(t *testing.T, result []TransactionResult, errs []error, wantProcessed, wantReplayed int) {
	t.Helper()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
	}
	assertProcessedAndReplayed(t, result, wantProcessed, wantReplayed)
}

func assertOneSucceededAndTheOthersFailedWith(t *testing.T, errs []error, want error) {
	t.Helper()
	succeeded := 0
	for i, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case !errors.Is(err, want):
			t.Errorf("request %d: expected %v to be %q", i, err, want)
		}
	}
	if succeeded != 1 {
		t.Errorf("expected exactly 1 request to succeed, got %d", succeeded)
	}
}

func assertNoWalletCallsAndStoreHolds(t *testing.T, fixture depositFixture, want storeContents) {
	t.Helper()
	assertWalletCalls(t, fixture.wallet)
	assertStoreHolds(t, fixture.store, want)
}

func assertOnlyTheBalanceWasReadAndStoreHolds(t *testing.T, fixture depositFixture, want storeContents) {
	t.Helper()
	assertWalletCalls(t, fixture.wallet, balanceCall())
	assertStoreHolds(t, fixture.store, want)
}

func assertStoreHolds(t *testing.T, store *domaintest.FakeStore, want storeContents) {
	t.Helper()
	assertTransactions(t, store.Transactions(), want.transactions)
	result := store.Bets()
	if len(result) != len(want.bets) {
		t.Fatalf("expected %d bet(s), got %d", len(want.bets), len(result))
	}
	for i := range want.bets {
		if describeBet(result[i]) != describeBet(want.bets[i]) {
			t.Errorf("expected bet\n  %s\ngot\n  %s", describeBet(want.bets[i]), describeBet(result[i]))
		}
	}
}
