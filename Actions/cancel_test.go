package actions

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
	"github.com/aaronhdez/kt_game-integration-api/Domain/domaintest"
)

// The cancel tests revert theWithdraw (provider ID testProviderID, bet
// testBetID) or theSettlingDeposit (provider ID testDepositProviderID) of
// the same bet. The Wallet balance is 4989.50.

const testSettlingDepositNumber = 80 // its transaction ID is FakeTransactionID(80)

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

/////////////////////////////////////////////////////////////////////////////////////

// cancelFixture keeps the fakes reachable, so a test can script or break one
// of them and inspect what it received or stored.
type cancelFixture struct {
	cancel  *CancelCommandHandler
	players *domaintest.FakePlayerRepository
	store   *domaintest.FakeStore
	wallet  *domaintest.FakeWallet
	logs    *bytes.Buffer
}

// givenACancel has a completed withdraw of 10.50 whose bet is still PLACED
// (theArrangedStore of the deposit tests), and a Wallet balance of 4989.50.
func givenACancel(t *testing.T) cancelFixture {
	t.Helper()
	return newCancelFixture(t, domaintest.NewFakePlayerRepository(aUSDPlayer()), theArrangedStore(t))
}

func givenACancelWithoutPlayers(t *testing.T) cancelFixture {
	t.Helper()
	return newCancelFixture(t, domaintest.NewFakePlayerRepository(), theArrangedStore(t))
}

func givenACancelWithStore(t *testing.T, stored storeContents) cancelFixture {
	t.Helper()
	return newCancelFixture(t, domaintest.NewFakePlayerRepository(aUSDPlayer()), stored)
}

func newCancelFixture(t *testing.T, players *domaintest.FakePlayerRepository, stored storeContents) cancelFixture {
	t.Helper()
	fixture := cancelFixture{
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
	fixture.cancel = fixture.cancelWith(fixture.wallet)
	return fixture
}

// cancelWith builds the handler with another Wallet (a wrapper of the fake).
func (fixture cancelFixture) cancelWith(wallet domain.WalletClient) *CancelCommandHandler {
	return NewCancelCommandHandler(
		fixture.players,
		fixture.store,
		wallet,
		&domaintest.FakeIDGenerator{},
		domaintest.FixedClock{Time: testNow},
		slog.New(slog.NewJSONHandler(fixture.logs, nil)),
	)
}

func aCancelCommand(providerID string) CancelCommand {
	return CancelCommand{UserID: testUserID, ProviderTransactionID: providerID}
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
