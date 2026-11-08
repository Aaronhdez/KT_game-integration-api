package actions

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
	"github.com/aaronhdez/kt_game-integration-api/Domain/domaintest"
)

// The cancel tests revert theWithdraw (provider ID testProviderID, bet
// testBetID) or theSettlingDeposit (provider ID testDepositProviderID) of
// the same bet. The Wallet balance is 4989.50.

const (
	testSettlingDepositNumber = 80        // its transaction ID is FakeTransactionID(80)
	testBalanceAfterWin       = "5014.50" // testBalanceAfterBet plus the 25.00 deposit
)

// Cancel of a withdraw: the stake is credited back, the bet is CANCELLED

func TestCancel_OfAWithdraw_ReadsTheOldBalanceThenCreditsTheStakeBackOnItsBet(t *testing.T) {
	fixture := givenACancel(t)

	fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertWalletCalls(t, fixture.wallet, balanceCall(), depositCall(t, testWithdrawnStake))
}

func TestCancel_OfAWithdraw_ReturnsTheCompletedCancelWithTheWithdrawsProviderID(t *testing.T) {
	fixture := givenACancel(t)

	result, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertResultTransaction(t, result, err, theCompletedCancelOf(t, theWithdraw(t, domain.TransactionCompleted), testBalanceAfterBet, "5000.00"))
}

func TestCancel_OfAWithdraw_ReturnsBetStatusCancelledAndNotReplayed(t *testing.T) {
	fixture := givenACancel(t)

	result, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertBetStatusAndReplayed(t, result, err, betStatus(domain.BetCancelled), false)
}

func TestCancel_OfAWithdraw_StoresTheCancelTheWithdrawCancelledAndTheBetCancelled(t *testing.T) {
	fixture := givenACancel(t)

	fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertStoreHolds(t, fixture.store, storeContents{
		transactions: []domain.Transaction{
			theCompletedCancelOf(t, theWithdraw(t, domain.TransactionCompleted), testBalanceAfterBet, "5000.00"),
			theWithdraw(t, domain.TransactionCancelled),
		},
		bets: []domain.Bet{theBet(t, domain.BetCancelled)},
	})
}

func TestCancel_OfAWithdraw_LogsTheOutcomeWithTheCancelledTransactionID(t *testing.T) {
	fixture := givenACancel(t)

	fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertLogContains(t, fixture.logs, `"msg":"cancel"`, `"provider_transaction_id":"prov-tx-0001"`,
		`"cancelled_transaction_id":"00000000-0000-4000-8000-000000000100"`, `"outcome":"completed"`)
}

// Intent first, also for compensating calls (§9).
func TestCancel_StoresThePendingCancelBeforeCallingTheWallet(t *testing.T) {
	fixture := givenACancel(t)
	wallet := &storeObservingWallet{FakeWallet: fixture.wallet, store: fixture.store}

	fixture.cancelWith(wallet).Execute(context.Background(), aCancelCommand(testProviderID))

	assertTransactionsSeenByTheWallet(t, wallet,
		thePendingCancelOf(t, theWithdraw(t, domain.TransactionCompleted), testBalanceAfterBet),
		theWithdraw(t, domain.TransactionCompleted))
}

// Cancel of a deposit: the winnings are debited back, the bet returns to PLACED

func TestCancel_OfADeposit_ReadsTheOldBalanceThenDebitsTheWinningsBackOnItsBet(t *testing.T) {
	fixture := givenACancelOfTheDeposit(t)

	fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

	assertWalletCalls(t, fixture.wallet, balanceCall(), debitBackCall(t, "25.00"))
}

func TestCancel_OfADeposit_ReturnsTheCompletedCancel(t *testing.T) {
	fixture := givenACancelOfTheDeposit(t)

	result, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

	assertResultTransaction(t, result, err, theCompletedCancelOf(t, theSettlingDeposit(t, domain.TransactionCompleted, "25.00"), testBalanceAfterWin, testBalanceAfterBet))
}

func TestCancel_OfADeposit_ReturnsBetStatusPlacedAndNotReplayed(t *testing.T) {
	fixture := givenACancelOfTheDeposit(t)

	result, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

	assertBetStatusAndReplayed(t, result, err, betStatus(domain.BetPlaced), false)
}

func TestCancel_OfADeposit_StoresTheCancelTheDepositCancelledAndTheBetPlaced(t *testing.T) {
	fixture := givenACancelOfTheDeposit(t)

	fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

	assertStoreHolds(t, fixture.store, storeContents{
		transactions: []domain.Transaction{
			theCompletedCancelOf(t, theSettlingDeposit(t, domain.TransactionCompleted, "25.00"), testBalanceAfterWin, testBalanceAfterBet),
			theSettlingDeposit(t, domain.TransactionCancelled, "25.00"),
			theWithdraw(t, domain.TransactionCompleted),
		},
		bets: []domain.Bet{theBet(t, domain.BetPlaced)},
	})
}

