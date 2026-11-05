package main

import (
	"log/slog"
	"os"
)

func main() {
	if err := run(); err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	slog.Info("starting game integration api")
	return nil
}
