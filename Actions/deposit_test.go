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
			transactions: []domain.Transaction{anotherPlayersWithdraw(t)},
		}, domain.ErrTransactionNotFound},
		{"it is a deposit", storeContents{
			transactions: []domain.Transaction{aDepositWithTheTestProviderID(t, "10.50")},
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
	fixture.deposit = NewDepositCommandHandler(
		fixture.players,
		fixture.store,
		fixture.wallet,
		&domaintest.FakeIDGenerator{},
		domaintest.FixedClock{Time: testNow},
		slog.New(slog.NewJSONHandler(fixture.logs, nil)),
	)
	return fixture
}

func theArrangedStore(t *testing.T) storeContents {
	t.Helper()
	return storeContents{
		transactions: []domain.Transaction{theWithdraw(t, domain.TransactionCompleted)},
		bets:         []domain.Bet{theBet(t, domain.BetPlaced)},
	}
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

// Assertions

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
