package application

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	actions "github.com/aaronhdez/kt_game-integration-api/Actions"
	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
	"github.com/aaronhdez/kt_game-integration-api/Domain/domaintest"
)

// The §8.3 example request, for the player of testUserID (USD 5000.00).
const validWithdrawBody = `{"currency":"USD","amount":10.50,"providerTransactionId":"prov-tx-0001"}`

// Success (§7.2, §7.4)

// §8.3 response, with the first ID of the FakeIDGenerator.
func TestWithdrawController_WalletApplies_Returns201WithTheSection74Body(t *testing.T) {
	fixture := givenAWithdrawController(t)

	recorder := fixture.postAs(testUserID, validWithdrawBody)

	assertStatusAndBody(t, recorder, http.StatusCreated, `{"transactionId":"00000000-0000-4000-8000-000000000001","providerTransactionId":"prov-tx-0001","oldBalance":5000.00,"newBalance":4989.50,"status":"COMPLETED","betStatus":"PLACED"}`)
}

func TestWithdrawController_SameRequestTwice_Returns200WithTheStoredResult(t *testing.T) {
	fixture := givenAWithdrawController(t)
	fixture.postAs(testUserID, validWithdrawBody)

	recorder := fixture.postAs(testUserID, validWithdrawBody)

	assertStatusAndBody(t, recorder, http.StatusOK, `{"transactionId":"00000000-0000-4000-8000-000000000001","providerTransactionId":"prov-tx-0001","oldBalance":5000.00,"newBalance":4989.50,"status":"COMPLETED","betStatus":"PLACED"}`)
}

// §9.3: an unknown outcome is explicit, never an error.
func TestWithdrawController_WalletOutcomeUnknown_Returns202Pending(t *testing.T) {
	fixture := givenAWithdrawController(t)
	fixture.wallet.Next(domaintest.Succeed, domaintest.FailAfterApply)

	recorder := fixture.postAs(testUserID, validWithdrawBody)

	assertStatusAndBody(t, recorder, http.StatusAccepted, `{"transactionId":"00000000-0000-4000-8000-000000000001","providerTransactionId":"prov-tx-0001","oldBalance":5000.00,"newBalance":null,"status":"PENDING","betStatus":null}`)
}

// Invalid requests (§7.3): 400 before anything reaches the Wallet

