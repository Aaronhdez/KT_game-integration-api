package application

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	actions "github.com/aaronhdez/kt_game-integration-api/Actions"
	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
	"github.com/aaronhdez/kt_game-integration-api/Domain/domaintest"
)

const (
	testUserID   domain.UserID = 34633089486
	testUsername               = "player_usd"
	testPassword               = "usd-pass-2026"
)

var testNow = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func TestLoginController_ValidCredentials_Returns200(t *testing.T) {
	fixture := givenALoginController()

	recorder := fixture.post(credentialsBody(testUsername, testPassword))

	assertStatusCode(t, recorder, http.StatusOK)
}

func TestLoginController_ValidCredentials_ReturnsTokenAndExpiresAt(t *testing.T) {
	fixture := givenALoginController()

	recorder := fixture.post(credentialsBody(testUsername, testPassword))

	result := decodeLoginResponse(t, recorder)
	assertLoginResponse(t, result, map[string]string{
		"token":     "token-for-34633089486",
		"expiresAt": "2026-11-07T13:00:00Z", // RFC 3339, UTC: testNow + FakeTokenTTL
	})
}

func TestLoginController_ValidCredentials_SetsJSONContentType(t *testing.T) {
	fixture := givenALoginController()

	recorder := fixture.post(credentialsBody(testUsername, testPassword))

	assertContentTypeJSON(t, recorder)
}

// The response carries a credential: proxies and browsers must not store it
// (RFC 6749 §5.1 practice).
func TestLoginController_ValidCredentials_ForbidsCaching(t *testing.T) {
	fixture := givenALoginController()

	recorder := fixture.post(credentialsBody(testUsername, testPassword))

	assertHeader(t, recorder, "Cache-Control", "no-store")
}

func TestLoginController_WrongPassword_Returns401Unauthorized(t *testing.T) {
	fixture := givenALoginController()

	recorder := fixture.post(credentialsBody(testUsername, "wrong-password"))

	assertStatusAndCode(t, recorder, http.StatusUnauthorized, CodeUnauthorized)
}

// Assessment §8.1: the client must not be able to tell which usernames exist.
func TestLoginController_UnknownUserAndWrongPassword_ReturnIdenticalResponses(t *testing.T) {
	fixture := givenALoginController()

	unknownUser := fixture.post(credentialsBody("nobody", testPassword))
	wrongPassword := fixture.post(credentialsBody(testUsername, "wrong-password"))

	assertSameResponse(t, unknownUser, wrongPassword)
}

func TestLoginController_InvalidRequest_Returns400InvalidRequest(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{name: "malformed JSON", body: `{"username":`},
		{name: "empty body", body: ""},
		{name: "empty object", body: `{}`},
		{name: "missing password", body: `{"username":"player_usd"}`},
		{name: "password is not a string", body: `{"username":"player_usd","password":123}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenALoginController()

			recorder := fixture.post(testCase.body)

			assertStatusAndCode(t, recorder, http.StatusBadRequest, CodeInvalidRequest)
		})
	}
}

func TestLoginController_RepositoryFails_Returns500InternalError(t *testing.T) {
	fixture := givenALoginController()
	fixture.players.Err = errors.New("database down")

	recorder := fixture.post(credentialsBody(testUsername, testPassword))

	assertStatusAndCode(t, recorder, http.StatusInternalServerError, CodeInternal)
}

func TestLoginController_RepositoryFails_LogDoesNotContainThePassword(t *testing.T) {
	fixture := givenALoginController()
	fixture.players.Err = errors.New("database down")

	fixture.post(credentialsBody(testUsername, testPassword))

	assertOutputNotContains(t, fixture.logOutput, testPassword)
}

/////////////////////////////////////////////////////////////////////////////////////

type loginControllerFixture struct {
	controller *LoginController
	players    *domaintest.FakePlayerRepository
	logOutput  *bytes.Buffer
}

func givenALoginController() loginControllerFixture {
	players := domaintest.NewFakePlayerRepository(domain.Player{
		UserID:       testUserID,
		Username:     testUsername,
		PasswordHash: domaintest.FakeHash(testPassword),
		Currency:     domain.USD,
	})
	login := actions.NewLoginCommandHandler(
		players,
		&domaintest.FakePasswordHasher{},
		&domaintest.FakeTokens{},
		domaintest.FixedClock{Time: testNow},
	)
	logOutput := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logOutput, nil))

	return loginControllerFixture{
		controller: NewLoginController(login, logger),
		players:    players,
		logOutput:  logOutput,
	}
}

func (fixture loginControllerFixture) post(body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	fixture.controller.ServeHTTP(recorder, request)
	return recorder
}

// credentialsBody builds the JSON with the encoder, so any character in the
// values is escaped correctly.
func credentialsBody(username, password string) string {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	return string(body)
}

func decodeLoginResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var result map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response body %q: %v", recorder.Body.String(), err)
	}
	return result
}

// assertLoginResponse compares the whole body, so an extra field (e.g. a
// password hash leaking into the DTO) also fails the test.
func assertLoginResponse(t *testing.T, result, want map[string]string) {
	t.Helper()
	if len(result) != len(want) {
		t.Errorf("expected fields %v, got %v", want, result)
		return
	}
	for key, value := range want {
		if result[key] != value {
			t.Errorf("expected %s %q, got %q", key, value, result[key])
		}
	}
}

func assertHeader(t *testing.T, recorder *httptest.ResponseRecorder, name, want string) {
	t.Helper()
	if result := recorder.Header().Get(name); result != want {
		t.Errorf("expected header %s %q, got %q", name, want, result)
	}
}

func assertSameResponse(t *testing.T, first, second *httptest.ResponseRecorder) {
	t.Helper()
	if first.Code != second.Code {
		t.Errorf("expected the same status, got %d and %d", first.Code, second.Code)
	}
	if !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Errorf("expected identical bodies, got %q and %q", first.Body.String(), second.Body.String())
	}
}
