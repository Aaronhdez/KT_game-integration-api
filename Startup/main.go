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

	"github.com/jackc/pgx/v5/pgxpool"

	persistence "github.com/aaronhdez/kt_game-integration-api/Persistence"
)

const (
	databaseConnectTimeout = 10 * time.Second
	databaseMigrateTimeout = 30 * time.Second
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

	pool, err := connectDatabase(cfg.DatabaseURL, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := migrateDatabase(pool, logger); err != nil {
		return err
	}

	server := newHTTPServer(cfg, NewRouter(logger))

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

// connectDatabase opens the PostgreSQL pool, giving up after
// databaseConnectTimeout so a missing database stops the service quickly.
func connectDatabase(databaseURL string, logger *slog.Logger) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), databaseConnectTimeout)
	defer cancel()

	pool, err := persistence.Connect(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	// Host and database name only: the URL contains the password.
	connConfig := pool.Config().ConnConfig
	logger.Info("database connected", "host", connConfig.Host, "database", connConfig.Database)
	return pool, nil
}

// migrateDatabase brings the schema up to date before the service accepts
// requests, so the code never runs against an older schema.
func migrateDatabase(pool *pgxpool.Pool, logger *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), databaseMigrateTimeout)
	defer cancel()

	applied, err := persistence.Migrate(ctx, pool)
	if err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	logger.Info("database migrations applied", "applied_versions", applied)
	return nil
}

func newHTTPServer(cfg Config, router http.Handler) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           router,
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