// The cancelled deposit frees the bet's single settlement (§8.4).
func TestCancel_OfADeposit_ThenANewDepositOnTheBet_SettlesItAgain(t *testing.T) {
	fixture := givenACancelOfTheDeposit(t)
	fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

	result, err := fixture.aDepositHandler().Execute(context.Background(), aDepositCommandWith(t, "prov-tx-0004", testProviderID, "40.00", "USD"))

	assertBetStatusAndReplayed(t, result, err, betStatus(domain.BetWon), false)
}

// Cancel of a zero-amount deposit (§8.5): no money moves, so the Wallet is
// only read for oldBalance (symmetric with the zero-amount deposit)

func TestCancel_OfAZeroDeposit_ReturnsACompletedCancelWithTheOldBalanceAsNewBalance(t *testing.T) {
	fixture := givenACancelOfTheZeroDeposit(t)

	result, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

	assertResultTransaction(t, result, err, theCompletedCancelOf(t, theSettlingDeposit(t, domain.TransactionCompleted, "0.00"), testBalanceAfterBet, testBalanceAfterBet))
}

func TestCancel_OfAZeroDeposit_ReturnsBetStatusPlacedAndNotReplayed(t *testing.T) {
	fixture := givenACancelOfTheZeroDeposit(t)

	result, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

	assertBetStatusAndReplayed(t, result, err, betStatus(domain.BetPlaced), false)
}

func TestCancel_OfAZeroDeposit_StoresTheCancelTheDepositCancelledAndTheBetPlaced(t *testing.T) {
	fixture := givenACancelOfTheZeroDeposit(t)

	fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

	assertStoreHolds(t, fixture.store, storeContents{
		transactions: []domain.Transaction{
			theCompletedCancelOf(t, theSettlingDeposit(t, domain.TransactionCompleted, "0.00"), testBalanceAfterBet, testBalanceAfterBet),
			theSettlingDeposit(t, domain.TransactionCancelled, "0.00"),
			theWithdraw(t, domain.TransactionCompleted),
		},
		bets: []domain.Bet{theBet(t, domain.BetPlaced)},
	})
}

func TestCancel_OfAZeroDeposit_OnlyReadsTheBalance(t *testing.T) {
	fixture := givenACancelOfTheZeroDeposit(t)

	fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

	assertWalletCalls(t, fixture.wallet, balanceCall())
}

func TestCancel_OfAZeroDeposit_LogsThatTheWalletWasNotCalled(t *testing.T) {
	fixture := givenACancelOfTheZeroDeposit(t)

	fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

	assertLogContains(t, fixture.logs, `"msg":"cancel"`, `"outcome":"completed_without_wallet"`)
}

// oldBalance must be one we observed (§7.4): without it nothing is stored,
// and the provider can retry.
func TestCancel_OfAZeroDepositAndBalanceReadFails_IsWalletFailureNeverTimeout(t *testing.T) {
	for _, testCase := range balanceReadFailures {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenACancelOfTheZeroDeposit(t)
			fixture.wallet.Next(testCase.behaviour)

			_, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

			assertWalletFailureNotTimeout(t, err)
		})
	}
}

func TestCancel_OfAZeroDepositAndBalanceReadFails_StoresNothing(t *testing.T) {
	for _, testCase := range balanceReadFailures {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenACancelOfTheZeroDeposit(t)
			fixture.wallet.Next(testCase.behaviour)

			fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

			assertStoreHolds(t, fixture.store, theStoreWithTheSettlingDeposit(t, domain.TransactionCompleted, "0.00", domain.BetLost))
		})
	}
}

func TestCancel_OfAZeroDepositAndStoreFails_ReturnsItsError(t *testing.T) {
	fixture := givenACancelOfTheZeroDeposit(t)
	fixture.store.InsertTransactionErr = errDatabaseDown

	_, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

	assertErrorIs(t, err, errDatabaseDown)
}

// The cancel, the deposit and the bet are written together: none alone.
func TestCancel_OfAZeroDepositAndStoreFails_ChangesNothing(t *testing.T) {
	fixture := givenACancelOfTheZeroDeposit(t)
	fixture.store.InsertTransactionErr = errDatabaseDown

	fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

	assertStoreHolds(t, fixture.store, theStoreWithTheSettlingDeposit(t, domain.TransactionCompleted, "0.00", domain.BetLost))
}

// Insufficient funds on a debit back (§8.5): 422 and nothing changes but the
// FAILED cancel kept for audit. 25.00 left is not enough to take 25.00 back:
// the Wallet requires amount < balance.

