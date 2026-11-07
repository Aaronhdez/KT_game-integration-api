//go:build integration

package persistence

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

const (
	eurPlayerID domain.UserID = 34679664254
	usdPlayerID domain.UserID = 34633089486
)

func TestBetRepository_FindByID_ExistingBet_ReturnsTheBet(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := NewBetRepository(pool)
	expected := newTestBet(t, pool, eurPlayerID, "9000000000.09")
	insertBetRow(t, pool, expected)

	result, err := repository.FindByID(testContext(t), eurPlayerID, expected.ID)

	assertBet(t, result, err, expected)
}

func TestBetRepository_FindByID_AnotherPlayersBet_ReturnsBetNotFound(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := NewBetRepository(pool)
	bet := newTestBet(t, pool, eurPlayerID, "10.50")
	insertBetRow(t, pool, bet)

	_, err := repository.FindByID(testContext(t), usdPlayerID, bet.ID)

	assertErrorIs(t, err, domain.ErrBetNotFound)
}

func TestBetRepository_FindByID_UnknownID_ReturnsBetNotFound(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := NewBetRepository(pool)
	neverInserted := newTestBet(t, pool, eurPlayerID, "10.50")

	_, err := repository.FindByID(testContext(t), eurPlayerID, neverInserted.ID)

	assertErrorIs(t, err, domain.ErrBetNotFound)
}

/////////////////////////////////////////////////////////////////////////////////////

// testNow is "now" at the precision PostgreSQL stores (microseconds), so a
// time written and read back compares equal.
func testNow() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

// newTestBet builds a PLACED bet with a fresh ID from bet_id_seq, so tests
// never collide with each other's bets.
func newTestBet(t *testing.T, pool *pgxpool.Pool, userID domain.UserID, stake string) domain.Bet {
	t.Helper()
	var id int64
	if err := pool.QueryRow(testContext(t), "SELECT nextval('bet_id_seq')").Scan(&id); err != nil {
		t.Fatalf("reserve bet id: %v", err)
	}
	amount, err := domain.ParseMoney(stake)
	if err != nil {
		t.Fatalf("parse stake %q: %v", stake, err)
	}
	return domain.NewPlacedBet(domain.BetID(id), userID, domain.EUR, amount, testNow())
}

// insertBetRow writes a bet with plain SQL, independently of the repository under test.
func insertBetRow(t *testing.T, pool *pgxpool.Pool, bet domain.Bet) {
	t.Helper()
	execSQL(t, pool, `
		INSERT INTO bets (id, wallet_user_id, currency, stake, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		int64(bet.ID), int64(bet.UserID), string(bet.Currency), bet.Stake.String(),
		string(bet.Status), bet.CreatedAt, bet.UpdatedAt)
}

// assertBet compares field by field: Money and time.Time need Equal, not ==
func assertBet(t *testing.T, result domain.Bet, err error, expected domain.Bet) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != expected.ID || result.UserID != expected.UserID ||
		result.Currency != expected.Currency || result.Status != expected.Status {
		t.Errorf("expected bet %d/%d/%s/%s, got %d/%d/%s/%s",
			expected.ID, expected.UserID, expected.Currency, expected.Status,
			result.ID, result.UserID, result.Currency, result.Status)
	}
	if !result.Stake.Equal(expected.Stake) {
		t.Errorf("expected stake %s, got %s", expected.Stake, result.Stake)
	}
	if !result.CreatedAt.Equal(expected.CreatedAt) || !result.UpdatedAt.Equal(expected.UpdatedAt) {
		t.Errorf("expected times %v/%v, got %v/%v",
			expected.CreatedAt, expected.UpdatedAt, result.CreatedAt, result.UpdatedAt)
	}
}
