package domain

import (
	"testing"
)

// Each test covers one constructor rule or one allowed / refused status
// transition of a transaction.

// ----- Constructors -----

func TestNewWithdraw_PositiveAmount_IsPending(t *testing.T) {
	result, err := NewWithdraw(givenTransactionDetails(t, "10.50"), testNow)

	assertNoError(t, err)
	assertTransactionStatus(t, &result, TransactionPending)
}

func TestNewWithdraw_ZeroAmount_ReturnsInvalidAmount(t *testing.T) {
	_, err := NewWithdraw(givenTransactionDetails(t, "0"), testNow)

	assertErrorIs(t, err, ErrInvalidAmount)
}

func TestNewWithdraw_HasNoNewBalance(t *testing.T) {
	result := givenAPendingWithdraw(t)

	assertTrue(t, result.NewBalance == nil, "a pending withdraw should have no new balance")
}

func TestNewDeposit_ZeroAmount_IsPending(t *testing.T) {
	result := NewDeposit(givenTransactionDetails(t, "0"), testNow)

	assertTransactionStatus(t, &result, TransactionPending)
}

func TestNewDeposit_HasDepositType(t *testing.T) {
	result := NewDeposit(givenTransactionDetails(t, "25.00"), testNow)

	assertTransactionType(t, &result, TransactionDeposit)
}

func TestNewCancel_HasCancelType(t *testing.T) {
	original := givenACompletedWithdraw(t)

	result := NewCancel("cancel-1", *original, givenAParsedMoney(t, "4989.50"), testLater)

	assertTransactionType(t, &result, TransactionCancel)
}

func TestNewCancel_PointsToOriginal(t *testing.T) {
	original := givenACompletedWithdraw(t)

	result := NewCancel("cancel-1", *original, givenAParsedMoney(t, "4989.50"), testLater)

	assertTrue(t, result.CancelledTransactionID == original.ID, "cancel should point to the original transaction")
}

func TestNewCancel_CopiesOriginalAmount(t *testing.T) {
	original := givenACompletedWithdraw(t)

	result := NewCancel("cancel-1", *original, givenAParsedMoney(t, "4989.50"), testLater)

	assertTrue(t, result.Amount.Equal(original.Amount), "cancel should revert the original amount")
}

func TestNewCancel_KeepsOriginalProviderTransactionID(t *testing.T) {
	original := givenACompletedWithdraw(t)

	result := NewCancel("cancel-1", *original, givenAParsedMoney(t, "4989.50"), testLater)

	assertTrue(t, result.ProviderTransactionID == original.ProviderTransactionID,
		"cancel should carry the provider transaction id of the original (§7.4)")
}

// ----- Complete (Wallet confirmed the operation) -----

func TestTransaction_CompletePending_IsCompleted(t *testing.T) {
	result := givenAPendingWithdraw(t)

	err := result.Complete(givenAParsedMoney(t, "4989.50"), testLater)

	assertTransactionTransitionTo(t, result, err, TransactionCompleted)
}

func TestTransaction_CompletePending_SetsNewBalance(t *testing.T) {
	result := givenAPendingWithdraw(t)

	_ = result.Complete(givenAParsedMoney(t, "4989.50"), testLater)

	assertNewBalance(t, result, givenAParsedMoney(t, "4989.50"))
}

func TestTransaction_CompleteCompleted_ReturnsInvalidTransition(t *testing.T) {
	result := givenACompletedWithdraw(t)

	err := result.Complete(givenAParsedMoney(t, "4989.50"), testLater)

	assertErrorIs(t, err, ErrInvalidTransition)
}

func TestTransaction_CompleteFailed_ReturnsInvalidTransition(t *testing.T) {
	result := givenAFailedWithdraw(t)

	err := result.Complete(givenAParsedMoney(t, "4989.50"), testLater)

	assertErrorIs(t, err, ErrInvalidTransition)
}

// ----- Fail (the Wallet did not apply the operation) -----

func TestTransaction_FailPending_IsFailed(t *testing.T) {
	result := givenAPendingWithdraw(t)

	err := result.Fail(FailureWalletRejected, testLater)

	assertTransactionTransitionTo(t, result, err, TransactionFailed)
}

func TestTransaction_FailPending_StoresTheReason(t *testing.T) {
	testCases := []struct {
		name   string
		reason FailureReason
	}{
		{name: "insufficient funds", reason: FailureInsufficientFunds},
		{name: "wallet rejected", reason: FailureWalletRejected},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := givenAPendingWithdraw(t)

			_ = result.Fail(testCase.reason, testLater)

			assertFailureReason(t, result, testCase.reason)
		})
	}
}

