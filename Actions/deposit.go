package actions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// DepositCommand represents the data required to settle a bet (§8.4).
type DepositCommand struct {
	UserID                         domain.UserID
	ProviderTransactionID          string
	ProviderWithdrawnTransactionID string
	Amount                         domain.Money
	Currency                       string
}

// DepositCommandHandler handles the execution of deposit commands.
type DepositCommandHandler struct {
	playerRepository domain.PlayerRepository
	locker           domain.PlayerLocker
	wallet           domain.WalletClient
	ids              domain.IDGenerator
	clock            domain.Clock
	logger           *slog.Logger
}

// NewDepositCommandHandler creates a new instance of DepositCommandHandler with the provided dependencies.
func NewDepositCommandHandler(
	playerRepository domain.PlayerRepository,
	locker domain.PlayerLocker,
	wallet domain.WalletClient,
	ids domain.IDGenerator,
	clock domain.Clock,
	logger *slog.Logger,
) *DepositCommandHandler {
	return &DepositCommandHandler{
		playerRepository: playerRepository,
		locker:           locker,
		wallet:           wallet,
		ids:              ids,
		clock:            clock,
		logger:           logger,
	}
}

// Execute settles the bet of a completed withdraw of the same player: an
// amount greater than zero makes it WON, zero makes it LOST.
func (h *DepositCommandHandler) Execute(ctx context.Context, command DepositCommand) (TransactionResult, error) {
	providerTransactionID, err := domain.ParseProviderTransactionID(command.ProviderTransactionID)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("deposit: %w", err)
	}
	withdrawnID, err := domain.ParseProviderTransactionID(command.ProviderWithdrawnTransactionID)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("deposit: provider withdrawn transaction id: %w", err)
	}
	currency, err := domain.ParseCurrency(command.Currency)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("deposit: %w", err)
	}
	player, err := h.playerRepository.FindByID(ctx, command.UserID)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("deposit: %w", err)
	}
	if currency != player.Currency {
		return TransactionResult{}, fmt.Errorf("deposit: %w", domain.ErrCurrencyMismatch)
	}

	var result TransactionResult
	err = h.locker.WithPlayerLock(ctx, player.UserID, func(ctx context.Context, uow domain.UnitOfWork) error {
		var lockedErr error
		result, lockedErr = h.depositLocked(ctx, uow, player, providerTransactionID, withdrawnID, command.Amount, currency)
		return lockedErr
	})
	return result, err
}

func (h *DepositCommandHandler) depositLocked(
	ctx context.Context,
	uow domain.UnitOfWork,
	player domain.Player,
	providerID domain.ProviderTransactionID,
	withdrawnID domain.ProviderTransactionID,
	amount domain.Money,
	currency domain.Currency,
) (TransactionResult, error) {
	if result, found, err := h.replayIfProcessed(ctx, uow, player.UserID, providerID, withdrawnID, amount, currency); found || err != nil {
		return result, err
	}

	// Under the lock: no other operation of the player can settle or cancel
	// the bet between this check and the deposit being stored.
	bet, err := h.findBetToSettle(ctx, uow, player.UserID, withdrawnID, amount, currency)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("deposit: %w", err)
	}

	oldBalance, err := readOldBalance(ctx, h.wallet, player)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("deposit: %w", err)
	}

	pending := domain.NewDeposit(domain.TransactionDetails{
		ID:                    h.ids.NewTransactionID(),
		UserID:                player.UserID,
		BetID:                 bet.ID,
		ProviderTransactionID: providerID,
		Amount:                amount,
		Currency:              player.Currency,
		OldBalance:            oldBalance,
	}, h.clock.Now())

	if amount.IsZero() {
		result, err := h.settleWithoutWallet(ctx, uow, pending, bet)
		if isDuplicateDeposit(err) {
			return h.replayAfterDuplicateInsert(ctx, uow, player.UserID, providerID, withdrawnID, amount, currency, err)
		}
		return result, err
	}

	err = uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		return repos.Transactions.Insert(ctx, pending)
	})
	if isDuplicateDeposit(err) {
		return h.replayAfterDuplicateInsert(ctx, uow, player.UserID, providerID, withdrawnID, amount, currency, err)
	}
	if err != nil {
		return TransactionResult{}, insertDepositError("store pending", err)
	}

	// Our transaction ID is the reference: the Wallet de-duplicates re-sends by
	// it (docs/wallet-findings.md, Q8).
	newBalance, walletErr := h.wallet.Deposit(ctx, domain.WalletOperation{
		UserID:    player.UserID,
		Currency:  player.Currency,
		Amount:    amount,
		BetID:     bet.ID,
		Reference: pending.ID,
	})

	// The Wallet may have paid even if the caller has gone away meanwhile, so
	// the outcome is stored with a context that is not cancelled with the
	// request, bounded by its own timeout.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), outcomeWriteTimeout)
	defer cancel()

	switch {
	case walletErr == nil:
		return h.complete(writeCtx, uow, pending, bet, newBalance)
	case errors.Is(walletErr, domain.ErrWalletAppliedBalanceUnknown):
		// Exact: the old balance was read under the player's lock just before.
		derived, err := domain.MoneyFromDecimal(pending.OldBalance.Decimal().Add(amount.Decimal()))
		if err != nil {
			return h.leavePending(pending, bet, "balance_unknown", err), nil
		}
		return h.complete(writeCtx, uow, pending, bet, derived)
	case errors.Is(walletErr, domain.ErrInsufficientFunds), errors.Is(walletErr, domain.ErrWalletRejected):
		return h.fail(writeCtx, uow, pending, bet, walletErr)
	default:
		// Unknown outcome: it stays PENDING and the API answers 202.
		return h.leavePending(pending, bet, "pending", walletErr), nil
	}
}

