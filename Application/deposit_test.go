package application

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	actions "github.com/aaronhdez/kt_game-integration-api/Actions"
	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
	"github.com/aaronhdez/kt_game-integration-api/Domain/domaintest"
)

// The §8.4 example request: it settles the bet of the completed withdraw
// prov-tx-0001 (10.50) of testUserID, whose Wallet balance is then 4989.50.
const validDepositBody = `{"currency":"USD","amount":25.00,"providerTransactionId":"prov-tx-0002","providerWithdrawnTransactionId":"prov-tx-0001"}`

// Success

// With the first ID of the FakeIDGenerator.
func TestDepositController_WalletApplies_Returns201WithTheSection74Body(t *testing.T) {
	fixture := givenADepositController(t)

	recorder := fixture.postAs(testUserID, validDepositBody)

	assertStatusAndBody(t, recorder, http.StatusCreated, `{"transactionId":"00000000-0000-4000-8000-000000000001","providerTransactionId":"prov-tx-0002","oldBalance":4989.50,"newBalance":5014.50,"status":"COMPLETED","betStatus":"WON"}`)
}

// A zero amount settles the bet as LOST; no money moves.
func TestDepositController_ZeroAmount_Returns201WithBetStatusLost(t *testing.T) {
	fixture := givenADepositController(t)

	recorder := fixture.postAs(testUserID, `{"currency":"USD","amount":0,"providerTransactionId":"prov-tx-0002","providerWithdrawnTransactionId":"prov-tx-0001"}`)

	assertStatusAndBody(t, recorder, http.StatusCreated, `{"transactionId":"00000000-0000-4000-8000-000000000001","providerTransactionId":"prov-tx-0002","oldBalance":4989.50,"newBalance":4989.50,"status":"COMPLETED","betStatus":"LOST"}`)
}

func TestDepositController_SameRequestTwice_Returns200WithTheStoredResult(t *testing.T) {
	fixture := givenADepositController(t)
	fixture.postAs(testUserID, validDepositBody)

	recorder := fixture.postAs(testUserID, validDepositBody)

	assertStatusAndBody(t, recorder, http.StatusOK, `{"transactionId":"00000000-0000-4000-8000-000000000001","providerTransactionId":"prov-tx-0002","oldBalance":4989.50,"newBalance":5014.50,"status":"COMPLETED","betStatus":"WON"}`)
}

// betStatus is null only while a withdraw is PENDING; a pending
// deposit leaves its bet PLACED.
func TestDepositController_WalletOutcomeUnknown_Returns202PendingWithTheBetPlaced(t *testing.T) {
	fixture := givenADepositController(t)
	fixture.wallet.Next(domaintest.Succeed, domaintest.FailAfterApply)

	recorder := fixture.postAs(testUserID, validDepositBody)

	assertStatusAndBody(t, recorder, http.StatusAccepted, `{"transactionId":"00000000-0000-4000-8000-000000000001","providerTransactionId":"prov-tx-0002","oldBalance":4989.50,"newBalance":null,"status":"PENDING","betStatus":"PLACED"}`)
}

// Invalid requests: 400 before anything reaches the Wallet

