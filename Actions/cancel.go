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

// cancelRequest is a CancelCommand once Execute has validated it: the player
// exists and providerID is well formed (it may still be unknown).
type cancelRequest struct {
	player     domain.Player
	providerID domain.ProviderTransactionID
}

// CancelCommandHandler handles the execution of cancel commands.
type CancelCommandHandler struct {
	operation
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
	return &CancelCommandHandler{operation{
		name:             "cancel",
		playerRepository: playerRepository,
		locker:           locker,
		wallet:           wallet,
		ids:              ids,
		clock:            clock,
		logger:           logger,
	}}
}

// cancelTarget is what a cancel reverts: the original transaction
type cancelTarget struct {
	original domain.Transaction
	bet      domain.Bet
	decision domain.CancelDecision
}

// Execute reverts a completed withdraw or deposit of the player through a
// compensating Wallet operation, as decided by domain.DecideCancel.
func (handler *CancelCommandHandler) Execute(ctx context.Context, command CancelCommand) (TransactionResult, error) {
	providerID, err := domain.ParseProviderTransactionID(command.ProviderTransactionID)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: %w", err)
	}
	player, err := handler.playerRepository.FindByID(ctx, command.UserID)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: %w", err)
	}

	request := cancelRequest{player: player, providerID: providerID}
	return handler.runLocked(ctx, player.UserID, func(ctx context.Context, uow domain.UnitOfWork) (TransactionResult, error) {
		return handler.cancelLocked(ctx, uow, request)
	})
}

func (handler *CancelCommandHandler) cancelLocked(ctx context.Context, uow domain.UnitOfWork, request cancelRequest) (TransactionResult, error) {
	// First: while a cancel is PENDING or FAILED its original is still
	// COMPLETED, so DecideCancel alone would accept a second cancel.
	if result, found, err := handler.replayIfCancelled(ctx, uow, request); found || err != nil {
		if err == nil && result.Transaction.Status == domain.TransactionPending {
			// A retry of a PENDING cancel re-sends it, if it is due.
			return asReplay(handler.resendPending(ctx, uow, result.Transaction))
		}
		return result, err
	}

	// Under the lock: no other operation of the player can settle, cancel or
	// change the bet between this decision and the cancel being stored.
	target, err := handler.findWhatToRevert(ctx, uow, request)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: %w", err)
	}

	oldBalance, err := handler.readOldBalance(ctx, request.player)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: %w", err)
	}

	pending := domain.NewCancel(handler.ids.NewTransactionID(), target.original, oldBalance, handler.clock.Now())

	if target.decision.Action == domain.CancelNoWalletCall {
		result, err := handler.cancelWithoutWallet(ctx, uow, pending, target)
		if errors.Is(err, domain.ErrAlreadyCancelled) {
			return handler.replayAfterDuplicateInsert(ctx, uow, request)
		}
		return result, err
	}

	err = uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		return repos.Transactions.Insert(ctx, pending)
	})
	if errors.Is(err, domain.ErrAlreadyCancelled) {
		return handler.replayAfterDuplicateInsert(ctx, uow, request)
	}
	if err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: store pending: %w", err)
	}

	// The compensating operation: same bet and amount as the original (copied
	// by NewCancel), our cancel's ID as reference, so the Wallet sees a new
	// movement and de-duplicates re-sends of this one.
	newBalance, walletErr := handler.sendCompensation(ctx, target.decision.Action, walletOperationOf(pending))
	return handler.finish(ctx, uow, pending, target, newBalance, walletErr)
}

// resendPending sends a PENDING cancel's compensating operation to the Wallet
// again, the same request as its first attempt, and stores the outcome with
// the same rules (finish). Nothing is sent if it is not due (see
// startResend); the result is then the cancel as stored, with its bet's
// status. The caller holds the player's lock.
//
// While the cancel is PENDING its original stays COMPLETED and the bet
// unchanged: a second cancel gets this one (replayIfCancelled) and a deposit
// on the withdraw is refused (checkNoCancelInProgress).
func (handler *CancelCommandHandler) resendPending(ctx context.Context, uow domain.UnitOfWork, pending domain.Transaction) (TransactionResult, error) {
	var (
		current domain.Transaction
		target  cancelTarget
		due     bool
	)
	err := uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		var err error
		current, due, err = handler.startResend(ctx, repos, pending)
		if err != nil {
			return err
		}
		original, err := repos.Transactions.FindByID(ctx, current.UserID, current.CancelledTransactionID)
		if err != nil {
			return err
		}
		bet, err := repos.Bets.FindByID(ctx, current.UserID, original.BetID)
		if err != nil {
			return err
		}
		target = cancelTarget{original: original, bet: bet, decision: decisionOfPendingCancel(original, current)}
		return nil
	})
	if err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: resend: %w", err)
	}
	if !due {
		return TransactionResult{Transaction: current, BetStatus: &target.bet.Status}, nil
	}

	newBalance, walletErr := handler.sendCompensation(ctx, target.decision.Action, walletOperationOf(current))
	return handler.finish(ctx, uow, current, target, newBalance, walletErr)
}

