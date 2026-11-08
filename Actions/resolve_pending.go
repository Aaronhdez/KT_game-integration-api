package actions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// resolveBatchSize bounds how many PENDING transactions one run re-sends.
// Each one takes at most about the Wallet's operation timeout plus the
// database writes, so a run stays bounded; the rest wait for the next run.
const resolveBatchSize = 100

// ResolveSummary counts what one run of the resolver did, for its log line.
// A transaction is counted by its status after the run.
type ResolveSummary struct {
	Checked      int // PENDING transactions found due
	Completed    int // now COMPLETED (or already resolved, then cancelled)
	Failed       int // now FAILED: the Wallet did not apply it
	StillPending int // still unknown, or no longer due when locked
	Errors       int // could not be re-sent or stored; logged, retried next run
}

// ResolvePendingCommandHandler resolves the PENDING transactions whose
// provider does not retry (§9.4): it re-sends every transaction that is due,
// with the same rules as a provider's retry. It is run periodically by
// Startup.
type ResolvePendingCommandHandler struct {
	finder   domain.PendingTransactionFinder
	locker   domain.PlayerLocker
	withdraw *WithdrawCommandHandler
	deposit  *DepositCommandHandler
	cancel   *CancelCommandHandler
	clock    domain.Clock
	logger   *slog.Logger
}

// NewResolvePendingCommandHandler creates the resolver on top of the three
// money operation handlers, which know how to re-send their own transactions.
func NewResolvePendingCommandHandler(
	finder domain.PendingTransactionFinder,
	locker domain.PlayerLocker,
	withdraw *WithdrawCommandHandler,
	deposit *DepositCommandHandler,
	cancel *CancelCommandHandler,
	clock domain.Clock,
	logger *slog.Logger,
) *ResolvePendingCommandHandler {
	return &ResolvePendingCommandHandler{
		finder:   finder,
		locker:   locker,
		withdraw: withdraw,
		deposit:  deposit,
		cancel:   cancel,
		clock:    clock,
		logger:   logger,
	}
}

// Execute re-sends the PENDING transactions whose last attempt started at
// least resendDelay ago, oldest first, at most resolveBatchSize of them. Each
// one is re-sent under its player's lock, so it never overlaps an operation
// or a retry of the same player. An error on one transaction is logged and
// does not stop the others. It stops early, with the context's error, when
// ctx is done (e.g. on shutdown).
func (handler *ResolvePendingCommandHandler) Execute(ctx context.Context) (ResolveSummary, error) {
	var summary ResolveSummary
	pending, err := handler.finder.FindPendingBefore(ctx, handler.clock.Now().Add(-resendDelay), resolveBatchSize)
	if err != nil {
		return summary, fmt.Errorf("resolve pending: %w", err)
	}

	for _, tx := range pending {
		if err := ctx.Err(); err != nil {
			return summary, fmt.Errorf("resolve pending: %w", err)
		}
		summary.Checked++

		result, err := handler.resolve(ctx, tx)
		switch {
		case errors.Is(err, domain.ErrInsufficientFunds), errors.Is(err, domain.ErrWalletRejected):
			// The Wallet's definitive "not applied": the transaction is stored FAILED.
			summary.Failed++
		case err != nil:
			summary.Errors++
			handler.logger.Error("resolve pending: transaction not resolved",
				"transaction_id", tx.ID, "type", tx.Type, "user_id", tx.UserID, "error", err.Error())
		case result.Transaction.Status == domain.TransactionPending:
			summary.StillPending++
		case result.Transaction.Status == domain.TransactionFailed:
			summary.Failed++
		default:
			summary.Completed++
		}
	}
	return summary, nil
}

// resolve re-sends one transaction under its player's lock, with the handler
// of its type.
func (handler *ResolvePendingCommandHandler) resolve(ctx context.Context, tx domain.Transaction) (TransactionResult, error) {
	var result TransactionResult
	err := handler.locker.WithPlayerLock(ctx, tx.UserID, func(ctx context.Context, uow domain.UnitOfWork) error {
		var err error
		switch tx.Type {
		case domain.TransactionWithdraw:
			result, err = handler.withdraw.resendPending(ctx, uow, tx)
		case domain.TransactionDeposit:
			result, err = handler.deposit.resendPending(ctx, uow, tx)
		case domain.TransactionCancel:
			result, err = handler.cancel.resendPending(ctx, uow, tx)
		default:
			err = fmt.Errorf("unknown transaction type %q", tx.Type)
		}
		return err
	})
	return result, err
}
