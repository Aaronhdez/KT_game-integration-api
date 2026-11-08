package actions

import (
	"context"
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
	balance, err := h.wallet.Balance(ctx, player.UserID)
	if err != nil {
		return TransactionResult{}, fmt.Errorf("withdraw: read old balance: %v: %w", err, domain.ErrWalletFailure)
	}
	if balance.Currency != player.Currency {
		return TransactionResult{}, fmt.Errorf("withdraw: wallet currency %s, account currency %s: %w", balance.Currency, player.Currency, domain.ErrWalletFailure)
	}

	// The old balance becomes part of the PENDING transaction in the next step.
	_ = balance
	return TransactionResult{}, nil
}