func TestDepositController_InvalidBody_Returns400InvalidRequest(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{"malformed JSON", `{"currency":"USD",`},
		{"missing amount", `{"currency":"USD","providerTransactionId":"prov-tx-0002","providerWithdrawnTransactionId":"prov-tx-0001"}`},
		{"null amount", `{"currency":"USD","amount":null,"providerTransactionId":"prov-tx-0002","providerWithdrawnTransactionId":"prov-tx-0001"}`},
		{"amount as a string", `{"currency":"USD","amount":"25.00","providerTransactionId":"prov-tx-0002","providerWithdrawnTransactionId":"prov-tx-0001"}`},
		{"amount with three decimals", `{"currency":"USD","amount":25.005,"providerTransactionId":"prov-tx-0002","providerWithdrawnTransactionId":"prov-tx-0001"}`},
		{"negative amount", `{"currency":"USD","amount":-1,"providerTransactionId":"prov-tx-0002","providerWithdrawnTransactionId":"prov-tx-0001"}`},
		{"missing provider transaction id", `{"currency":"USD","amount":25.00,"providerWithdrawnTransactionId":"prov-tx-0001"}`},
		{"missing provider withdrawn transaction id", `{"currency":"USD","amount":25.00,"providerTransactionId":"prov-tx-0002"}`},
		{"missing currency", `{"amount":25.00,"providerTransactionId":"prov-tx-0002","providerWithdrawnTransactionId":"prov-tx-0001"}`},
		{"lowercase currency", `{"currency":"usd","amount":25.00,"providerTransactionId":"prov-tx-0002","providerWithdrawnTransactionId":"prov-tx-0001"}`},
		{"currency of another wallet", `{"currency":"EUR","amount":25.00,"providerTransactionId":"prov-tx-0002","providerWithdrawnTransactionId":"prov-tx-0001"}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenADepositController(t)

			recorder := fixture.postAs(testUserID, testCase.body)

			assertStatusAndCode(t, recorder, http.StatusBadRequest, CodeInvalidRequest)
		})
	}
}

// A missing amount is not a zero amount (which would settle the bet as LOST):
// the provider is told it is missing.
func TestDepositController_MissingAmount_SaysItIsRequired(t *testing.T) {
	fixture := givenADepositController(t)

	recorder := fixture.postAs(testUserID, `{"currency":"USD","providerTransactionId":"prov-tx-0002","providerWithdrawnTransactionId":"prov-tx-0001"}`)

	assertErrorMessageContains(t, recorder, "amount is required")
}

func TestDepositController_InvalidBody_DoesNotCallTheWallet(t *testing.T) {
	fixture := givenADepositController(t)

	fixture.postAs(testUserID, `{"currency":"USD","providerTransactionId":"prov-tx-0002","providerWithdrawnTransactionId":"prov-tx-0001"}`)

	assertNoWalletCalls(t, fixture.wallet)
}

// Referenced withdraw

func TestDepositController_UnknownWithdraw_Returns404NotFound(t *testing.T) {
	fixture := givenADepositController(t)

	recorder := fixture.postAs(testUserID, `{"currency":"USD","amount":25.00,"providerTransactionId":"prov-tx-0002","providerWithdrawnTransactionId":"prov-tx-unknown"}`)

	assertStatusAndCode(t, recorder, http.StatusNotFound, CodeNotFound)
}

// unknown or belonging to another player → 404, with no hint of which.
func TestDepositController_AnotherPlayersWithdraw_Returns404NotFound(t *testing.T) {
	fixture := givenADepositController(t)

	recorder := fixture.postAs(testSecondUserID, validDepositBody)

	assertStatusAndCode(t, recorder, http.StatusNotFound, CodeNotFound)
}

// Conflicts

func TestDepositController_SecondDepositOnASettledBet_Returns409Conflict(t *testing.T) {
	fixture := givenADepositController(t)
	fixture.postAs(testUserID, validDepositBody)

	recorder := fixture.postAs(testUserID, `{"currency":"USD","amount":30.00,"providerTransactionId":"prov-tx-0003","providerWithdrawnTransactionId":"prov-tx-0001"}`)

	assertStatusAndCode(t, recorder, http.StatusConflict, CodeConflict)
}

func TestDepositController_SameProviderIDWithAnotherAmount_Returns409Conflict(t *testing.T) {
	fixture := givenADepositController(t)
	fixture.postAs(testUserID, validDepositBody)

	recorder := fixture.postAs(testUserID, `{"currency":"USD","amount":26.00,"providerTransactionId":"prov-tx-0002","providerWithdrawnTransactionId":"prov-tx-0001"}`)

	assertStatusAndCode(t, recorder, http.StatusConflict, CodeConflict)
}

// Wallet answers

func TestDepositController_WalletRejects_Returns502WalletError(t *testing.T) {
	fixture := givenADepositController(t)
	fixture.wallet.Next(domaintest.Succeed, domaintest.Reject)

	recorder := fixture.postAs(testUserID, validDepositBody)

	assertStatusAndCode(t, recorder, http.StatusBadGateway, CodeWalletError)
}

// 504 is only for reads such as GET /player; nothing was sent yet.
func TestDepositController_OldBalanceReadTimesOut_Returns502WalletError(t *testing.T) {
	fixture := givenADepositController(t)
	fixture.wallet.Next(domaintest.TimeOut)

	recorder := fixture.postAs(testUserID, validDepositBody)

	assertStatusAndCode(t, recorder, http.StatusBadGateway, CodeWalletError)
}

// Authentication

// Only possible if the route was registered without RequireAuth: a wiring bug.
func TestDepositController_NoUserInContext_Returns500InternalError(t *testing.T) {
	fixture := givenADepositController(t)

	recorder := fixture.postWithoutUser(validDepositBody)

	assertStatusAndCode(t, recorder, http.StatusInternalServerError, CodeInternal)
}

func TestDepositController_BehindRequireAuth_NoToken_Returns401Unauthorized(t *testing.T) {
	fixture := givenADepositController(t)

	recorder := fixture.postThroughAuth("", validDepositBody)

	assertStatusAndCode(t, recorder, http.StatusUnauthorized, CodeUnauthorized)
}

func TestDepositController_BehindRequireAuth_NoToken_DoesNotCallTheWallet(t *testing.T) {
	fixture := givenADepositController(t)

	fixture.postThroughAuth("", validDepositBody)

	assertNoWalletCalls(t, fixture.wallet)
}

func TestDepositController_BehindRequireAuth_ValidToken_Returns201(t *testing.T) {
	fixture := givenADepositController(t)
	token := fixture.issueTokenFor(t, testUserID)

	recorder := fixture.postThroughAuth("Bearer "+token, validDepositBody)

	assertStatusCode(t, recorder, http.StatusCreated)
}

/////////////////////////////////////////////////////////////////////////////////////

type depositControllerFixture struct {
	controller *DepositController
	wallet     *domaintest.FakeWallet
	tokens     *domaintest.FakeTokens
}

// givenADepositController builds the controller with the real use case and
// fakes: player_usd (and player_usd_2) with a completed withdraw prov-tx-0001
// of 10.50, its bet still PLACED, and USD 4989.50 in the Wallet.
func givenADepositController(t *testing.T) depositControllerFixture {
	t.Helper()
	players := domaintest.NewFakePlayerRepository(
		domain.Player{UserID: testUserID, Username: "player_usd", Currency: domain.USD},
		domain.Player{UserID: testSecondUserID, Username: "player_usd_2", Currency: domain.USD},
	)
	wallet := domaintest.NewFakeWallet(map[domain.UserID]domain.WalletBalance{
		testUserID:       {Amount: givenAParsedMoney(t, "4989.50"), Currency: domain.USD},
		testSecondUserID: {Amount: givenAParsedMoney(t, "31415.25"), Currency: domain.USD},
	})
	store := domaintest.NewFakeStore()
	store.PutTransaction(aCompletedWithdrawToSettle(t))
	store.PutBet(domain.NewPlacedBet(7, testUserID, domain.USD, givenAParsedMoney(t, "10.50"), testNow))
	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	deposit := actions.NewDepositCommandHandler(
		players,
		store,
		wallet,
		&domaintest.FakeIDGenerator{},
		domaintest.FixedClock{Time: testNow},
		logger,
	)

	return depositControllerFixture{
		controller: NewDepositController(deposit, logger),
		wallet:     wallet,
		tokens:     &domaintest.FakeTokens{},
	}
}

// aCompletedWithdrawToSettle is the withdraw prov-tx-0001 of player_usd,
// with bet 7, referenced by validDepositBody.
func aCompletedWithdrawToSettle(t *testing.T) domain.Transaction {
	t.Helper()
	newBalance := givenAParsedMoney(t, "4989.50")
	return domain.Transaction{
		ID:                    domaintest.FakeTransactionID(100),
		Type:                  domain.TransactionWithdraw,
		Status:                domain.TransactionCompleted,
		UserID:                testUserID,
		BetID:                 7,
		ProviderTransactionID: "prov-tx-0001",
		Amount:                givenAParsedMoney(t, "10.50"),
		Currency:              domain.USD,
		OldBalance:            givenAParsedMoney(t, "5000.00"),
		NewBalance:            &newBalance,
		CreatedAt:             testNow,
		UpdatedAt:             testNow,
	}
}

// postAs calls the controller as RequireAuth would leave the request: with the
// user ID already in the context.
func (fixture depositControllerFixture) postAs(userID domain.UserID, body string) *httptest.ResponseRecorder {
	request := newDepositRequest(body)
	request = request.WithContext(context.WithValue(request.Context(), userIDKey{}, userID))
	return serveRequest(fixture.controller, request)
}

func (fixture depositControllerFixture) postWithoutUser(body string) *httptest.ResponseRecorder {
	return serveRequest(fixture.controller, newDepositRequest(body))
}

// postThroughAuth calls the controller wrapped in RequireAuth, like the router does.
func (fixture depositControllerFixture) postThroughAuth(authorization, body string) *httptest.ResponseRecorder {
	request := newDepositRequest(body)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	protected := RequireAuth(fixture.tokens, domaintest.FixedClock{Time: testNow})(fixture.controller)
	return serveRequest(protected, request)
}

func (fixture depositControllerFixture) issueTokenFor(t *testing.T, userID domain.UserID) string {
	t.Helper()
	issued, err := fixture.tokens.Issue(userID, testNow)
	if err != nil {
		t.Fatalf("issue test token: %v", err)
	}
	return issued.Value
}

func newDepositRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/deposit", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}
