package application

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

const (
	CodeInvalidRequest    = "INVALID_REQUEST"
	CodeUnauthorized      = "UNAUTHORIZED"
	CodeNotFound          = "NOT_FOUND"
	CodeConflict          = "CONFLICT"
	CodeInsufficientFunds = "INSUFFICIENT_FUNDS"
	CodeWalletError       = "WALLET_ERROR"
	CodeWalletTimeout     = "WALLET_TIMEOUT"
	CodeInternal          = "INTERNAL_ERROR"
)

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteError writes an error response with the assessment's JSON body {code, message}.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorResponse{
		Code:    code,
		Message: message,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeDomainError(w http.ResponseWriter, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		WriteError(w, http.StatusBadRequest, CodeInvalidRequest, err.Error())
	case errors.Is(err, domain.ErrUnauthorized):
		WriteError(w, http.StatusUnauthorized, CodeUnauthorized, "invalid credentials or token")
	case errors.Is(err, domain.ErrNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "resource not found")
	case errors.Is(err, domain.ErrConflict):
		WriteError(w, http.StatusConflict, CodeConflict, err.Error())
	case errors.Is(err, domain.ErrInsufficientFunds):
		WriteError(w, http.StatusUnprocessableEntity, CodeInsufficientFunds, "insufficient funds")
	case errors.Is(err, domain.ErrWalletFailure):
		WriteError(w, http.StatusBadGateway, CodeWalletError, "wallet error")
	case errors.Is(err, domain.ErrWalletTimeout):
		WriteError(w, http.StatusGatewayTimeout, CodeWalletTimeout, "wallet did not answer in time")
	default:
		WriteError(w, http.StatusInternalServerError, CodeInternal, "internal error")
		logger.Error("unexpected error", "error", err)
	}
}
