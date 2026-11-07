package token

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

var (
	testSecret  = []byte("test-jwt-secret-of-at-least-32-bytes")
	otherSecret = []byte("another-secret-of-at-least-32-bytes!")
	issuedAt    = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
)

const (
	testTTL    = time.Hour
	testUserID = domain.UserID(34633089486) // more than 32 bits, like the real IDs
)

func TestIssue_ExpiresAtIsNowPlusTTL(t *testing.T) {
	issuer := NewJWT(testSecret, testTTL)

	result := givenAnIssuedToken(t, issuer, issuedAt)

	assertTimeEqual(t, result.ExpiresAt, issuedAt.Add(testTTL))
}

func TestIssue_SubjectIsTheUserIDAsDecimalString(t *testing.T) {
	issuer := NewJWT(testSecret, testTTL)
	issued := givenAnIssuedToken(t, issuer, issuedAt)

	result := readSubject(t, issued.Value)

	assertStringEqual(t, result, "34633089486")
}

func TestIssue_SignsWithHS256(t *testing.T) {
	issuer := NewJWT(testSecret, testTTL)
	issued := givenAnIssuedToken(t, issuer, issuedAt)

	result := readAlgorithm(t, issued.Value)

	assertStringEqual(t, result, "HS256")
}

func TestVerify_TokenJustIssued_ReturnsItsUserID(t *testing.T) {
	jwtService := NewJWT(testSecret, testTTL)
	issued := givenAnIssuedToken(t, jwtService, issuedAt)

	result, err := jwtService.Verify(issued.Value, issuedAt)

	assertUserID(t, result, err, testUserID)
}

func TestVerify_OneSecondBeforeExpiry_ReturnsItsUserID(t *testing.T) {
	jwtService := NewJWT(testSecret, testTTL)
	issued := givenAnIssuedToken(t, jwtService, issuedAt)

	result, err := jwtService.Verify(issued.Value, issued.ExpiresAt.Add(-time.Second))

	assertUserID(t, result, err, testUserID)
}

func TestVerify_AtExpiry_ReturnsInvalidToken(t *testing.T) {
	jwtService := NewJWT(testSecret, testTTL)
	issued := givenAnIssuedToken(t, jwtService, issuedAt)

	_, err := jwtService.Verify(issued.Value, issued.ExpiresAt)

	assertErrorIs(t, err, domain.ErrInvalidToken)
}

// The exp claim only has whole seconds. If "now" has a fraction of a second,
// the ExpiresAt we report must not be later than the expiry in the token, or a
// client would be refused before the time we told it.
func TestVerify_JustBeforeReportedExpiry_WithSubSecondNow_ReturnsItsUserID(t *testing.T) {
	jwtService := NewJWT(testSecret, testTTL)
	issued := givenAnIssuedToken(t, jwtService, issuedAt.Add(500*time.Millisecond))

	result, err := jwtService.Verify(issued.Value, issued.ExpiresAt.Add(-time.Millisecond))

	assertUserID(t, result, err, testUserID)
}

func TestVerify_InvalidTokens_ReturnInvalidToken(t *testing.T) {
	validClaims := jwt.RegisteredClaims{
		Subject:   "34633089486",
		IssuedAt:  jwt.NewNumericDate(issuedAt),
		ExpiresAt: jwt.NewNumericDate(issuedAt.Add(testTTL)),
	}

	testCases := []struct {
		name  string
		token string
	}{
		{name: "empty string", token: ""},
		{name: "not a JWT", token: "not-a-jwt"},
		{name: "three parts but garbage", token: "aaa.bbb.ccc"},
		{name: "signed with another secret", token: signed(t, jwt.SigningMethodHS256, otherSecret, validClaims)},
		{name: "alg none", token: unsigned(t, validClaims)},
		{name: "signed with HS512", token: signed(t, jwt.SigningMethodHS512, testSecret, validClaims)},
		{name: "expired", token: signed(t, jwt.SigningMethodHS256, testSecret, withExpiry(validClaims, issuedAt.Add(-time.Second)))},
		{name: "without exp", token: signed(t, jwt.SigningMethodHS256, testSecret, withoutExpiry(validClaims))},
		{name: "without sub", token: signed(t, jwt.SigningMethodHS256, testSecret, withSubject(validClaims, ""))},
		{name: "non-numeric sub", token: signed(t, jwt.SigningMethodHS256, testSecret, withSubject(validClaims, "player_usd"))},
		{name: "zero sub", token: signed(t, jwt.SigningMethodHS256, testSecret, withSubject(validClaims, "0"))},
		{name: "negative sub", token: signed(t, jwt.SigningMethodHS256, testSecret, withSubject(validClaims, "-5"))},
		{name: "sub larger than int64", token: signed(t, jwt.SigningMethodHS256, testSecret, withSubject(validClaims, "9223372036854775808"))},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			verifier := NewJWT(testSecret, testTTL)

			_, err := verifier.Verify(testCase.token, issuedAt)

			assertErrorIs(t, err, domain.ErrInvalidToken)
		})
	}
}

/////////////////////////////////////////////////////////////////////////////////////

func givenAnIssuedToken(t *testing.T, issuer *JWT, now time.Time) domain.IssuedToken {
	t.Helper()
	issued, err := issuer.Issue(testUserID, now)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return issued
}

// signed builds a token the way a third party could, with the library directly.
func signed(t *testing.T, method jwt.SigningMethod, secret []byte, claims jwt.Claims) string {
	t.Helper()
	value, err := jwt.NewWithClaims(method, claims).SignedString(secret)
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return value
}

// unsigned builds a token with "alg": "none", the classic forgery attempt.
func unsigned(t *testing.T, claims jwt.Claims) string {
	t.Helper()
	value, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("build unsigned test token: %v", err)
	}
	return value
}

func withSubject(claims jwt.RegisteredClaims, subject string) jwt.RegisteredClaims {
	claims.Subject = subject
	return claims
}

func withExpiry(claims jwt.RegisteredClaims, expiresAt time.Time) jwt.RegisteredClaims {
	claims.ExpiresAt = jwt.NewNumericDate(expiresAt)
	return claims
}

func withoutExpiry(claims jwt.RegisteredClaims) jwt.RegisteredClaims {
	claims.ExpiresAt = nil
	return claims
}

// parseIssued reads a token produced by our issuer with the library directly,
// to check what is inside it.
func parseIssued(t *testing.T, value string) (*jwt.Token, jwt.RegisteredClaims) {
	t.Helper()
	var claims jwt.RegisteredClaims
	parsed, err := jwt.ParseWithClaims(value, &claims,
		func(*jwt.Token) (any, error) { return testSecret, nil },
		jwt.WithTimeFunc(func() time.Time { return issuedAt }),
	)
	if err != nil {
		t.Fatalf("parse issued token: %v", err)
	}
	return parsed, claims
}

func readSubject(t *testing.T, value string) string {
	t.Helper()
	_, claims := parseIssued(t, value)
	return claims.Subject
}

func readAlgorithm(t *testing.T, value string) string {
	t.Helper()
	parsed, _ := parseIssued(t, value)
	return parsed.Method.Alg()
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

func assertTimeEqual(t *testing.T, result, want time.Time) {
	t.Helper()
	if !result.Equal(want) {
		t.Errorf("expected %s, got %s", want, result)
	}
}

func assertStringEqual(t *testing.T, result, want string) {
	t.Helper()
	if result != want {
		t.Errorf("expected %q, got %q", want, result)
	}
}

func assertErrorIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("expected %v to be %q", err, target)
	}
}
