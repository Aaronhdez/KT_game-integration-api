package wallet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

const (
	withdrawPath        = "/api/v1/withdraw"
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
	return client.operate(ctx, withdrawPath, operation)
}

func (client *Client) operate(ctx context.Context, path string, operation domain.WalletOperation) (domain.Money, error) {
	result := client.sendOperation(ctx, path, operation)
	return result.balance, result.err
}

func (client *Client) sendOperation(ctx context.Context, path string, operation domain.WalletOperation) operationResult {
	body, err := buildRequestBody(operation)
	if err != nil {
		return operationResult{err: fmt.Errorf("build wallet request: %v: %w", err, domain.ErrWalletRejected)}
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, client.config.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return operationResult{err: fmt.Errorf("build wallet request: %v: %w", err, domain.ErrWalletRejected)}
	}

	setHeaders(httpRequest, operation, client)

	response, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return operationResult{err: fmt.Errorf("send wallet request: %w: %w", err, domain.ErrWalletOutcomeUnknown)}
	}
	defer response.Body.Close()

	status := response.StatusCode
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBodySize))
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		return decodeSuccess(responseBody)
	}

	return responseAsError(responseBody, status)
}

// decodeSuccess reads the new balance from a 2xx response. The operation was
// applied whatever the body contains, so any problem with it means "applied,
// balance unknown", never a failure.
func decodeSuccess(body []byte) operationResult {
	var walletResponse operationResponse
	if err := json.Unmarshal(body, &walletResponse); err != nil {
		return operationResult{
			err: fmt.Errorf("decode wallet response: %v: %w", err, domain.ErrWalletAppliedBalanceUnknown),
		}
	}

	// %v, not %w: the Money error is an invalid input, which this is not.
	balance, err := domain.ParseMoney(walletResponse.Balance.String())
	if err != nil {
		return operationResult{
			err: fmt.Errorf("read wallet balance: %v: %w", err, domain.ErrWalletAppliedBalanceUnknown),
		}
	}

	return operationResult{balance: balance}
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

func setHeaders(httpRequest *http.Request, operation domain.WalletOperation, client *Client) {
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("X-Trace-ID", string(operation.Reference))
	httpRequest.Header.Set("X-Api-Key", client.config.APIKey)
}

// responseAsError classifies a non-2xx answer by its status and the Wallet's
// error code. Without a readable error body the code stays "" and only the
// status counts.
func responseAsError(responseBody []byte, status int) operationResult {
	var errorBody errorResponse
	_ = json.Unmarshal(responseBody, &errorBody)
	return operationResult{err: classifyOperation(status, errorBody.Code)}
}

type operationResult struct {
	balance domain.Money
	err     error
}
