package actions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
	"github.com/aaronhdez/kt_game-integration-api/Domain/domaintest"
)

const testProviderID = "prov-tx-0001"

var errDatabaseDown = errors.New("database down")

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

func aPlacedBet(t *testing.T, stake string) domain.Bet {
	t.Helper()
	return domain.NewPlacedBet(1, testUserID, domain.USD, givenAParsedMoney(t, stake), testNow)
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

// storeObservingWallet records what the store held when Withdraw was called.
type storeObservingWallet struct {
	*domaintest.FakeWallet
	store *domaintest.FakeStore
	seen  []domain.Transaction
}

func (wallet *storeObservingWallet) Withdraw(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	wallet.seen = wallet.store.Transactions()
	return wallet.FakeWallet.Withdraw(ctx, operation)
}

// balanceUnknownWallet applies the withdraw but reports that the new balance
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

// callerLeavingWallet cancels the caller's context while the Wallet applies
// the withdraw, as when the provider closes the connection.
type callerLeavingWallet struct {
	*domaintest.FakeWallet
	leave context.CancelFunc
}

func (wallet callerLeavingWallet) Withdraw(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	wallet.leave()
	return wallet.FakeWallet.Withdraw(ctx, operation)
}

// Assertions

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