func TestCancel_OfADepositWithInsufficientFunds_IsInsufficientFunds(t *testing.T) {
	fixture := givenACancelOfTheDepositWithBalance(t, "25.00")

	_, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

	assertErrorIs(t, err, domain.ErrInsufficientFunds)
}

func TestCancel_OfADepositWithInsufficientFunds_KeepsAFailedCancelAndChangesNothingElse(t *testing.T) {
	fixture := givenACancelOfTheDepositWithBalance(t, "25.00")

	fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

	assertStoreHolds(t, fixture.store, storeContents{
		transactions: []domain.Transaction{
			theFailedCancelOf(t, theSettlingDeposit(t, domain.TransactionCompleted, "25.00"), "25.00", domain.FailureInsufficientFunds),
			theSettlingDeposit(t, domain.TransactionCompleted, "25.00"),
			theWithdraw(t, domain.TransactionCompleted),
		},
		bets: []domain.Bet{theBet(t, domain.BetWon)},
	})
}

// Applied, new balance unknown: derived from the balance read under the lock

func TestCancel_WalletAppliedButBalanceUnknown_CompletesWithTheDerivedBalance(t *testing.T) {
	testCases := []struct {
		name       string
		fixture    func(t *testing.T) cancelFixture
		providerID string
		want       func(t *testing.T) domain.Transaction
	}{
		{"withdraw cancel adds the stake", givenACancel, testProviderID, func(t *testing.T) domain.Transaction {
			return theCompletedCancelOf(t, theWithdraw(t, domain.TransactionCompleted), testBalanceAfterBet, "5000.00")
		}},
		{"deposit cancel subtracts the winnings", givenACancelOfTheDeposit, testDepositProviderID, func(t *testing.T) domain.Transaction {
			return theCompletedCancelOf(t, theSettlingDeposit(t, domain.TransactionCompleted, "25.00"), testBalanceAfterWin, testBalanceAfterBet)
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := testCase.fixture(t)
			wallet := balanceUnknownWallet{FakeWallet: fixture.wallet}

			result, err := fixture.cancelWith(wallet).Execute(context.Background(), aCancelCommand(testCase.providerID))

			assertResultTransaction(t, result, err, testCase.want(t))
		})
	}
}

// Unknown outcome: never FAILED, the original and its bet do not change

func TestCancel_WalletOutcomeUnknown_ReturnsThePendingCancelWithoutError(t *testing.T) {
	for _, testCase := range unknownOutcomes {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenACancel(t)
			fixture.wallet.Next(domaintest.Succeed, testCase.behaviour)

			result, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

			assertResultTransaction(t, result, err, thePendingCancelOf(t, theWithdraw(t, domain.TransactionCompleted), testBalanceAfterBet))
		})
	}
}

// The bet keeps its status until the cancel is confirmed.
func TestCancel_WalletOutcomeUnknown_ReturnsTheBetStatusUnchanged(t *testing.T) {
	for _, testCase := range unknownOutcomes {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenACancel(t)
			fixture.wallet.Next(domaintest.Succeed, testCase.behaviour)

			result, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

			assertBetStatusAndReplayed(t, result, err, betStatus(domain.BetPlaced), false)
		})
	}
}

func TestCancel_WalletOutcomeUnknown_KeepsThePendingCancelAndTheWithdrawCompleted(t *testing.T) {
	for _, testCase := range unknownOutcomes {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenACancel(t)
			fixture.wallet.Next(domaintest.Succeed, testCase.behaviour)

			fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

			assertStoreHolds(t, fixture.store, theArrangedStoreWith(t, thePendingCancelOf(t, theWithdraw(t, domain.TransactionCompleted), testBalanceAfterBet)))
		})
	}
}

func TestCancel_WalletTooSlowForTheCaller_ReturnsPendingWithoutError(t *testing.T) {
	fixture := givenACancel(t)
	fixture.wallet.Next(domaintest.Succeed, domaintest.Slow)

	result, err := fixture.cancel.Execute(givenACallerGivingUpAfter(t, 20*time.Millisecond), aCancelCommand(testProviderID))

	assertResultTransaction(t, result, err, thePendingCancelOf(t, theWithdraw(t, domain.TransactionCompleted), testBalanceAfterBet))
}

// Not applied: FAILED cancel kept, nothing else changes

func TestCancel_WalletRejects_IsWalletRejected(t *testing.T) {
	fixture := givenACancel(t)
	fixture.wallet.Next(domaintest.Succeed, domaintest.Reject)

	_, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertErrorIs(t, err, domain.ErrWalletRejected)
}

