package wallet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

const (
	testAPIKey  = "test-api-key"
	testTimeout = time.Second
)

func TestWithdraw_Success_ReturnsTheNewBalance(t *testing.T) {
	testCases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "balance as a string, as the mock sends it",
			body: `{"balance":"4989.5","transactions":[{"id":1,"reference":"tx-1"}]}`,
			want: "4989.50",
		},
		{
			name: "balance as a number, as swagger documents it",
			body: `{"balance":4989.5,"transactions":[{"id":1,"reference":"tx-1"}]}`,
			want: "4989.50",
		},
		{
			name: "large balance with cents",
			body: `{"balance":"9000000000.09","transactions":[{"id":1,"reference":"tx-1"}]}`,
			want: "9000000000.09",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := givenAWalletAnswering(t, http.StatusOK, testCase.body)
			client := newTestClient(server)

			result, err := client.Withdraw(context.Background(), givenAWithdraw(t))

			assertBalance(t, result, err, testCase.want)
		})
	}
}

func TestWithdraw_SuccessWithUnusableBody_IsAppliedWithUnknownBalance(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: `{"balance":`},
		{name: "empty body", body: ``},
		{name: "missing balance", body: `{"transactions":[{"id":1,"reference":"tx-1"}]}`},
		{name: "balance with three decimals", body: `{"balance":"4989.505"}`},
		{name: "negative balance", body: `{"balance":"-1.00"}`},
		{name: "balance that is not a number", body: `{"balance":"abc"}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := givenAWalletAnswering(t, http.StatusOK, testCase.body)
			client := newTestClient(server)

			_, err := client.Withdraw(context.Background(), givenAWithdraw(t))

			assertErrorIs(t, err, domain.ErrWalletAppliedBalanceUnknown)
		})
	}
}

// The Money parse error must not leak its category: an applied operation is
// never an invalid request.
func TestWithdraw_SuccessWithInvalidBalance_IsNotInvalidInput(t *testing.T) {
	server := givenAWalletAnswering(t, http.StatusOK, `{"balance":"4989.505"}`)
	client := newTestClient(server)

	_, err := client.Withdraw(context.Background(), givenAWithdraw(t))

	assertErrorIsNot(t, err, domain.ErrInvalidInput)
}

func TestWithdraw_InsufficientBalance_IsInsufficientFunds(t *testing.T) {
	server := givenAWalletAnswering(t, http.StatusBadRequest, `{"msg":"Insufficient balance for the user.","code":"InsufficientBalance"}`)
	client := newTestClient(server)

	_, err := client.Withdraw(context.Background(), givenAWithdraw(t))

	assertErrorIs(t, err, domain.ErrInsufficientFunds)
}

func TestWithdraw_DuplicatedTransaction_IsAppliedWithUnknownBalance(t *testing.T) {
	server := givenAWalletAnswering(t, http.StatusConflict, `{"msg":"Transaction already exists.","code":"DuplicatedTransaction"}`)
	client := newTestClient(server)

	_, err := client.Withdraw(context.Background(), givenAWithdraw(t))

	assertErrorIs(t, err, domain.ErrWalletAppliedBalanceUnknown)
}

func TestWithdraw_Rejection_IsRejected(t *testing.T) {
	testCases := []struct {
		name   string
		status int
		body   string
	}{
		{
			name:   "400 invalid request",
			status: http.StatusBadRequest,
			body:   `{"msg":"Invalid request.","code":"InvalidRequest"}`,
		},
		{
			name:   "401 wrong api key",
			status: http.StatusUnauthorized,
			body:   `{"msg":"Authentication error, see logs for more details.","code":"AuthenticatonError"}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := givenAWalletAnswering(t, testCase.status, testCase.body)
			client := newTestClient(server)

			_, err := client.Withdraw(context.Background(), givenAWithdraw(t))

			assertErrorIs(t, err, domain.ErrWalletRejected)
		})
	}
}

