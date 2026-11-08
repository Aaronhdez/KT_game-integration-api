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

// WithdrawCommand represents the data required to execute a withdraw operation.
type WithdrawCommand struct {
	UserID                domain.UserID
	ProviderTransactionID string
	Amount                domain.Money
	Currency              string
}

// WithdrawCommandHandler handles the execution of withdraw commands.
type WithdrawCommandHandler struct {
	playerRepository domain.PlayerRepository
	locker           domain.PlayerLocker
	wallet           domain.WalletClient
	ids              domain.IDGenerator
	clock            domain.Clock
	logger           *slog.Logger
}

// NewWithdrawCommandHandler creates a new instance of WithdrawCommandHandler with the provided dependencies.
func NewWithdrawCommandHandler(
	playerRepository domain.PlayerRepository,
	locker domain.PlayerLocker,
	wallet domain.WalletClient,
	ids domain.IDGenerator,
	clock domain.Clock,
	logger *slog.Logger,
) *WithdrawCommandHandler {
	return &WithdrawCommandHandler{
		playerRepository: playerRepository,
		locker:           locker,
		wallet:           wallet,
		ids:              ids,
		clock:            clock,
		logger:           logger,
	}
}

// Execute handles the withdraw command by validating input, acquiring a player lock, and performing the withdrawal.
func (h *WithdrawCommandHandler) Execute(ctx context.Context, command WithdrawCommand) (TransactionResult, error) {
	providerTransactionID, err := domain.ParseProviderTransactionID(command.ProviderTransactionID)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("withdraw: %w", err)
	}
	if !command.Amount.IsPositive() {
		return TransactionResult{}, fmt.Errorf("withdraw: must be greater than zero %w", domain.ErrInvalidAmount)
	}
	currency, err := domain.ParseCurrency(command.Currency)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("withdraw: %w", err)
	}
	player, err := h.playerRepository.FindByID(ctx, command.UserID)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("withdraw: %w", err)
	}
	if currency != player.Currency {
		return TransactionResult{}, fmt.Errorf("withdraw: %w", domain.ErrCurrencyMismatch)
	}

	var result TransactionResult
	err = h.locker.WithPlayerLock(ctx, player.UserID, func(ctx context.Context, uow domain.UnitOfWork) error {
		var lockedErr error
		result, lockedErr = h.withdrawLocked(ctx, uow, player, providerTransactionID, command.Amount)
		return lockedErr
	})
	return result, err
}

func (h *WithdrawCommandHandler) withdrawLocked(
	ctx context.Context,
	uow domain.UnitOfWork,
	player domain.Player,
	providerID domain.ProviderTransactionID,
	amount domain.Money,
) (TransactionResult, error) {
	// A request already processed is answered from the database, without
	// touching the Wallet (§7.1). Under the lock, so concurrent duplicates see
	// the transaction stored by the first one.
	if result, found, err := h.replayIfProcessed(ctx, uow, player.UserID, providerID, amount, player.Currency); found || err != nil {
		return result, err
	}

	balance, err := h.wallet.Balance(ctx, player.UserID)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("withdraw: read old balance: %v: %w", err, domain.ErrWalletFailure)
	}
	if balance.Currency != player.Currency {
		return TransactionResult{}, fmt.Errorf("withdraw: wallet currency %s, account currency %s: %w", balance.Currency, player.Currency, domain.ErrWalletFailure)
	}

	var pending domain.Transaction
	err = uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		betID, err := repos.Bets.NextID(ctx)
		if err != nil {
			return err
		}
		pending, err = domain.NewWithdraw(domain.TransactionDetails{
			ID:                    h.ids.NewTransactionID(),
			UserID:                player.UserID,
			BetID:                 betID,
			ProviderTransactionID: providerID,
			Amount:                amount,
			Currency:              player.Currency,
			OldBalance:            balance.Amount,
		}, h.clock.Now())
		if err != nil {
			return err
		}
		return repos.Transactions.Insert(ctx, pending)
	})
	// The unique index found the provider ID taken although our lookup did not:
	// either the same request was stored meanwhile (only possible if the lock
	// failed), which is a replay, or the ID belongs to another player (§6.1),
	// which stays ErrTransactionExists (409) without revealing their data.
	if errors.Is(err, domain.ErrTransactionExists) {
		if result, found, lookupErr := h.replayIfProcessed(ctx, uow, player.UserID, providerID, amount, player.Currency); found || lookupErr != nil {
			return result, lookupErr
		}
		// Its message reaches the provider: the reason only, no internal
		// details such as the ID of the transaction we tried to store.
		return TransactionResult{}, fmt.Errorf("withdraw: %w", domain.ErrTransactionExists)
	}
	if err != nil {
		return TransactionResult{}, fmt.Errorf("withdraw: store pending: %w", err)
	}

	// Our transaction ID is the reference: the Wallet de-duplicates re-sends by
	// it (docs/wallet-findings.md, Q8)
	newBalance, walletErr := h.wallet.Withdraw(ctx, domain.WalletOperation{
		UserID:    player.UserID,
		Currency:  player.Currency,
		Amount:    amount,
		BetID:     pending.BetID,
		Reference: pending.ID,
	})

	// The Wallet may have taken the money even if the caller has gone away
	// meanwhile, so the outcome is stored with a context that is not cancelled
	// with the request, bounded by its own timeout.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), outcomeWriteTimeout)
	defer cancel()

	switch {
	case walletErr == nil:
		return h.complete(writeCtx, uow, pending, newBalance)
	case errors.Is(walletErr, domain.ErrWalletAppliedBalanceUnknown):
		// Exact: the old balance was read under the player's lock just before.
		derived, err := domain.MoneyFromDecimal(pending.OldBalance.Decimal().Sub(amount.Decimal()))
		if err != nil {
			return h.leavePending(pending, "balance_unknown", err), nil
		}
		return h.complete(writeCtx, uow, pending, derived)
	case errors.Is(walletErr, domain.ErrInsufficientFunds), errors.Is(walletErr, domain.ErrWalletRejected):
		return h.fail(writeCtx, uow, pending, walletErr)
	default:
		// Unknown outcome (§9.3): it stays PENDING and the API answers 202.
		return h.leavePending(pending, "pending", walletErr), nil
	}
}

