package application

import (
	"log/slog"
	"net/http"
	"time"

	actions "github.com/aaronhdez/kt_game-integration-api/Actions"
)

// loginRequest represents the expected structure of the login request payload.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// loginResponse represents the structure of the response returned after a successful login.
type loginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}

// LoginController handles the login HTTP requests.
type LoginController struct {
	login  *actions.LoginCommandHandler
	logger *slog.Logger
}

// NewLoginController creates a new instance of LoginController with the provided login command handler and logger.
func NewLoginController(login *actions.LoginCommandHandler, logger *slog.Logger) *LoginController {
	return &LoginController{
		login:  login,
		logger: logger,
	}
}

// ServeHTTP handles the HTTP request for the login endpoint.
func (c *LoginController) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDomainError(w, c.logger, err)
		return
	}
	result, err := c.login.Execute(r.Context(), actions.LoginCommand{
		Username: request.Username,
		Password: request.Password,
	})
	if err != nil {
		writeDomainError(w, c.logger, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, loginResponse{Token: result.Token, ExpiresAt: result.ExpiresAt.UTC().Format(time.RFC3339)})
}
