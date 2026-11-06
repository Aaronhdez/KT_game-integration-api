package main

import (
	"log/slog"
	"net/http"

	application "github.com/aaronhdez/game-integration-api/Application"
)

func NewRouter(logger *slog.Logger) http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("GET /health", application.Health)
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		application.WriteError(w, http.StatusNotFound, application.CodeNotFound, "resource not found")
	})
	return application.RequestLogger(logger, router)
}
