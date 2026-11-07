package persistence

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// SQLSTATE code PostgreSQL returns when a row breaks a unique index.
const uniqueViolation = "23505"

const (
	idempotencyKeyIndex   = "transactions_idempotency_key"
	activeSettlementIndex = "transactions_active_settlement"
	singleCancelIndex     = "transactions_single_cancel"
)

func mapUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolation {
		return err
	}

	switch pgErr.ConstraintName {
	case idempotencyKeyIndex:
		return domain.ErrTransactionExists
	case activeSettlementIndex:
		return domain.ErrBetAlreadySettled
	case singleCancelIndex:
		return domain.ErrAlreadyCancelled
	default:
		return err
	}
}
