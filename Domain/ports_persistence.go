package domain

import "context"

// PlayerRepository reads the seeded player accounts. Both methods
// return ErrPlayerNotFound when there is no such player.
//   - FindByUsername is used by login.
//   - FindByID is used once the player is known from the token, e.g. to check
//     that a withdraw's currency matches the player's wallet.
type PlayerRepository interface {
	FindByUsername(ctx context.Context, username string) (Player, error)
	FindByID(ctx context.Context, userID UserID) (Player, error)
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
