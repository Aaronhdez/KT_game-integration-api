package application

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
	"github.com/aaronhdez/kt_game-integration-api/Domain/domaintest"
	queries "github.com/aaronhdez/kt_game-integration-api/Queries"
)

// player_usd_2, a second player, to prove the answer is about the token's player.
const testSecondUserID domain.UserID = 34673635133

func TestPlayerController_KnownPlayer_Returns200(t *testing.T) {
	fixture := givenAPlayerController(t)

	recorder := fixture.getAs(testUserID)

	assertStatusCode(t, recorder, http.StatusOK)
}

// §8.2: { "userId": 34633089486, "balance": 5000.00, "currency": "USD" }
func TestPlayerController_KnownPlayer_ReturnsUserIDBalanceAndCurrency(t *testing.T) {
	fixture := givenAPlayerController(t)

	recorder := fixture.getAs(testUserID)

	assertBody(t, recorder, `{"userId":34633089486,"balance":5000.00,"currency":"USD"}`)
}

func TestPlayerController_KnownPlayer_SetsJSONContentType(t *testing.T) {
	fixture := givenAPlayerController(t)

	recorder := fixture.getAs(testUserID)

	assertContentTypeJSON(t, recorder)
}

// §8.2: a balance must never be presented from a cache, not even an HTTP one.
func TestPlayerController_KnownPlayer_ForbidsCaching(t *testing.T) {
	fixture := givenAPlayerController(t)

	recorder := fixture.getAs(testUserID)

	assertHeader(t, recorder, "Cache-Control", "no-store")
}

func TestPlayerController_WalletFails_Returns502WalletError(t *testing.T) {
	fixture := givenAPlayerController(t)
	fixture.wallet.Next(domaintest.Reject)

	recorder := fixture.getAs(testUserID)

	assertStatusAndCode(t, recorder, http.StatusBadGateway, CodeWalletError)
}

func TestPlayerController_WalletTimesOut_Returns504WalletTimeout(t *testing.T) {
	fixture := givenAPlayerController(t)
	fixture.wallet.Next(domaintest.TimeOut)

	recorder := fixture.getAs(testUserID)

	assertStatusAndCode(t, recorder, http.StatusGatewayTimeout, CodeWalletTimeout)
}

// A valid token whose player is not in our database.
func TestPlayerController_UnknownPlayer_Returns404NotFound(t *testing.T) {
	fixture := givenAPlayerController(t)

	recorder := fixture.getAs(1)

	assertStatusAndCode(t, recorder, http.StatusNotFound, CodeNotFound)
}

// Only possible if the route was registered without RequireAuth: a wiring
// bug, so a 500, never a normal-looking answer.
func TestPlayerController_NoUserInContext_Returns500InternalError(t *testing.T) {
	fixture := givenAPlayerController(t)

	recorder := fixture.getWithoutUser()

	assertStatusAndCode(t, recorder, http.StatusInternalServerError, CodeInternal)
}

func TestPlayerController_NoUserInContext_IsLoggedAtErrorLevel(t *testing.T) {
	fixture := givenAPlayerController(t)

	fixture.getWithoutUser()

	assertLogField(t, parseLogLine(t, fixture.logOutput), "level", "ERROR")
}

func TestPlayerController_BehindRequireAuth_NoToken_Returns401Unauthorized(t *testing.T) {
	fixture := givenAPlayerController(t)

	recorder := fixture.getThroughAuth("")

	assertStatusAndCode(t, recorder, http.StatusUnauthorized, CodeUnauthorized)
}

func TestPlayerController_BehindRequireAuth_NoToken_DoesNotCallTheWallet(t *testing.T) {
	fixture := givenAPlayerController(t)

	fixture.getThroughAuth("")

	assertNoWalletCalls(t, fixture.wallet)
}