func TestWithdraw_ServerError_IsUnknown(t *testing.T) {
	testCases := []struct {
		name   string
		status int
		body   string
	}{
		{
			name:   "500 error after applying",
			status: http.StatusInternalServerError,
			body:   `{"msg":"Internal server response.","code":"666"}`,
		},
		{
			name:   "500 without body",
			status: http.StatusInternalServerError,
			body:   ``,
		},
		{
			name:   "503 with a non JSON body",
			status: http.StatusServiceUnavailable,
			body:   `Service Unavailable`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := givenAWalletAnswering(t, testCase.status, testCase.body)
			client := newTestClient(server)

			_, err := client.Withdraw(context.Background(), givenAWithdraw(t))

			assertErrorIs(t, err, domain.ErrWalletOutcomeUnknown)
		})
	}
}

func TestWithdraw_PostsToTheWithdrawEndpoint(t *testing.T) {
	server, sent := givenAWalletRecordingRequests(t)
	client := newTestClient(server)

	_, err := client.Withdraw(context.Background(), givenAWithdraw(t))

	assertEndpoint(t, sent, err, http.MethodPost, "/api/v1/withdraw")
}

func TestWithdraw_SendsTheExpectedHeaders(t *testing.T) {
	testCases := []struct {
		header string
		want   string
	}{
		{header: "x-api-key", want: testAPIKey},
		{header: "Content-Type", want: "application/json"},
		{header: "X-TRACE-ID", want: "8f14e45f-ceea-4f6a-9b1e-0c7a3e2b1d55"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.header, func(t *testing.T) {
			server, sent := givenAWalletRecordingRequests(t)
			client := newTestClient(server)

			_, err := client.Withdraw(context.Background(), givenAWithdraw(t))

			assertHeader(t, sent, err, testCase.header, testCase.want)
		})
	}
}

// The amount travels as a JSON number with two decimals, and the list payload
// always carries exactly one transaction (docs/wallet-findings.md, Q4 and Q8).
func TestWithdraw_SendsOneTransactionWithTheOperation(t *testing.T) {
	server, sent := givenAWalletRecordingRequests(t)
	client := newTestClient(server)

	_, err := client.Withdraw(context.Background(), givenAWithdraw(t))

	assertBody(t, sent, err,
		`{"userId":34633089486,"currency":"USD","transactions":[{"amount":10.50,"betId":1001,"reference":"8f14e45f-ceea-4f6a-9b1e-0c7a3e2b1d55"}]}`)
}

// A Wallet that does not answer within OperationTimeout may still apply the
// operation, so the outcome is unknown, never a failure.
func TestWithdraw_SlowerThanTimeout_IsUnknown(t *testing.T) {
	server := givenASlowWallet(t, 2*time.Second)
	client := newTestClientWithTimeout(server, 50*time.Millisecond)

	_, err := client.Withdraw(context.Background(), givenAWithdraw(t))

	assertErrorIs(t, err, domain.ErrWalletOutcomeUnknown)
}

// The real Wallet closes the connection without answering when a request takes
// ~5 s, sometimes after applying it (docs/wallet-findings.md, Q6).
func TestWithdraw_DroppedConnection_IsUnknown(t *testing.T) {
	server, _ := givenAWalletDroppingConnections(t)
	client := newTestClient(server)

	_, err := client.Withdraw(context.Background(), givenAWithdraw(t))

	assertErrorIs(t, err, domain.ErrWalletOutcomeUnknown)
}

// Re-sending is the resolution mechanism of Phase 11, done later and on
// purpose; an immediate automatic retry could race the original request.
func TestWithdraw_DroppedConnection_IsNotRetried(t *testing.T) {
	server, requests := givenAWalletDroppingConnections(t)
	client := newTestClient(server)

	client.Withdraw(context.Background(), givenAWithdraw(t))

	assertRequestCount(t, requests, 1)
}

func TestWithdraw_LogsOneLinePerCall(t *testing.T) {
	server := givenAWalletAnswering(t, http.StatusOK, `{"balance":"4989.5"}`)
	logs := &bytes.Buffer{}
	client := newTestClientLoggingTo(server, logs)

	client.Withdraw(context.Background(), givenAWithdraw(t))

	assertLogLineCount(t, logs, 1)
}

