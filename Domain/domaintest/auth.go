package domaintest

import (
	"fmt"
	"slices"
	"sync"
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// FakeHash is the "hash" FakePasswordHasher accepts for password. Use it to
// build players in tests: Player{PasswordHash: FakeHash("secret"), …}.
func FakeHash(password string) string {
	return "fake-hash:" + password
}

// FakePasswordHasher is a domain.PasswordHasher without bcrypt: a password
// matches only the hash built by FakeHash. It records every hash it was asked
// to verify, so tests can check that a verification happened. Set Err to make
// every call fail with it (an unusable hash). It is safe for concurrent use.
type FakePasswordHasher struct {
	Err error

	mutex    sync.Mutex
	verified []string
}

var _ domain.PasswordHasher = (*FakePasswordHasher)(nil)

func (hasher *FakePasswordHasher) Verify(passwordHash, password string) error {
	hasher.mutex.Lock()
	defer hasher.mutex.Unlock()

	hasher.verified = append(hasher.verified, passwordHash)
	if hasher.Err != nil {
		return hasher.Err
	}
	if passwordHash != FakeHash(password) {
		return domain.ErrInvalidCredentials
	}
	return nil
}

// Verified returns the hashes passed to Verify so far, in order.
func (hasher *FakePasswordHasher) Verified() []string {
	hasher.mutex.Lock()
	defer hasher.mutex.Unlock()
	return slices.Clone(hasher.verified)
}

// FakeTokenTTL is the lifetime of the tokens issued by FakeTokens.
const FakeTokenTTL = time.Hour

// FakeTokens is a domain.TokenIssuer and domain.TokenVerifier without
// cryptography. Issue returns a predictable value ("token-for-<userID>")
// that expires FakeTokenTTL after now; Verify accepts only tokens it issued,
// until they expire. Set IssueErr to make Issue fail with it.
// It is safe for concurrent use.
type FakeTokens struct {
	IssueErr error

	mutex  sync.Mutex
	issued map[string]issuedFakeToken
}

type issuedFakeToken struct {
	userID    domain.UserID
	expiresAt time.Time
}

var (
	_ domain.TokenIssuer   = (*FakeTokens)(nil)
	_ domain.TokenVerifier = (*FakeTokens)(nil)
)

func (tokens *FakeTokens) Issue(userID domain.UserID, now time.Time) (domain.IssuedToken, error) {
	tokens.mutex.Lock()
	defer tokens.mutex.Unlock()

	if tokens.IssueErr != nil {
		return domain.IssuedToken{}, tokens.IssueErr
	}
	value := fmt.Sprintf("token-for-%d", userID)
	expiresAt := now.Add(FakeTokenTTL)
	if tokens.issued == nil {
		tokens.issued = make(map[string]issuedFakeToken)
	}
	tokens.issued[value] = issuedFakeToken{userID: userID, expiresAt: expiresAt}
	return domain.IssuedToken{Value: value, ExpiresAt: expiresAt}, nil
}

func (tokens *FakeTokens) Verify(token string, now time.Time) (domain.UserID, error) {
	tokens.mutex.Lock()
	defer tokens.mutex.Unlock()

	issued, found := tokens.issued[token]
	if !found || !now.Before(issued.expiresAt) {
		return 0, domain.ErrInvalidToken
	}
	return issued.userID, nil
}
