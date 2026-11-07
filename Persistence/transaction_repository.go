package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

const transactionColumns = `id::text, type, status, wallet_user_id, bet_id,
	provider_transaction_id, cancelled_transaction_id::text, amount::text, currency,
	old_balance::text, new_balance::text, created_at, updated_at`

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

func (r *transactionRepository) FindByID(ctx context.Context, userID domain.UserID, id domain.TransactionID) (domain.Transaction, error) {
	query := "SELECT " + transactionColumns + " FROM transactions WHERE id = $1 AND wallet_user_id = $2"

	tx, err := scanTransaction(r.db.QueryRow(ctx, query, string(id), int64(userID)))

	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Transaction{}, domain.ErrTransactionNotFound
	}
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("find transaction: %w", err)
	}
	return tx, nil
}

func (r *transactionRepository) FindByProviderTransactionID(ctx context.Context, userID domain.UserID, providerTransactionID domain.ProviderTransactionID) (domain.Transaction, error) {
	query := "SELECT " + transactionColumns + ` FROM transactions
		WHERE provider_transaction_id = $1
		AND wallet_user_id = $2
		AND type IN ('WITHDRAW', 'DEPOSIT')`

	tx, err := scanTransaction(r.db.QueryRow(ctx, query, string(providerTransactionID), int64(userID)))

	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Transaction{}, domain.ErrTransactionNotFound
	}
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("find transaction by provider transaction id: %w", err)
	}
	return tx, nil
}

func scanTransaction(row pgx.Row) (domain.Transaction, error) {
	var (
		id                     string
		txType                 string
		status                 string
		walletUserID           int64
		betID                  int64
		providerTransactionID  string
		cancelledTransactionID *string
		amountText             string
		currencyCode           string
		oldBalanceText         string
		newBalanceText         *string
		createdAt              time.Time
		updatedAt              time.Time
	)
	if err := row.Scan(&id, &txType, &status, &walletUserID, &betID,
		&providerTransactionID, &cancelledTransactionID, &amountText, &currencyCode,
		&oldBalanceText, &newBalanceText, &createdAt, &updatedAt); err != nil {
		return domain.Transaction{}, err
	}

	currency, err := domain.ParseCurrency(currencyCode)
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("transaction %s: %w", id, err)
	}

	amount, err := domain.ParseMoney(amountText)
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("transaction %s: %w", id, err)
	}

	oldBalance, err := domain.ParseMoney(oldBalanceText)
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("transaction %s: %w", id, err)
	}

	tx := domain.Transaction{
		ID:                    domain.TransactionID(id),
		Type:                  domain.TransactionType(txType),
		Status:                domain.TransactionStatus(status),
		UserID:                domain.UserID(walletUserID),
		BetID:                 domain.BetID(betID),
		ProviderTransactionID: domain.ProviderTransactionID(providerTransactionID),
		Amount:                amount,
		Currency:              currency,
		OldBalance:            oldBalance,
		CreatedAt:             createdAt,
		UpdatedAt:             updatedAt,
	}

	if cancelledTransactionID != nil {
		tx.CancelledTransactionID = domain.TransactionID(*cancelledTransactionID)
	}

	if newBalanceText != nil {
		newBalance, err := domain.ParseMoney(*newBalanceText)
		if err != nil {
			return domain.Transaction{}, fmt.Errorf("transaction %s: %w", id, err)
		}
		tx.NewBalance = &newBalance
	}

	return tx, nil
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
