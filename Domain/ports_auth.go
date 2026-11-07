package domain

import "time"

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