func TestWithdraw_LogsTheCallDetails(t *testing.T) {
	testCases := []struct {
		field string
		want  string
	}{
		{field: "msg", want: "wallet call"},
		{field: "operation", want: "withdraw"},
		{field: "user_id", want: "34633089486"},
		{field: "bet_id", want: "1001"},
		{field: "reference", want: "8f14e45f-ceea-4f6a-9b1e-0c7a3e2b1d55"},
		{field: "status", want: "200"},
		{field: "wallet_transaction_id", want: "7"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.field, func(t *testing.T) {
			server := givenAWalletAnswering(t, http.StatusOK, `{"balance":"4989.5","transactions":[{"id":7,"reference":"tx-1"}]}`)
			logs := &bytes.Buffer{}
			client := newTestClientLoggingTo(server, logs)

			client.Withdraw(context.Background(), givenAWithdraw(t))

			assertLogField(t, logs, testCase.field, testCase.want)
		})
	}
}

func TestWithdraw_LogsTheDuration(t *testing.T) {
	server := givenAWalletAnswering(t, http.StatusOK, `{"balance":"4989.5"}`)
	logs := &bytes.Buffer{}
	client := newTestClientLoggingTo(server, logs)

	client.Withdraw(context.Background(), givenAWithdraw(t))

	assertLogFieldPresent(t, logs, "duration_ms")
}

func TestWithdraw_LogsTheWalletCode(t *testing.T) {
	server := givenAWalletAnswering(t, http.StatusBadRequest, `{"msg":"Insufficient balance for the user.","code":"InsufficientBalance"}`)
	logs := &bytes.Buffer{}
	client := newTestClientLoggingTo(server, logs)

	client.Withdraw(context.Background(), givenAWithdraw(t))

	assertLogField(t, logs, "wallet_code", "InsufficientBalance")
}

// The outcome tells at a glance what the use case will do; the level makes
// unknown outcomes (warn) and our own mistakes (error) stand out.
func TestWithdraw_LogsTheOutcomeAndLevel(t *testing.T) {
	testCases := []struct {
		name    string
		status  int
		body    string
		outcome string
		level   string
	}{
		{name: "applied", status: http.StatusOK, body: `{"balance":"4989.5"}`, outcome: "applied", level: "INFO"},
		{name: "applied with unknown balance", status: http.StatusConflict, body: `{"code":"DuplicatedTransaction"}`, outcome: "applied_balance_unknown", level: "WARN"},
		{name: "insufficient funds", status: http.StatusBadRequest, body: `{"code":"InsufficientBalance"}`, outcome: "insufficient_funds", level: "INFO"},
		{name: "rejected", status: http.StatusUnauthorized, body: `{"code":"AuthenticatonError"}`, outcome: "rejected", level: "ERROR"},
		{name: "unknown", status: http.StatusInternalServerError, body: `{"code":"666"}`, outcome: "unknown", level: "WARN"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := givenAWalletAnswering(t, testCase.status, testCase.body)
			logs := &bytes.Buffer{}
			client := newTestClientLoggingTo(server, logs)

			client.Withdraw(context.Background(), givenAWithdraw(t))

			assertLogOutcome(t, logs, testCase.outcome, testCase.level)
		})
	}
}

func TestWithdraw_FailedCall_LogsTheError(t *testing.T) {
	server := givenAWalletAnswering(t, http.StatusInternalServerError, `{"code":"666"}`)
	logs := &bytes.Buffer{}
	client := newTestClientLoggingTo(server, logs)

	client.Withdraw(context.Background(), givenAWithdraw(t))

	assertLogFieldContains(t, logs, "error", `code "666"`)
}

func TestWithdraw_DroppedConnection_LogsStatusZero(t *testing.T) {
	server, _ := givenAWalletDroppingConnections(t)
	logs := &bytes.Buffer{}
	client := newTestClientLoggingTo(server, logs)

	client.Withdraw(context.Background(), givenAWithdraw(t))

	assertLogField(t, logs, "status", "0")
}

