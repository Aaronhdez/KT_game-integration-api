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
	server := newHTTPServer(cfg, NewRouter())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErrors := startServer(server, logger)
	if err := waitForShutdownSignal(ctx, serverErrors, logger); err != nil {
		return err
	}
	return shutdownServer(server, cfg.ShutdownTimeout, logger)
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

func newHTTPServer(cfg Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func startServer(server *http.Server, logger *slog.Logger) <-chan error {
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("http server starting", "addr", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()
	return serverErrors
}

func waitForShutdownSignal(ctx context.Context, serverErrors <-chan error, logger *slog.Logger) error {
	select {
	case err := <-serverErrors:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		logger.Info("shutdown signal received")
		return nil
	}
}

func shutdownServer(server *http.Server, timeout time.Duration, logger *slog.Logger) error {
	// Not the signal ctx: it is already cancelled, so Shutdown would not wait at all.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("http server shutdown: %w", err)
	}
	logger.Info("http server stopped")
	return nil
}
