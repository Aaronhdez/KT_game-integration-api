package domaintest

import (
	"context"
	"sync"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// FakePlayerRepository is an in-memory domain.PlayerRepository.
// Set Err to make every call fail with it (e.g. to simulate the database
// being down). It is safe for concurrent use.
type FakePlayerRepository struct {
	Err error

	mutex   sync.Mutex
	players map[domain.UserID]domain.Player
}

var _ domain.PlayerRepository = (*FakePlayerRepository)(nil)

// NewFakePlayerRepository returns a repository holding the given players.
func NewFakePlayerRepository(players ...domain.Player) *FakePlayerRepository {
	repository := &FakePlayerRepository{
		players: make(map[domain.UserID]domain.Player),
	}
	for _, player := range players {
		repository.players[player.UserID] = player
	}
	return repository
}

func (repository *FakePlayerRepository) FindByUsername(_ context.Context, username string) (domain.Player, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()

	if repository.Err != nil {
		return domain.Player{}, repository.Err
	}
	for _, player := range repository.players {
		if player.Username == username {
			return player, nil
		}
	}
	return domain.Player{}, domain.ErrPlayerNotFound
}

func (repository *FakePlayerRepository) FindByID(_ context.Context, userID domain.UserID) (domain.Player, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()

	if repository.Err != nil {
		return domain.Player{}, repository.Err
	}
	player, found := repository.players[userID]
	if !found {
		return domain.Player{}, domain.ErrPlayerNotFound
	}
	return player, nil
}
