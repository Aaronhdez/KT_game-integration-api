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
	repository := newBetRepository(pool)
	expected := newTestBet(t, pool, eurPlayerID, "9000000000.09")
	insertBetRow(t, pool, expected)

	result, err := repository.FindByID(testContext(t), eurPlayerID, expected.ID)

	assertBet(t, result, err, expected)
}

func TestBetRepository_FindByID_AnotherPlayersBet_ReturnsBetNotFound(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newBetRepository(pool)
	bet := newTestBet(t, pool, eurPlayerID, "10.50")
	insertBetRow(t, pool, bet)

	_, err := repository.FindByID(testContext(t), usdPlayerID, bet.ID)

	assertErrorIs(t, err, domain.ErrBetNotFound)
}

func TestBetRepository_FindByID_UnknownID_ReturnsBetNotFound(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newBetRepository(pool)
	neverInserted := newTestBet(t, pool, eurPlayerID, "10.50")

	_, err := repository.FindByID(testContext(t), eurPlayerID, neverInserted.ID)

	assertErrorIs(t, err, domain.ErrBetNotFound)
}

func TestBetRepository_Insert_NewBet_StoresEveryField(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newBetRepository(pool)
	bet := newTestBet(t, pool, eurPlayerID, "9000000000.09")

	err := repository.Insert(testContext(t), bet)

	assertStoredBet(t, pool, err, bet)
}

func TestBetRepository_Insert_SameIDTwice_ReturnsError(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newBetRepository(pool)
	bet := newTestBet(t, pool, eurPlayerID, "10.50")
	insertBetRow(t, pool, bet)

	err := repository.Insert(testContext(t), bet)

	assertError(t, err)
}

func TestBetRepository_Insert_UnknownPlayer_ReturnsError(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newBetRepository(pool)
	bet := newTestBet(t, pool, 999, "10.50")

	err := repository.Insert(testContext(t), bet)

	assertError(t, err)
}

func TestBetRepository_NextID_CalledTwice_ReturnsIncreasingIDs(t *testing.T) {
	repository := newBetRepository(migratedTestDatabase(t))
	first := reserveBetID(t, repository)

	result, err := repository.NextID(testContext(t))

	assertBetIDGreaterThan(t, result, err, first)
}

func TestBetRepository_NextID_AfterBetsWereInserted_ReturnsAnUnusedID(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newBetRepository(pool)
	existing := newTestBet(t, pool, eurPlayerID, "10.50")
	insertBetRow(t, pool, existing)

	result, err := repository.NextID(testContext(t))

	assertBetIDGreaterThan(t, result, err, existing.ID)
}

func TestBetRepository_Update_SettledBet_StoresStatusAndUpdatedAt(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newBetRepository(pool)
	bet := newTestBet(t, pool, eurPlayerID, "10.50")
	insertBetRow(t, pool, bet)
	settleBet(t, &bet, "25.00")

	err := repository.Update(testContext(t), bet)

	assertStoredBet(t, pool, err, bet)
}

func TestBetRepository_Update_ChangedStake_IsNotStored(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newBetRepository(pool)
	original := newTestBet(t, pool, eurPlayerID, "10.50")
	insertBetRow(t, pool, original)
	changed := original
	changed.Stake = parseMoney(t, "99.99")

	err := repository.Update(testContext(t), changed)

	assertStoredBet(t, pool, err, original)
}

func TestBetRepository_Update_UnknownBet_ReturnsBetNotFound(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newBetRepository(pool)
	neverInserted := newTestBet(t, pool, eurPlayerID, "10.50")

	err := repository.Update(testContext(t), neverInserted)

	assertErrorIs(t, err, domain.ErrBetNotFound)
}

func TestBetRepository_Update_AnotherPlayersBet_ReturnsBetNotFound(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newBetRepository(pool)
	bet := newTestBet(t, pool, eurPlayerID, "10.50")
	insertBetRow(t, pool, bet)
	asAnotherPlayer := bet
	asAnotherPlayer.UserID = usdPlayerID

	err := repository.Update(testContext(t), asAnotherPlayer)

	assertErrorIs(t, err, domain.ErrBetNotFound)
}

func TestBetRepository_Update_AnotherPlayersBet_LeavesTheBetUnchanged(t *testing.T) {
	pool := migratedTestDatabase(t)
	repository := newBetRepository(pool)
	original := newTestBet(t, pool, eurPlayerID, "10.50")
	insertBetRow(t, pool, original)
	asAnotherPlayer := original
	asAnotherPlayer.UserID = usdPlayerID
	settleBet(t, &asAnotherPlayer, "25.00")

	_ = repository.Update(testContext(t), asAnotherPlayer)

	assertStoredBet(t, pool, nil, original)
}

/////////////////////////////////////////////////////////////////////////////////////

// settleBet settles the bet in memory, one second after its creation, as a
// use case would before calling Update.
func settleBet(t *testing.T, bet *domain.Bet, amount string) {
	t.Helper()
	if err := bet.Settle(parseMoney(t, amount), bet.CreatedAt.Add(time.Second)); err != nil {
		t.Fatalf("settle bet %d: %v", bet.ID, err)
	}
}

func parseMoney(t *testing.T, value string) domain.Money {
	t.Helper()
	amount, err := domain.ParseMoney(value)
	if err != nil {
		t.Fatalf("parse money %q: %v", value, err)
	}
	return amount
}

// reserveBetID calls the repository's NextID as setup, failing the test on error.
func reserveBetID(t *testing.T, repository *betRepository) domain.BetID {
	t.Helper()
	id, err := repository.NextID(testContext(t))
	if err != nil {
		t.Fatalf("reserve bet id: %v", err)
	}
	return id
}

// assertBetIDGreaterThan checks only the order, not "+1": a sequence can skip
// numbers (nextval is never rolled back) and other tests consume IDs too.
func assertBetIDGreaterThan(t *testing.T, result domain.BetID, err error, previous domain.BetID) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result <= previous {
		t.Errorf("expected a bet id greater than %d, got %d", previous, result)
	}
}

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

// readBetRow reads a bet with plain SQL, independently of the repository under test.
func readBetRow(t *testing.T, pool *pgxpool.Pool, id domain.BetID) domain.Bet {
	t.Helper()
	var (
		userID    int64
		currency  string
		stakeText string
		status    string
		createdAt time.Time
		updatedAt time.Time
	)
	err := pool.QueryRow(testContext(t), `
		SELECT wallet_user_id, currency, stake::text, status, created_at, updated_at
		FROM bets WHERE id = $1`, int64(id)).
		Scan(&userID, &currency, &stakeText, &status, &createdAt, &updatedAt)
	if err != nil {
		t.Fatalf("read bet %d: %v", id, err)
	}
	stake, err := domain.ParseMoney(stakeText)
	if err != nil {
		t.Fatalf("parse stake %q of bet %d: %v", stakeText, id, err)
	}
	return domain.Bet{
		ID:        id,
		UserID:    domain.UserID(userID),
		Currency:  domain.Currency(currency),
		Stake:     stake,
		Status:    domain.BetStatus(status),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}

// assertStoredBet checks that the write succeeded and that the row in the
// database, read with plain SQL, equals expected.
func assertStoredBet(t *testing.T, pool *pgxpool.Pool, err error, expected domain.Bet) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := readBetRow(t, pool, expected.ID)
	assertBet(t, result, nil, expected)
}

func assertError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("expected an error, got nil")
	}
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
