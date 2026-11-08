package application

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	actions "github.com/aaronhdez/kt_game-integration-api/Actions"
	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// depositRequest is the body of POST /api/v1/deposit. Amount is a
// pointer so that a missing amount can be told apart from 0, which settles the
// bet as LOST. The player is never taken from the body: it comes from the token.
type depositRequest struct {
	Currency                       string        `json:"currency"`
	Amount                         *domain.Money `json:"amount"`
	ProviderTransactionID          string        `json:"providerTransactionId"`
	ProviderWithdrawnTransactionID string        `json:"providerWithdrawnTransactionId"`
}

// DepositController handles POST /api/v1/deposit (bet settlement).
type DepositController struct {
	deposit *actions.DepositCommandHandler
	logger  *slog.Logger
}

// NewDepositController creates a DepositController with its use case and logger.
func NewDepositController(deposit *actions.DepositCommandHandler, logger *slog.Logger) *DepositController {
	return &DepositController{
		deposit: deposit,
		logger:  logger,
	}
}

// ServeHTTP decodes the request, runs the deposit for the token's player and
// writes the response (201, 200 or 202) or the error.
func (controller *DepositController) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeDomainError(w, controller.logger, errors.New("deposit controller: no user in request context"))
		return
	}

	var request depositRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDomainError(w, controller.logger, err)
		return
	}
	if request.Amount == nil {
		writeDomainError(w, controller.logger, fmt.Errorf("%w: amount is required", domain.ErrInvalidAmount))
		return
	}

	result, err := controller.deposit.Execute(r.Context(), actions.DepositCommand{
		UserID:                         userID,
		ProviderTransactionID:          request.ProviderTransactionID,
		ProviderWithdrawnTransactionID: request.ProviderWithdrawnTransactionID,
		Amount:                         *request.Amount,
		Currency:                       request.Currency,
	})
	if err != nil {
		writeDomainError(w, controller.logger, err)
		return
	}

	writeTransactionResult(w, result)
}
