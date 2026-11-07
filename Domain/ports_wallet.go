package domain

import "context"

// WalletOperation is one money movement sent to the Wallet. Our API processes
// a single transaction per request, so each call carries exactly one.
type WalletOperation struct {
	UserID    UserID
	Currency  Currency
	Amount    Money
	BetID     BetID
	Reference TransactionID // our transaction ID; the Wallet de-duplicates re-sends by it
}

// WalletBalance is a player's balance as read from the Wallet, which is the
// source of truth for balances.
type WalletBalance struct {
	Amount   Money
	Currency Currency
}

// WalletClient is the Wallet service. Every implementation must classify each
// result as follows, because the use cases decide the transaction status from
// it:
//
// Withdraw and Deposit:
//   - nil                     → applied: the returned Money is the new balance.
//   - ErrWalletAppliedBalanceUnknown → applied, but the new balance is not
//     known (an earlier attempt with the same reference was applied, or the
//     reported balance is unusable). The transaction is COMPLETED.
//   - ErrInsufficientFunds   → not applied (the transaction becomes FAILED, 422).
//   - ErrWalletRejected       → not applied, any other definitive rejection (FAILED, 502).
//   - ErrWalletOutcomeUnknown → it may or may not have been applied: timeout,
//     dropped connection, 5xx… The transaction stays PENDING (202). It must
//     never be reported as not applied.
//
// Balance:
//   - ErrWalletFailure → the balance could not be read (502).
//   - ErrWalletTimeout → the Wallet did not answer in time (504).
//
// Every call is bounded by a timeout.
type WalletClient interface {
	Balance(ctx context.Context, userID UserID) (WalletBalance, error)
	Withdraw(ctx context.Context, operation WalletOperation) (Money, error)
	Deposit(ctx context.Context, operation WalletOperation) (Money, error)
}