func TestCancel_WalletRejects_KeepsAFailedCancelAndTheWithdrawCompleted(t *testing.T) {
	fixture := givenACancel(t)
	fixture.wallet.Next(domaintest.Succeed, domaintest.Reject)

	fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertStoreHolds(t, fixture.store, theArrangedStoreWith(t, theFailedCancelOf(t, theWithdraw(t, domain.TransactionCompleted), testBalanceAfterBet, domain.FailureWalletRejected)))
}

// Database failures around the Wallet call

func TestCancel_PendingInsertFails_ReturnsItsError(t *testing.T) {
	fixture := givenACancel(t)
	fixture.store.InsertTransactionErr = errDatabaseDown

	_, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertErrorIs(t, err, errDatabaseDown)
}

// Without a stored PENDING row, the Wallet could move money with no trace of
// it in our database.
func TestCancel_PendingInsertFails_DoesNotSendTheCompensation(t *testing.T) {
	fixture := givenACancel(t)
	fixture.store.InsertTransactionErr = errDatabaseDown

	fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertWalletCalls(t, fixture.wallet, balanceCall())
}

// The Wallet credited the stake back but the outcome could not be saved: the
// database still says PENDING, so that is what the API reports (202).
func TestCancel_OutcomeWriteFails_ReturnsPendingWithoutError(t *testing.T) {
	fixture := givenACancel(t)
	fixture.store.UpdateTransactionErr = errDatabaseDown

	result, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertResultTransaction(t, result, err, thePendingCancelOf(t, theWithdraw(t, domain.TransactionCompleted), testBalanceAfterBet))
}

func TestCancel_OutcomeWriteFails_KeepsThePendingCancelAndChangesNothingElse(t *testing.T) {
	fixture := givenACancel(t)
	fixture.store.UpdateTransactionErr = errDatabaseDown

	fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertStoreHolds(t, fixture.store, theArrangedStoreWith(t, thePendingCancelOf(t, theWithdraw(t, domain.TransactionCompleted), testBalanceAfterBet)))
}

func TestCancel_CallerLeavesDuringTheWalletCall_StillStoresTheOutcome(t *testing.T) {
	fixture := givenACancel(t)
	ctx, leave := context.WithCancel(context.Background())
	defer leave()
	wallet := callerLeavingWallet{FakeWallet: fixture.wallet, leave: leave}

	fixture.cancelWith(wallet).Execute(ctx, aCancelCommand(testProviderID))

	assertStoreHolds(t, fixture.store, storeContents{
		transactions: []domain.Transaction{
			theCompletedCancelOf(t, theWithdraw(t, domain.TransactionCompleted), testBalanceAfterBet, "5000.00"),
			theWithdraw(t, domain.TransactionCancelled),
		},
		bets: []domain.Bet{theBet(t, domain.BetCancelled)},
	})
}

// Validation: before the lock and any Wallet call

var invalidCancelProviderIDs = []struct {
	name       string
	providerID string
}{
	{"empty provider transaction id", ""},
	{"blank provider transaction id", "   "},
	{"provider transaction id too long", strings.Repeat("x", 129)},
}

func TestCancel_InvalidProviderTransactionID_IsInvalidProviderTransactionID(t *testing.T) {
	for _, testCase := range invalidCancelProviderIDs {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenACancel(t)

			_, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testCase.providerID))

			assertErrorIs(t, err, domain.ErrInvalidProviderTransactionID)
		})
	}
}

func TestCancel_InvalidProviderTransactionID_DoesNotCallTheWalletNorStoreAnything(t *testing.T) {
	for _, testCase := range invalidCancelProviderIDs {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenACancel(t)

			fixture.cancel.Execute(context.Background(), aCancelCommand(testCase.providerID))

			assertNoCancelWalletCallsAndStoreHolds(t, fixture, theArrangedStore(t))
		})
	}
}

// Player

func TestCancel_UnknownPlayer_IsPlayerNotFound(t *testing.T) {
	fixture := givenACancelWithoutPlayers(t)

	_, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertErrorIs(t, err, domain.ErrPlayerNotFound)
}

func TestCancel_UnknownPlayer_DoesNotCallTheWalletNorStoreAnything(t *testing.T) {
	fixture := givenACancelWithoutPlayers(t)

	fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertNoCancelWalletCallsAndStoreHolds(t, fixture, theArrangedStore(t))
}

func TestCancel_PlayerRepositoryFails_ReturnsItsError(t *testing.T) {
	fixture := givenACancel(t)
	fixture.players.Err = errDatabaseDown

	_, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertErrorIs(t, err, errDatabaseDown)
}

// Transactions that cannot be cancelled: decided before any Wallet call

type cannotCancelCase struct {
	name       string
	stored     storeContents
	providerID string
	want       error
}

