package actions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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

	// The cancel is stored and sent to the Wallet in the next steps.
	_, _ = target, oldBalance
	return TransactionResult{}, nil
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
