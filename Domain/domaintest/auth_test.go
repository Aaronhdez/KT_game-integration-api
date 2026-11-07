package domaintest

import (
	"errors"
	"slices"
	"testing"
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

var testNow = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func TestFixedClock_Now_ReturnsTheFixedTime(t *testing.T) {
	clock := FixedClock{Time: testNow}

	result := clock.Now()

	assertTimeEqual(t, result, testNow)
}

func TestFakePasswordHasher_PasswordOfTheHash_ReturnsNil(t *testing.T) {
	hasher := &FakePasswordHasher{}

	result := hasher.Verify(FakeHash("secret"), "secret")

	assertNoError(t, result)
}

func TestFakePasswordHasher_OtherPassword_IsInvalidCredentials(t *testing.T) {
	hasher := &FakePasswordHasher{}

	result := hasher.Verify(FakeHash("secret"), "wrong")

	assertErrorIs(t, result, domain.ErrInvalidCredentials)
}

func TestFakePasswordHasher_WithErr_FailsWithIt(t *testing.T) {
	hasher := &FakePasswordHasher{Err: errors.New("unusable hash")}

	result := hasher.Verify(FakeHash("secret"), "secret")

	assertErrorIs(t, result, hasher.Err)
}

func TestFakePasswordHasher_Verified_RecordsTheHashesInOrder(t *testing.T) {
	hasher := &FakePasswordHasher{}
	hasher.Verify("first-hash", "x")
	hasher.Verify("second-hash", "x")

	result := hasher.Verified()

	assertStrings(t, result, []string{"first-hash", "second-hash"})
}

func TestFakeTokens_Issue_ExpiresAfterFakeTokenTTL(t *testing.T) {
	tokens := &FakeTokens{}

	result, err := tokens.Issue(testUserID, testNow)

	assertIssuedToken(t, result, err, "token-for-34633089486", testNow.Add(FakeTokenTTL))
}

func TestFakeTokens_Issue_WithIssueErr_FailsWithIt(t *testing.T) {
	tokens := &FakeTokens{IssueErr: errors.New("cannot sign")}

	_, err := tokens.Issue(testUserID, testNow)

	assertErrorIs(t, err, tokens.IssueErr)
}

func TestFakeTokens_Verify_IssuedToken_ReturnsItsUserID(t *testing.T) {
	tokens := &FakeTokens{}
	issued, _ := tokens.Issue(testUserID, testNow)

	result, err := tokens.Verify(issued.Value, testNow)

	assertUserID(t, result, err, testUserID)
}

func TestFakeTokens_Verify_AtExpiry_IsInvalidToken(t *testing.T) {
	tokens := &FakeTokens{}
	issued, _ := tokens.Issue(testUserID, testNow)

	_, err := tokens.Verify(issued.Value, issued.ExpiresAt)

	assertErrorIs(t, err, domain.ErrInvalidToken)
}

func TestFakeTokens_Verify_TokenNeverIssued_IsInvalidToken(t *testing.T) {
	tokens := &FakeTokens{}

	_, err := tokens.Verify("token-for-34633089486", testNow)

	assertErrorIs(t, err, domain.ErrInvalidToken)
}

/////////////////////////////////////////////////////////////////////////////////////

func assertTimeEqual(t *testing.T, result, want time.Time) {
	t.Helper()
	if !result.Equal(want) {
		t.Errorf("expected %s, got %s", want, result)
	}
}

func assertStrings(t *testing.T, result, want []string) {
	t.Helper()
	if !slices.Equal(result, want) {
		t.Errorf("expected %v, got %v", want, result)
	}
}

func assertIssuedToken(t *testing.T, result domain.IssuedToken, err error, wantValue string, wantExpiresAt time.Time) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Value != wantValue {
		t.Errorf("expected token %q, got %q", wantValue, result.Value)
	}
	assertTimeEqual(t, result.ExpiresAt, wantExpiresAt)
}

func assertUserID(t *testing.T, result domain.UserID, err error, want domain.UserID) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != want {
		t.Errorf("expected user id %d, got %d", want, result)
	}
}
