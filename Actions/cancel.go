package actions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// CancelCommand represents the data required to revert a transaction (§8.5):
// the provider ID of the withdraw or deposit to cancel.
type CancelCommand struct {
	UserID                domain.UserID
	ProviderTransactionID string
}

// CancelCommandHandler handles the execution of cancel commands.
type CancelCommandHandler struct {
	playerRepository domain.PlayerRepository
	locker           domain.PlayerLocker
	wallet           domain.WalletClient
	ids              domain.IDGenerator
	clock            domain.Clock
	logger           *slog.Logger
}

// NewCancelCommandHandler creates a new instance of CancelCommandHandler with the provided dependencies.
func NewCancelCommandHandler(
	playerRepository domain.PlayerRepository,
	locker domain.PlayerLocker,
	wallet domain.WalletClient,
	ids domain.IDGenerator,
	clock domain.Clock,
	logger *slog.Logger,
) *CancelCommandHandler {
	return &CancelCommandHandler{
		playerRepository: playerRepository,
		locker:           locker,
		wallet:           wallet,
		ids:              ids,
		clock:            clock,
		logger:           logger,
	}
}

// cancelTarget is what a cancel reverts: the original transaction
type cancelTarget struct {
	original domain.Transaction
	bet      domain.Bet
	decision domain.CancelDecision
}

// Execute reverts a completed withdraw or deposit of the player through a
// compensating Wallet operation, as decided by domain.DecideCancel.
func (h *CancelCommandHandler) Execute(ctx context.Context, command CancelCommand) (TransactionResult, error) {
	providerID, err := domain.ParseProviderTransactionID(command.ProviderTransactionID)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: %w", err)
	}
	player, err := h.playerRepository.FindByID(ctx, command.UserID)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: %w", err)
	}

	var result TransactionResult
	err = h.locker.WithPlayerLock(ctx, player.UserID, func(ctx context.Context, uow domain.UnitOfWork) error {
		var lockedErr error
		result, lockedErr = h.cancelLocked(ctx, uow, player, providerID)
		return lockedErr
	})
	return result, err
}

func (h *CancelCommandHandler) cancelLocked(
	ctx context.Context,
	uow domain.UnitOfWork,
	player domain.Player,
	providerID domain.ProviderTransactionID,
) (TransactionResult, error) {
	// Under the lock: no other operation of the player can settle, cancel or
	// change the bet between this decision and the cancel being stored.
	target, err := h.findWhatToRevert(ctx, uow, player.UserID, providerID)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: %w", err)
	}

	oldBalance, err := readOldBalance(ctx, h.wallet, player)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: %w", err)
	}

	if target.decision.Action == domain.CancelNoWalletCall {
		return TransactionResult{}, nil
	}

	pending := domain.NewCancel(h.ids.NewTransactionID(), target.original, oldBalance, h.clock.Now())
	err = uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		return repos.Transactions.Insert(ctx, pending)
	})
	if err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: store pending: %w", err)
	}

	// The compensating operation: same bet, our cancel's ID as reference, so
	// the Wallet sees a new movement and de-duplicates re-sends of this one.
	newBalance, walletErr := h.sendCompensation(ctx, target.decision.Action, domain.WalletOperation{
		UserID:    player.UserID,
		Currency:  target.original.Currency,
		Amount:    target.decision.Amount,
		BetID:     target.bet.ID,
		Reference: pending.ID,
	})

	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), outcomeWriteTimeout)
	defer cancel()

	switch {
	case walletErr == nil:
		return h.complete(writeCtx, uow, pending, target, newBalance)
	case errors.Is(walletErr, domain.ErrWalletAppliedBalanceUnknown):
		derived, err := balanceAfterCompensation(pending.OldBalance, target.decision)
		if err != nil {
			return h.leavePending(pending, target.bet, "balance_unknown", err), nil
		}
		return h.complete(writeCtx, uow, pending, target, derived)
	case errors.Is(walletErr, domain.ErrInsufficientFunds), errors.Is(walletErr, domain.ErrWalletRejected):
		return h.fail(writeCtx, uow, pending, target.bet, walletErr)
	default:
		// Unknown outcome: it stays PENDING and the API answers 202.
		return h.leavePending(pending, target.bet, "pending", walletErr), nil
	}
}

// sendCompensation sends the movement opposite to the original: a withdraw is
// credited back with a Wallet deposit, a deposit is debited back with a
// Wallet withdraw.
func (h *CancelCommandHandler) sendCompensation(ctx context.Context, action domain.CancelAction, operation domain.WalletOperation) (domain.Money, error) {
	if action == domain.CancelCreditBack {
		return h.wallet.Deposit(ctx, operation)
	}
	return h.wallet.Withdraw(ctx, operation)
}

// balanceAfterCompensation is the new balance when the Wallet applied the
// compensation without reporting it: the old balance plus or minus the amount.
func balanceAfterCompensation(oldBalance domain.Money, decision domain.CancelDecision) (domain.Money, error) {
	if decision.Action == domain.CancelCreditBack {
		return domain.MoneyFromDecimal(oldBalance.Decimal().Add(decision.Amount.Decimal()))
	}
	return domain.MoneyFromDecimal(oldBalance.Decimal().Sub(decision.Amount.Decimal()))
}

