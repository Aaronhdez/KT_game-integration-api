package domain

import "fmt"

type CancelAction string

const (
	CancelCreditBack   CancelAction = "CREDIT_BACK"
	CancelDebitBack    CancelAction = "DEBIT_BACK"
	CancelNoWalletCall CancelAction = "NO_WALLET_CALL"
)

type CancelDecision struct {
	Action CancelAction
	Amount Money
}

// DecideCancel applies the Cancel table of the assessmentto the
// transaction being cancelled and its bet. It only decides; it calls nothing.
// The "unknown or another player's transaction → 404" row is not handled here:
// it needs a repository lookup, done by the Cancel use case.
func DecideCancel(original Transaction, bet Bet) (CancelDecision, error) {
	switch original.Status {
	case TransactionCancelled:
		return CancelDecision{}, fmt.Errorf("%w: transaction %s", ErrAlreadyCancelled, original.ID)
	case TransactionPending:
		return CancelDecision{}, fmt.Errorf("%w: transaction %s", ErrTransactionPending, original.ID)
	case TransactionFailed:
		return CancelDecision{}, fmt.Errorf("%w: transaction %s", ErrNothingToRevert, original.ID)
	case TransactionCompleted:
		// handled below
	default:
		return CancelDecision{}, fmt.Errorf("%w: transaction %s is %s", ErrInvalidTransition, original.ID, original.Status)
	}

	switch original.Type {
	case TransactionWithdraw:
		return decideWithdrawCancel(original, bet)
	case TransactionDeposit:
		return decideDepositCancel(original)
	default:
		return CancelDecision{}, fmt.Errorf("%w: a %s transaction cannot be cancelled", ErrInvalidTransition, original.Type)
	}
}

// decideWithdrawCancel: the stake can only be given back while the bet is still
// PLACED; a settled bet needs its deposit cancelled first.
func decideWithdrawCancel(withdraw Transaction, bet Bet) (CancelDecision, error) {
	if bet.Status != BetPlaced {
		return CancelDecision{}, fmt.Errorf("%w: cancel the deposit of bet %d first", ErrBetAlreadySettled, bet.ID)
	}
	return CancelDecision{Action: CancelCreditBack, Amount: withdraw.Amount}, nil
}

// decideDepositCancel: a paid deposit is taken back; a zero deposit (lost bet)
// moved no money, so nothing is sent to the Wallet.
func decideDepositCancel(deposit Transaction) (CancelDecision, error) {
	if deposit.Amount.IsPositive() {
		return CancelDecision{Action: CancelDebitBack, Amount: deposit.Amount}, nil
	}
	return CancelDecision{Action: CancelNoWalletCall}, nil
}
