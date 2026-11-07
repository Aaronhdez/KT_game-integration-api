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

// IDGenerator creates the IDs our service assigns to its transactions (UUIDs,
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

// PlayerRepository reads the seeded player accounts (§8.1). Both methods
// return ErrPlayerNotFound when there is no such player.
//   - FindByUsername is used by login.
//   - FindByID is used once the player is known from the token, e.g. to check
//     that a withdraw's currency matches the player's wallet (§8.3).
type PlayerRepository interface {
	FindByUsername(ctx context.Context, username string) (Player, error)
	FindByID(ctx context.Context, userID UserID) (Player, error)
}
