package actions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// WithdrawCommand represents the data required to execute a withdraw operation.
type WithdrawCommand struct {
	UserID                domain.UserID
	ProviderTransactionID string
	Amount                domain.Money
	Currency              string
}

// WithdrawCommandHandler handles the execution of withdraw commands.
type WithdrawCommandHandler struct {
	operation
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
	return &WithdrawCommandHandler{operation{
		name:             "withdraw",
		playerRepository: playerRepository,
		locker:           locker,
		wallet:           wallet,
		ids:              ids,
		clock:            clock,
		logger:           logger,
	}}
}

// Execute handles the withdraw command by validating input, acquiring a player lock, and performing the withdrawal.
func (handler *WithdrawCommandHandler) Execute(ctx context.Context, command WithdrawCommand) (TransactionResult, error) {
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
	player, err := handler.playerRepository.FindByID(ctx, command.UserID)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("withdraw: %w", err)
	}
	if currency != player.Currency {
		return TransactionResult{}, fmt.Errorf("withdraw: %w", domain.ErrCurrencyMismatch)
	}

	var result TransactionResult
	err = handler.locker.WithPlayerLock(ctx, player.UserID, func(ctx context.Context, uow domain.UnitOfWork) error {
		var lockedErr error
		result, lockedErr = handler.withdrawLocked(ctx, uow, player, providerTransactionID, command.Amount)
		return lockedErr
	})
	return result, err
}

func (handler *WithdrawCommandHandler) withdrawLocked(
	ctx context.Context,
	uow domain.UnitOfWork,
	player domain.Player,
	providerID domain.ProviderTransactionID,
	amount domain.Money,
) (TransactionResult, error) {
	if result, found, err := handler.replayIfProcessed(ctx, uow, player.UserID, providerID, amount, player.Currency); found || err != nil {
		return result, err
	}

	oldBalance, err := handler.readOldBalance(ctx, player)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("withdraw: %w", err)
	}

	var pending domain.Transaction
	err = uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		betID, err := repos.Bets.NextID(ctx)
		if err != nil {
			return err
		}
		pending, err = domain.NewWithdraw(domain.TransactionDetails{
			ID:                    handler.ids.NewTransactionID(),
			UserID:                player.UserID,
			BetID:                 betID,
			ProviderTransactionID: providerID,
			Amount:                amount,
			Currency:              player.Currency,
			OldBalance:            oldBalance,
		}, handler.clock.Now())
		if err != nil {
			return err
		}
		return repos.Transactions.Insert(ctx, pending)
	})

	if errors.Is(err, domain.ErrTransactionExists) {
		if result, found, lookupErr := handler.replayIfProcessed(ctx, uow, player.UserID, providerID, amount, player.Currency); found || lookupErr != nil {
			return result, lookupErr
		}
		return TransactionResult{}, fmt.Errorf("withdraw: %w", domain.ErrTransactionExists)
	}
	if err != nil {
		return TransactionResult{}, fmt.Errorf("withdraw: store pending: %w", err)
	}

	// Our transaction ID is the reference: the Wallet de-duplicates re-sends by
	// it (docs/wallet-findings.md, Q8)
	newBalance, walletErr := handler.wallet.Withdraw(ctx, domain.WalletOperation{
		UserID:    player.UserID,
		Currency:  player.Currency,
		Amount:    amount,
		BetID:     pending.BetID,
		Reference: pending.ID,
	})

	writeCtx, cancel := handler.outcomeContext(ctx)
	defer cancel()

	switch {
	case walletErr == nil:
		return handler.complete(writeCtx, uow, pending, newBalance)
	case errors.Is(walletErr, domain.ErrWalletAppliedBalanceUnknown):
		// Exact: the old balance was read under the player's lock just before.
		derived, err := domain.MoneyFromDecimal(pending.OldBalance.Decimal().Sub(amount.Decimal()))
		if err != nil {
			return handler.leavePending(pending, nil, "balance_unknown", err), nil
		}
		return handler.complete(writeCtx, uow, pending, derived)
	case errors.Is(walletErr, domain.ErrInsufficientFunds), errors.Is(walletErr, domain.ErrWalletRejected):
		return handler.fail(writeCtx, uow, pending, nil, walletErr)
	default:
		// Unknown outcome (§9.3): it stays PENDING and the API answers 202.
		return handler.leavePending(pending, nil, "pending", walletErr), nil
	}
}

// complete stores the withdraw as COMPLETED and creates its PLACED bet, both
// in one database transaction. If that cannot be stored, the database still
// says PENDING, and so does the result.
func (handler *WithdrawCommandHandler) complete(ctx context.Context, uow domain.UnitOfWork, pending domain.Transaction, newBalance domain.Money) (TransactionResult, error) {
	now := handler.clock.Now()
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
		return handler.leavePending(pending, nil, "outcome_not_stored", err), nil
	}

	handler.log(slog.LevelInfo, completed, "completed", nil)
	return TransactionResult{Transaction: completed, BetStatus: &bet.Status}, nil
}

// replayIfProcessed looks for a transaction already stored for providerID.
// found is false when there is none. When there is one, the stored result is
// returned without any Wallet call.
//   - not a withdraw, or another amount or currency → ErrIdempotencyConflict (409);
//   - FAILED → ErrPreviousAttemptFailed (409);
//   - PENDING → the transaction, with no bet (202);
//   - otherwise (COMPLETED, CANCELLED) → its current state and its bet's (200).
func (handler *WithdrawCommandHandler) replayIfProcessed(
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
