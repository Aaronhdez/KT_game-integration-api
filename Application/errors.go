package application

import (
	"encoding/json"
	"net/http"
)

const (
	CodeInvalidRequest    = "INVALID_REQUEST"
	CodeUnauthorized      = "UNAUTHORIZED"
	CodeNotFound          = "NOT_FOUND"
	CodeConflict          = "CONFLICT"
	CodeInsufficientFunds = "INSUFFICIENT_FUNDS"
	CodeWalletError       = "WALLET_ERROR"
	CodeWalletTimeout     = "WALLET_TIMEOUT"
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
