package domaintest

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

const testUserID domain.UserID = 34633089486

func TestFakeWallet_Balance_ReturnsTheSeededBalance(t *testing.T) {
	wallet := givenAFakeWallet(t)

	result, err := wallet.Balance(context.Background(), testUserID)

	assertWalletBalance(t, result, err, "5000.00", domain.USD)
}

func TestFakeWallet_Balance_UnknownUser_IsWalletFailure(t *testing.T) {
	wallet := givenAFakeWallet(t)

	_, err := wallet.Balance(context.Background(), 12345)

	assertErrorIs(t, err, domain.ErrWalletFailure)
}

func TestFakeWallet_Withdraw_ReturnsTheNewBalance(t *testing.T) {
	wallet := givenAFakeWallet(t)

	result, err := wallet.Withdraw(context.Background(), givenAnOperation(t, "10.50", "ref-1"))

	assertMoney(t, result, err, "4989.50")
}

func TestFakeWallet_Withdraw_UpdatesTheBalance(t *testing.T) {
	wallet := givenAFakeWallet(t)

	wallet.Withdraw(context.Background(), givenAnOperation(t, "10.50", "ref-1"))

	assertBalanceOf(t, wallet, "4989.50")
}

func TestFakeWallet_Deposit_AddsTheAmount(t *testing.T) {
	wallet := givenAFakeWallet(t)

	result, err := wallet.Deposit(context.Background(), givenAnOperation(t, "25.25", "ref-1"))

	assertMoney(t, result, err, "5025.25")
}

// The real Wallet requires amount < balance (findings Q5).
func TestFakeWallet_Withdraw_NotLowerThanTheBalance_IsInsufficientFunds(t *testing.T) {
	testCases := []struct {
		name   string
		amount string
	}{
		{name: "the whole balance", amount: "5000.00"},
		{name: "more than the balance", amount: "5000.01"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			wallet := givenAFakeWallet(t)

			_, err := wallet.Withdraw(context.Background(), givenAnOperation(t, testCase.amount, "ref-1"))

			assertErrorIs(t, err, domain.ErrInsufficientFunds)
		})
	}
}

func TestFakeWallet_Withdraw_AnotherCurrency_IsRejected(t *testing.T) {
	wallet := givenAFakeWallet(t)
	operation := givenAnOperation(t, "10.00", "ref-1")
	operation.Currency = domain.EUR

	_, err := wallet.Withdraw(context.Background(), operation)

	assertErrorIs(t, err, domain.ErrWalletRejected)
}

// A re-send of an applied reference is what the real client sees as a 409.
func TestFakeWallet_SameReferenceTwice_IsAppliedWithUnknownBalance(t *testing.T) {
	wallet := givenAFakeWallet(t)
	wallet.Withdraw(context.Background(), givenAnOperation(t, "10.00", "ref-1"))

	_, err := wallet.Withdraw(context.Background(), givenAnOperation(t, "10.00", "ref-1"))

	assertErrorIs(t, err, domain.ErrWalletAppliedBalanceUnknown)
}

func TestFakeWallet_SameReferenceTwice_IsAppliedOnce(t *testing.T) {
	wallet := givenAFakeWallet(t)
	wallet.Withdraw(context.Background(), givenAnOperation(t, "10.00", "ref-1"))

	wallet.Withdraw(context.Background(), givenAnOperation(t, "10.00", "ref-1"))

	assertBalanceOf(t, wallet, "4990.00")
}

func TestFakeWallet_ScriptedOperationFailure_ReturnsTheError(t *testing.T) {
	testCases := []struct {
		behaviour Behaviour
		want      error
	}{
		{behaviour: FailBeforeApply, want: domain.ErrWalletOutcomeUnknown},
		{behaviour: FailAfterApply, want: domain.ErrWalletOutcomeUnknown},
		{behaviour: Reject, want: domain.ErrWalletRejected},
		{behaviour: TimeOut, want: domain.ErrWalletOutcomeUnknown},
	}

	for _, testCase := range testCases {
		t.Run(behaviourName(testCase.behaviour), func(t *testing.T) {
			wallet := givenAFakeWallet(t)
			wallet.Next(testCase.behaviour)

			_, err := wallet.Withdraw(context.Background(), givenAnOperation(t, "10.00", "ref-1"))

			assertErrorIs(t, err, testCase.want)
		})
	}
}

func TestFakeWallet_ScriptedOperationFailure_AppliesOnlyAfterApply(t *testing.T) {
	testCases := []struct {
		behaviour Behaviour
		want      string
	}{
		{behaviour: FailBeforeApply, want: "5000.00"},
		{behaviour: FailAfterApply, want: "4990.00"},
		{behaviour: Reject, want: "5000.00"},
		{behaviour: TimeOut, want: "5000.00"},
	}

	for _, testCase := range testCases {
		t.Run(behaviourName(testCase.behaviour), func(t *testing.T) {
			wallet := givenAFakeWallet(t)
			wallet.Next(testCase.behaviour)

			wallet.Withdraw(context.Background(), givenAnOperation(t, "10.00", "ref-1"))

			assertBalanceOf(t, wallet, testCase.want)
		})
	}
}

// The Phase 11 resolution: re-sending tells whether the first attempt applied.
func TestFakeWallet_ResendAfterFailBeforeApply_IsApplied(t *testing.T) {
	wallet := givenAFakeWallet(t)
	wallet.Next(FailBeforeApply)
	wallet.Withdraw(context.Background(), givenAnOperation(t, "10.00", "ref-1"))

	result, err := wallet.Withdraw(context.Background(), givenAnOperation(t, "10.00", "ref-1"))

	assertMoney(t, result, err, "4990.00")
}

