package domaintest

import (
	"context"
	"errors"
	"testing"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

var errDatabaseDown = errors.New("database down")

func TestFakePlayerRepository_FindByUsername_ReturnsThePlayer(t *testing.T) {
	repository := NewFakePlayerRepository(givenAPlayer())

	result, err := repository.FindByUsername(context.Background(), "player_usd")

	assertPlayerID(t, result, err, testUserID)
}

func TestFakePlayerRepository_FindByUsername_Unknown_IsPlayerNotFound(t *testing.T) {
	repository := NewFakePlayerRepository(givenAPlayer())

	_, err := repository.FindByUsername(context.Background(), "nobody")

	assertErrorIs(t, err, domain.ErrPlayerNotFound)
}

func TestFakePlayerRepository_FindByID_ReturnsThePlayer(t *testing.T) {
	repository := NewFakePlayerRepository(givenAPlayer())

	result, err := repository.FindByID(context.Background(), testUserID)

	assertPlayerID(t, result, err, testUserID)
}

func TestFakePlayerRepository_FindByID_Unknown_IsPlayerNotFound(t *testing.T) {
	repository := NewFakePlayerRepository(givenAPlayer())

	_, err := repository.FindByID(context.Background(), 12345)

	assertErrorIs(t, err, domain.ErrPlayerNotFound)
}

func TestFakePlayerRepository_WithErr_EveryCallFailsWithIt(t *testing.T) {
	repository := NewFakePlayerRepository(givenAPlayer())
	repository.Err = errDatabaseDown

	_, err := repository.FindByUsername(context.Background(), "player_usd")

	assertErrorIs(t, err, errDatabaseDown)
}

/////////////////////////////////////////////////////////////////////////////////////

func givenAPlayer() domain.Player {
	return domain.Player{
		UserID:       testUserID,
		Username:     "player_usd",
		PasswordHash: FakeHash("usd-pass"),
		Currency:     domain.USD,
	}
}

func assertPlayerID(t *testing.T, result domain.Player, err error, want domain.UserID) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.UserID != want {
		t.Errorf("expected player %d, got %d", want, result.UserID)
	}
}
