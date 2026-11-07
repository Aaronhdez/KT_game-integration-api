//go:build integration

package persistence

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type seededPlayer struct {
	userID   int64
	username string
	currency string
}

// The user IDs and currencies must match the Wallet mock (docs/wallet-findings.md, Q10).
var expectedSeededPlayers = []seededPlayer{
	{userID: 34616761765, username: "player_kes", currency: "KES"},
	{userID: 34633089486, username: "player_usd", currency: "USD"},
	{userID: 34673635133, username: "player_usd_2", currency: "USD"},
	{userID: 34679664254, username: "player_eur", currency: "EUR"},
}

func TestSeed_MigratedDatabase_ContainsTheFourWalletPlayers(t *testing.T) {
	pool := migratedTestDatabase(t)

	result := readSeededPlayers(t, pool)

	assertSeededPlayers(t, result, expectedSeededPlayers)
}

func TestSeed_MigratedDatabase_StoresBcryptHashesOnly(t *testing.T) {
	pool := migratedTestDatabase(t)

	var result int
	err := pool.QueryRow(testContext(t), `
		SELECT count(*) FROM players
		WHERE wallet_user_id <> $1 AND password_hash NOT LIKE '$2a$10$%'`, testPlayerID).Scan(&result)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 0 {
		t.Errorf("expected every seeded password to be a bcrypt hash, %d are not", result)
	}
}

/////////////////////////////////////////////////////////////////////////////////////

func readSeededPlayers(t *testing.T, pool *pgxpool.Pool) []seededPlayer {
	t.Helper()
	rows, err := pool.Query(testContext(t), `
		SELECT wallet_user_id, username, currency FROM players
		WHERE wallet_user_id <> $1
		ORDER BY wallet_user_id`, testPlayerID)
	if err != nil {
		t.Fatalf("query players: %v", err)
	}
	defer rows.Close()

	var players []seededPlayer
	for rows.Next() {
		var player seededPlayer
		if err := rows.Scan(&player.userID, &player.username, &player.currency); err != nil {
			t.Fatalf("scan player: %v", err)
		}
		players = append(players, player)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read players: %v", err)
	}
	return players
}

func assertSeededPlayers(t *testing.T, got, want []seededPlayer) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d players, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("player %d: expected %+v, got %+v", i, want[i], got[i])
		}
	}
}
