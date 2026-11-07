package wallet

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

/////////////////////////////////////////////////////////////////////////////////////

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
// receives and answers with a valid balance. The stored request can be read
// once the client call has returned.
func givenAWalletRecordingRequests(t *testing.T) (*httptest.Server, *sentRequest) {
	t.Helper()
	sent := &sentRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*sent = sentRequest{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: string(body)}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"balance":"4989.5"}`)
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

// assertBalance checks that the call succeeded and returned the expected balance.
func assertBalance(t *testing.T, result domain.Money, err error, want string) {
	t.Helper()
	assertNoError(t, err)
	if !result.Equal(givenAParsedMoney(t, want)) {
		t.Errorf("expected balance %s, got %s", want, result)
	}
}
