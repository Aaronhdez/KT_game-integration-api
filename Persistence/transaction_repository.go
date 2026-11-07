package persistence

import (
	"context"
	"fmt"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// transactionRepository stores transactions in PostgreSQL. Its unique indexes
// enforce idempotency, a single active settlement per bet and a single cancel
// per transaction.
type transactionRepository struct {
	db querier
}

func newTransactionRepository(db querier) *transactionRepository {
	return &transactionRepository{db: db}
}

// Insert stores a new transaction. A duplicate rejected by a business unique
// index is returned as its domain error (see mapUniqueViolation).
func (r *transactionRepository) Insert(ctx context.Context, tx domain.Transaction) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO transactions (id, type, status, wallet_user_id, bet_id,
			provider_transaction_id, cancelled_transaction_id, amount, currency,
			old_balance, new_balance, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		string(tx.ID),
		string(tx.Type),
		string(tx.Status),
		int64(tx.UserID),
		int64(tx.BetID),
		string(tx.ProviderTransactionID),
		nullableTransactionID(tx.CancelledTransactionID),
		tx.Amount.String(),
		string(tx.Currency),
		tx.OldBalance.String(),
		nullableMoney(tx.NewBalance),
		tx.CreatedAt,
		tx.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert transaction %s: %w", tx.ID, mapUniqueViolation(err))
	}
	return nil
}

// nullableTransactionID returns nil (SQL NULL) for an empty ID: only a CANCEL
// has a cancelled transaction.
func nullableTransactionID(id domain.TransactionID) any {
	if id == "" {
		return nil
	}
	return string(id)
}

// nullableMoney returns nil (SQL NULL) for a missing amount: NewBalance is
// unknown until the transaction completes.
func nullableMoney(amount *domain.Money) any {
	if amount == nil {
		return nil
	}
	return amount.String()
}
