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

// resendDelay is how long after the start of its last attempt a PENDING
// transaction may be sent to the Wallet again. A re-send that reached the
// Wallet while the earlier attempt was still being processed could be applied
// twice: the Wallet never answers a withdraw or deposit after ~5 s, finishes
// slow ones at ~5.7 s, and our timeout is 6 s (docs/wallet-findings.md, Q7, Q8).
const resendDelay = 10 * time.Second

// overdueAfter is how long a transaction may stay PENDING before each new
// attempt that leaves it unknown is logged as an error, for an operator.
const overdueAfter = 5 * time.Minute

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

// fail stores the transaction as FAILED (the Wallet did not apply it) with
// the reason, and returns the Wallet's error, which the API maps to 422 or
// 502. Nothing else changes. betStatus is what leavePending reports if the
// write fails.
func (o operation) fail(ctx context.Context, uow domain.UnitOfWork, pending domain.Transaction, betStatus *domain.BetStatus, walletErr error) (TransactionResult, error) {
	reason, outcome, level := domain.FailureWalletRejected, outcomeRejected, slog.LevelError
	if errors.Is(walletErr, domain.ErrInsufficientFunds) {
		reason, outcome, level = domain.FailureInsufficientFunds, outcomeInsufficientFunds, slog.LevelWarn
	}

	failed := pending
	if err := failed.Fail(reason, o.clock.Now()); err != nil {
		return TransactionResult{}, fmt.Errorf("%s: %w", o.name, err)
	}

	err := uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		return repos.Transactions.Update(ctx, failed)
	})
	if err != nil {
		return o.leavePending(pending, betStatus, outcomeNotStored, err), nil
	}

	o.log(level, failed, outcome, walletErr)
	return TransactionResult{}, fmt.Errorf("%s: %w", o.name, walletErr)
}

// leavePending returns the transaction as it is stored: PENDING, with the
// bet's status unchanged. betStatus is nil for a withdraw, whose bet does not
// exist until it completes. A transaction still unknown after
// overdueAfter is logged as outcomePendingOverdue, at error level.
func (o operation) leavePending(pending domain.Transaction, betStatus *domain.BetStatus, outcome logOutcome, cause error) TransactionResult {
	if outcome == outcomePending && o.clock.Now().Sub(pending.CreatedAt) >= overdueAfter {
		outcome = outcomePendingOverdue
	}
	level := slog.LevelWarn
	if outcome == outcomeNotStored || outcome == outcomePendingOverdue {
		level = slog.LevelError
	}
	o.log(level, pending, outcome, cause)
	return TransactionResult{Transaction: pending, BetStatus: betStatus}
}

// startResend re-reads a PENDING transaction inside repos's database
// transaction and, if it is due, records a new attempt (UpdatedAt = now),
// which is committed before the Wallet is called: if the process stops during
// the re-send, the next one still waits. The caller holds the player's lock.
//
// due is false when nothing must be sent now, and current is then the
// transaction as stored: it is no longer PENDING (resolved between the
// resolver's scan and the lock), or its last attempt started less than
// resendDelay ago.
func (o operation) startResend(ctx context.Context, repos domain.Repositories, pending domain.Transaction) (current domain.Transaction, due bool, err error) {
	current, err = repos.Transactions.FindByID(ctx, pending.UserID, pending.ID)
	if err != nil {
		return domain.Transaction{}, false, err
	}
	now := o.clock.Now()
	if current.Status != domain.TransactionPending || now.Sub(current.UpdatedAt) < resendDelay {
		return current, false, nil
	}
	if err := current.RecordAttempt(now); err != nil {
		return domain.Transaction{}, false, err
	}
	if err := repos.Transactions.Update(ctx, current); err != nil {
		return domain.Transaction{}, false, err
	}
	return current, true, nil
}

// walletOperationOf builds the Wallet request of a stored transaction. The
// first attempt and every re-send use it, so a re-send is the identical
// request, which the Wallet de-duplicates by its reference
// (docs/wallet-findings.md, Q8).
func walletOperationOf(tx domain.Transaction) domain.WalletOperation {
	return domain.WalletOperation{
		UserID:    tx.UserID,
		Currency:  tx.Currency,
		Amount:    tx.Amount,
		BetID:     tx.BetID,
		Reference: tx.ID,
	}
}

// asReplay marks the result of a provider's retry as a replay: the
// transaction was created by the first request, so the retry is never a 201
// (§7.2). An error is returned unchanged.
func asReplay(result TransactionResult, err error) (TransactionResult, error) {
	if err == nil {
		result.Replayed = true
	}
	return result, err
}

// log writes the one line per operation: only identifiers, the amount and
// the outcome.
func (o operation) log(level slog.Level, tx domain.Transaction, outcome logOutcome, cause error) {
	attributes := []any{
		"transaction_id", tx.ID,
		"provider_transaction_id", tx.ProviderTransactionID,
		"user_id", tx.UserID,
		"bet_id", tx.BetID,
		"amount", tx.Amount.String(),
		"outcome", outcome.String(),
	}
	if tx.CancelledTransactionID != "" {
		attributes = append(attributes, "cancelled_transaction_id", tx.CancelledTransactionID)
	}
	if cause != nil {
		attributes = append(attributes, "error", cause.Error())
	}
	o.logger.Log(context.Background(), level, o.name, attributes...)
}