// The API key is a secret (AGENTS.md): it must never reach the logs, also
// when the Wallet rejects it.
func TestWithdraw_NeverLogsTheAPIKey(t *testing.T) {
	testCases := []struct {
		name   string
		status int
		body   string
	}{
		{name: "applied", status: http.StatusOK, body: `{"balance":"4989.5"}`},
		{name: "wrong api key", status: http.StatusUnauthorized, body: `{"code":"AuthenticatonError"}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := givenAWalletAnswering(t, testCase.status, testCase.body)
			logs := &bytes.Buffer{}
			client := newTestClientLoggingTo(server, logs)

			client.Withdraw(context.Background(), givenAWithdraw(t))

			assertLogsDoNotContain(t, logs, testAPIKey)
		})
	}
}

// Deposit shares every step with Withdraw (request, classification, timeout,
// logging), so its tests only cover what differs: the endpoint and the name.
func TestDeposit_PostsToTheDepositEndpoint(t *testing.T) {
	server, sent := givenAWalletRecordingRequests(t)
	client := newTestClient(server)

	_, err := client.Deposit(context.Background(), givenADeposit(t))

	assertEndpoint(t, sent, err, http.MethodPost, "/api/v1/deposit")
}

func TestDeposit_Success_ReturnsTheNewBalance(t *testing.T) {
	server := givenAWalletAnswering(t, http.StatusOK, `{"balance":"5014.75","transactions":[{"id":2,"reference":"tx-2"}]}`)
	client := newTestClient(server)

	result, err := client.Deposit(context.Background(), givenADeposit(t))

	assertBalance(t, result, err, "5014.75")
}

func TestDeposit_LogsTheOperationName(t *testing.T) {
	server := givenAWalletAnswering(t, http.StatusOK, `{"balance":"5014.75"}`)
	logs := &bytes.Buffer{}
	client := newTestClientLoggingTo(server, logs)

	client.Deposit(context.Background(), givenADeposit(t))

	assertLogField(t, logs, "operation", "deposit")
}

func TestBalance_Success_ReturnsTheBalanceAndCurrency(t *testing.T) {
	server := givenAWalletAnswering(t, http.StatusOK, `{"balance":"5000","currency":"USD"}`)
	client := newTestClient(server)

	result, err := client.Balance(context.Background(), 34633089486)

	assertWalletBalance(t, result, err, "5000.00", domain.USD)
}

func TestBalance_GetsTheBalanceOfTheUser(t *testing.T) {
	server, sent := givenAWalletRecordingRequests(t)
	client := newTestClient(server)

	_, err := client.Balance(context.Background(), 34633089486)

	assertEndpoint(t, sent, err, http.MethodGet, "/api/v1/balance/34633089486")
}

func TestBalance_SendsTheAPIKey(t *testing.T) {
	server, sent := givenAWalletRecordingRequests(t)
	client := newTestClient(server)

	_, err := client.Balance(context.Background(), 34633089486)

	assertHeader(t, sent, err, "x-api-key", testAPIKey)
}

// A balance we cannot trust must never be shown to the player: it is a Wallet
// failure (502), not a zero balance.
func TestBalance_InvalidResponse_IsWalletFailure(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{name: "unknown user, as the mock answers it", body: `{"balance":"0","currency":""}`},
		{name: "unsupported currency", body: `{"balance":"10.00","currency":"GBP"}`},
		{name: "balance with three decimals", body: `{"balance":"10.005","currency":"USD"}`},
		{name: "negative balance", body: `{"balance":"-1.00","currency":"USD"}`},
		{name: "missing balance", body: `{"currency":"USD"}`},
		{name: "invalid JSON", body: `{"balance":`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := givenAWalletAnswering(t, http.StatusOK, testCase.body)
			client := newTestClient(server)

			_, err := client.Balance(context.Background(), 34633089486)

			assertErrorIs(t, err, domain.ErrWalletFailure)
		})
	}
}

// The Money and Currency errors must not leak their category: a bad Wallet
// answer is never an invalid request from our client.
func TestBalance_InvalidResponse_IsNotInvalidInput(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{name: "invalid amount", body: `{"balance":"10.005","currency":"USD"}`},
		{name: "invalid currency", body: `{"balance":"10.00","currency":"GBP"}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := givenAWalletAnswering(t, http.StatusOK, testCase.body)
			client := newTestClient(server)

			_, err := client.Balance(context.Background(), 34633089486)

			assertErrorIsNot(t, err, domain.ErrInvalidInput)
		})
	}
}

