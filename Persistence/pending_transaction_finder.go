package persistence

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

var _ domain.PendingTransactionFinder = (*PendingTransactionFinder)(nil)

// PendingTransactionFinder reads PENDING transactions of every player for the
// pending resolver. It uses the pool directly: it runs before any player's
// lock is taken, and it only reads.
type PendingTransactionFinder struct {
	db querier
}

func NewPendingTransactionFinder(pool *pgxpool.Pool) *PendingTransactionFinder {
	return &PendingTransactionFinder{db: pool}
}

// FindPendingBefore uses the partial index transactions_pending (migration
// 00006), which holds only PENDING rows.
func (f *PendingTransactionFinder) FindPendingBefore(ctx context.Context, updatedBefore time.Time, limit int) ([]domain.Transaction, error) {
	query := "SELECT " + transactionColumns + ` FROM transactions
		WHERE status = 'PENDING'
		AND updated_at <= $1
		ORDER BY updated_at, id
		LIMIT $2`

	rows, err := f.db.Query(ctx, query, updatedBefore, limit)
	if err != nil {
		return nil, fmt.Errorf("find pending transactions: %w", err)
	}
	defer rows.Close()

	transactions := []domain.Transaction{}
	for rows.Next() {
		tx, err := scanTransaction(rows)
		if err != nil {
			return nil, fmt.Errorf("find pending transactions: %w", err)
		}
		transactions = append(transactions, tx)
	}
	// rows.Next stops both at the end and on an error; Err tells them apart.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("find pending transactions: %w", err)
	}
	return transactions, nil
}
