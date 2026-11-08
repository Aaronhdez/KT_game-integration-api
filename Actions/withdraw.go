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

// withdrawRequest is a WithdrawCommand once Execute has validated it: the
// player exists, the amount is positive and the currency is the player's
// (player.Currency).
type withdrawRequest struct {
	player     domain.Player
	providerID domain.ProviderTransactionID
	amount     domain.Money
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
		return TransactionResult{}, fmt.Errorf("withdraw: %w: must be greater than zero", domain.ErrInvalidAmount)
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

	request := withdrawRequest{player: player, providerID: providerTransactionID, amount: command.Amount}
	return handler.runLocked(ctx, player.UserID, func(ctx context.Context, uow domain.UnitOfWork) (TransactionResult, error) {
		return handler.withdrawLocked(ctx, uow, request)
	})
}

func (handler *WithdrawCommandHandler) withdrawLocked(ctx context.Context, uow domain.UnitOfWork, request withdrawRequest) (TransactionResult, error) {
	if result, found, err := handler.replayIfProcessed(ctx, uow, request); found || err != nil {
		if err == nil && result.Transaction.Status == domain.TransactionPending {
			// A retry of a PENDING withdraw re-sends it, if it is due.
			return asReplay(handler.resendPending(ctx, uow, result.Transaction))
		}
		return result, err
	}

	oldBalance, err := handler.readOldBalance(ctx, request.player)
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
			UserID:                request.player.UserID,
			BetID:                 betID,
			ProviderTransactionID: request.providerID,
			Amount:                request.amount,
			Currency:              request.player.Currency,
			OldBalance:            oldBalance,
		}, handler.clock.Now())
		if err != nil {
			return err
		}
		return repos.Transactions.Insert(ctx, pending)
	})

	if errors.Is(err, domain.ErrTransactionExists) {
		if result, found, lookupErr := handler.replayIfProcessed(ctx, uow, request); found || lookupErr != nil {
			return result, lookupErr
		}
		return TransactionResult{}, fmt.Errorf("withdraw: %w", domain.ErrTransactionExists)
	}
	if err != nil {
		return TransactionResult{}, fmt.Errorf("withdraw: store pending: %w", err)
	}

	// Our transaction ID is the reference: the Wallet de-duplicates re-sends by
	// it (docs/wallet-findings.md, Q8)
	newBalance, walletErr := handler.wallet.Withdraw(ctx, walletOperationOf(pending))
	return handler.finish(ctx, uow, pending, newBalance, walletErr)
}

// resendPending sends a PENDING withdraw to the Wallet again, with the same
// request as its first attempt, and stores the outcome with the same rules
// (finish). Nothing is sent if it is not due (see startResend); the result is
// then the transaction as stored. The caller holds the player's lock.
func (handler *WithdrawCommandHandler) resendPending(ctx context.Context, uow domain.UnitOfWork, pending domain.Transaction) (TransactionResult, error) {
	var current domain.Transaction
	due := false
	err := uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		var err error
		current, due, err = handler.startResend(ctx, repos, pending)
		return err
	})
	if err != nil {
		return TransactionResult{}, fmt.Errorf("withdraw: resend: %w", err)
	}
	if !due {
		return TransactionResult{Transaction: current}, nil
	}

	newBalance, walletErr := handler.wallet.Withdraw(ctx, walletOperationOf(current))
	return handler.finish(ctx, uow, current, newBalance, walletErr)
}

// finish stores what the Wallet's answer means for the pending withdraw
// and returns the result.
func (handler *WithdrawCommandHandler) finish(
	ctx context.Context,
	uow domain.UnitOfWork,
	pending domain.Transaction,
	newBalance domain.Money,
	walletErr error,
) (TransactionResult, error) {
	writeCtx, cancel := handler.outcomeContext(ctx)
	defer cancel()

	switch classifyWalletResult(walletErr) {
	case walletApplied:
		return handler.complete(writeCtx, uow, pending, newBalance)
	case walletAppliedBalanceUnknown:
		// The balance right after the first attempt: its old balance was read
		// under the player's lock just before it.
		derived, err := pending.OldBalance.Sub(pending.Amount)
		if err != nil {
			return handler.leavePending(pending, nil, outcomeBalanceUnknown, err), nil
		}
		return handler.complete(writeCtx, uow, pending, derived)
	case walletNotApplied:
		return handler.fail(writeCtx, uow, pending, nil, walletErr)
	default:
		// Unknown outcome (§9.3): it stays PENDING and the API answers 202.
		return handler.leavePending(pending, nil, outcomePending, walletErr), nil
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
		return handler.leavePending(pending, nil, outcomeNotStored, err), nil
	}

	handler.log(slog.LevelInfo, completed, outcomeCompleted, nil)
	return TransactionResult{Transaction: completed, BetStatus: &bet.Status}, nil
}

// replayIfProcessed looks for a transaction already stored for the request's
// provider ID. found is false when there is none. When there is one, the
// stored result is returned without any Wallet call.
//   - not a withdraw, or another amount or currency → ErrIdempotencyConflict (409);
//   - FAILED → the error of the first attempt (422 or 502; 409 if it has no reason);
//   - PENDING → the transaction, with no bet (202);
//   - otherwise (COMPLETED, CANCELLED) → its current state and its bet's (200).
func (handler *WithdrawCommandHandler) replayIfProcessed(ctx context.Context, uow domain.UnitOfWork, request withdrawRequest) (TransactionResult, bool, error) {
	userID := request.player.UserID
	var result TransactionResult
	found := false
	err := uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		previous, err := repos.Transactions.FindByProviderTransactionID(ctx, userID, request.providerID)
		if errors.Is(err, domain.ErrTransactionNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true

		if previous.Type != domain.TransactionWithdraw ||
			!previous.Amount.Equal(request.amount) || previous.Currency != request.player.Currency {
			return domain.ErrIdempotencyConflict
		}

		switch previous.Status {
		case domain.TransactionFailed:
			return previous.FailureError()
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
