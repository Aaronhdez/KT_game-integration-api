package actions

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
	"github.com/aaronhdez/kt_game-integration-api/Domain/domaintest"
)

const (
	testUserID   domain.UserID = 34633089486
	testUsername               = "player_usd"
	testPassword               = "usd-pass-2026"
)

var testNow = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func TestLogin_ValidCredentials_ReturnsTheIssuedToken(t *testing.T) {
	fixture := givenALogin()

	result, err := fixture.login.Execute(context.Background(), aLoginCommand(testUsername, testPassword))

	assertToken(t, result, err, "token-for-34633089486")
}

func TestLogin_ValidCredentials_ExpiresAtComesFromTheClock(t *testing.T) {
	fixture := givenALogin()

	result, err := fixture.login.Execute(context.Background(), aLoginCommand(testUsername, testPassword))

	assertExpiresAt(t, result, err, testNow.Add(domaintest.FakeTokenTTL))
}

func TestLogin_UnknownUser_IsInvalidCredentials(t *testing.T) {
	fixture := givenALogin()

	_, err := fixture.login.Execute(context.Background(), aLoginCommand("nobody", testPassword))

	assertErrorIs(t, err, domain.ErrInvalidCredentials)
}

func TestLogin_WrongPassword_IsInvalidCredentials(t *testing.T) {
	fixture := givenALogin()

	_, err := fixture.login.Execute(context.Background(), aLoginCommand(testUsername, "wrong-password"))

	assertErrorIs(t, err, domain.ErrInvalidCredentials)
}

// Without this, an unknown username would answer much faster than a wrong
// password (no bcrypt work), and the timing would reveal which usernames exist.
func TestLogin_UnknownUser_StillVerifiesTheDummyHash(t *testing.T) {
	fixture := givenALogin()

	fixture.login.Execute(context.Background(), aLoginCommand("nobody", testPassword))

	assertVerifiedHashes(t, fixture.hasher, []string{dummyPasswordHash})
}

func TestLogin_MissingField_IsMissingCredentials(t *testing.T) {
	testCases := []struct {
		name     string
		username string
		password string
	}{
		{name: "empty username", username: "", password: testPassword},
		{name: "empty password", username: testUsername, password: ""},
		{name: "both empty", username: "", password: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenALogin()

			_, err := fixture.login.Execute(context.Background(), aLoginCommand(testCase.username, testCase.password))

			assertErrorIs(t, err, domain.ErrMissingCredentials)
		})
	}
}

// A database failure is not a wrong password: it must keep its cause (and
// become a 500), never be turned into invalid credentials.
func TestLogin_RepositoryFails_ReturnsItsError(t *testing.T) {
	fixture := givenALogin()
	fixture.players.Err = errors.New("database down")

	_, err := fixture.login.Execute(context.Background(), aLoginCommand(testUsername, testPassword))

	assertErrorIs(t, err, fixture.players.Err)
}

func TestLogin_HasherFails_ReturnsItsError(t *testing.T) {
	fixture := givenALogin()
	fixture.hasher.Err = errors.New("unusable hash")

	_, err := fixture.login.Execute(context.Background(), aLoginCommand(testUsername, testPassword))

	assertErrorIs(t, err, fixture.hasher.Err)
}

func TestLogin_IssuerFails_ReturnsItsError(t *testing.T) {
	fixture := givenALogin()
	fixture.tokens.IssueErr = errors.New("cannot sign")

	_, err := fixture.login.Execute(context.Background(), aLoginCommand(testUsername, testPassword))

	assertErrorIs(t, err, fixture.tokens.IssueErr)
}

/////////////////////////////////////////////////////////////////////////////////////

// loginFixture keeps the fakes reachable, so a test can make one of them fail
// or inspect what it received.
type loginFixture struct {
	login   *LoginCommandHandler
	players *domaintest.FakePlayerRepository
	hasher  *domaintest.FakePasswordHasher
	tokens  *domaintest.FakeTokens
}

func givenALogin() loginFixture {
	players := domaintest.NewFakePlayerRepository(domain.Player{
		UserID:       testUserID,
		Username:     testUsername,
		PasswordHash: domaintest.FakeHash(testPassword),
		Currency:     domain.USD,
	})
	hasher := &domaintest.FakePasswordHasher{}
	tokens := &domaintest.FakeTokens{}
	clock := domaintest.FixedClock{Time: testNow}

	return loginFixture{
		login:   NewLoginCommandHandler(players, hasher, tokens, clock),
		players: players,
		hasher:  hasher,
		tokens:  tokens,
	}
}

func aLoginCommand(username, password string) LoginCommand {
	return LoginCommand{Username: username, Password: password}
}

func assertToken(t *testing.T, result LoginResult, err error, want string) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Token != want {
		t.Errorf("expected token %q, got %q", want, result.Token)
	}
}

func assertExpiresAt(t *testing.T, result LoginResult, err error, want time.Time) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ExpiresAt.Equal(want) {
		t.Errorf("expected expiresAt %s, got %s", want, result.ExpiresAt)
	}
}

func assertVerifiedHashes(t *testing.T, hasher *domaintest.FakePasswordHasher, want []string) {
	t.Helper()
	if result := hasher.Verified(); !slices.Equal(result, want) {
		t.Errorf("expected verified hashes %v, got %v", want, result)
	}
}

func assertErrorIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("expected %v to be %q", err, target)
	}
}
