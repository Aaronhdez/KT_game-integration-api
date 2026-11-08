package domain

import (
	"errors"
	"fmt"
	"testing"
)

// Every specific error must be recognised as its category: that is what the
// Application layer relies on to choose the HTTP status.
func TestSpecificErrors_AreRecognisedAsTheirCategory(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		category error
	}{
		{name: "invalid amount", err: ErrInvalidAmount, category: ErrInvalidInput},
		{name: "invalid currency", err: ErrInvalidCurrency, category: ErrInvalidInput},
		{name: "currency mismatch", err: ErrCurrencyMismatch, category: ErrInvalidInput},
		{name: "invalid provider transaction id", err: ErrInvalidProviderTransactionID, category: ErrInvalidInput},
		{name: "missing credentials", err: ErrMissingCredentials, category: ErrInvalidInput},
		{name: "invalid credentials", err: ErrInvalidCredentials, category: ErrUnauthorized},
		{name: "invalid token", err: ErrInvalidToken, category: ErrUnauthorized},
		{name: "transaction not found", err: ErrTransactionNotFound, category: ErrNotFound},
		{name: "player not found", err: ErrPlayerNotFound, category: ErrNotFound},
		{name: "bet not found", err: ErrBetNotFound, category: ErrNotFound},
		{name: "idempotency conflict", err: ErrIdempotencyConflict, category: ErrConflict},
		{name: "transaction exists", err: ErrTransactionExists, category: ErrConflict},
		{name: "previous attempt failed", err: ErrPreviousAttemptFailed, category: ErrConflict},
		{name: "bet already settled", err: ErrBetAlreadySettled, category: ErrConflict},
		{name: "bet not settled", err: ErrBetNotSettled, category: ErrConflict},
		{name: "transaction pending", err: ErrTransactionPending, category: ErrConflict},
		{name: "nothing to revert", err: ErrNothingToRevert, category: ErrConflict},
		{name: "withdraw not completed", err: ErrWithdrawNotCompleted, category: ErrConflict},
		{name: "invalid transition", err: ErrInvalidTransition, category: ErrConflict},
		{name: "wallet rejected", err: ErrWalletRejected, category: ErrWalletFailure},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assertErrorIs(t, testCase.err, testCase.category)
		})
	}
}

func TestSpecificError_WrappedAgainWithContext_IsStillItsCategory(t *testing.T) {
	err := fmt.Errorf("withdraw: %w", ErrBetAlreadySettled)

	assertErrorIs(t, err, ErrConflict)
}

func TestSpecificError_WrappedAgainWithContext_IsStillItself(t *testing.T) {
	err := fmt.Errorf("withdraw: %w", ErrBetAlreadySettled)

	assertErrorIs(t, err, ErrBetAlreadySettled)
}

func TestSpecificError_IsNotAnotherCategory(t *testing.T) {
	assertErrorIsNot(t, ErrBetAlreadySettled, ErrInvalidInput)
}

func TestWalletOutcomeUnknown_IsNotAnErrorCategory(t *testing.T) {
	assertErrorIsNoCategory(t, ErrWalletOutcomeUnknown)
}

func TestWalletAppliedBalanceUnknown_IsNotAnErrorCategory(t *testing.T) {
	assertErrorIsNoCategory(t, ErrWalletAppliedBalanceUnknown)
}

func TestAlreadyCancelled_IsNotAnErrorCategory(t *testing.T) {
	assertErrorIsNoCategory(t, ErrAlreadyCancelled)
}

/////////////////////////////////////////////////////////////////////////////////////

var allCategories = []error{
	ErrInvalidInput, ErrUnauthorized, ErrNotFound, ErrConflict,
	ErrInsufficientFunds, ErrWalletFailure, ErrWalletTimeout,
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
		t.Errorf("expected %q to be %q", err, target)
	}
}

func assertErrorIsNot(t *testing.T, err, target error) {
	t.Helper()
	if errors.Is(err, target) {
		t.Errorf("expected %q not to be %q", err, target)
	}
}

// assertErrorIsNoCategory checks that err would never be mapped to an HTTP error.
func assertErrorIsNoCategory(t *testing.T, err error) {
	t.Helper()
	for _, category := range allCategories {
		assertErrorIsNot(t, err, category)
	}
}