// decisionOfPendingCancel is the decision taken when the cancel was accepted:
// a withdraw is credited back and a deposit debited back, by the amount the
// cancel copied from it. A re-send repeats that decision; it does not run
// DecideCancel again. (A zero-amount deposit's cancel never calls the Wallet,
// so it is never PENDING.)
func decisionOfPendingCancel(original, cancel domain.Transaction) domain.CancelDecision {
	if original.Type == domain.TransactionWithdraw {
		return domain.CancelDecision{Action: domain.CancelCreditBack, Amount: cancel.Amount}
	}
	return domain.CancelDecision{Action: domain.CancelDebitBack, Amount: cancel.Amount}
}

// finish stores what the Wallet's answer means for the pending cancel, the
// original and its bet and returns the result.
func (handler *CancelCommandHandler) finish(
	ctx context.Context,
	uow domain.UnitOfWork,
	pending domain.Transaction,
	target cancelTarget,
	newBalance domain.Money,
	walletErr error,
) (TransactionResult, error) {
	writeCtx, cancel := handler.outcomeContext(ctx)
	defer cancel()

	// Until the cancel completes, the original and its bet do not change.
	betStatus := &target.bet.Status
	switch classifyWalletResult(walletErr) {
	case walletApplied:
		return handler.complete(writeCtx, uow, pending, target, newBalance)
	case walletAppliedBalanceUnknown:
		derived, err := balanceAfterCompensation(pending.OldBalance, target.decision)
		if err != nil {
			return handler.leavePending(pending, betStatus, outcomeBalanceUnknown, err), nil
		}
		return handler.complete(writeCtx, uow, pending, target, derived)
	case walletNotApplied:
		return handler.fail(writeCtx, uow, pending, betStatus, walletErr)
	default:
		// Unknown outcome: it stays PENDING and the API answers 202.
		return handler.leavePending(pending, betStatus, outcomePending, walletErr), nil
	}
}

// replayIfCancelled looks for the cancel of the transaction identified by the
// request's providerID. found is false when the transaction or its cancel does
// not exist; findWhatToRevert then reports an unknown transaction. When there
// is a cancel, its stored result is returned without any Wallet call:
//   - FAILED → the error of the first attempt (422 or 502; 409 if it has no reason);
//   - otherwise (PENDING, COMPLETED) → its current state and its bet's
//     (202 or 200).
//
// The request carries nothing but the key, so a cancel can never be a
// "different payload" conflict.
func (handler *CancelCommandHandler) replayIfCancelled(ctx context.Context, uow domain.UnitOfWork, request cancelRequest) (TransactionResult, bool, error) {
	userID := request.player.UserID
	var result TransactionResult
	found := false
	err := uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		original, err := repos.Transactions.FindByProviderTransactionID(ctx, userID, request.providerID)
		if errors.Is(err, domain.ErrTransactionNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		previous, err := repos.Transactions.FindCancelOf(ctx, userID, original.ID)
		if errors.Is(err, domain.ErrTransactionNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true

		if previous.Status == domain.TransactionFailed {
			return previous.FailureError()
		}
		bet, err := repos.Bets.FindByID(ctx, userID, original.BetID)
		if err != nil {
			return err
		}
		result = TransactionResult{Transaction: previous, BetStatus: &bet.Status, Replayed: true}
		return nil
	})
	if err != nil {
		return TransactionResult{}, true, fmt.Errorf("cancel: %w", err)
	}
	return result, found, nil
}

// replayAfterDuplicateInsert handles a cancel rejected by the single-cancel
// unique index although our lookup found none: the same cancel was stored
// meanwhile (only possible if the lock failed, e.g. two API instances), so it
// is a replay.
func (handler *CancelCommandHandler) replayAfterDuplicateInsert(ctx context.Context, uow domain.UnitOfWork, request cancelRequest) (TransactionResult, error) {
	result, found, err := handler.replayIfCancelled(ctx, uow, request)
	if found || err != nil {
		return result, err
	}
	return TransactionResult{}, fmt.Errorf("cancel: %w: the transaction already has a cancel", domain.ErrConflict)
}

// cancelWithoutWallet cancels a zero-amount deposit: no money moves, so there
// is no Wallet call to keep track of and no PENDING step. One database
// transaction stores the cancel as COMPLETED (newBalance = oldBalance), the
// deposit as CANCELLED and the bet back to PLACED.
func (handler *CancelCommandHandler) cancelWithoutWallet(ctx context.Context, uow domain.UnitOfWork, cancel domain.Transaction, target cancelTarget) (TransactionResult, error) {
	state, err := applyCancel(cancel, target, cancel.OldBalance, handler.clock.Now())
	if err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: %w", err)
	}

	err = uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		if err := repos.Transactions.Insert(ctx, state.cancel); err != nil {
			return err
		}
		if err := repos.Transactions.Update(ctx, state.original); err != nil {
			return err
		}
		return repos.Bets.Update(ctx, state.bet)
	})
	if err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: store: %w", err)
	}

	handler.log(slog.LevelInfo, state.cancel, outcomeCompletedWithoutWallet, nil)
	return TransactionResult{Transaction: state.cancel, BetStatus: &state.bet.Status}, nil
}

