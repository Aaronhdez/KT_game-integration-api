package actions

import (
	"errors"
	"fmt"
	"testing"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

func TestClassifyWalletResult_NoError_IsApplied(t *testing.T) {
	result := classifyWalletResult(nil)

	assertWalletOutcome(t, result, walletApplied)
}

// The WalletClient contract: only a definitive rejection is "not
// applied"; anything the Wallet client did not classify is unknown.
var walletErrors = []struct {
	name string
	err  error
	want walletOutcome
}{
	{"applied, balance unknown", domain.ErrWalletAppliedBalanceUnknown, walletAppliedBalanceUnknown},
	{"insufficient funds", domain.ErrInsufficientFunds, walletNotApplied},
	{"rejected", domain.ErrWalletRejected, walletNotApplied},
	{"outcome unknown", domain.ErrWalletOutcomeUnknown, walletOutcomeUnknown},
	{"unrecognised error", errors.New("connection reset by peer"), walletOutcomeUnknown},
	// A wallet failure that is not a rejection (e.g. a failed read) is not a
	// proof that an operation was not applied.
	{"wallet failure that is not a rejection", domain.ErrWalletFailure, walletOutcomeUnknown},
}

func TestClassifyWalletResult_Error_ReturnsTheOutcomeOfTheContract(t *testing.T) {
	for _, testCase := range walletErrors {
		t.Run(testCase.name, func(t *testing.T) {
			result := classifyWalletResult(testCase.err)

			assertWalletOutcome(t, result, testCase.want)
		})
	}
}

// The Wallet client wraps its errors with context; the outcome must not change.
func TestClassifyWalletResult_WrappedError_ReturnsTheSameOutcome(t *testing.T) {
	for _, testCase := range walletErrors {
		t.Run(testCase.name, func(t *testing.T) {
			result := classifyWalletResult(fmt.Errorf("wallet answered status 500: %w", testCase.err))

			assertWalletOutcome(t, result, testCase.want)
		})
	}
}

func assertWalletOutcome(t *testing.T, result, want walletOutcome) {
	t.Helper()
	if result != want {
		t.Errorf("expected %s, got %s", want, result)
	}
}
