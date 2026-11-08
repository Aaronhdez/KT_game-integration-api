package actions

import (
	"context"
	"errors"
	"fmt"
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// dummyPasswordHash is verified when the username does not exist, so an
// unknown user takes as long to reject as a wrong password (bcrypt cost 10,
// like the seed). It is the hash of a random, discarded password.
const dummyPasswordHash = "$2a$10$PxTFhVic9gtZt5tDJRa.S.DPLyFavtUpBM7LVC/KnoqyL2BgfEKqm"

// LoginCommand represents the data required to perform a login operation.
type LoginCommand struct {
	Username string
	Password string
}

// LoginResult represents the result of a successful login, including the issued token and its expiration time.
type LoginResult struct {
	Token     string
	ExpiresAt time.Time
}

// LoginCommandHandler handles the login process, including verifying credentials and issuing tokens.
type LoginCommandHandler struct {
	players domain.PlayerRepository
	hasher  domain.PasswordHasher
	tokens  domain.TokenIssuer
	clock   domain.Clock
}

// NewLoginCommandHandler creates a new instance of LoginCommandHandler with the provided dependencies.
func NewLoginCommandHandler(players domain.PlayerRepository, hasher domain.PasswordHasher, tokens domain.TokenIssuer, clock domain.Clock) *LoginCommandHandler {
	return &LoginCommandHandler{
		players: players,
		hasher:  hasher,
		tokens:  tokens,
		clock:   clock,
	}
}

// Execute handles the login command, verifying credentials and issuing a token.
func (handler *LoginCommandHandler) Execute(ctx context.Context, command LoginCommand) (LoginResult, error) {
	if command.Username == "" || command.Password == "" {
		return LoginResult{}, domain.ErrMissingCredentials
	}
	player, err := handler.players.FindByUsername(ctx, command.Username)
	if errors.Is(err, domain.ErrPlayerNotFound) {
		_ = handler.hasher.Verify(dummyPasswordHash, command.Password)
		return LoginResult{}, domain.ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, fmt.Errorf("login: %w", err)
	}

	if err := handler.hasher.Verify(player.PasswordHash, command.Password); err != nil {
		return LoginResult{}, fmt.Errorf("login: %w", err)
	}

	issued, err := handler.tokens.Issue(player.UserID, handler.clock.Now())
	if err != nil {
		return LoginResult{}, fmt.Errorf("login: %w", err)
	}

	return LoginResult{
		Token:     issued.Value,
		ExpiresAt: issued.ExpiresAt,
	}, nil
}