func transactionsThatCannotBeCancelled(t *testing.T) []cannotCancelCase {
	t.Helper()
	return []cannotCancelCase{
		{"no transaction with that id", theArrangedStore(t), unknownProviderID, domain.ErrTransactionNotFound},
		{"withdraw of another player", storeContents{
			transactions: []domain.Transaction{theWithdrawOfAnotherPlayer(t)},
		}, testProviderID, domain.ErrTransactionNotFound},
		{"pending withdraw", storeContents{
			transactions: []domain.Transaction{theWithdraw(t, domain.TransactionPending)},
		}, testProviderID, domain.ErrTransactionPending},
		{"failed withdraw", storeContents{
			transactions: []domain.Transaction{theWithdraw(t, domain.TransactionFailed)},
		}, testProviderID, domain.ErrNothingToRevert},
		{"withdraw of a won bet", theStoreWithTheSettlingDeposit(t, domain.TransactionCompleted, "25.00", domain.BetWon),
			testProviderID, domain.ErrBetAlreadySettled},
		{"withdraw of a lost bet", theStoreWithTheSettlingDeposit(t, domain.TransactionCompleted, "0.00", domain.BetLost),
			testProviderID, domain.ErrBetAlreadySettled},
		{"withdraw of a bet with a pending deposit", theStoreWithTheSettlingDeposit(t, domain.TransactionPending, "25.00", domain.BetPlaced),
			testProviderID, domain.ErrTransactionPending},
		{"pending deposit", theStoreWithTheSettlingDeposit(t, domain.TransactionPending, "25.00", domain.BetPlaced),
			testDepositProviderID, domain.ErrTransactionPending},
		{"failed deposit", theStoreWithTheSettlingDeposit(t, domain.TransactionFailed, "25.00", domain.BetPlaced),
			testDepositProviderID, domain.ErrNothingToRevert},
	}
}

func TestCancel_TransactionCannotBeCancelled_IsItsError(t *testing.T) {
	for _, testCase := range transactionsThatCannotBeCancelled(t) {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenACancelWithStore(t, testCase.stored)

			_, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testCase.providerID))

			assertErrorIs(t, err, testCase.want)
		})
	}
}

func TestCancel_TransactionCannotBeCancelled_DoesNotCallTheWalletNorStoreAnything(t *testing.T) {
	for _, testCase := range transactionsThatCannotBeCancelled(t) {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenACancelWithStore(t, testCase.stored)

			fixture.cancel.Execute(context.Background(), aCancelCommand(testCase.providerID))

			assertNoCancelWalletCallsAndStoreHolds(t, fixture, testCase.stored)
		})
	}
}

// Old balance read: nothing has been sent yet, so a failure is "not applied"
// (502) and never a timeout.

func TestCancel_BalanceReadFails_IsWalletFailureNeverTimeout(t *testing.T) {
	for _, testCase := range balanceReadFailures {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenACancel(t)
			fixture.wallet.Next(testCase.behaviour)

			_, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

			assertWalletFailureNotTimeout(t, err)
		})
	}
}

func TestCancel_BalanceReadFails_DoesNotRevertNorStoreAnything(t *testing.T) {
	for _, testCase := range balanceReadFailures {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenACancel(t)
			fixture.wallet.Next(testCase.behaviour)

			fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

			assertOnlyTheBalanceWasReadForCancelAndStoreHolds(t, fixture, theArrangedStore(t))
		})
	}
}

// Idempotency: a replay never reaches the Wallet

func TestCancel_SameCancelTwice_SecondIsAReplayOfTheCompletedCancel(t *testing.T) {
	fixture := givenACancel(t)
	fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	result, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertReplayOf(t, result, err, theCompletedCancelOf(t, theWithdraw(t, domain.TransactionCompleted), testBalanceAfterBet, "5000.00"), betStatus(domain.BetCancelled))
}

func TestCancel_SameCancelTwice_SendsTheCompensationOnlyOnce(t *testing.T) {
	fixture := givenACancel(t)
	fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertWalletCalls(t, fixture.wallet, balanceCall(), depositCall(t, testWithdrawnStake))
}

func TestCancel_SameCancelTwice_StoresOneCancel(t *testing.T) {
	fixture := givenACancel(t)
	fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertStoreHolds(t, fixture.store, storeContents{
		transactions: []domain.Transaction{
			theCompletedCancelOf(t, theWithdraw(t, domain.TransactionCompleted), testBalanceAfterBet, "5000.00"),
			theWithdraw(t, domain.TransactionCancelled),
		},
		bets: []domain.Bet{theBet(t, domain.BetCancelled)},
	})
}

func TestCancel_ZeroDepositCancelTwice_SecondIsAReplayWithoutWalletCalls(t *testing.T) {
	fixture := givenACancelOfTheZeroDeposit(t)
	fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

	fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

	assertWalletCalls(t, fixture.wallet, balanceCall())
}

