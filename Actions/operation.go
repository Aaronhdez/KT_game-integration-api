package actions

import (
	"context"
	"fmt"
	"log/slog"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// Helpers shared by the money operations (Withdraw, Deposit, Cancel).

// readOldBalance reads the player's balance from the Wallet before an
// operation. Any failure, a timeout included, is ErrWalletFailure:
// no operation has been sent yet, so we know nothing was applied.
func readOldBalance(ctx context.Context, wallet domain.WalletClient, player domain.Player) (domain.Money, error) {
	balance, err := wallet.Balance(ctx, player.UserID)
	if err != nil {
		return domain.Money{}, fmt.Errorf("read old balance: %v: %w", err, domain.ErrWalletFailure)
	}
	if balance.Currency != player.Currency {
		return domain.Money{}, fmt.Errorf("wallet currency %s, account currency %s: %w", balance.Currency, player.Currency, domain.ErrWalletFailure)
	}
	return balance.Amount, nil
}

// logTransaction writes the one line per operation.
// Only identifiers, the amount and the outcome.
func logTransaction(logger *slog.Logger, level slog.Level, operation string, tx domain.Transaction, outcome string, cause error) {
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
	logger.Log(context.Background(), level, operation, attributes...)
}
