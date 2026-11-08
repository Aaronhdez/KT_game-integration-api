package actions

import domain "github.com/aaronhdez/kt_game-integration-api/Domain"

// TransactionResult represents the result of a transaction, including its status and whether it was replayed.
// Shared by Withdraw, Deposit and Cancel transactions.
type TransactionResult struct {
	Transaction domain.Transaction
	BetStatus   *domain.BetStatus
	Replayed    bool
}