// Resolving a PENDING cancel (re-sending it to the Wallet) is Phase 11.
func TestCancel_ReplayOfAPendingCancel_ReturnsItPendingWithTheBetUnchanged(t *testing.T) {
	stored := theArrangedStoreWith(t, aCancelOfTheWithdraw(t, domain.TransactionPending))
	fixture := givenACancelWithStore(t, stored)

	result, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertReplayOf(t, result, err, aCancelOfTheWithdraw(t, domain.TransactionPending), betStatus(domain.BetPlaced))
}

func TestCancel_ReplayOfAPendingCancel_DoesNotCallTheWallet(t *testing.T) {
	stored := theArrangedStoreWith(t, aCancelOfTheWithdraw(t, domain.TransactionPending))
	fixture := givenACancelWithStore(t, stored)

	fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertNoCancelWalletCallsAndStoreHolds(t, fixture, stored)
}

// Final FAILED-retry policy: Phase 11.
func TestCancel_ReplayOfAFailedCancel_IsPreviousAttemptFailed(t *testing.T) {
	fixture := givenACancelWithStore(t, theArrangedStoreWith(t, aCancelOfTheWithdraw(t, domain.TransactionFailed)))

	_, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertErrorIs(t, err, domain.ErrPreviousAttemptFailed)
}

func TestCancel_ReplayOfAFailedCancel_DoesNotCallTheWalletNorStoreAnything(t *testing.T) {
	stored := theArrangedStoreWith(t, aCancelOfTheWithdraw(t, domain.TransactionFailed))
	fixture := givenACancelWithStore(t, stored)

	fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertNoCancelWalletCallsAndStoreHolds(t, fixture, stored)
}

// A replay "reflects the transaction's current state": after the
// deposit was cancelled, a new deposit settled the bet again.
func TestCancel_ReplayAfterTheBetWasSettledAgain_ReturnsTheCancelWithTheCurrentBetStatus(t *testing.T) {
	fixture := givenACancelOfTheDeposit(t)
	fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))
	fixture.aDepositHandler().Execute(context.Background(), aDepositCommandWith(t, "prov-tx-0004", testProviderID, "40.00", "USD"))

	result, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testDepositProviderID))

	assertReplayOf(t, result, err, theCompletedCancelOf(t, theSettlingDeposit(t, domain.TransactionCompleted, "25.00"), testBalanceAfterWin, testBalanceAfterBet), betStatus(domain.BetWon))
}

// A cancelled original always has its completed cancel; if it were missing,
// the request cannot be answered as a replay.
func TestCancel_CancelledTransactionWithoutItsCancel_IsConflict(t *testing.T) {
	fixture := givenACancelWithStore(t, storeContents{
		transactions: []domain.Transaction{theWithdraw(t, domain.TransactionCancelled)},
		bets:         []domain.Bet{theBet(t, domain.BetCancelled)},
	})

	_, err := fixture.cancel.Execute(context.Background(), aCancelCommand(testProviderID))

	assertErrorIs(t, err, domain.ErrConflict)
}

// The safety net: an identical cancel stored by someone else between our
// lookup and our insert (only possible if the lock failed, e.g. two API
// instances) is still a replay, thanks to the single-cancel unique index.
func TestCancel_IdenticalCancelStoredMeanwhile_IsAReplayWithoutTheCompensation(t *testing.T) {
	fixture := givenACancel(t)
	wallet := &racingWallet{FakeWallet: fixture.wallet, store: fixture.store, stored: aCancelOfTheWithdraw(t, domain.TransactionPending)}

	result, err := fixture.cancelWith(wallet).Execute(context.Background(), aCancelCommand(testProviderID))

	assertReplayOf(t, result, err, aCancelOfTheWithdraw(t, domain.TransactionPending), betStatus(domain.BetPlaced))
}

func TestCancel_IdenticalZeroDepositCancelStoredMeanwhile_IsAReplay(t *testing.T) {
	fixture := givenACancelOfTheZeroDeposit(t)
	stored := aCompletedCancelOfTheZeroDepositByAnotherInstance(t)
	wallet := &racingWallet{FakeWallet: fixture.wallet, store: fixture.store, stored: stored}

	result, err := fixture.cancelWith(wallet).Execute(context.Background(), aCancelCommand(testDepositProviderID))

	assertReplayOf(t, result, err, stored, betStatus(domain.BetLost))
}

// §9.2: "including when two identical requests arrive at the same time".
func TestCancel_ConcurrentIdenticalCancels_SendTheCompensationOnce(t *testing.T) {
	fixture := givenACancel(t)

	executeCancelsConcurrently(t, fixture.cancel, identicalCancelCommands(10))

	assertDepositCalls(t, fixture.wallet, 1)
}

