package main

import (
	"fmt"
	"log/slog"
	"os"
)

func main() {
	if err := run(); err != nil {
		// run failed, maybe before its own logger existed (e.g. invalid config),
		// so report the error with a logger that has a fixed level.
		NewLogger(os.Stdout, slog.LevelInfo).Error("startup failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := setupLogging(cfg)
	logger.Info("logger initialized")

	return nil
}

func setupLogging(cfg Config) *slog.Logger {
	logger := NewLogger(os.Stdout, cfg.LogLevel)
	logger.Info("starting game integration api",
		"http_port", cfg.HTTPPort,
		"log_level", cfg.LogLevel.String(),
		"shutdown_timeout", cfg.ShutdownTimeout.String(),
	)
	logger.Debug("debug logging enabled")
	return logger
}