func (h *DepositCommandHandler) settleWithoutWallet(ctx context.Context, uow domain.UnitOfWork, deposit domain.Transaction, bet domain.Bet) (TransactionResult, error) {
	now := h.clock.Now()
	completed := deposit
	if err := completed.Complete(deposit.OldBalance, now); err != nil {
		return TransactionResult{}, fmt.Errorf("deposit: %w", err)
	}
	settled := bet
	if err := settled.Settle(completed.Amount, now); err != nil {
		return TransactionResult{}, fmt.Errorf("deposit: %w", err)
	}

	err := uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		if err := repos.Transactions.Insert(ctx, completed); err != nil {
			return err
		}
		return repos.Bets.Update(ctx, settled)
	})
	if err != nil {
		return TransactionResult{}, insertDepositError("store", err)
	}

	h.log(slog.LevelInfo, completed, "completed_without_wallet", nil)
	return TransactionResult{Transaction: completed, BetStatus: &settled.Status}, nil
}

// isDuplicateDeposit tells whether a unique index rejected the new deposit:
// its provider ID is taken, or its bet already has an active deposit.
func isDuplicateDeposit(err error) bool {
	return errors.Is(err, domain.ErrTransactionExists) || errors.Is(err, domain.ErrBetAlreadySettled)
}

// replayAfterDuplicateInsert handles a deposit rejected by a unique index
// although our lookup found nothing. Either the same request was stored
// meanwhile (only possible if the lock failed, e.g. two API instances): a
// replay; or the provider ID belongs to another player, or another
// deposit settles the bet: the insert error stands (409). Both indexes are
// checked because PostgreSQL does not say which one it reports first when a
// row breaks the two.
func (h *DepositCommandHandler) replayAfterDuplicateInsert(
	ctx context.Context,
	uow domain.UnitOfWork,
	userID domain.UserID,
	providerID domain.ProviderTransactionID,
	withdrawnID domain.ProviderTransactionID,
	amount domain.Money,
	currency domain.Currency,
	insertErr error,
) (TransactionResult, error) {
	if result, found, err := h.replayIfProcessed(ctx, uow, userID, providerID, withdrawnID, amount, currency); found || err != nil {
		return result, err
	}
	if errors.Is(insertErr, domain.ErrTransactionExists) {
		// Its message reaches the provider: the reason only, no internal details.
		return TransactionResult{}, fmt.Errorf("deposit: %w", domain.ErrTransactionExists)
	}
	return TransactionResult{}, insertDepositError("store", insertErr)
}

// replayIfProcessed looks for a deposit already stored for providerID. found
// is false when there is none. When there is one, the stored result is
// returned without any Wallet call:
//   - not a deposit, or another amount, currency or withdraw →
//     ErrIdempotencyConflict (409);
//   - FAILED → ErrPreviousAttemptFailed (409);
//   - otherwise (PENDING, COMPLETED, CANCELLED) → its current state and its
//     bet's (202 or 200).
//
// "Same withdraw" is checked through the bet: every withdraw has its own bet,
// and the deposit stores the bet it settles.
func (h *DepositCommandHandler) replayIfProcessed(
	ctx context.Context,
	uow domain.UnitOfWork,
	userID domain.UserID,
	providerID domain.ProviderTransactionID,
	withdrawnID domain.ProviderTransactionID,
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

		if previous.Type != domain.TransactionDeposit ||
			!previous.Amount.Equal(amount) || previous.Currency != currency {
			return domain.ErrIdempotencyConflict
		}
		withdraw, err := repos.Transactions.FindByProviderTransactionID(ctx, userID, withdrawnID)
		if errors.Is(err, domain.ErrTransactionNotFound) {
			return domain.ErrIdempotencyConflict
		}
		if err != nil {
			return err
		}
		if withdraw.Type != domain.TransactionWithdraw || withdraw.BetID != previous.BetID {
			return domain.ErrIdempotencyConflict
		}

		if previous.Status == domain.TransactionFailed {
			return domain.ErrPreviousAttemptFailed
		}
		bet, err := repos.Bets.FindByID(ctx, userID, previous.BetID)
		if err != nil {
			return err
		}
		result = TransactionResult{Transaction: previous, BetStatus: &bet.Status, Replayed: true}
		return nil
	})
	if err != nil {
		return TransactionResult{}, true, fmt.Errorf("deposit: %w", err)
	}
	return result, found, nil
}

