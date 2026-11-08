package application

import (
	"errors"
	"log/slog"
	"net/http"

	actions "github.com/aaronhdez/kt_game-integration-api/Actions"
)

// cancelRequest is the body of POST /api/v1/cancel: the provider ID
// of the withdraw or deposit to cancel. The player is never taken from the
// body: it comes from the token.
type cancelRequest struct {
	ProviderTransactionID string `json:"providerTransactionId"`
}

// CancelController handles POST /api/v1/cancel (revert a withdraw or deposit).
type CancelController struct {
	cancel *actions.CancelCommandHandler
	logger *slog.Logger
}

// NewCancelController creates a CancelController with its use case and logger.
func NewCancelController(cancel *actions.CancelCommandHandler, logger *slog.Logger) *CancelController {
	return &CancelController{
		cancel: cancel,
		logger: logger,
	}
}

// ServeHTTP decodes the request, runs the cancel for the token's player and
// writes the response (201, 200 or 202) or the error.
func (controller *CancelController) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeDomainError(w, controller.logger, errors.New("cancel controller: no user in request context"))
		return
	}

	var request cancelRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDomainError(w, controller.logger, err)
		return
	}

	result, err := controller.cancel.Execute(r.Context(), actions.CancelCommand{
		UserID:                userID,
		ProviderTransactionID: request.ProviderTransactionID,
	})
	if err != nil {
		writeDomainError(w, controller.logger, err)
		return
	}

	writeTransactionResult(w, result)
}
