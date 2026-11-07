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

// TokenVerifier checks a token produced by TokenIssuer and returns its subject,
// the player's wallet user ID. Every failure (bad signature, expired at now,
// malformed, wrong algorithm, missing or invalid subject) returns
// ErrInvalidToken, so callers cannot tell the reasons apart.
type TokenVerifier interface {
	Verify(token string, now time.Time) (UserID, error)
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

// BetRepository stores bets. Reads are scoped by the owner: another player's
// bet is reported as ErrBetNotFound, exactly like a bet that does not exist.
//   - NextID reserves a bet ID before the bet exists, because a PENDING
//     withdraw already sends its betId to the Wallet.
//   - Update writes the status and UpdatedAt; the other fields never change.
type BetRepository interface {
	NextID(ctx context.Context) (BetID, error)
	Insert(ctx context.Context, bet Bet) error
	FindByID(ctx context.Context, userID UserID, id BetID) (Bet, error)
	Update(ctx context.Context, bet Bet) error
}

// TransactionRepository stores transactions. Reads are scoped by the owner and
// every Find… returns ErrTransactionNotFound when there is no match (including
// another player's transaction).
//   - Insert reports duplicates rejected by the database as domain errors:
//     ErrTransactionExists (same provider transaction ID for the same player),
//     ErrBetAlreadySettled (the bet already has a pending or completed deposit),
//     ErrAlreadyCancelled (the transaction already has a cancel).
//   - FindByProviderTransactionID only looks at WITHDRAW and DEPOSIT
//     transactions: a CANCEL carries the provider ID of the one it cancels.
//   - FindCancelOf returns the CANCEL of the given transaction.
//   - Update writes the status, NewBalance and UpdatedAt; the other fields
//     never change.
type TransactionRepository interface {
	Insert(ctx context.Context, tx Transaction) error
	FindByID(ctx context.Context, userID UserID, id TransactionID) (Transaction, error)
	FindByProviderTransactionID(ctx context.Context, userID UserID, id ProviderTransactionID) (Transaction, error)
	FindCancelOf(ctx context.Context, userID UserID, originalID TransactionID) (Transaction, error)
	Update(ctx context.Context, tx Transaction) error
}

// Repositories groups the repositories that take part in one database
// transaction (see UnitOfWork).
type Repositories struct {
	Transactions TransactionRepository
	Bets         BetRepository
}

// UnitOfWork runs groups of reads and writes atomically.
type UnitOfWork interface {
	// Atomic runs fn in one database transaction: it is committed if fn
	// returns nil and rolled back otherwise. Use only the repositories
	// received by fn, so that everything happens inside that transaction.
	Atomic(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
}

// PlayerLocker serialises the operations of one player, so that two requests
// of the same player never interleave (e.g. two deposits on one bet).
type PlayerLocker interface {
	// WithPlayerLock runs fn while no other operation of userID can run, and
	// releases the lock when fn returns, whatever the result. The lock is held
	// across several Atomic calls and the Wallet call between them: the PENDING
	// transaction is committed before calling the Wallet, and the player stays
	// locked until its outcome is stored. It returns fn's error, or an error if
	// the lock could not be acquired (e.g. ctx cancelled while waiting).
	WithPlayerLock(ctx context.Context, userID UserID, fn func(ctx context.Context, uow UnitOfWork) error) error
}
