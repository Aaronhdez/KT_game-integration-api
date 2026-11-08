package application

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	actions "github.com/aaronhdez/kt_game-integration-api/Actions"
	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// withdrawRequest is the body of POST /api/v1/withdraw (§8.3). Amount is a
// pointer so that a missing amount can be told apart from 0. The player is
// never taken from the body: it comes from the token.
type withdrawRequest struct {
	Currency              string        `json:"currency"`
	Amount                *domain.Money `json:"amount"`
	ProviderTransactionID string        `json:"providerTransactionId"`
}

// WithdrawController handles POST /api/v1/withdraw (bet placement).
type WithdrawController struct {
	withdraw *actions.WithdrawCommandHandler
	logger   *slog.Logger
}

// NewWithdrawController creates a WithdrawController with its use case and logger.
func NewWithdrawController(withdraw *actions.WithdrawCommandHandler, logger *slog.Logger) *WithdrawController {
	return &WithdrawController{
		withdraw: withdraw,
		logger:   logger,
	}
}

// ServeHTTP decodes the request, runs the withdraw for the token's player and
// writes the §7.4 response (201, 200 or 202) or the error.
func (controller *WithdrawController) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeDomainError(w, controller.logger, errors.New("withdraw controller: no user in request context"))
		return
	}

	var request withdrawRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDomainError(w, controller.logger, err)
		return
	}
	if request.Amount == nil {
		writeDomainError(w, controller.logger, fmt.Errorf("%w: amount is required", domain.ErrInvalidAmount))
		return
	}

	result, err := controller.withdraw.Execute(r.Context(), actions.WithdrawCommand{
		UserID:                userID,
		ProviderTransactionID: request.ProviderTransactionID,
		Amount:                *request.Amount,
		Currency:              request.Currency,
	})
	if err != nil {
		writeDomainError(w, controller.logger, err)
		return
	}

	writeTransactionResult(w, result)
}
