package wallet

import (
	"context"
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

/////////////////////////////////////////////////////////////////////////////////////

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
	config := Config{
		BaseURL:          server.URL,
		APIKey:           testAPIKey,
		OperationTimeout: testTimeout,
		BalanceTimeout:   testTimeout,
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

// assertBalance checks that the call succeeded and returned the expected balance.
func assertBalance(t *testing.T, result domain.Money, err error, want string) {
	t.Helper()
	assertNoError(t, err)
	if !result.Equal(givenAParsedMoney(t, want)) {
		t.Errorf("expected balance %s, got %s", want, result)
	}
}
