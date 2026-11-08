package actions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// outcomeWriteTimeout bounds the database writes made after the Wallet call,
// which no longer depend on the request's context.
const outcomeWriteTimeout = 5 * time.Second

// operation holds what the money operations (Withdraw, Deposit, Cancel)
// share: their dependencies and the steps that are the same for the three.
// Each command handler embeds it, so its fields and methods are used as if
// they were the handler's own (h.wallet, h.fail…).
type operation struct {
	name             string // "withdraw", "deposit" or "cancel": log message and error prefix
	playerRepository domain.PlayerRepository
	locker           domain.PlayerLocker
	wallet           domain.WalletClient
	ids              domain.IDGenerator
	clock            domain.Clock
	logger           *slog.Logger
}

// readOldBalance reads the player's balance from the Wallet before an
// operation. Any failure, a timeout included, is ErrWalletFailure:
// no operation has been sent yet, so we know nothing was applied.
func (o operation) readOldBalance(ctx context.Context, player domain.Player) (domain.Money, error) {
	balance, err := o.wallet.Balance(ctx, player.UserID)
	if err != nil {
		return domain.Money{}, fmt.Errorf("read old balance: %v: %w", err, domain.ErrWalletFailure)
	}
	if balance.Currency != player.Currency {
		return domain.Money{}, fmt.Errorf("wallet currency %s, account currency %s: %w", balance.Currency, player.Currency, domain.ErrWalletFailure)
	}
	return balance.Amount, nil
}

// outcomeContext is the context for the database writes made after the
// Wallet call. The Wallet may have moved the money even if the caller has
// gone away meanwhile, so it is not cancelled with the request; it has its
// own timeout instead. The caller must call the returned cancel function.
func (o operation) outcomeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), outcomeWriteTimeout)
}

// fail stores the transaction as FAILED (the Wallet did not apply it) and
// returns the Wallet's error, which the API maps to 422 or 502. Nothing else
// changes. betStatus is what leavePending reports if the write fails.
func (o operation) fail(ctx context.Context, uow domain.UnitOfWork, pending domain.Transaction, betStatus *domain.BetStatus, walletErr error) (TransactionResult, error) {
	failed := pending
	if err := failed.Fail(o.clock.Now()); err != nil {
		return TransactionResult{}, fmt.Errorf("%s: %w", o.name, err)
	}

	err := uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		return repos.Transactions.Update(ctx, failed)
	})
	if err != nil {
		return o.leavePending(pending, betStatus, "outcome_not_stored", err), nil
	}

	outcome, level := "rejected", slog.LevelError
	if errors.Is(walletErr, domain.ErrInsufficientFunds) {
		outcome, level = "insufficient_funds", slog.LevelWarn
	}
	o.log(level, failed, outcome, walletErr)
	return TransactionResult{}, fmt.Errorf("%s: %w", o.name, walletErr)
}

// leavePending returns the transaction as it is stored: PENDING, with the
// bet's status unchanged. betStatus is nil for a withdraw, whose bet does not
// exist until it completes (§7.4).
func (o operation) leavePending(pending domain.Transaction, betStatus *domain.BetStatus, outcome string, cause error) TransactionResult {
	level := slog.LevelWarn
	if outcome == "outcome_not_stored" {
		level = slog.LevelError
	}
	o.log(level, pending, outcome, cause)
	return TransactionResult{Transaction: pending, BetStatus: betStatus}
}

// log writes the one line per operation: only identifiers, the amount and
// the outcome.
func (o operation) log(level slog.Level, tx domain.Transaction, outcome string, cause error) {
	attributes := []any{
		"transaction_id", tx.ID,
		"provider_transaction_id", tx.ProviderTransactionID,
		"user_id", tx.UserID,
		"bet_id", tx.BetID,
		"amount", tx.Amount.String(),
		"outcome", outcome,
	}
	if tx.CancelledTransactionID != "" {
		attributes = append(attributes, "cancelled_transaction_id", tx.CancelledTransactionID)
	}
	if cause != nil {
		attributes = append(attributes, "error", cause.Error())
	}
	o.logger.Log(context.Background(), level, o.name, attributes...)
}