// insertDepositError describes a failed insert of a new deposit. The bet is
// still PLACED while another deposit of it is PENDING, so only the unique
// index on active settlements sees that the bet already has a deposit (409).
func insertDepositError(action string, err error) error {
	if errors.Is(err, domain.ErrBetAlreadySettled) {
		return fmt.Errorf("deposit: %w: the bet already has a pending or completed deposit", domain.ErrBetAlreadySettled)
	}
	return fmt.Errorf("deposit: %s: %w", action, err)
}

// complete stores the deposit as COMPLETED and settles its bet (WON or LOST),
// both in one database transaction. If that cannot be stored, the database
// still says PENDING, and so does the result.
func (h *DepositCommandHandler) complete(ctx context.Context, uow domain.UnitOfWork, pending domain.Transaction, bet domain.Bet, newBalance domain.Money) (TransactionResult, error) {
	now := h.clock.Now()
	completed := pending
	if err := completed.Complete(newBalance, now); err != nil {
		return TransactionResult{}, fmt.Errorf("deposit: %w", err)
	}
	settled := bet
	if err := settled.Settle(completed.Amount, now); err != nil {
		return TransactionResult{}, fmt.Errorf("deposit: %w", err)
	}

	err := uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		if err := repos.Bets.Update(ctx, settled); err != nil {
			return err
		}
		return repos.Transactions.Update(ctx, completed)
	})
	if err != nil {
		return h.leavePending(pending, bet, "outcome_not_stored", err), nil
	}

	h.log(slog.LevelInfo, completed, "completed", nil)
	return TransactionResult{Transaction: completed, BetStatus: &settled.Status}, nil
}

// fail stores the deposit as FAILED (the Wallet did not apply it) and returns
// the Wallet's error, which the API maps to 422 or 502. The bet stays PLACED.
func (h *DepositCommandHandler) fail(ctx context.Context, uow domain.UnitOfWork, pending domain.Transaction, bet domain.Bet, walletErr error) (TransactionResult, error) {
	failed := pending
	if err := failed.Fail(h.clock.Now()); err != nil {
		return TransactionResult{}, fmt.Errorf("deposit: %w", err)
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
	return TransactionResult{}, fmt.Errorf("deposit: %w", walletErr)
}

// leavePending returns the deposit as it is stored: PENDING, with its bet
// unchanged (PLACED). Unlike a pending withdraw, the bet exists, so its status
// is reported.
func (h *DepositCommandHandler) leavePending(pending domain.Transaction, bet domain.Bet, outcome string, cause error) TransactionResult {
	level := slog.LevelWarn
	if outcome == "outcome_not_stored" {
		level = slog.LevelError
	}
	h.log(level, pending, outcome, cause)
	return TransactionResult{Transaction: pending, BetStatus: &bet.Status}
}

// log writes the one line per deposit (see logTransaction).
func (h *DepositCommandHandler) log(level slog.Level, tx domain.Transaction, outcome string, cause error) {
	logTransaction(h.logger, level, "deposit", tx, outcome, cause)
}

// findBetToSettle returns the bet of the withdraw referenced by withdrawnID,
// which this deposit can settle:
//   - unknown, another player's, or not a withdraw → ErrTransactionNotFound (404);
//   - in another currency than the deposit → ErrCurrencyMismatch (400);
//   - PENDING or FAILED → ErrWithdrawNotCompleted (409);
//   - its bet already settled or cancelled → ErrBetAlreadySettled (409).
func (h *DepositCommandHandler) findBetToSettle(
	ctx context.Context,
	uow domain.UnitOfWork,
	userID domain.UserID,
	withdrawnID domain.ProviderTransactionID,
	amount domain.Money,
	currency domain.Currency,
) (domain.Bet, error) {
	var bet domain.Bet
	err := uow.Atomic(ctx, func(ctx context.Context, repos domain.Repositories) error {
		withdraw, err := repos.Transactions.FindByProviderTransactionID(ctx, userID, withdrawnID)
		if err != nil {
			return err
		}
		if withdraw.Type != domain.TransactionWithdraw {
			return domain.ErrTransactionNotFound
		}
		if withdraw.Currency != currency {
			return domain.ErrCurrencyMismatch
		}
		if withdraw.Status == domain.TransactionPending || withdraw.Status == domain.TransactionFailed {
			return fmt.Errorf("%w: it is %s", domain.ErrWithdrawNotCompleted, withdraw.Status)
		}

		bet, err = repos.Bets.FindByID(ctx, userID, withdraw.BetID)
		if err != nil {
			return err
		}
		// The bet rule lives in the domain: settle a copy to ask whether the
		// bet can be settled. Nothing is stored here.
		settled := bet
		return settled.Settle(amount, h.clock.Now())
	})
	if err != nil {
		return domain.Bet{}, err
	}
	return bet, nil
}
