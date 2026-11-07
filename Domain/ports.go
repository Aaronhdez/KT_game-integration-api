package domain

import "time"

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
