package persistence

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

const playerColumns = "wallet_user_id, username, password_hash, currency"

var _ domain.PlayerRepository = (*PlayerRepository)(nil)

// PlayerRepository reads the seeded players from PostgreSQL.
type PlayerRepository struct {
	db querier
}

func NewPlayerRepository(pool *pgxpool.Pool) *PlayerRepository {
	return &PlayerRepository{
		db: pool,
	}
}

func (r *PlayerRepository) FindByUsername(ctx context.Context, username string) (domain.Player, error) {
	row := r.db.QueryRow(ctx,
		"SELECT "+playerColumns+" FROM players WHERE username = $1",
		username)
	player, err := scanPlayer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Player{}, domain.ErrPlayerNotFound
	}
	if err != nil {
		return domain.Player{}, fmt.Errorf("find player by username: %w", err)
	}
	return player, nil
}

func (r *PlayerRepository) FindByID(ctx context.Context, userID domain.UserID) (domain.Player, error) {
	row := r.db.QueryRow(ctx,
		"SELECT "+playerColumns+" FROM players WHERE wallet_user_id = $1",
		int64(userID))

	player, err := scanPlayer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Player{}, domain.ErrPlayerNotFound
	}
	if err != nil {
		return domain.Player{}, fmt.Errorf("find player by id: %w", err)
	}
	return player, nil
}

func scanPlayer(row pgx.Row) (domain.Player, error) {
	var (
		userID       int64
		username     string
		passwordHash string
		currencyCode string
	)
	if err := row.Scan(&userID, &username, &passwordHash, &currencyCode); err != nil {
		return domain.Player{}, err
	}

	currency, err := domain.ParseCurrency(currencyCode)
	if err != nil {
		return domain.Player{}, fmt.Errorf("player %d: %w", userID, err)
	}

	return domain.Player{
		UserID:       domain.UserID(userID),
		Username:     username,
		PasswordHash: passwordHash,
		Currency:     currency,
	}, nil
}