func TestFakeWallet_ResendAfterFailAfterApply_IsAppliedWithUnknownBalance(t *testing.T) {
	wallet := givenAFakeWallet(t)
	wallet.Next(FailAfterApply)
	wallet.Withdraw(context.Background(), givenAnOperation(t, "10.00", "ref-1"))

	_, err := wallet.Withdraw(context.Background(), givenAnOperation(t, "10.00", "ref-1"))

	assertErrorIs(t, err, domain.ErrWalletAppliedBalanceUnknown)
}

func TestFakeWallet_ScriptedBalanceFailure_ReturnsTheError(t *testing.T) {
	testCases := []struct {
		behaviour Behaviour
		want      error
	}{
		{behaviour: FailBeforeApply, want: domain.ErrWalletFailure},
		{behaviour: FailAfterApply, want: domain.ErrWalletFailure},
		{behaviour: Reject, want: domain.ErrWalletFailure},
		{behaviour: TimeOut, want: domain.ErrWalletTimeout},
	}

	for _, testCase := range testCases {
		t.Run(behaviourName(testCase.behaviour), func(t *testing.T) {
			wallet := givenAFakeWallet(t)
			wallet.Next(testCase.behaviour)

			_, err := wallet.Balance(context.Background(), testUserID)

			assertErrorIs(t, err, testCase.want)
		})
	}
}

func TestFakeWallet_Next_ScriptsTheCallsInOrder(t *testing.T) {
	wallet := givenAFakeWallet(t)
	wallet.Next(Reject, Succeed)
	wallet.Withdraw(context.Background(), givenAnOperation(t, "10.00", "ref-1"))

	result, err := wallet.Withdraw(context.Background(), givenAnOperation(t, "10.00", "ref-2"))

	assertMoney(t, result, err, "4990.00")
}

func TestFakeWallet_Slow_SucceedsAfterTheDelay(t *testing.T) {
	wallet := givenAFakeWallet(t)
	wallet.SlowDelay = 10 * time.Millisecond
	wallet.Next(Slow)

	result, err := wallet.Withdraw(context.Background(), givenAnOperation(t, "10.00", "ref-1"))

	assertMoney(t, result, err, "4990.00")
}

func TestFakeWallet_Slow_CallerGivesUp_IsUnknown(t *testing.T) {
	wallet := givenAFakeWallet(t)
	wallet.SlowDelay = time.Second
	wallet.Next(Slow)
	ctx := givenACallerGivingUpAfter(t, 10*time.Millisecond)

	_, err := wallet.Withdraw(ctx, givenAnOperation(t, "10.00", "ref-1"))

	assertErrorIs(t, err, domain.ErrWalletOutcomeUnknown)
}

func TestFakeWallet_Slow_CallerGivesUp_IsNotApplied(t *testing.T) {
	wallet := givenAFakeWallet(t)
	wallet.SlowDelay = time.Second
	wallet.Next(Slow)
	ctx := givenACallerGivingUpAfter(t, 10*time.Millisecond)

	wallet.Withdraw(ctx, givenAnOperation(t, "10.00", "ref-1"))

	assertBalanceOf(t, wallet, "5000.00")
}

func TestFakeWallet_Calls_RecordsWhatWasSent(t *testing.T) {
	wallet := givenAFakeWallet(t)
	operation := givenAnOperation(t, "10.00", "ref-1")
	wallet.Withdraw(context.Background(), operation)

	result := wallet.Calls()

	assertCalls(t, result, []Call{{Method: "Withdraw", UserID: testUserID, Operation: operation}})
}

/////////////////////////////////////////////////////////////////////////////////////

// givenAFakeWallet holds one USD player with 5000.00.
func givenAFakeWallet(t *testing.T) *FakeWallet {
	t.Helper()
	return NewFakeWallet(map[domain.UserID]domain.WalletBalance{
		testUserID: {Amount: givenAParsedMoney(t, "5000.00"), Currency: domain.USD},
	})
}

func givenAnOperation(t *testing.T, amount string, reference domain.TransactionID) domain.WalletOperation {
	t.Helper()
	return domain.WalletOperation{
		UserID:    testUserID,
		Currency:  domain.USD,
		Amount:    givenAParsedMoney(t, amount),
		BetID:     1001,
		Reference: reference,
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

func behaviourName(behaviour Behaviour) string {
	return [...]string{"Succeed", "FailBeforeApply", "FailAfterApply", "Reject", "TimeOut", "Slow"}[behaviour]
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func assertErrorIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("expected %v to be %q", err, target)
	}
}

func assertMoney(t *testing.T, result domain.Money, err error, want string) {
	t.Helper()
	assertNoError(t, err)
	if !result.Equal(givenAParsedMoney(t, want)) {
		t.Errorf("expected %s, got %s", want, result)
	}
}

func assertBalanceOf(t *testing.T, wallet *FakeWallet, want string) {
	t.Helper()
	if result := wallet.BalanceOf(testUserID); !result.Equal(givenAParsedMoney(t, want)) {
		t.Errorf("expected balance %s, got %s", want, result)
	}
}

func assertWalletBalance(t *testing.T, result domain.WalletBalance, err error, amount string, currency domain.Currency) {
	t.Helper()
	assertNoError(t, err)
	if !result.Amount.Equal(givenAParsedMoney(t, amount)) || result.Currency != currency {
		t.Errorf("expected %s %s, got %s %s", amount, currency, result.Amount, result.Currency)
	}
}

func assertCalls(t *testing.T, result, want []Call) {
	t.Helper()
	if len(result) != len(want) {
		t.Fatalf("expected %d call(s), got %d: %+v", len(want), len(result), result)
	}
	for index := range want {
		if result[index] != want[index] {
			t.Errorf("call %d: expected %+v, got %+v", index, want[index], result[index])
		}
	}
}
