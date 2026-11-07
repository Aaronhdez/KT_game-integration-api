//go:build integration

package persistence

import (
	"errors"
	"testing"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// The tests read the seeded players (00004_seed_players.sql).

func TestPlayerRepository_FindByUsername_KnownUser_ReturnsThePlayer(t *testing.T) {
	repository := NewPlayerRepository(migratedTestDatabase(t))

	result, err := repository.FindByUsername(testContext(t), "player_eur")

	assertPlayer(t, result, err, domain.Player{
		UserID: 34679664254, Username: "player_eur", Currency: domain.EUR,
	})
}

func TestPlayerRepository_FindByUsername_UnknownUser_ReturnsPlayerNotFound(t *testing.T) {
	repository := NewPlayerRepository(migratedTestDatabase(t))

	_, err := repository.FindByUsername(testContext(t), "unknown_user")

	assertErrorIs(t, err, domain.ErrPlayerNotFound)
}

func TestPlayerRepository_FindByUsername_DifferentCase_ReturnsPlayerNotFound(t *testing.T) {
	repository := NewPlayerRepository(migratedTestDatabase(t))

	_, err := repository.FindByUsername(testContext(t), "PLAYER_EUR")

	assertErrorIs(t, err, domain.ErrPlayerNotFound)
}

func TestPlayerRepository_FindByID_KnownID_ReturnsThePlayer(t *testing.T) {
	repository := NewPlayerRepository(migratedTestDatabase(t))

	result, err := repository.FindByID(testContext(t), 34633089486)

	assertPlayer(t, result, err, domain.Player{
		UserID: 34633089486, Username: "player_usd", Currency: domain.USD,
	})
}

func TestPlayerRepository_FindByID_UnknownID_ReturnsPlayerNotFound(t *testing.T) {
	repository := NewPlayerRepository(migratedTestDatabase(t))

	_, err := repository.FindByID(testContext(t), 999)

	assertErrorIs(t, err, domain.ErrPlayerNotFound)
}

/////////////////////////////////////////////////////////////////////////////////////

func assertPlayer(t *testing.T, result domain.Player, err error, expected domain.Player) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.UserID != expected.UserID || result.Username != expected.Username || result.Currency != expected.Currency {
		t.Errorf("expected %d/%s/%s, got %d/%s/%s",
			expected.UserID, expected.Username, expected.Currency,
			result.UserID, result.Username, result.Currency)
	}
	if result.PasswordHash == "" {
		t.Errorf("expected a password hash, got an empty one")
	}
}

func assertErrorIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("expected error %q, got %v", target, err)
	}
}