func TestWithdrawController_InvalidBody_Returns400InvalidRequest(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{"malformed JSON", `{"currency":"USD",`},
		{"missing amount", `{"currency":"USD","providerTransactionId":"prov-tx-0001"}`},
		{"null amount", `{"currency":"USD","amount":null,"providerTransactionId":"prov-tx-0001"}`},
		{"amount as a string", `{"currency":"USD","amount":"10.50","providerTransactionId":"prov-tx-0001"}`},
		{"amount with three decimals", `{"currency":"USD","amount":10.505,"providerTransactionId":"prov-tx-0001"}`},
		{"zero amount", `{"currency":"USD","amount":0,"providerTransactionId":"prov-tx-0001"}`},
		{"negative amount", `{"currency":"USD","amount":-1,"providerTransactionId":"prov-tx-0001"}`},
		{"missing provider transaction id", `{"currency":"USD","amount":10.50}`},
		{"missing currency", `{"amount":10.50,"providerTransactionId":"prov-tx-0001"}`},
		{"lowercase currency", `{"currency":"usd","amount":10.50,"providerTransactionId":"prov-tx-0001"}`},
		{"currency of another wallet", `{"currency":"EUR","amount":10.50,"providerTransactionId":"prov-tx-0001"}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenAWithdrawController(t)

			recorder := fixture.postAs(testUserID, testCase.body)

			assertStatusAndCode(t, recorder, http.StatusBadRequest, CodeInvalidRequest)
		})
	}
}

// A missing amount is not a zero amount: the provider is told it is missing.
func TestWithdrawController_MissingAmount_SaysItIsRequired(t *testing.T) {
	fixture := givenAWithdrawController(t)

	recorder := fixture.postAs(testUserID, `{"currency":"USD","providerTransactionId":"prov-tx-0001"}`)

	assertErrorMessageContains(t, recorder, "amount is required")
}

func TestWithdrawController_InvalidBody_DoesNotCallTheWallet(t *testing.T) {
	fixture := givenAWithdrawController(t)

	fixture.postAs(testUserID, `{"currency":"USD","providerTransactionId":"prov-tx-0001"}`)

	assertNoWalletCalls(t, fixture.wallet)
}

// Conflicts (§7.1, §7.3)

func TestWithdrawController_SameProviderIDWithAnotherAmount_Returns409Conflict(t *testing.T) {
	fixture := givenAWithdrawController(t)
	fixture.postAs(testUserID, validWithdrawBody)

	recorder := fixture.postAs(testUserID, `{"currency":"USD","amount":11.00,"providerTransactionId":"prov-tx-0001"}`)

	assertStatusAndCode(t, recorder, http.StatusConflict, CodeConflict)
}

func TestWithdrawController_RetryOfAFailedWithdraw_Returns409Conflict(t *testing.T) {
	fixture := givenAWithdrawController(t)
	fixture.wallet.Next(domaintest.Succeed, domaintest.Reject)
	fixture.postAs(testUserID, validWithdrawBody)

	recorder := fixture.postAs(testUserID, validWithdrawBody)

	assertStatusAndCode(t, recorder, http.StatusConflict, CodeConflict)
}

// Wallet answers (§7.3, §8.3)

func TestWithdrawController_InsufficientFunds_Returns422InsufficientFunds(t *testing.T) {
	fixture := givenAWithdrawController(t)

	recorder := fixture.postAs(testUserID, `{"currency":"USD","amount":6000.00,"providerTransactionId":"prov-tx-0001"}`)

	assertStatusAndCode(t, recorder, http.StatusUnprocessableEntity, CodeInsufficientFunds)
}

func TestWithdrawController_WalletRejects_Returns502WalletError(t *testing.T) {
	fixture := givenAWithdrawController(t)
	fixture.wallet.Next(domaintest.Succeed, domaintest.Reject)

	recorder := fixture.postAs(testUserID, validWithdrawBody)

	assertStatusAndCode(t, recorder, http.StatusBadGateway, CodeWalletError)
}

// §7.3: 504 is only for reads such as GET /player; nothing was sent yet.
func TestWithdrawController_OldBalanceReadTimesOut_Returns502WalletError(t *testing.T) {
	fixture := givenAWithdrawController(t)
	fixture.wallet.Next(domaintest.TimeOut)

	recorder := fixture.postAs(testUserID, validWithdrawBody)

	assertStatusAndCode(t, recorder, http.StatusBadGateway, CodeWalletError)
}

// Authentication

// Only possible if the route was registered without RequireAuth: a wiring bug.
func TestWithdrawController_NoUserInContext_Returns500InternalError(t *testing.T) {
	fixture := givenAWithdrawController(t)

	recorder := fixture.postWithoutUser(validWithdrawBody)

	assertStatusAndCode(t, recorder, http.StatusInternalServerError, CodeInternal)
}

func TestWithdrawController_BehindRequireAuth_NoToken_Returns401Unauthorized(t *testing.T) {
	fixture := givenAWithdrawController(t)

	recorder := fixture.postThroughAuth("", validWithdrawBody)

	assertStatusAndCode(t, recorder, http.StatusUnauthorized, CodeUnauthorized)
}

func TestWithdrawController_BehindRequireAuth_NoToken_DoesNotCallTheWallet(t *testing.T) {
	fixture := givenAWithdrawController(t)

	fixture.postThroughAuth("", validWithdrawBody)

	assertNoWalletCalls(t, fixture.wallet)
}

func TestWithdrawController_BehindRequireAuth_ValidToken_Returns201(t *testing.T) {
	fixture := givenAWithdrawController(t)
	token := fixture.issueTokenFor(t, testUserID)

	recorder := fixture.postThroughAuth("Bearer "+token, validWithdrawBody)

	assertStatusCode(t, recorder, http.StatusCreated)
}

/////////////////////////////////////////////////////////////////////////////////////

type withdrawControllerFixture struct {
	controller *WithdrawController
	wallet     *domaintest.FakeWallet
	tokens     *domaintest.FakeTokens
}

// givenAWithdrawController builds the controller with the real use case and
// fakes: player_usd with USD 5000.00 in the Wallet and an empty store.
func givenAWithdrawController(t *testing.T) withdrawControllerFixture {
	t.Helper()
	players := domaintest.NewFakePlayerRepository(
		domain.Player{UserID: testUserID, Username: "player_usd", Currency: domain.USD},
	)
	wallet := domaintest.NewFakeWallet(map[domain.UserID]domain.WalletBalance{
		testUserID: {Amount: givenAParsedMoney(t, "5000.00"), Currency: domain.USD},
	})
	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	withdraw := actions.NewWithdrawCommandHandler(
		players,
		domaintest.NewFakeStore(),
		wallet,
		&domaintest.FakeIDGenerator{},
		domaintest.FixedClock{Time: testNow},
		logger,
	)

	return withdrawControllerFixture{
		controller: NewWithdrawController(withdraw, logger),
		wallet:     wallet,
		tokens:     &domaintest.FakeTokens{},
	}
}

// postAs calls the controller as RequireAuth would leave the request: with the
// user ID already in the context.
func (fixture withdrawControllerFixture) postAs(userID domain.UserID, body string) *httptest.ResponseRecorder {
	request := newWithdrawRequest(body)
	request = request.WithContext(context.WithValue(request.Context(), userIDKey{}, userID))
	return serveRequest(fixture.controller, request)
}

func (fixture withdrawControllerFixture) postWithoutUser(body string) *httptest.ResponseRecorder {
	return serveRequest(fixture.controller, newWithdrawRequest(body))
}

// postThroughAuth calls the controller wrapped in RequireAuth, like the router does.
func (fixture withdrawControllerFixture) postThroughAuth(authorization, body string) *httptest.ResponseRecorder {
	request := newWithdrawRequest(body)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	protected := RequireAuth(fixture.tokens, domaintest.FixedClock{Time: testNow})(fixture.controller)
	return serveRequest(protected, request)
}

func (fixture withdrawControllerFixture) issueTokenFor(t *testing.T, userID domain.UserID) string {
	t.Helper()
	issued, err := fixture.tokens.Issue(userID, testNow)
	if err != nil {
		t.Fatalf("issue test token: %v", err)
	}
	return issued.Value
}

func newWithdrawRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/withdraw", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func serveRequest(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func assertErrorMessageContains(t *testing.T, recorder *httptest.ResponseRecorder, want string) {
	t.Helper()
	var response ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid error body %q: %v", recorder.Body.String(), err)
	}
	if !strings.Contains(response.Message, want) {
		t.Errorf("expected the error message to contain %q, got %q", want, response.Message)
	}
}

func assertStatusAndBody(t *testing.T, recorder *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	assertStatusCode(t, recorder, status)
	assertBody(t, recorder, body)
}
