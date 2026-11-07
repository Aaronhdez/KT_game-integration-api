package wallet

import (
	"encoding/json"
)

type operationRequest struct {
	UserID       int64                  `json:"userId"`
	Currency     string                 `json:"currency"`
	Transactions []operationTransaction `json:"transactions"`
}

type operationTransaction struct {
	Amount    json.Number `json:"amount"`
	BetID     int64       `json:"betId"`
	Reference string      `json:"reference"`
}

type operationResponse struct {
	Balance      json.Number         `json:"balance"`
	Transactions []walletTransaction `json:"transactions"`
}

type walletTransaction struct {
	ID        int64  `json:"id"`
	Reference string `json:"reference"`
}

type errorResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
}

type balanceResponse struct {
	Balance  json.Number `json:"balance"`
	Currency string      `json:"currency"`
}