func TestBalance_Rejection_IsWalletFailure(t *testing.T) {
	testCases := []struct {
		name   string
		status int
		body   string
	}{
		{name: "400 invalid request", status: http.StatusBadRequest, body: `{"code":"InvalidRequest"}`},
		{name: "401 wrong api key", status: http.StatusUnauthorized, body: `{"code":"AuthenticatonError"}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := givenAWalletAnswering(t, testCase.status, testCase.body)
			client := newTestClient(server)

			_, err := client.Balance(context.Background(), 34633089486)

			assertErrorIs(t, err, domain.ErrWalletFailure)
		})
	}
}

// A 4xx will not change on a second attempt, so it is not retried.
func TestBalance_Rejection_IsNotRetried(t *testing.T) {
	testCases := []struct {
		name   string
		answer walletAnswer
	}{
		{name: "400 invalid request", answer: walletAnswer{status: http.StatusBadRequest, body: `{"code":"InvalidRequest"}`}},
		{name: "401 wrong api key", answer: walletAnswer{status: http.StatusUnauthorized, body: `{"code":"AuthenticatonError"}`}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server, requests := givenAWalletAnsweringInTurn(t, testCase.answer, testCase.answer)
			client := newTestClient(server)

			client.Balance(context.Background(), 34633089486)

			assertRequestCount(t, requests, 1)
		})
	}
}

// A read is always safe to repeat, and ~9 % of them fail randomly
// (docs/wallet-findings.md, Q6), so a failed read is retried once.
func TestBalance_ServerErrorThenSuccess_ReturnsTheBalance(t *testing.T) {
	server, _ := givenAWalletAnsweringInTurn(t, serverError, balanceOf5000USD)
	client := newTestClient(server)

	result, err := client.Balance(context.Background(), 34633089486)

	assertWalletBalance(t, result, err, "5000.00", domain.USD)
}

func TestBalance_DroppedConnectionThenSuccess_ReturnsTheBalance(t *testing.T) {
	server, _ := givenAWalletAnsweringInTurn(t, droppedConnection, balanceOf5000USD)
	client := newTestClient(server)

	result, err := client.Balance(context.Background(), 34633089486)

	assertWalletBalance(t, result, err, "5000.00", domain.USD)
}

func TestBalance_ServerErrorTwice_IsWalletFailure(t *testing.T) {
	server, _ := givenAWalletAnsweringInTurn(t, serverError, serverError)
	client := newTestClient(server)

	_, err := client.Balance(context.Background(), 34633089486)

	assertErrorIs(t, err, domain.ErrWalletFailure)
}

// One retry only: the wait stays bounded (assessment §9.5).
func TestBalance_KeepsFailing_StopsAfterTwoAttempts(t *testing.T) {
	server, requests := givenAWalletAnsweringInTurn(t, serverError, serverError, serverError)
	client := newTestClient(server)

	client.Balance(context.Background(), 34633089486)

	assertRequestCount(t, requests, 2)
}

// The balance read has its own timeout (findings Q7: up to ~15 s), and running
// out of it is a timeout (504), not a generic Wallet failure (502).
func TestBalance_SlowerThanTimeoutTwice_IsWalletTimeout(t *testing.T) {
	server := givenASlowWallet(t, 2*time.Second)
	client := newTestClientWithTimeout(server, 50*time.Millisecond)

	_, err := client.Balance(context.Background(), 34633089486)

	assertErrorIs(t, err, domain.ErrWalletTimeout)
}

func TestBalance_SlowerThanTimeoutTwice_IsNotWalletFailure(t *testing.T) {
	server := givenASlowWallet(t, 2*time.Second)
	client := newTestClientWithTimeout(server, 50*time.Millisecond)

	_, err := client.Balance(context.Background(), 34633089486)

	assertErrorIsNot(t, err, domain.ErrWalletFailure)
}

// Each attempt has its own timeout: if the first one were not cut, its late
// answer (a different balance) would be returned instead of the second one.
func TestBalance_SlowThenFast_ReturnsTheSecondAnswer(t *testing.T) {
	lateBalance := walletAnswer{status: http.StatusOK, body: `{"balance":"1.00","currency":"USD"}`, delay: 2 * time.Second}
	server, _ := givenAWalletAnsweringInTurn(t, lateBalance, balanceOf5000USD)
	client := newTestClientWithTimeout(server, 50*time.Millisecond)

	result, err := client.Balance(context.Background(), 34633089486)

	assertWalletBalance(t, result, err, "5000.00", domain.USD)
}

// When the caller gives up (e.g. the HTTP request of GET /player is cancelled),
// there is nobody left to answer, so there is no second attempt.
func TestBalance_CallerGivesUp_IsNotRetried(t *testing.T) {
	server, requests := givenAWalletAnsweringInTurn(t, walletAnswer{status: http.StatusOK, body: `{"balance":"5000","currency":"USD"}`, delay: 2 * time.Second})
	client := newTestClient(server)
	ctx := givenACallerGivingUpAfter(t, 50*time.Millisecond)

	client.Balance(ctx, 34633089486)

	assertRequestCount(t, requests, 1)
}

/////////////////////////////////////////////////////////////////////////////////////

// walletAnswer is one scripted answer of the fake Wallet: a status and body,
// or a connection closed without any response. With a delay, the answer is
// sent late, unless the client gives up first.
type walletAnswer struct {
	status int
	body   string
	drop   bool
	delay  time.Duration
}

var (
	balanceOf5000USD  = walletAnswer{status: http.StatusOK, body: `{"balance":"5000","currency":"USD"}`}
	serverError       = walletAnswer{status: http.StatusInternalServerError, body: `{"msg":"Internal server response.","code":"666"}`}
	droppedConnection = walletAnswer{drop: true}
)

// givenAWalletAnsweringInTurn starts a fake Wallet that gives the answers in
// order, one per request, repeating the last one when it runs out. It also
// counts the requests it gets.
func givenAWalletAnsweringInTurn(t *testing.T, answers ...walletAnswer) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	requests := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		turn := int(requests.Add(1)) - 1
		answer := answers[min(turn, len(answers)-1)]
		if !waitOrGiveUp(r, answer.delay) {
			return
		}
		if answer.drop {
			dropConnection(t, w)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(answer.status)
		io.WriteString(w, answer.body)
	}))
	t.Cleanup(server.Close)
	return server, requests
}

// waitOrGiveUp waits for delay and reports true, or reports false as soon as
// the client gives up. The body is read first: only then does the server
// notice that the client has gone away.
func waitOrGiveUp(r *http.Request, delay time.Duration) bool {
	io.Copy(io.Discard, r.Body)
	select {
	case <-time.After(delay):
		return true
	case <-r.Context().Done():
		return false
	}
}

// givenACallerGivingUpAfter returns a context that is cancelled after timeout,
// as when the client of our own API goes away.
func givenACallerGivingUpAfter(t *testing.T, timeout time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx
}

// dropConnection closes the connection without sending a response.
func dropConnection(t *testing.T, w http.ResponseWriter) {
	connection, _, err := http.NewResponseController(w).Hijack()
	if err != nil {
		t.Errorf("hijack connection: %v", err)
		return
	}
	connection.Close()
}

// givenAWalletDroppingConnections starts a fake Wallet that closes every
// connection without sending a response. It also counts the requests it gets.
func givenAWalletDroppingConnections(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	requests := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		dropConnection(t, w)
	}))
	t.Cleanup(server.Close)
	return server, requests
}

// givenASlowWallet starts a fake Wallet that waits for delay before answering
// with a valid balance. It stops waiting if the client gives up, so the test
// does not have to wait for the whole delay when it ends. The body is read
// first: only then does the server notice that the client has gone away.
func givenASlowWallet(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		select {
		case <-time.After(delay):
			io.WriteString(w, `{"balance":"4989.5"}`)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// sentRequest is what the fake Wallet received.
type sentRequest struct {
	method string
	path   string
	header http.Header
	body   string
}

// givenAWalletRecordingRequests starts a fake Wallet that stores the request it
// receives and answers with a valid balance, valid for both operations and
// balance reads. The stored request can be read once the client call has returned.
func givenAWalletRecordingRequests(t *testing.T) (*httptest.Server, *sentRequest) {
	t.Helper()
	sent := &sentRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*sent = sentRequest{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: string(body)}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"balance":"4989.5","currency":"USD"}`)
	}))
	t.Cleanup(server.Close)
	return server, sent
}

