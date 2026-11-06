package main

import (
	"net/http"

	application "github.com/aaronhdez/game-integration-api/Application"
)

func NewRouter() http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("GET /health", application.Health)
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		application.WriteError(w, http.StatusNotFound, application.CodeNotFound, "resource not found")
	})
	return router
}
