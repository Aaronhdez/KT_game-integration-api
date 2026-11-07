package application

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
	"github.com/aaronhdez/kt_game-integration-api/Domain/domaintest"
)

func TestRequireAuth_ValidToken_CallsTheNextHandler(t *testing.T) {
	fixture := givenRequireAuth()

	fixture.serve("Bearer " + fixture.validToken)

	assertNextCalled(t, fixture.next)
}

func TestRequireAuth_ValidToken_NextHandlerSeesTheUserID(t *testing.T) {
	fixture := givenRequireAuth()

	fixture.serve("Bearer " + fixture.validToken)

	assertNextSawUserID(t, fixture.next, testUserID)
}

// RFC 7235: the authentication scheme is case-insensitive.
func TestRequireAuth_LowerCaseScheme_IsAccepted(t *testing.T) {
	fixture := givenRequireAuth()

	fixture.serve("bearer " + fixture.validToken)

	assertNextCalled(t, fixture.next)
}

func TestRequireAuth_MissingOrInvalidCredentials_Returns401Unauthorized(t *testing.T) {
	testCases := []struct {
		name          string
		authorization string
	}{
		{name: "no header", authorization: ""},
		{name: "another scheme", authorization: "Basic cGxheWVyOnBhc3M="},
		{name: "scheme without token", authorization: "Bearer"},
		{name: "scheme and space without token", authorization: "Bearer "},
		{name: "token without scheme", authorization: "token-for-34633089486"},
		{name: "token never issued", authorization: "Bearer forged-token"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenRequireAuth()

			recorder := fixture.serve(testCase.authorization)

			assertStatusAndCode(t, recorder, http.StatusUnauthorized, CodeUnauthorized)
		})
	}
}

func TestRequireAuth_ExpiredToken_Returns401Unauthorized(t *testing.T) {
	fixture := givenRequireAuthWithTokenIssuedAt(testNow.Add(-domaintest.FakeTokenTTL))

	recorder := fixture.serve("Bearer " + fixture.validToken)

	assertStatusAndCode(t, recorder, http.StatusUnauthorized, CodeUnauthorized)
}

func TestRequireAuth_InvalidToken_DoesNotCallTheNextHandler(t *testing.T) {
	fixture := givenRequireAuth()

	fixture.serve("Bearer forged-token")

	assertNextNotCalled(t, fixture.next)
}

func TestRequireAuth_NoHeader_DoesNotCallTheNextHandler(t *testing.T) {
	fixture := givenRequireAuth()

	fixture.serve("")

	assertNextNotCalled(t, fixture.next)
}

// One answer for every failure: a caller cannot probe whether a token was
// expired, forged or badly formatted.
func TestRequireAuth_EveryFailure_ReturnsTheSameResponse(t *testing.T) {
	fixture := givenRequireAuth()

	noHeader := fixture.serve("")
	forgedToken := fixture.serve("Bearer forged-token")

	assertSameResponse(t, noHeader, forgedToken)
}

// RFC 6750 §3: a 401 tells the client which scheme to use.
func TestRequireAuth_Failure_SetsWWWAuthenticateHeader(t *testing.T) {
	fixture := givenRequireAuth()

	recorder := fixture.serve("")

	assertHeader(t, recorder, "WWW-Authenticate", "Bearer")
}

func TestUserIDFromContext_ContextWithoutUser_ReportsNotFound(t *testing.T) {
	_, found := UserIDFromContext(context.Background())

	assertFalse(t, found, "expected no user id in a plain context")
}

/////////////////////////////////////////////////////////////////////////////////////

type requireAuthFixture struct {
	handler    http.Handler
	next       *recordingHandler
	validToken string
}

func givenRequireAuth() requireAuthFixture {
	return givenRequireAuthWithTokenIssuedAt(testNow)
}

// givenRequireAuthWithTokenIssuedAt issues validToken at issuedAt; the
// middleware's clock is always testNow.
func givenRequireAuthWithTokenIssuedAt(issuedAt time.Time) requireAuthFixture {
	tokens := &domaintest.FakeTokens{}
	issued, _ := tokens.Issue(testUserID, issuedAt)
	next := &recordingHandler{}
	requireAuth := RequireAuth(tokens, domaintest.FixedClock{Time: testNow})

	return requireAuthFixture{
		handler:    requireAuth(next),
		next:       next,
		validToken: issued.Value,
	}
}

// serve sends a request with the given Authorization header ("" = no header).
func (fixture requireAuthFixture) serve(authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/player", nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	recorder := httptest.NewRecorder()

	fixture.handler.ServeHTTP(recorder, request)
	return recorder
}

// recordingHandler stands in for the protected endpoint and remembers what it saw.
type recordingHandler struct {
	called bool
	userID domain.UserID
	found  bool
}

func (handler *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler.called = true
	handler.userID, handler.found = UserIDFromContext(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

func assertNextCalled(t *testing.T, next *recordingHandler) {
	t.Helper()
	if !next.called {
		t.Errorf("expected the next handler to be called")
	}
}

func assertNextNotCalled(t *testing.T, next *recordingHandler) {
	t.Helper()
	if next.called {
		t.Errorf("expected the next handler not to be called")
	}
}

func assertNextSawUserID(t *testing.T, next *recordingHandler, want domain.UserID) {
	t.Helper()
	if !next.found {
		t.Fatalf("expected the next handler to find a user id in the context")
	}
	if next.userID != want {
		t.Errorf("expected user id %d, got %d", want, next.userID)
	}
}

func assertFalse(t *testing.T, value bool, message string) {
	t.Helper()
	if value {
		t.Error(message)
	}
}
