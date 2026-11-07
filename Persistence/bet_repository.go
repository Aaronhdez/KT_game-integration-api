package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

const betColumns = "id, wallet_user_id, currency, stake::text, status, created_at, updated_at"

var _ domain.BetRepository = (*BetRepository)(nil)

// BetRepository reads the seeded bets from PostgreSQL.
type BetRepository struct {
	db querier
}

func NewBetRepository(pool *pgxpool.Pool) *BetRepository {
	return &BetRepository{
		db: pool,
	}
}

func (r *BetRepository) FindByID(ctx context.Context, userID domain.UserID, id domain.BetID) (domain.Bet, error) {
	query := "SELECT " + betColumns + " FROM bets WHERE id = $1 AND wallet_user_id = $2"

	bet, err := scanBet(r.db.QueryRow(ctx, query, int64(id), int64(userID)))

	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Bet{}, domain.ErrBetNotFound
	}

	if err != nil {
		return domain.Bet{}, err
	}
	return bet, nil
}

func (r *BetRepository) Insert(ctx context.Context, bet domain.Bet) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO bets (id, wallet_user_id, currency, stake, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		int64(bet.ID),
		int64(bet.UserID),
		string(bet.Currency),
		bet.Stake.String(),
		string(bet.Status),
		bet.CreatedAt,
		bet.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert bet %d: %w", bet.ID, err)
	}
	return nil
}

func (r *BetRepository) NextID(ctx context.Context) (domain.BetID, error) {
	var id int64
	if err := r.db.QueryRow(ctx, "SELECT nextval('bet_id_seq')").Scan(&id); err != nil {
		return 0, fmt.Errorf("next bet id: %w", err)
	}
	return domain.BetID(id), nil
}

func scanBet(row pgx.Row) (domain.Bet, error) {
	var (
		id           int64
		userID       int64
		currencyCode string
		stakeText    string
		status       string
		createdAt    time.Time
		updatedAt    time.Time
	)
	if err := row.Scan(&id, &userID, &currencyCode, &stakeText, &status, &createdAt, &updatedAt); err != nil {
		return domain.Bet{}, err
	}

	currency, err := domain.ParseCurrency(currencyCode)
	if err != nil {
		return domain.Bet{}, fmt.Errorf("bet %d: %w", id, err)
	}

	stake, err := domain.ParseMoney(stakeText)
	if err != nil {
		return domain.Bet{}, fmt.Errorf("bet %d: %w", id, err)
	}

	return domain.Bet{
		ID:        domain.BetID(id),
		UserID:    domain.UserID(userID),
		Currency:  currency,
		Stake:     stake,
		Status:    domain.BetStatus(status),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}