// givenAWalletAnswering starts a fake Wallet that answers every request with
// the given status and JSON body. It is stopped when the test ends.
func givenAWalletAnswering(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

// newTestClient returns a Client that calls the fake Wallet and discards its logs.
func newTestClient(server *httptest.Server) *Client {
	return newTestClientWithTimeout(server, testTimeout)
}

// newTestClientWithTimeout is newTestClient with both Wallet timeouts set to timeout.
func newTestClientWithTimeout(server *httptest.Server, timeout time.Duration) *Client {
	config := Config{
		BaseURL:          server.URL,
		APIKey:           testAPIKey,
		OperationTimeout: timeout,
		BalanceTimeout:   timeout,
	}
	return NewClient(config, slog.New(slog.DiscardHandler))
}

func givenAWithdraw(t *testing.T) domain.WalletOperation {
	t.Helper()
	return domain.WalletOperation{
		UserID:    34633089486,
		Currency:  domain.USD,
		Amount:    givenAParsedMoney(t, "10.50"),
		BetID:     1001,
		Reference: "8f14e45f-ceea-4f6a-9b1e-0c7a3e2b1d55",
	}
}

func givenADeposit(t *testing.T) domain.WalletOperation {
	t.Helper()
	return domain.WalletOperation{
		UserID:    34633089486,
		Currency:  domain.USD,
		Amount:    givenAParsedMoney(t, "25.25"),
		BetID:     1001,
		Reference: "c9f0f895-fb98-4b91-8d6e-2f1a7c3b4e60",
	}
}

func givenAParsedMoney(t *testing.T, value string) domain.Money {
	t.Helper()
	money, err := domain.ParseMoney(value)
	if err != nil {
		t.Fatalf("invalid test amount %q: %v", value, err)
	}
	return money
}

func assertErrorIsNot(t *testing.T, err, target error) {
	t.Helper()
	if errors.Is(err, target) {
		t.Errorf("expected %v not to be %q", err, target)
	}
}

func assertEndpoint(t *testing.T, sent *sentRequest, err error, method, path string) {
	t.Helper()
	assertNoError(t, err)
	if sent.method != method || sent.path != path {
		t.Errorf("expected %s %s, got %s %s", method, path, sent.method, sent.path)
	}
}

func assertHeader(t *testing.T, sent *sentRequest, err error, name, want string) {
	t.Helper()
	assertNoError(t, err)
	if result := sent.header.Get(name); result != want {
		t.Errorf("expected header %s = %q, got %q", name, want, result)
	}
}

func assertBody(t *testing.T, sent *sentRequest, err error, want string) {
	t.Helper()
	assertNoError(t, err)
	if sent.body != want {
		t.Errorf("expected body\n  %s\ngot\n  %s", want, sent.body)
	}
}

func assertRequestCount(t *testing.T, requests *atomic.Int32, want int32) {
	t.Helper()
	if result := requests.Load(); result != want {
		t.Errorf("expected %d request(s) to the Wallet, got %d", want, result)
	}
}

// assertWalletBalance checks that the balance read succeeded with the expected
// amount and currency.
func assertWalletBalance(t *testing.T, result domain.WalletBalance, err error, amount string, currency domain.Currency) {
	t.Helper()
	assertNoError(t, err)
	if !result.Amount.Equal(givenAParsedMoney(t, amount)) || result.Currency != currency {
		t.Errorf("expected balance %s %s, got %s %s", amount, currency, result.Amount, result.Currency)
	}
}

// assertBalance checks that the call succeeded and returned the expected balance.
func assertBalance(t *testing.T, result domain.Money, err error, want string) {
	t.Helper()
	assertNoError(t, err)
	if !result.Equal(givenAParsedMoney(t, want)) {
		t.Errorf("expected balance %s, got %s", want, result)
	}
}

// newTestClientLoggingTo is newTestClient writing its logs as JSON lines into logs.
func newTestClientLoggingTo(server *httptest.Server, logs *bytes.Buffer) *Client {
	config := Config{
		BaseURL:          server.URL,
		APIKey:           testAPIKey,
		OperationTimeout: testTimeout,
		BalanceTimeout:   testTimeout,
	}
	return NewClient(config, slog.New(slog.NewJSONHandler(logs, nil)))
}

// readLogLines decodes every JSON log line. Numbers are kept as json.Number so
// that large IDs are compared exactly.
func readLogLines(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(logs.Bytes()))
	decoder.UseNumber()
	for decoder.More() {
		var line map[string]any
		if err := decoder.Decode(&line); err != nil {
			t.Fatalf("invalid log line: %v\n%s", err, logs.String())
		}
		lines = append(lines, line)
	}
	return lines
}

