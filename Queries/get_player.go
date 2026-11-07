package queries

import (
	"context"
	"fmt"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// GetPlayerQuery asks for the account data and live balance of one player.
// UserID comes from the verified token, never from the request.
type GetPlayerQuery struct {
	UserID domain.UserID
}

// PlayerResult is the player's information with the balance read from the
// Wallet at request time (§8.2).
type PlayerResult struct {
	UserID   domain.UserID
	Balance  domain.Money
	Currency domain.Currency
}

// GetPlayerQueryHandler reads a player and their current Wallet balance.
type GetPlayerQueryHandler struct {
	players domain.PlayerRepository
	wallet  domain.WalletClient
}

// NewGetPlayerQueryHandler creates a GetPlayerQueryHandler with its dependencies.
func NewGetPlayerQueryHandler(players domain.PlayerRepository, wallet domain.WalletClient) *GetPlayerQueryHandler {
	return &GetPlayerQueryHandler{
		players: players,
		wallet:  wallet,
	}
}

// Execute handles the GetPlayerQuery by fetching the player and their current Wallet balance.
func (handler *GetPlayerQueryHandler) Execute(ctx context.Context, query GetPlayerQuery) (PlayerResult, error) {
	player, err := handler.players.FindByID(ctx, query.UserID)
	if err != nil {
		return PlayerResult{}, fmt.Errorf("get player: %w", err)
	}

	balance, err := handler.wallet.Balance(ctx, player.UserID)
	if err != nil {
		return PlayerResult{}, fmt.Errorf("get player: %w", err)
	}

	if balance.Currency != player.Currency {
		return PlayerResult{}, fmt.Errorf("get player: wallet currency %s, account currency %s: %w",
			balance.Currency, player.Currency, domain.ErrWalletFailure)
	}

	return PlayerResult{
		UserID:   player.UserID,
		Balance:  balance.Amount,
		Currency: balance.Currency,
	}, nil
}
