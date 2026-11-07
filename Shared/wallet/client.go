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
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

const (
	withdrawPath        = "/api/v1/withdraw"
	depositPath         = "/api/v1/deposit"
	maxResponseBodySize = 1 << 20
)

// Client represents a client for interacting with the wallet service.
type Client struct {
	config     Config
	httpClient *http.Client
	logger     *slog.Logger
}

// NewClient creates a new instance of Client with the provided configuration and logger.
func NewClient(config Config, logger *slog.Logger) *Client {
	return &Client{
		config:     config,
		httpClient: &http.Client{},
		logger:     logger,
	}
}

// Withdraw takes money from the player's wallet.
// Its result is classified as domain.Money, and any error indicates the outcome of the operation.
func (client *Client) Withdraw(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	return client.operate(ctx, withdrawPath, "withdraw", operation)
}

// Deposit adds money to the player's wallet. Its result is classified as
// domain.WalletClient describes.
func (client *Client) Deposit(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	return client.operate(ctx, depositPath, "deposit", operation)
}

func (client *Client) operate(ctx context.Context, path string, name string, operation domain.WalletOperation) (domain.Money, error) {
	start := time.Now()
	result := client.sendOperation(ctx, path, operation)
	logOperation(client.logger, name, operation, result, time.Since(start))
	return result.balance, result.err
}

func (client *Client) sendOperation(ctx context.Context, path string, operation domain.WalletOperation) operationResult {
	ctx, cancel := context.WithTimeout(ctx, client.config.OperationTimeout)
	defer cancel()

	request, err := client.newRequest(ctx, path, operation)
	if err != nil {
		return operationResult{err: fmt.Errorf("build wallet request: %v: %w", err, domain.ErrWalletRejected)}
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return operationResult{err: fmt.Errorf("send wallet request: %w: %w", err, domain.ErrWalletOutcomeUnknown)}
	}
	defer response.Body.Close()

	// The status is already known, so a body that cannot be read is treated
	// as unreadable content, never as an unknown outcome.
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBodySize))
	if isSuccess(response.StatusCode) {
		return decodeSuccess(response.StatusCode, body)
	}
	return decodeFailure(response.StatusCode, body)
}

// newRequest builds the POST for an operation: body, URL and headers. An error
// means the request was never sent.
func (client *Client) newRequest(ctx context.Context, path string, operation domain.WalletOperation) (*http.Request, error) {
	body, err := buildRequestBody(operation)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.config.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	client.setHeaders(request, operation.Reference)
	return request, nil
}

// buildRequestBody encodes the operation as the Wallet's list payload, always
// with exactly one transaction.
func buildRequestBody(operation domain.WalletOperation) ([]byte, error) {
	request := operationRequest{
		UserID:   int64(operation.UserID),
		Currency: string(operation.Currency),
		Transactions: []operationTransaction{
			{
				Amount:    json.Number(operation.Amount.String()),
				BetID:     int64(operation.BetID),
				Reference: string(operation.Reference),
			},
		},
	}
	return json.Marshal(request)
}

// setHeaders authenticates the request and tags it with our transaction ID,
// which the Wallet logs as its trace ID.
func (client *Client) setHeaders(request *http.Request, reference domain.TransactionID) {
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Trace-ID", string(reference))
	request.Header.Set("X-Api-Key", client.config.APIKey)
}

// decodeSuccess reads the new balance from a 2xx response. The operation was
// applied whatever the body contains, so any problem with it means "applied,
// balance unknown", never a failure.
func decodeSuccess(status int, body []byte) operationResult {
	var walletResponse operationResponse
	if err := json.Unmarshal(body, &walletResponse); err != nil {
		return operationResult{
			err:    fmt.Errorf("decode wallet response: %v: %w", err, domain.ErrWalletAppliedBalanceUnknown),
			status: status,
		}
	}

	var walletTransactionID int64
	if len(walletResponse.Transactions) > 0 {
		walletTransactionID = walletResponse.Transactions[0].ID
	}

	// %v, not %w: the Money error is an invalid input, which this is not.
	balance, err := domain.ParseMoney(walletResponse.Balance.String())
	if err != nil {
		return operationResult{
			err:                 fmt.Errorf("read wallet balance: %v: %w", err, domain.ErrWalletAppliedBalanceUnknown),
			status:              status,
			walletTransactionID: walletTransactionID,
		}
	}

	return operationResult{balance: balance, status: status, walletTransactionID: walletTransactionID}
}

// decodeFailure classifies a non-2xx answer by its status and the Wallet's
// error code. Without a readable error body the code stays "" and only the
// status counts.
func decodeFailure(status int, body []byte) operationResult {
	var errorBody errorResponse
	_ = json.Unmarshal(body, &errorBody)
	return operationResult{
		err:        classifyOperation(status, errorBody.Code),
		status:     status,
		walletCode: errorBody.Code,
	}
}

// logOperation writes one line per Wallet call. It never logs the API key,
// the request headers or the bodies.
func logOperation(logger *slog.Logger, name string, operation domain.WalletOperation, result operationResult, duration time.Duration) {
	outcome, level := describeOutcome(result.err)
	attrs := []any{
		"operation", name,
		"user_id", int64(operation.UserID),
		"bet_id", int64(operation.BetID),
		"reference", string(operation.Reference),
		"status", result.status,
		"wallet_code", result.walletCode,
		"wallet_transaction_id", result.walletTransactionID,
		"outcome", outcome,
		"duration_ms", duration.Milliseconds(),
	}
	if result.err != nil {
		attrs = append(attrs, "error", result.err.Error())
	}
	logger.Log(context.Background(), level, "wallet call", attrs...)
}

// describeOutcome names the classification of a Wallet call for the logs and
// picks its level: unknown outcomes are warnings, and rejections are errors
// because they mean a bug or a wrong WALLET_API_KEY on our side.
// ErrWalletRejected wraps ErrWalletFailure, so the order of the cases matters.
func describeOutcome(err error) (string, slog.Level) {
	switch {
	case err == nil:
		return "applied", slog.LevelInfo
	case errors.Is(err, domain.ErrWalletAppliedBalanceUnknown):
		return "applied_balance_unknown", slog.LevelWarn
	case errors.Is(err, domain.ErrInsufficientFunds):
		return "insufficient_funds", slog.LevelInfo
	case errors.Is(err, domain.ErrWalletRejected):
		return "rejected", slog.LevelError
	default:
		return "unknown", slog.LevelWarn
	}
}

type operationResult struct {
	balance             domain.Money
	err                 error
	status              int
	walletCode          string
	walletTransactionID int64
}
