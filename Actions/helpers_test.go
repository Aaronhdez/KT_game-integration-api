package actions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
	"github.com/aaronhdez/kt_game-integration-api/Domain/domaintest"
)

// Test data, builders, assertions and descriptions shared by the use case
// tests of this package. Wallet wrappers are in wallets_test.go.

const (
	testUserID     domain.UserID = 34633089486
	testUsername                 = "player_usd"
	testProviderID               = "prov-tx-0001"
	anotherUserID  domain.UserID = 34673635133
)

var testNow = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

var errDatabaseDown = errors.New("database down")

// Wallet behaviours that leave an operation's outcome unknown (§9.3).
var unknownOutcomes = []struct {
	name      string
	behaviour domaintest.Behaviour
}{
	{"error after applying", domaintest.FailAfterApply},
	{"error before applying", domaintest.FailBeforeApply},
	{"timeout", domaintest.TimeOut},
}

// Ways the balance read before an operation can fail.
var balanceReadFailures = []struct {
	name      string
	behaviour domaintest.Behaviour
}{
	{"failure", domaintest.Reject},
	{"timeout", domaintest.TimeOut},
}

// Builders

func aUSDPlayer() domain.Player {
	return domain.Player{UserID: testUserID, Username: testUsername, Currency: domain.USD}
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

// resendTime is the first moment a transaction left PENDING at testNow can be
// re-sent.
var resendTime = testNow.Add(resendDelay)

// resentAt is tx after a re-send started at the given time: UpdatedAt records
// the last attempt (and the outcome, if any, is stored with the same time).
func resentAt(tx domain.Transaction, at time.Time) domain.Transaction {
	tx.UpdatedAt = at
	return tx
}

// givenThePlayerWonElsewhereMeanwhile credits the test player in the Wallet
// with another operation, as another game would between two attempts.
func givenThePlayerWonElsewhereMeanwhile(t *testing.T, wallet *domaintest.FakeWallet, amount string) {
	t.Helper()
	if _, err := wallet.Deposit(context.Background(), anotherGamesOperation(t, amount)); err != nil {
		t.Fatalf("arrange: credit the player elsewhere: %v", err)
	}
}

// givenThePlayerSpentElsewhereMeanwhile debits the test player in the Wallet
// with another operation, as another game would between two attempts.
func givenThePlayerSpentElsewhereMeanwhile(t *testing.T, wallet *domaintest.FakeWallet, amount string) {
	t.Helper()
	if _, err := wallet.Withdraw(context.Background(), anotherGamesOperation(t, amount)); err != nil {
		t.Fatalf("arrange: debit the player elsewhere: %v", err)
	}
}

func anotherGamesOperation(t *testing.T, amount string) domain.WalletOperation {
	t.Helper()
	return domain.WalletOperation{
		UserID:    testUserID,
		Currency:  domain.USD,
		Amount:    givenAParsedMoney(t, amount),
		BetID:     999,
		Reference: "another-game",
	}
}

func betStatus(status domain.BetStatus) *domain.BetStatus {
	return &status
}

func balanceCall() domaintest.Call {
	return domaintest.Call{Method: "Balance", UserID: testUserID}
}

// Assertions

func assertErrorIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("expected %v to be %q", err, target)
	}
}

func assertReplayOf(t *testing.T, result TransactionResult, err error, want domain.Transaction, wantBetStatus *domain.BetStatus) {
	t.Helper()
	assertResultTransaction(t, result, err, want)
	assertBetStatusAndReplayed(t, result, err, wantBetStatus, true)
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

func assertWalletBalance(t *testing.T, wallet *domaintest.FakeWallet, want string) {
	t.Helper()
	if result := wallet.BalanceOf(testUserID); !result.Equal(givenAParsedMoney(t, want)) {
		t.Errorf("expected the Wallet balance to be %s, got %s", want, result)
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
	return fmt.Sprintf("%s %s %s reason=%q user=%d bet=%d provider=%s cancels=%q amount=%s %s old=%s new=%s created=%s updated=%s",
		tx.ID, tx.Type, tx.Status, tx.FailureReason, tx.UserID, tx.BetID, tx.ProviderTransactionID, tx.CancelledTransactionID,
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
