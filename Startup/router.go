package main

import (
	"log/slog"
	"net/http"

	application "github.com/aaronhdez/kt_game-integration-api/Application"
)

// Handlers holds the HTTP handler of each endpoint. Startup builds them with
// their real dependencies; router tests pass stubs. Each phase adds the
// endpoints it implements.
type Handlers struct {
	Login  http.Handler
	Player http.Handler // already wrapped in RequireAuth by newHandlers
}

func NewRouter(logger *slog.Logger, handlers Handlers) http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("GET /health", application.Health)
	router.Handle("POST /api/v1/auth/login", handlers.Login)
	router.Handle("GET /api/v1/player", handlers.Player)
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		application.WriteError(w, http.StatusNotFound, application.CodeNotFound, "resource not found")
	})
	return application.RequestLogger(logger, router)
}
