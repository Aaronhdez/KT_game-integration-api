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

// (10.50) of testUserID. cancelDepositBody cancels its deposit prov-tx-0002
// (25.00), stored only by givenACancelControllerWithAWonBet.
const (
	cancelWithdrawBody = `{"providerTransactionId":"prov-tx-0001"}`
	cancelDepositBody  = `{"providerTransactionId":"prov-tx-0002"}`
)

// Success

// The stake is credited back; providerTransactionId is the cancelled one's (§7.4).
func TestCancelController_CancelOfAWithdraw_Returns201WithBetStatusCancelled(t *testing.T) {
	fixture := givenACancelController(t)

	recorder := fixture.postAs(testUserID, cancelWithdrawBody)

	assertStatusAndBody(t, recorder, http.StatusCreated, `{"transactionId":"00000000-0000-4000-8000-000000000001","providerTransactionId":"prov-tx-0001","oldBalance":4989.50,"newBalance":5000.00,"status":"COMPLETED","betStatus":"CANCELLED"}`)
}

// The winnings are debited back and the bet returns to PLACED.
func TestCancelController_CancelOfADeposit_Returns201WithBetStatusPlaced(t *testing.T) {
	fixture := givenACancelControllerWithAWonBet(t, "5014.50")

	recorder := fixture.postAs(testUserID, cancelDepositBody)

	assertStatusAndBody(t, recorder, http.StatusCreated, `{"transactionId":"00000000-0000-4000-8000-000000000001","providerTransactionId":"prov-tx-0002","oldBalance":5014.50,"newBalance":4989.50,"status":"COMPLETED","betStatus":"PLACED"}`)
}

func TestCancelController_SameCancelTwice_Returns200WithTheStoredResult(t *testing.T) {
	fixture := givenACancelController(t)
	fixture.postAs(testUserID, cancelWithdrawBody)

	recorder := fixture.postAs(testUserID, cancelWithdrawBody)

	assertStatusAndBody(t, recorder, http.StatusOK, `{"transactionId":"00000000-0000-4000-8000-000000000001","providerTransactionId":"prov-tx-0001","oldBalance":4989.50,"newBalance":5000.00,"status":"COMPLETED","betStatus":"CANCELLED"}`)
}

// The bet keeps its status until the cancel is confirmed.
func TestCancelController_WalletOutcomeUnknown_Returns202PendingWithTheBetUnchanged(t *testing.T) {
	fixture := givenACancelController(t)
	fixture.wallet.Next(domaintest.Succeed, domaintest.FailAfterApply)

	recorder := fixture.postAs(testUserID, cancelWithdrawBody)

	assertStatusAndBody(t, recorder, http.StatusAccepted, `{"transactionId":"00000000-0000-4000-8000-000000000001","providerTransactionId":"prov-tx-0001","oldBalance":4989.50,"newBalance":null,"status":"PENDING","betStatus":"PLACED"}`)
}

// Invalid requests: 400 before anything reaches the Wallet

var invalidCancelBodies = []struct {
	name string
	body string
}{
	{"malformed JSON", `{"providerTransactionId":`},
	{"missing provider transaction id", `{}`},
	{"empty provider transaction id", `{"providerTransactionId":""}`},
	{"provider transaction id as a number", `{"providerTransactionId":1}`},
}

func TestCancelController_InvalidBody_Returns400InvalidRequest(t *testing.T) {
	for _, testCase := range invalidCancelBodies {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenACancelController(t)

			recorder := fixture.postAs(testUserID, testCase.body)

			assertStatusAndCode(t, recorder, http.StatusBadRequest, CodeInvalidRequest)
		})
	}
}

func TestCancelController_InvalidBody_DoesNotCallTheWallet(t *testing.T) {
	for _, testCase := range invalidCancelBodies {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := givenACancelController(t)

			fixture.postAs(testUserID, testCase.body)

			assertNoWalletCalls(t, fixture.wallet)
		})
	}
}

// Transaction to cancel

func TestCancelController_UnknownTransaction_Returns404NotFound(t *testing.T) {
	fixture := givenACancelController(t)

	recorder := fixture.postAs(testUserID, `{"providerTransactionId":"prov-tx-unknown"}`)

	assertStatusAndCode(t, recorder, http.StatusNotFound, CodeNotFound)
}

// unknown or belonging to another player → 404, with no hint of which.
func TestCancelController_AnotherPlayersTransaction_Returns404NotFound(t *testing.T) {
	fixture := givenACancelController(t)

	recorder := fixture.postAs(testSecondUserID, cancelWithdrawBody)

	assertStatusAndCode(t, recorder, http.StatusNotFound, CodeNotFound)
}

