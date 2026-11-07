package domain

import (
	"testing"
)

func TestDecideCancel_CompletedWithdrawPlacedBet_CreditsBackWithdrawAmount(t *testing.T) {
	withdraw := givenACompletedWithdraw(t)
	bet := givenAPlacedBet(t)

	result, err := DecideCancel(*withdraw, *bet)

	assertCancelDecision(t, result, err, CancelCreditBack, withdraw.Amount)
}

func TestDecideCancel_CompletedWithdrawWonBet_ReturnsBetAlreadySettled(t *testing.T) {
	withdraw := givenACompletedWithdraw(t)
	bet := givenAWonBet(t)

	_, err := DecideCancel(*withdraw, *bet)

	assertErrorIs(t, err, ErrBetAlreadySettled)
}

func TestDecideCancel_CompletedWithdrawLostBet_ReturnsBetAlreadySettled(t *testing.T) {
	withdraw := givenACompletedWithdraw(t)
	bet := givenALostBet(t)

	_, err := DecideCancel(*withdraw, *bet)

	assertErrorIs(t, err, ErrBetAlreadySettled)
}

func TestDecideCancel_CompletedPositiveDeposit_DebitsBackDepositAmount(t *testing.T) {
	deposit := givenACompletedDeposit(t)
	bet := givenAWonBet(t)

	result, err := DecideCancel(*deposit, *bet)

	assertCancelDecision(t, result, err, CancelDebitBack, deposit.Amount)
}

func TestDecideCancel_CompletedZeroDeposit_NeedsNoWalletCall(t *testing.T) {
	deposit := givenACompletedZeroDeposit(t)
	bet := givenALostBet(t)

	result, err := DecideCancel(*deposit, *bet)

	assertCancelDecision(t, result, err, CancelNoWalletCall, Money{})
}

func TestDecideCancel_CancelledTransaction_ReturnsAlreadyCancelled(t *testing.T) {
	withdraw := givenACancelledWithdraw(t)
	bet := givenACancelledBet(t)

	_, err := DecideCancel(*withdraw, *bet)

	assertErrorIs(t, err, ErrAlreadyCancelled)
}

func TestDecideCancel_PendingTransaction_ReturnsTransactionPending(t *testing.T) {
	withdraw := givenAPendingWithdraw(t)
	bet := givenAPlacedBet(t)

	_, err := DecideCancel(*withdraw, *bet)

	assertErrorIs(t, err, ErrTransactionPending)
}

func TestDecideCancel_FailedTransaction_ReturnsNothingToRevert(t *testing.T) {
	withdraw := givenAFailedWithdraw(t)
	bet := givenAPlacedBet(t)

	_, err := DecideCancel(*withdraw, *bet)

	assertErrorIs(t, err, ErrNothingToRevert)
}

// Not a row of §8.5: a CANCEL transaction itself cannot be cancelled.
func TestDecideCancel_CompletedCancel_ReturnsInvalidTransition(t *testing.T) {
	cancel := givenACompletedCancel(t)
	bet := givenACancelledBet(t)

	_, err := DecideCancel(*cancel, *bet)

	assertErrorIs(t, err, ErrInvalidTransition)
}

/////////////////////////////////////////////////////////////////////////////////////

func givenACompletedZeroDeposit(t *testing.T) *Transaction {
	t.Helper()
	tx := NewDeposit(givenTransactionDetails(t, "0"), testNow)
	givenTransactionStepSucceeds(t, tx.Complete(givenAParsedMoney(t, "4989.50"), testNow), "complete zero deposit")
	return &tx
}

func givenACancelledWithdraw(t *testing.T) *Transaction {
	t.Helper()
	tx := givenACompletedWithdraw(t)
	givenTransactionStepSucceeds(t, tx.MarkCancelled(testNow), "cancel withdraw")
	return tx
}

func assertCancelDecision(t *testing.T, result CancelDecision, err error, expectedAction CancelAction, expectedAmount Money) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != expectedAction {
		t.Errorf("expected action %q, got %q", expectedAction, result.Action)
	}
	if !result.Amount.Equal(expectedAmount) {
		t.Errorf("expected amount %s, got %s", expectedAmount, result.Amount)
	}
}