// readTheOnlyLogLine returns the single log line written by the call.
func readTheOnlyLogLine(t *testing.T, logs *bytes.Buffer) map[string]any {
	t.Helper()
	lines := readLogLines(t, logs)
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 log line, got %d:\n%s", len(lines), logs.String())
	}
	return lines[0]
}

func assertLogLineCount(t *testing.T, logs *bytes.Buffer, want int) {
	t.Helper()
	if result := len(readLogLines(t, logs)); result != want {
		t.Errorf("expected %d log line(s), got %d:\n%s", want, result, logs.String())
	}
}

func assertLogField(t *testing.T, logs *bytes.Buffer, field, want string) {
	t.Helper()
	line := readTheOnlyLogLine(t, logs)
	value, found := line[field]
	if !found {
		t.Fatalf("expected log field %q, got %v", field, line)
	}
	if result := fmt.Sprint(value); result != want {
		t.Errorf("expected log field %s = %q, got %q", field, want, result)
	}
}

func assertLogFieldPresent(t *testing.T, logs *bytes.Buffer, field string) {
	t.Helper()
	line := readTheOnlyLogLine(t, logs)
	if _, found := line[field]; !found {
		t.Errorf("expected log field %q, got %v", field, line)
	}
}

func assertLogFieldContains(t *testing.T, logs *bytes.Buffer, field, text string) {
	t.Helper()
	line := readTheOnlyLogLine(t, logs)
	if result := fmt.Sprint(line[field]); !strings.Contains(result, text) {
		t.Errorf("expected log field %s to contain %q, got %q", field, text, result)
	}
}

func assertLogOutcome(t *testing.T, logs *bytes.Buffer, outcome, level string) {
	t.Helper()
	line := readTheOnlyLogLine(t, logs)
	if line["outcome"] != outcome || line["level"] != level {
		t.Errorf("expected outcome %q at level %s, got outcome %v at level %v", outcome, level, line["outcome"], line["level"])
	}
}

func assertLogsDoNotContain(t *testing.T, logs *bytes.Buffer, secret string) {
	t.Helper()
	if strings.Contains(logs.String(), secret) {
		t.Errorf("logs contain a secret:\n%s", logs.String())
	}
}
