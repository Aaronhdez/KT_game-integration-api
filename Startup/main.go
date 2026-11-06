package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
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
	var server *http.Server
	server = &http.Server{
		Addr:              ":8080",
		Handler:           NewRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()
	select {
	case err := <-serverErrors:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http server shutdown: %w", err)
	}
	logger.Info("http server stopped")
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