// complete stores, in one database transaction, the cancel as COMPLETED, the
// original as CANCELLED and its bet reverted: CANCELLED after a
// withdraw cancel, back to PLACED after a deposit cancel. If that cannot be
// stored, the database still says PENDING, and so does the result.
func (h *CancelCommandHandler) complete(ctx context.Context, uow domain.UnitOfWork, pending domain.Transaction, target cancelTarget, newBalance domain.Money) (TransactionResult, error) {
	now := h.clock.Now()
	completed := pending
	if err := completed.Complete(newBalance, now); err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: %w", err)
	}
	original := target.original
	if err := original.MarkCancelled(now); err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: %w", err)
	}
	bet := target.bet
	if err := revertBet(&bet, original.Type, now); err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: %w", err)
	}

	err := uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		if err := repos.Transactions.Update(ctx, completed); err != nil {
			return err
		}
		if err := repos.Transactions.Update(ctx, original); err != nil {
			return err
		}
		return repos.Bets.Update(ctx, bet)
	})
	if err != nil {
		return h.leavePending(pending, target.bet, "outcome_not_stored", err), nil
	}

	h.log(slog.LevelInfo, completed, "completed", nil)
	return TransactionResult{Transaction: completed, BetStatus: &bet.Status}, nil
}

// revertBet applies the cancel of the bet's withdraw or deposit to the bet.
func revertBet(bet *domain.Bet, cancelled domain.TransactionType, now time.Time) error {
	if cancelled == domain.TransactionWithdraw {
		return bet.Cancel(now)
	}
	return bet.RevertSettlement(now)
}

// fail stores the cancel as FAILED (the Wallet did not apply the
// compensation) and returns the Wallet's error, which the API maps to 422 or
// 502. The original and its bet do not change.
func (h *CancelCommandHandler) fail(ctx context.Context, uow domain.UnitOfWork, pending domain.Transaction, bet domain.Bet, walletErr error) (TransactionResult, error) {
	failed := pending
	if err := failed.Fail(h.clock.Now()); err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: %w", err)
	}

	err := uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		return repos.Transactions.Update(ctx, failed)
	})
	if err != nil {
		return h.leavePending(pending, bet, "outcome_not_stored", err), nil
	}

	outcome, level := "rejected", slog.LevelError
	if errors.Is(walletErr, domain.ErrInsufficientFunds) {
		outcome, level = "insufficient_funds", slog.LevelWarn
	}
	h.log(level, failed, outcome, walletErr)
	return TransactionResult{}, fmt.Errorf("cancel: %w", walletErr)
}

// leavePending returns the cancel as it is stored: PENDING, with the original
// and its bet unchanged.
func (h *CancelCommandHandler) leavePending(pending domain.Transaction, bet domain.Bet, outcome string, cause error) TransactionResult {
	level := slog.LevelWarn
	if outcome == "outcome_not_stored" {
		level = slog.LevelError
	}
	h.log(level, pending, outcome, cause)
	return TransactionResult{Transaction: pending, BetStatus: &bet.Status}
}

// log writes the one line per cancel (see logTransaction).
func (h *CancelCommandHandler) log(level slog.Level, tx domain.Transaction, outcome string, cause error) {
	logTransaction(h.logger, level, "cancel", tx, outcome, cause)
}

// findWhatToRevert loads the transaction to cancel and its bet:
//   - unknown or another player's → ErrTransactionNotFound (404);
//   - PENDING → ErrTransactionPending, FAILED → ErrNothingToRevert (409);
//   - a withdraw whose bet is settled → ErrBetAlreadySettled (409);
//   - a withdraw whose bet has a PENDING deposit → ErrTransactionPending (409).
func (h *CancelCommandHandler) findWhatToRevert(
	ctx context.Context,
	uow domain.UnitOfWork,
	userID domain.UserID,
	providerID domain.ProviderTransactionID,
) (cancelTarget, error) {
	var target cancelTarget
	err := uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		original, err := repos.Transactions.FindByProviderTransactionID(ctx, userID, providerID)
		if err != nil {
			return err
		}

		// Only a completed transaction has a bet to look at: a PENDING or
		// FAILED withdraw never created one, and DecideCancel rejects those
		// by their status alone.
		var bet domain.Bet
		if original.Status == domain.TransactionCompleted {
			bet, err = repos.Bets.FindByID(ctx, userID, original.BetID)
			if err != nil {
				return err
			}
		}

		decision, err := domain.DecideCancel(original, bet)
		if err != nil {
			return err
		}
		if original.Type == domain.TransactionWithdraw {
			if err := checkNoDepositInProgress(ctx, repos, userID, bet); err != nil {
				return err
			}
		}

		target = cancelTarget{original: original, bet: bet, decision: decision}
		return nil
	})
	if err != nil {
		return cancelTarget{}, err
	}
	return target, nil
}

// checkNoDepositInProgress refuses to cancel the withdraw of a bet that has a
// PENDING deposit.
func checkNoDepositInProgress(ctx context.Context, repos domain.Repositories, userID domain.UserID, bet domain.Bet) error {
	_, err := repos.Transactions.FindActiveDeposit(ctx, userID, bet.ID)
	if errors.Is(err, domain.ErrTransactionNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: the bet has a deposit in progress", domain.ErrTransactionPending)
}