func TestCancel_ConcurrentIdenticalCancels_OneIsProcessedAndTheOthersAreReplays(t *testing.T) {
	fixture := givenACancel(t)

	result, errs := executeCancelsConcurrently(t, fixture.cancel, identicalCancelCommands(10))

	assertProcessedAndReplayedWithoutErrors(t, result, errs, 1, 9)
}

/////////////////////////////////////////////////////////////////////////////////////

// cancelFixture keeps the fakes reachable, so a test can script or break one
// of them and inspect what it received or stored.
type cancelFixture struct {
	cancel  *CancelCommandHandler
	players *domaintest.FakePlayerRepository
	store   *domaintest.FakeStore
	wallet  *domaintest.FakeWallet
	ids     *domaintest.FakeIDGenerator
	logs    *bytes.Buffer
}

// givenACancel has a completed withdraw of 10.50 whose bet is still PLACED
// (theArrangedStore of the deposit tests), and a Wallet balance of 4989.50.
func givenACancel(t *testing.T) cancelFixture {
	t.Helper()
	return newCancelFixture(t, domaintest.NewFakePlayerRepository(aUSDPlayer()), theArrangedStore(t), testBalanceAfterBet)
}

func givenACancelWithoutPlayers(t *testing.T) cancelFixture {
	t.Helper()
	return newCancelFixture(t, domaintest.NewFakePlayerRepository(), theArrangedStore(t), testBalanceAfterBet)
}

func givenACancelWithStore(t *testing.T, stored storeContents) cancelFixture {
	t.Helper()
	return newCancelFixture(t, domaintest.NewFakePlayerRepository(aUSDPlayer()), stored, testBalanceAfterBet)
}

// givenACancelOfTheDeposit has the completed withdraw, its completed deposit
// of 25.00 and the bet WON, and a Wallet balance of 5014.50.
func givenACancelOfTheDeposit(t *testing.T) cancelFixture {
	t.Helper()
	return givenACancelOfTheDepositWithBalance(t, testBalanceAfterWin)
}

// givenACancelOfTheZeroDeposit has the completed withdraw, its completed
// deposit of 0.00 and the bet LOST, and a Wallet balance of 4989.50.
func givenACancelOfTheZeroDeposit(t *testing.T) cancelFixture {
	t.Helper()
	stored := theStoreWithTheSettlingDeposit(t, domain.TransactionCompleted, "0.00", domain.BetLost)
	return newCancelFixture(t, domaintest.NewFakePlayerRepository(aUSDPlayer()), stored, testBalanceAfterBet)
}

// givenACancelOfTheDepositWithBalance is givenACancelOfTheDeposit after the
// player spent part of the winnings.
func givenACancelOfTheDepositWithBalance(t *testing.T, balance string) cancelFixture {
	t.Helper()
	stored := theStoreWithTheSettlingDeposit(t, domain.TransactionCompleted, "25.00", domain.BetWon)
	return newCancelFixture(t, domaintest.NewFakePlayerRepository(aUSDPlayer()), stored, balance)
}

func newCancelFixture(t *testing.T, players *domaintest.FakePlayerRepository, stored storeContents, balance string) cancelFixture {
	t.Helper()
	fixture := cancelFixture{
		players: players,
		store:   domaintest.NewFakeStore(),
		wallet: domaintest.NewFakeWallet(map[domain.UserID]domain.WalletBalance{
			testUserID: {Amount: givenAParsedMoney(t, balance), Currency: domain.USD},
		}),
		ids:  &domaintest.FakeIDGenerator{},
		logs: &bytes.Buffer{},
	}
	for _, tx := range stored.transactions {
		fixture.store.PutTransaction(tx)
	}
	for _, bet := range stored.bets {
		fixture.store.PutBet(bet)
	}
	fixture.cancel = fixture.cancelWith(fixture.wallet)
	return fixture
}

// cancelWith builds the handler with another Wallet (a wrapper of the fake).
func (fixture cancelFixture) cancelWith(wallet domain.WalletClient) *CancelCommandHandler {
	return NewCancelCommandHandler(
		fixture.players,
		fixture.store,
		wallet,
		fixture.ids,
		domaintest.FixedClock{Time: testNow},
		slog.New(slog.NewJSONHandler(fixture.logs, nil)),
	)
}

// aDepositHandler works on the same fakes as the cancel, IDs included, to
// check what can happen to the bet after a cancel.
func (fixture cancelFixture) aDepositHandler() *DepositCommandHandler {
	return NewDepositCommandHandler(
		fixture.players,
		fixture.store,
		fixture.wallet,
		fixture.ids,
		domaintest.FixedClock{Time: testNow},
		slog.New(slog.NewJSONHandler(fixture.logs, nil)),
	)
}

