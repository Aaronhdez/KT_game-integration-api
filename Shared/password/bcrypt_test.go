package password

import (
	"errors"
	"strings"
	"testing"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// The hash of player_usd in Persistence/migrations/00004_seed_players.sql and
// its password documented in README.md.
const (
	seededHash     = "$2a$10$8YD78fz6fF2LgDPYhVqyeONIEfRCvIekJHrDUVMXBcbCAUl8osmhW"
	seededPassword = "usd-pass-2026"
)

func TestBcryptVerify_MatchingPassword_ReturnsNil(t *testing.T) {
	hasher := NewBcrypt()

	result := hasher.Verify(seededHash, seededPassword)

	assertNoError(t, result)
}

func TestBcryptVerify_WrongPassword_ReturnsInvalidCredentials(t *testing.T) {
	hasher := NewBcrypt()

	result := hasher.Verify(seededHash, "wrong-password")

	assertErrorIs(t, result, domain.ErrInvalidCredentials)
}

// bcrypt only looks at the first 72 bytes of a password, so a longer one could
// match a hash it was never meant to match. No seeded password is that long.
func TestBcryptVerify_PasswordLongerThan72Bytes_ReturnsInvalidCredentials(t *testing.T) {
	hasher := NewBcrypt()
	tooLong := seededPassword + strings.Repeat("x", 73-len(seededPassword))

	result := hasher.Verify(seededHash, tooLong)

	assertErrorIs(t, result, domain.ErrInvalidCredentials)
}

// A broken hash in the database is our problem, not a wrong password: it must
// not be reported as invalid credentials (it becomes a 500).
func TestBcryptVerify_MalformedHash_ReturnsErrorThatIsNotInvalidCredentials(t *testing.T) {
	hasher := NewBcrypt()

	result := hasher.Verify("not-a-bcrypt-hash", seededPassword)

	assertErrorIsNotInvalidCredentials(t, result)
}

/////////////////////////////////////////////////////////////////////////////////////

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func assertErrorIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("expected %v to be %q", err, target)
	}
}

func assertErrorIsNotInvalidCredentials(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error, got nil")
	}
	if errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("expected an error that is not invalid credentials, got %v", err)
	}
}
