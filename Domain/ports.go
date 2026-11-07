package domain

import (
	"context"
	"time"
)

// Clock tells the current time. Use cases ask it for "now" and pass that value
// to the domain (NewPlacedBet, tx.Complete, …), so tests can use a fixed time.
type Clock interface {
	Now() time.Time
}

// IDGenerator creates the IDs our service assigns to its transactions (UUIDs)
type IDGenerator interface {
	NewTransactionID() TransactionID
}

// PasswordHasher checks a password against the stored hash.
// Verify returns nil if they match and ErrInvalidCredentials if they do not;
// any other error means the hash itself is unusable. Hashes for the seeded
// players are produced once, when the seed is written, so the login
// use case only needs to verify.
type PasswordHasher interface {
	Verify(passwordHash, password string) error
}

// IssuedToken is a signed access token and the moment it stops being valid.
// Value is a secret: it must never be logged.
type IssuedToken struct {
	Value     string
	ExpiresAt time.Time
}

// TokenIssuer creates the signed token returned by login: its subject is
// the player's wallet user ID and it expires after a configured lifetime.
type TokenIssuer interface {
	Issue(userID UserID, now time.Time) (IssuedToken, error)
}

// PlayerRepository reads the seeded player accounts. Both methods
// return ErrPlayerNotFound when there is no such player.
//   - FindByUsername is used by login.
//   - FindByID is used once the player is known from the token, e.g. to check
//     that a withdraw's currency matches the player's wallet.
type PlayerRepository interface {
	FindByUsername(ctx context.Context, username string) (Player, error)
	FindByID(ctx context.Context, userID UserID) (Player, error)
}

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
//   - ErrInsufficientFunds    → not applied (the transaction becomes FAILED, 422).
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