// sendCompensation sends the movement opposite to the original: a withdraw is
// credited back with a Wallet deposit, a deposit is debited back with a
// Wallet withdraw.
func (handler *CancelCommandHandler) sendCompensation(ctx context.Context, action domain.CancelAction, compensation domain.WalletOperation) (domain.Money, error) {
	if action == domain.CancelCreditBack {
		return handler.wallet.Deposit(ctx, compensation)
	}
	return handler.wallet.Withdraw(ctx, compensation)
}

// balanceAfterCompensation is the new balance when the Wallet applied the
// compensation without reporting it: the old balance plus or minus the amount.
func balanceAfterCompensation(oldBalance domain.Money, decision domain.CancelDecision) (domain.Money, error) {
	if decision.Action == domain.CancelCreditBack {
		return oldBalance.Add(decision.Amount)
	}
	return oldBalance.Sub(decision.Amount)
}

// complete stores, in one database transaction, the cancel as COMPLETED, the
// original as CANCELLED and its bet reverted: CANCELLED after a
// withdraw cancel, back to PLACED after a deposit cancel. If that cannot be
// stored, the database still says PENDING, and so does the result.
func (handler *CancelCommandHandler) complete(ctx context.Context, uow domain.UnitOfWork, pending domain.Transaction, target cancelTarget, newBalance domain.Money) (TransactionResult, error) {
	state, err := applyCancel(pending, target, newBalance, handler.clock.Now())
	if err != nil {
		return TransactionResult{}, fmt.Errorf("cancel: %w", err)
	}

	err = uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		if err := repos.Transactions.Update(ctx, state.cancel); err != nil {
			return err
		}
		if err := repos.Transactions.Update(ctx, state.original); err != nil {
			return err
		}
		return repos.Bets.Update(ctx, state.bet)
	})
	if err != nil {
		return handler.leavePending(pending, &target.bet.Status, outcomeNotStored, err), nil
	}

	handler.log(slog.LevelInfo, state.cancel, outcomeCompleted, nil)
	return TransactionResult{Transaction: state.cancel, BetStatus: &state.bet.Status}, nil
}

// cancelledState is what a completed cancel stores: the cancel COMPLETED, the
// original CANCELLED and its bet reverted.
type cancelledState struct {
	cancel   domain.Transaction
	original domain.Transaction
	bet      domain.Bet
}

// applyCancel computes the cancelledState in memory, through the domain's
// transitions; nothing is stored here. The bet becomes CANCELLED after a
// withdraw cancel, and returns to PLACED after a deposit cancel.
func applyCancel(pending domain.Transaction, target cancelTarget, newBalance domain.Money, now time.Time) (cancelledState, error) {
	state := cancelledState{cancel: pending, original: target.original, bet: target.bet}
	if err := state.cancel.Complete(newBalance, now); err != nil {
		return cancelledState{}, err
	}
	if err := state.original.MarkCancelled(now); err != nil {
		return cancelledState{}, err
	}
	if state.original.Type == domain.TransactionWithdraw {
		if err := state.bet.Cancel(now); err != nil {
			return cancelledState{}, err
		}
	} else {
		if err := state.bet.RevertSettlement(now); err != nil {
			return cancelledState{}, err
		}
	}
	return state, nil
}

// findWhatToRevert loads the transaction to cancel and its bet:
//   - unknown or another player's → ErrTransactionNotFound (404);
//   - PENDING → ErrTransactionPending, FAILED → ErrNothingToRevert (409);
//   - a withdraw whose bet is settled → ErrBetAlreadySettled (409);
//   - a withdraw whose bet has a PENDING deposit → ErrTransactionPending (409).
func (handler *CancelCommandHandler) findWhatToRevert(ctx context.Context, uow domain.UnitOfWork, request cancelRequest) (cancelTarget, error) {
	userID := request.player.UserID
	var target cancelTarget
	err := uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		original, err := repos.Transactions.FindByProviderTransactionID(ctx, userID, request.providerID)
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
		if errors.Is(err, domain.ErrAlreadyCancelled) {
			// replayIfCancelled found no cancel for this CANCELLED original:
			// the data is inconsistent, so there is no earlier result to return.
			handler.logger.Error("cancelled transaction without its cancel",
				"transaction_id", original.ID, "provider_transaction_id", original.ProviderTransactionID)
			return fmt.Errorf("%w: the transaction is already cancelled", domain.ErrConflict)
		}
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
