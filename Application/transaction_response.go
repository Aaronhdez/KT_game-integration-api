package application

import (
	"net/http"

	actions "github.com/aaronhdez/kt_game-integration-api/Actions"
	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// transactionResponse is the body of Withdraw, Deposit and Cancel (§7.4), with
// the fields in the order of the §8.3 example. NewBalance and BetStatus are
// pointers so that they are written as null while they do not exist.
type transactionResponse struct {
	TransactionID         domain.TransactionID         `json:"transactionId"`
	ProviderTransactionID domain.ProviderTransactionID `json:"providerTransactionId"`
	OldBalance            domain.Money                 `json:"oldBalance"`
	NewBalance            *domain.Money                `json:"newBalance"`
	Status                domain.TransactionStatus     `json:"status"`
	BetStatus             *domain.BetStatus            `json:"betStatus"`
}

// writeTransactionResult writes the result of a money operation with the
// status code of §7.2:
//   - 202 while the transaction is PENDING (new or replayed): outcome unknown;
//   - 200 for a replay: the stored result, in its current state;
//   - 201 when it was processed for the first time and is COMPLETED.
//
// The body carries balances, so it must not be cached.
func writeTransactionResult(w http.ResponseWriter, result actions.TransactionResult) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, transactionStatusCode(result), transactionResponse{
		TransactionID:         result.Transaction.ID,
		ProviderTransactionID: result.Transaction.ProviderTransactionID,
		OldBalance:            result.Transaction.OldBalance,
		NewBalance:            result.Transaction.NewBalance,
		Status:                result.Transaction.Status,
		BetStatus:             result.BetStatus,
	})
}

func transactionStatusCode(result actions.TransactionResult) int {
	switch {
	case result.Transaction.Status == domain.TransactionPending:
		return http.StatusAccepted
	case result.Replayed:
		return http.StatusOK
	default:
		return http.StatusCreated
	}
}
