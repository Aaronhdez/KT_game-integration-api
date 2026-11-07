package application

import (
	"errors"
	"log/slog"
	"net/http"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
	queries "github.com/aaronhdez/kt_game-integration-api/Queries"
)

// playerResponse represents the JSON response structure for player information.
type playerResponse struct {
	UserID   domain.UserID   `json:"userId"`
	Balance  domain.Money    `json:"balance"`
	Currency domain.Currency `json:"currency"`
}

// PlayerController handles HTTP requests related to player information.
type PlayerController struct {
	getPlayer *queries.GetPlayerQueryHandler
	logger    *slog.Logger
}

// NewPlayerController creates a new instance of PlayerController with the provided query handler and logger.
func NewPlayerController(getPlayerQueryHandler *queries.GetPlayerQueryHandler, logger *slog.Logger) *PlayerController {
	return &PlayerController{
		getPlayer: getPlayerQueryHandler,
		logger:    logger,
	}
}

// ServeHTTP handles HTTP requests for the PlayerController.
func (controller *PlayerController) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeDomainError(w, controller.logger, errors.New("player controller: no user in request context"))
		return
	}

	query := queries.GetPlayerQuery{
		UserID: userID,
	}
	result, err := controller.getPlayer.Execute(r.Context(), query)
	if err != nil {
		writeDomainError(w, controller.logger, err)
		return
	}

	response := playerResponse{
		UserID:   result.UserID,
		Balance:  result.Balance,
		Currency: result.Currency,
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, response)
}