// complete stores the withdraw as COMPLETED and creates its PLACED bet, both
// in one database transaction. If that cannot be stored, the database still
// says PENDING, and so does the result.
func (h *WithdrawCommandHandler) complete(ctx context.Context, uow domain.UnitOfWork, pending domain.Transaction, newBalance domain.Money) (TransactionResult, error) {
	now := h.clock.Now()
	completed := pending
	if err := completed.Complete(newBalance, now); err != nil {
		return TransactionResult{}, fmt.Errorf("withdraw: %w", err)
	}
	bet := domain.NewPlacedBet(completed.BetID, completed.UserID, completed.Currency, completed.Amount, now)

	err := uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		if err := repos.Bets.Insert(ctx, bet); err != nil {
			return err
		}
		return repos.Transactions.Update(ctx, completed)
	})
	if err != nil {
		return h.leavePending(pending, "outcome_not_stored", err), nil
	}

	h.log(slog.LevelInfo, completed, "completed", nil)
	return TransactionResult{Transaction: completed, BetStatus: &bet.Status}, nil
}

// fail stores the withdraw as FAILED (the Wallet did not apply it) and
// returns the Wallet's error, which the API maps to 422 or 502.
func (h *WithdrawCommandHandler) fail(ctx context.Context, uow domain.UnitOfWork, pending domain.Transaction, walletErr error) (TransactionResult, error) {
	failed := pending
	if err := failed.Fail(h.clock.Now()); err != nil {
		return TransactionResult{}, fmt.Errorf("withdraw: %w", err)
	}

	err := uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		return repos.Transactions.Update(ctx, failed)
	})
	if err != nil {
		return h.leavePending(pending, "outcome_not_stored", err), nil
	}

	outcome, level := "rejected", slog.LevelError
	if errors.Is(walletErr, domain.ErrInsufficientFunds) {
		outcome, level = "insufficient_funds", slog.LevelWarn
	}
	h.log(level, failed, outcome, walletErr)
	return TransactionResult{}, fmt.Errorf("withdraw: %w", walletErr)
}

// leavePending returns the transaction as it is stored: PENDING, with no bet.
func (h *WithdrawCommandHandler) leavePending(pending domain.Transaction, outcome string, cause error) TransactionResult {
	level := slog.LevelWarn
	if outcome == "outcome_not_stored" {
		level = slog.LevelError
	}
	h.log(level, pending, outcome, cause)
	return TransactionResult{Transaction: pending}
}

// log writes the one line per withdraw required by §9.7. It never includes
// tokens or secrets: only identifiers, the amount and the outcome.
func (h *WithdrawCommandHandler) log(level slog.Level, tx domain.Transaction, outcome string, cause error) {
	attributes := []any{
		"transaction_id", tx.ID,
		"provider_transaction_id", tx.ProviderTransactionID,
		"user_id", tx.UserID,
		"bet_id", tx.BetID,
		"amount", tx.Amount.String(),
		"outcome", outcome,
	}
	if cause != nil {
		attributes = append(attributes, "error", cause.Error())
	}
	h.logger.Log(context.Background(), level, "withdraw", attributes...)
}

// replayIfProcessed looks for a transaction already stored for providerID.
// found is false when there is none. When there is one, the stored result is
// returned without any Wallet call (§7.1, §7.2):
//   - not a withdraw, or another amount or currency → ErrIdempotencyConflict (409);
//   - FAILED → ErrPreviousAttemptFailed (409);
//   - PENDING → the transaction, with no bet (202);
//   - otherwise (COMPLETED, CANCELLED) → its current state and its bet's (200).
func (h *WithdrawCommandHandler) replayIfProcessed(
	ctx context.Context,
	uow domain.UnitOfWork,
	userID domain.UserID,
	providerID domain.ProviderTransactionID,
	amount domain.Money,
	currency domain.Currency,
) (TransactionResult, bool, error) {
	var result TransactionResult
	found := false
	err := uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		previous, err := repos.Transactions.FindByProviderTransactionID(ctx, userID, providerID)
		if errors.Is(err, domain.ErrTransactionNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true

		// The lookup also returns deposits: a deposit with this provider ID
		// is another request, never an earlier attempt of this withdraw.
		if previous.Type != domain.TransactionWithdraw ||
			!previous.Amount.Equal(amount) || previous.Currency != currency {
			return domain.ErrIdempotencyConflict
		}

		switch previous.Status {
		case domain.TransactionFailed:
			return domain.ErrPreviousAttemptFailed
		case domain.TransactionPending:
			result = TransactionResult{Transaction: previous, Replayed: true}
			return nil
		default:
			bet, err := repos.Bets.FindByID(ctx, userID, previous.BetID)
			if err != nil {
				return err
			}
			result = TransactionResult{Transaction: previous, BetStatus: &bet.Status, Replayed: true}
			return nil
		}
	})
	if err != nil {
		return TransactionResult{}, true, fmt.Errorf("withdraw: %w", err)
	}
	return result, found, nil
}