// Conflicts

// §8.5: "The deposit has to be cancelled first."
func TestCancelController_WithdrawOfASettledBet_Returns409Conflict(t *testing.T) {
	fixture := givenACancelControllerWithAWonBet(t, "5014.50")

	recorder := fixture.postAs(testUserID, cancelWithdrawBody)

	assertStatusAndCode(t, recorder, http.StatusConflict, CodeConflict)
}

// A retry while the first cancel is PENDING is a replay, not a conflict.
func TestCancelController_PendingCancelOfTheSameTransaction_Returns202AsAReplay(t *testing.T) {
	fixture := givenACancelController(t)
	fixture.wallet.Next(domaintest.Succeed, domaintest.FailAfterApply)
	fixture.postAs(testUserID, cancelWithdrawBody)

	recorder := fixture.postAs(testUserID, cancelWithdrawBody)

	assertStatusCode(t, recorder, http.StatusAccepted)
}

// Wallet answers

// §8.5: "If funds are insufficient, nothing changes." 25.00 left is not
// enough to take 25.00 back (the Wallet requires amount < balance).
func TestCancelController_DebitBackWithInsufficientFunds_Returns422InsufficientFunds(t *testing.T) {
	fixture := givenACancelControllerWithAWonBet(t, "25.00")

	recorder := fixture.postAs(testUserID, cancelDepositBody)

	assertStatusAndCode(t, recorder, http.StatusUnprocessableEntity, CodeInsufficientFunds)
}

// §7.1: a retry of a FAILED cancel gets the same answer as the first attempt.
func TestCancelController_RetryOfACancelWithInsufficientFunds_Returns422InsufficientFunds(t *testing.T) {
	fixture := givenACancelControllerWithAWonBet(t, "25.00")
	fixture.postAs(testUserID, cancelDepositBody)

	recorder := fixture.postAs(testUserID, cancelDepositBody)

	assertStatusAndCode(t, recorder, http.StatusUnprocessableEntity, CodeInsufficientFunds)
}

func TestCancelController_WalletRejects_Returns502WalletError(t *testing.T) {
	fixture := givenACancelController(t)
	fixture.wallet.Next(domaintest.Succeed, domaintest.Reject)

	recorder := fixture.postAs(testUserID, cancelWithdrawBody)

	assertStatusAndCode(t, recorder, http.StatusBadGateway, CodeWalletError)
}

// 504 is only for reads such as GET /player; nothing was sent yet.
func TestCancelController_OldBalanceReadTimesOut_Returns502WalletError(t *testing.T) {
	fixture := givenACancelController(t)
	fixture.wallet.Next(domaintest.TimeOut)

	recorder := fixture.postAs(testUserID, cancelWithdrawBody)

	assertStatusAndCode(t, recorder, http.StatusBadGateway, CodeWalletError)
}

// Authentication

// Only possible if the route was registered without RequireAuth: a wiring bug.
func TestCancelController_NoUserInContext_Returns500InternalError(t *testing.T) {
	fixture := givenACancelController(t)

	recorder := fixture.postWithoutUser(cancelWithdrawBody)

	assertStatusAndCode(t, recorder, http.StatusInternalServerError, CodeInternal)
}

func TestCancelController_BehindRequireAuth_NoToken_Returns401Unauthorized(t *testing.T) {
	fixture := givenACancelController(t)

	recorder := fixture.postThroughAuth("", cancelWithdrawBody)

	assertStatusAndCode(t, recorder, http.StatusUnauthorized, CodeUnauthorized)
}

func TestCancelController_BehindRequireAuth_NoToken_DoesNotCallTheWallet(t *testing.T) {
	fixture := givenACancelController(t)

	fixture.postThroughAuth("", cancelWithdrawBody)

	assertNoWalletCalls(t, fixture.wallet)
}

func TestCancelController_BehindRequireAuth_ValidToken_Returns201(t *testing.T) {
	fixture := givenACancelController(t)
	token := fixture.issueTokenFor(t, testUserID)

	recorder := fixture.postThroughAuth("Bearer "+token, cancelWithdrawBody)

	assertStatusCode(t, recorder, http.StatusCreated)
}

/////////////////////////////////////////////////////////////////////////////////////

type cancelControllerFixture struct {
	controller *CancelController
	wallet     *domaintest.FakeWallet
	tokens     *domaintest.FakeTokens
}