// The player comes from the token: player_usd_2's token gets player_usd_2's
// balance, never player_usd's.
func TestPlayerController_BehindRequireAuth_ValidToken_ReturnsTheTokensPlayer(t *testing.T) {
	fixture := givenAPlayerController(t)
	token := fixture.issueTokenFor(t, testSecondUserID)

	recorder := fixture.getThroughAuth("Bearer " + token)

	assertBody(t, recorder, `{"userId":34673635133,"balance":31415.25,"currency":"USD"}`)
}

/////////////////////////////////////////////////////////////////////////////////////

type playerControllerFixture struct {
	controller *PlayerController
	wallet     *domaintest.FakeWallet
	tokens     *domaintest.FakeTokens
	logOutput  *bytes.Buffer
}

// givenAPlayerController builds the controller with the real query and fakes:
// player_usd (USD 5000.00) and player_usd_2 (USD 31415.25).
func givenAPlayerController(t *testing.T) playerControllerFixture {
	t.Helper()
	players := domaintest.NewFakePlayerRepository(
		domain.Player{UserID: testUserID, Username: "player_usd", Currency: domain.USD},
		domain.Player{UserID: testSecondUserID, Username: "player_usd_2", Currency: domain.USD},
	)
	wallet := domaintest.NewFakeWallet(map[domain.UserID]domain.WalletBalance{
		testUserID:       {Amount: givenAParsedMoney(t, "5000"), Currency: domain.USD},
		testSecondUserID: {Amount: givenAParsedMoney(t, "31415.25"), Currency: domain.USD},
	})
	logOutput := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logOutput, nil))

	return playerControllerFixture{
		controller: NewPlayerController(queries.NewGetPlayerQueryHandler(players, wallet), logger),
		wallet:     wallet,
		tokens:     &domaintest.FakeTokens{},
		logOutput:  logOutput,
	}
}

// getAs calls the controller as RequireAuth would leave the request: with the
// user ID already in the context.
func (fixture playerControllerFixture) getAs(userID domain.UserID) *httptest.ResponseRecorder {
	request := newPlayerRequest()
	request = request.WithContext(context.WithValue(request.Context(), userIDKey{}, userID))
	return fixture.serve(fixture.controller, request)
}

func (fixture playerControllerFixture) getWithoutUser() *httptest.ResponseRecorder {
	return fixture.serve(fixture.controller, newPlayerRequest())
}

// getThroughAuth calls the controller wrapped in RequireAuth, like the router does.
func (fixture playerControllerFixture) getThroughAuth(authorization string) *httptest.ResponseRecorder {
	request := newPlayerRequest()
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	protected := RequireAuth(fixture.tokens, domaintest.FixedClock{Time: testNow})(fixture.controller)
	return fixture.serve(protected, request)
}

func (fixture playerControllerFixture) issueTokenFor(t *testing.T, userID domain.UserID) string {
	t.Helper()
	issued, err := fixture.tokens.Issue(userID, testNow)
	if err != nil {
		t.Fatalf("issue test token: %v", err)
	}
	return issued.Value
}

func (fixture playerControllerFixture) serve(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func newPlayerRequest() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/api/v1/player", nil)
}

func givenAParsedMoney(t *testing.T, value string) domain.Money {
	t.Helper()
	money, err := domain.ParseMoney(value)
	if err != nil {
		t.Fatalf("invalid test money %q: %v", value, err)
	}
	return money
}

// assertBody compares the exact JSON text (field order and number format
// included), ignoring the newline the encoder adds at the end.
func assertBody(t *testing.T, recorder *httptest.ResponseRecorder, want string) {
	t.Helper()
	if result := strings.TrimSuffix(recorder.Body.String(), "\n"); result != want {
		t.Errorf("expected body %s, got %s", want, result)
	}
}

func assertNoWalletCalls(t *testing.T, wallet *domaintest.FakeWallet) {
	t.Helper()
	if calls := wallet.Calls(); len(calls) != 0 {
		t.Errorf("expected no wallet calls, got %v", calls)
	}
}