func aCancelCommand(providerID string) CancelCommand {
	return CancelCommand{UserID: testUserID, ProviderTransactionID: providerID}
}

// thePendingCancelOf is the cancel the handler creates for original: the
// first ID of the FakeIDGenerator and the balance read just before.
func thePendingCancelOf(t *testing.T, original domain.Transaction, oldBalance string) domain.Transaction {
	t.Helper()
	return domain.NewCancel(domaintest.FakeTransactionID(1), original, givenAParsedMoney(t, oldBalance), testNow)
}

func theCompletedCancelOf(t *testing.T, original domain.Transaction, oldBalance, newBalance string) domain.Transaction {
	t.Helper()
	tx := thePendingCancelOf(t, original, oldBalance)
	balance := givenAParsedMoney(t, newBalance)
	tx.Status = domain.TransactionCompleted
	tx.NewBalance = &balance
	return tx
}

func theFailedCancelOf(t *testing.T, original domain.Transaction, oldBalance string, reason domain.FailureReason) domain.Transaction {
	t.Helper()
	tx := thePendingCancelOf(t, original, oldBalance)
	tx.Status = domain.TransactionFailed
	tx.FailureReason = reason
	return tx
}

// aCompletedCancelOfTheZeroDepositByAnotherInstance is the same zero-deposit
// cancel, already stored by another API instance with its own ID. Only the
// cancel row is put in the store, so the bet still reads LOST.
func aCompletedCancelOfTheZeroDepositByAnotherInstance(t *testing.T) domain.Transaction {
	t.Helper()
	tx := theCompletedCancelOf(t, theSettlingDeposit(t, domain.TransactionCompleted, "0.00"), testBalanceAfterBet, testBalanceAfterBet)
	tx.ID = domaintest.FakeTransactionID(50)
	return tx
}

func identicalCancelCommands(count int) []CancelCommand {
	commands := make([]CancelCommand, count)
	for i := range commands {
		commands[i] = aCancelCommand(testProviderID)
	}
	return commands
}

// executeCancelsConcurrently runs every command from its own goroutine at
// once and returns every result and error, in the order of commands.
func executeCancelsConcurrently(t *testing.T, cancel *CancelCommandHandler, commands []CancelCommand) ([]TransactionResult, []error) {
	t.Helper()
	results := make([]TransactionResult, len(commands))
	errs := make([]error, len(commands))
	var wg sync.WaitGroup
	for i, command := range commands {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = cancel.Execute(context.Background(), command)
		}()
	}
	wg.Wait()
	return results, errs
}

// debitBackCall is the Wallet withdraw that takes a deposit back.
func debitBackCall(t *testing.T, amount string) domaintest.Call {
	t.Helper()
	call := depositCall(t, amount)
	call.Method = "Withdraw"
	return call
}

// theSettlingDeposit is the deposit of theWithdraw's bet, with its own
// transaction ID (the new cancel takes FakeTransactionID(1)).
func theSettlingDeposit(t *testing.T, status domain.TransactionStatus, amount string) domain.Transaction {
	t.Helper()
	tx := aPendingDeposit(t, amount)
	tx.ID = domaintest.FakeTransactionID(testSettlingDepositNumber)
	tx.Status = status
	if status == domain.TransactionCompleted || status == domain.TransactionCancelled {
		sum := givenAParsedMoney(t, testBalanceAfterBet).Decimal().Add(tx.Amount.Decimal())
		newBalance, err := domain.MoneyFromDecimal(sum)
		if err != nil {
			t.Fatalf("new balance of the settling deposit: %v", err)
		}
		tx.NewBalance = &newBalance
	}
	return tx
}

// theStoreWithTheSettlingDeposit holds the completed withdraw, its deposit
// in the given status, and the bet in the given status.
func theStoreWithTheSettlingDeposit(t *testing.T, depositStatus domain.TransactionStatus, amount string, betStatus domain.BetStatus) storeContents {
	t.Helper()
	return storeContents{
		transactions: []domain.Transaction{
			theSettlingDeposit(t, depositStatus, amount),
			theWithdraw(t, domain.TransactionCompleted),
		},
		bets: []domain.Bet{theBet(t, betStatus)},
	}
}

// Assertions

func assertNoCancelWalletCallsAndStoreHolds(t *testing.T, fixture cancelFixture, want storeContents) {
	t.Helper()
	assertWalletCalls(t, fixture.wallet)
	assertStoreHolds(t, fixture.store, want)
}

func assertOnlyTheBalanceWasReadForCancelAndStoreHolds(t *testing.T, fixture cancelFixture, want storeContents) {
	t.Helper()
	assertWalletCalls(t, fixture.wallet, balanceCall())
	assertStoreHolds(t, fixture.store, want)
}