func TestTransaction_FailWithUnknownReason_ReturnsError(t *testing.T) {
	result := givenAPendingWithdraw(t)

	err := result.Fail("SOMETHING_ELSE", testLater)

	assertTrue(t, err != nil, "an unknown failure reason should be refused")
}

func TestTransaction_FailWithUnknownReason_KeepsPending(t *testing.T) {
	result := givenAPendingWithdraw(t)

	_ = result.Fail("SOMETHING_ELSE", testLater)

	assertTransactionStatus(t, result, TransactionPending)
}

func TestTransaction_FailCompleted_ReturnsInvalidTransition(t *testing.T) {
	result := givenACompletedWithdraw(t)

	err := result.Fail(FailureWalletRejected, testLater)

	assertErrorIs(t, err, ErrInvalidTransition)
}

// ----- RecordAttempt (the Wallet is called again for a PENDING one) -----

func TestTransaction_RecordAttemptPending_MovesUpdatedAt(t *testing.T) {
	result := givenAPendingWithdraw(t)

	_ = result.RecordAttempt(testLater)

	assertTrue(t, result.UpdatedAt.Equal(testLater), "UpdatedAt should be the time of the new attempt")
}

func TestTransaction_RecordAttemptPending_KeepsItPending(t *testing.T) {
	result := givenAPendingWithdraw(t)

	err := result.RecordAttempt(testLater)

	assertTransactionTransitionTo(t, result, err, TransactionPending)
}

func TestTransaction_RecordAttemptCompleted_ReturnsInvalidTransition(t *testing.T) {
	result := givenACompletedWithdraw(t)

	err := result.RecordAttempt(testLater)

	assertErrorIs(t, err, ErrInvalidTransition)
}

// ----- FailureError (what a retry of a FAILED transaction gets) -----

func TestTransaction_FailureError_IsTheErrorOfTheFirstAttempt(t *testing.T) {
	testCases := []struct {
		name     string
		reason   FailureReason
		expected error
	}{
		{name: "insufficient funds", reason: FailureInsufficientFunds, expected: ErrInsufficientFunds},
		{name: "wallet rejected", reason: FailureWalletRejected, expected: ErrWalletRejected},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tx := givenAFailedWithdrawBecause(t, testCase.reason)

			result := tx.FailureError()

			assertErrorIs(t, result, testCase.expected)
		})
	}
}

func TestTransaction_FailureErrorWithoutReason_IsPreviousAttemptFailed(t *testing.T) {
	tx := givenAFailedWithdrawWithoutReason(t)

	result := tx.FailureError()

	assertErrorIs(t, result, ErrPreviousAttemptFailed)
}

// ----- MarkCancelled (reverted by a Cancel) -----

func TestTransaction_MarkCancelledCompletedWithdraw_IsCancelled(t *testing.T) {
	result := givenACompletedWithdraw(t)

	err := result.MarkCancelled(testLater)

	assertTransactionTransitionTo(t, result, err, TransactionCancelled)
}

func TestTransaction_MarkCancelledCompletedDeposit_IsCancelled(t *testing.T) {
	result := givenACompletedDeposit(t)

	err := result.MarkCancelled(testLater)

	assertTransactionTransitionTo(t, result, err, TransactionCancelled)
}

func TestTransaction_MarkCancelledPending_ReturnsInvalidTransition(t *testing.T) {
	result := givenAPendingWithdraw(t)

	err := result.MarkCancelled(testLater)

	assertErrorIs(t, err, ErrInvalidTransition)
}

func TestTransaction_MarkCancelledFailed_ReturnsInvalidTransition(t *testing.T) {
	result := givenAFailedWithdraw(t)

	err := result.MarkCancelled(testLater)

	assertErrorIs(t, err, ErrInvalidTransition)
}

func TestTransaction_MarkCancelledCompletedCancel_ReturnsInvalidTransition(t *testing.T) {
	result := givenACompletedCancel(t)

	err := result.MarkCancelled(testLater)

	assertErrorIs(t, err, ErrInvalidTransition)
}

// ----- A refused transition changes nothing -----

func TestTransaction_RefusedTransition_KeepsStatus(t *testing.T) {
	result := givenAFailedWithdraw(t)

	_ = result.Complete(givenAParsedMoney(t, "4989.50"), testLater)

	assertTransactionStatus(t, result, TransactionFailed)
}