// givenACancelController builds the controller with the real use case and
// fakes: player_usd (and player_usd_2) with a completed withdraw prov-tx-0001
// of 10.50, its bet still PLACED, and USD 4989.50 in the Wallet.
func givenACancelController(t *testing.T) cancelControllerFixture {
	t.Helper()
	store := domaintest.NewFakeStore()
	store.PutTransaction(aCompletedWithdrawToSettle(t))
	store.PutBet(domain.NewPlacedBet(7, testUserID, domain.USD, givenAParsedMoney(t, "10.50"), testNow))
	return newCancelControllerFixture(t, store, "4989.50")
}

// givenACancelControllerWithAWonBet adds the completed deposit prov-tx-0002
// of 25.00 that settled the bet as WON, with balance in the Wallet.
func givenACancelControllerWithAWonBet(t *testing.T, balance string) cancelControllerFixture {
	t.Helper()
	store := domaintest.NewFakeStore()
	store.PutTransaction(aCompletedWithdrawToSettle(t))
	store.PutTransaction(aCompletedDepositToCancel(t))
	wonBet := domain.NewPlacedBet(7, testUserID, domain.USD, givenAParsedMoney(t, "10.50"), testNow)
	wonBet.Status = domain.BetWon
	store.PutBet(wonBet)
	return newCancelControllerFixture(t, store, balance)
}

func newCancelControllerFixture(t *testing.T, store *domaintest.FakeStore, balance string) cancelControllerFixture {
	t.Helper()
	players := domaintest.NewFakePlayerRepository(
		domain.Player{UserID: testUserID, Username: "player_usd", Currency: domain.USD},
		domain.Player{UserID: testSecondUserID, Username: "player_usd_2", Currency: domain.USD},
	)
	wallet := domaintest.NewFakeWallet(map[domain.UserID]domain.WalletBalance{
		testUserID:       {Amount: givenAParsedMoney(t, balance), Currency: domain.USD},
		testSecondUserID: {Amount: givenAParsedMoney(t, "31415.25"), Currency: domain.USD},
	})
	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	cancel := actions.NewCancelCommandHandler(
		players,
		store,
		wallet,
		&domaintest.FakeIDGenerator{},
		domaintest.FixedClock{Time: testNow},
		logger,
	)

	return cancelControllerFixture{
		controller: NewCancelController(cancel, logger),
		wallet:     wallet,
		tokens:     &domaintest.FakeTokens{},
	}
}

// aCompletedDepositToCancel is the deposit prov-tx-0002 of 25.00 that settled
// bet 7, with its own transaction ID (the new cancel takes the first one).
func aCompletedDepositToCancel(t *testing.T) domain.Transaction {
	t.Helper()
	newBalance := givenAParsedMoney(t, "5014.50")
	return domain.Transaction{
		ID:                    domaintest.FakeTransactionID(80),
		Type:                  domain.TransactionDeposit,
		Status:                domain.TransactionCompleted,
		UserID:                testUserID,
		BetID:                 7,
		ProviderTransactionID: "prov-tx-0002",
		Amount:                givenAParsedMoney(t, "25.00"),
		Currency:              domain.USD,
		OldBalance:            givenAParsedMoney(t, "4989.50"),
		NewBalance:            &newBalance,
		CreatedAt:             testNow,
		UpdatedAt:             testNow,
	}
}

// postAs calls the controller as RequireAuth would leave the request: with the
// user ID already in the context.
func (fixture cancelControllerFixture) postAs(userID domain.UserID, body string) *httptest.ResponseRecorder {
	request := newCancelRequest(body)
	request = request.WithContext(context.WithValue(request.Context(), userIDKey{}, userID))
	return serveRequest(fixture.controller, request)
}

func (fixture cancelControllerFixture) postWithoutUser(body string) *httptest.ResponseRecorder {
	return serveRequest(fixture.controller, newCancelRequest(body))
}

// postThroughAuth calls the controller wrapped in RequireAuth, like the router does.
func (fixture cancelControllerFixture) postThroughAuth(authorization, body string) *httptest.ResponseRecorder {
	request := newCancelRequest(body)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	protected := RequireAuth(fixture.tokens, domaintest.FixedClock{Time: testNow})(fixture.controller)
	return serveRequest(protected, request)
}

func (fixture cancelControllerFixture) issueTokenFor(t *testing.T, userID domain.UserID) string {
	t.Helper()
	issued, err := fixture.tokens.Issue(userID, testNow)
	if err != nil {
		t.Fatalf("issue test token: %v", err)
	}
	return issued.Value
}

func newCancelRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/cancel", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}
