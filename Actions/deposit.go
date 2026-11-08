package actions

import (
	"context"
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

	// The deposit is stored and sent to the Wallet in the next steps.
	_, _, _ = providerID, bet, oldBalance
	return TransactionResult{}, nil
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