func TestTransaction_RefusedTransition_KeepsNoNewBalance(t *testing.T) {
	result := givenAFailedWithdraw(t)

	_ = result.Complete(givenAParsedMoney(t, "4989.50"), testLater)

	assertTrue(t, result.NewBalance == nil, "a refused Complete should not set a new balance")
}

/////////////////////////////////////////////////////////////////////////////////////

func givenTransactionDetails(t *testing.T, amount string) TransactionDetails {
	t.Helper()
	return TransactionDetails{
		ID:                    "tx-1",
		UserID:                UserID(34633089486),
		BetID:                 BetID(1),
		ProviderTransactionID: "prov-tx-0001",
		Amount:                givenAParsedMoney(t, amount),
		Currency:              USD,
		OldBalance:            givenAParsedMoney(t, "5000.00"),
	}
}

func givenAPendingWithdraw(t *testing.T) *Transaction {
	t.Helper()
	tx, err := NewWithdraw(givenTransactionDetails(t, "10.50"), testNow)
	givenTransactionStepSucceeds(t, err, "create withdraw")
	return &tx
}

func givenACompletedWithdraw(t *testing.T) *Transaction {
	t.Helper()
	tx := givenAPendingWithdraw(t)
	givenTransactionStepSucceeds(t, tx.Complete(givenAParsedMoney(t, "4989.50"), testNow), "complete withdraw")
	return tx
}

func givenAFailedWithdraw(t *testing.T) *Transaction {
	t.Helper()
	return givenAFailedWithdrawBecause(t, FailureWalletRejected)
}

func givenAFailedWithdrawBecause(t *testing.T, reason FailureReason) *Transaction {
	t.Helper()
	tx := givenAPendingWithdraw(t)
	givenTransactionStepSucceeds(t, tx.Fail(reason, testNow), "fail withdraw")
	return tx
}

// givenAFailedWithdrawWithoutReason is a row that failed before failure
// reasons were stored.
func givenAFailedWithdrawWithoutReason(t *testing.T) *Transaction {
	t.Helper()
	tx := givenAFailedWithdraw(t)
	tx.FailureReason = ""
	return tx
}

func givenACompletedDeposit(t *testing.T) *Transaction {
	t.Helper()
	tx := NewDeposit(givenTransactionDetails(t, "25.00"), testNow)
	givenTransactionStepSucceeds(t, tx.Complete(givenAParsedMoney(t, "5014.50"), testNow), "complete deposit")
	return &tx
}

func givenACompletedCancel(t *testing.T) *Transaction {
	t.Helper()
	original := givenACompletedWithdraw(t)
	tx := NewCancel("cancel-1", *original, givenAParsedMoney(t, "4989.50"), testNow)
	givenTransactionStepSucceeds(t, tx.Complete(givenAParsedMoney(t, "5000.00"), testNow), "complete cancel")
	return &tx
}

// givenTransactionStepSucceeds stops the test if an Arrange step failed:
// that is broken test data, not the behaviour under test.
func givenTransactionStepSucceeds(t *testing.T, err error, step string) {
	t.Helper()
	if err != nil {
		t.Fatalf("arrange: could not %s: %v", step, err)
	}
}

// assertTransactionTransitionTo checks that a transition succeeded and left
// the transaction in the expected status.
func assertTransactionTransitionTo(t *testing.T, result *Transaction, err error, expected TransactionStatus) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertTransactionStatus(t, result, expected)
}

func assertTransactionStatus(t *testing.T, result *Transaction, expected TransactionStatus) {
	t.Helper()
	if result.Status != expected {
		t.Errorf("expected transaction status %q, got %q", expected, result.Status)
	}
}

func assertFailureReason(t *testing.T, result *Transaction, expected FailureReason) {
	t.Helper()
	if result.FailureReason != expected {
		t.Errorf("expected failure reason %q, got %q", expected, result.FailureReason)
	}
}

func assertTransactionType(t *testing.T, result *Transaction, expected TransactionType) {
	t.Helper()
	if result.Type != expected {
		t.Errorf("expected transaction type %q, got %q", expected, result.Type)
	}
}

func assertNewBalance(t *testing.T, result *Transaction, expected Money) {
	t.Helper()
	if result.NewBalance == nil {
		t.Fatalf("expected new balance %s, got nil", expected)
	}
	if !result.NewBalance.Equal(expected) {
		t.Errorf("expected new balance %s, got %s", expected, *result.NewBalance)
	}
}
