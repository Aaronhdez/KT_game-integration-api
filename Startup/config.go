package main

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the settings read from environment variables.
// Each phase adds the fields it needs (database, wallet, JWT...).
type Config struct {
	HTTPPort        int
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
}

const (
	defaultHTTPPort        = "8080"
	defaultLogLevel        = "info"
	defaultShutdownTimeout = "10s"
)

// LoadConfig reads the configuration from environment variables,
// applying defaults for unset values and validating every value.
func LoadConfig() (Config, error) {
	var cfg Config

	port, err := parsePort(getEnv("HTTP_PORT", defaultHTTPPort))
	if err != nil {
		return Config{}, fmt.Errorf("HTTP_PORT: %w", err)
	}
	cfg.HTTPPort = port

	level, err := parseLogLevel(getEnv("LOG_LEVEL", defaultLogLevel))
	if err != nil {
		return Config{}, fmt.Errorf("LOG_LEVEL: %w", err)
	}
	cfg.LogLevel = level

	timeout, err := parsePositiveDuration(getEnv("SHUTDOWN_TIMEOUT", defaultShutdownTimeout))
	if err != nil {
		return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT: %w", err)
	}
	cfg.ShutdownTimeout = timeout

	return cfg, nil
}

// getEnv returns the value of the variable, or fallback if it is unset or empty.
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func parsePort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", value)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("%d is out of range 1-65535", port)
	}
	return port, nil
}

func parseLogLevel(value string) (slog.Level, error) {
	switch strings.ToLower(value) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("%q is not one of debug, info, warn, error", value)
	}
}

func parsePositiveDuration(value string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%q is not a duration (examples: 500ms, 10s, 1m)", value)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", d)
	}
	return d, nil
}
