package main

import (
	"fmt"
	"log/slog"
	"net/url"
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
	DatabaseURL     string // contains the database password: never log it

	WalletBaseURL          string        // absolute http(s) URL, without trailing slash
	WalletAPIKey           string        // secret: never log it
	WalletOperationTimeout time.Duration // per withdraw/deposit call
	WalletBalanceTimeout   time.Duration // per balance read attempt

	JWTSecret string        // HMAC key that signs the tokens; secret: never log it
	JWTTTL    time.Duration // lifetime of an issued token

	PendingResolverInterval time.Duration // time between two runs of the pending resolver
}

const (
	defaultHTTPPort        = "8080"
	defaultLogLevel        = "info"
	defaultShutdownTimeout = "10s"

	defaultWalletBaseURL = "http://wallet:8000"
	// The Wallet never answers a withdraw/deposit after ~5 s, but a balance
	// read can take up to ~15 s (docs/wallet-findings.md, Q7).
	defaultWalletOperationTimeout = "6s"
	defaultWalletBalanceTimeout   = "10s"

	defaultJWTTTL = "1h"

	// A PENDING transaction is re-sent at least 10 s after its last attempt
	// (Actions, resendDelay), so checking every 10 s resolves it within ~20 s.
	defaultPendingResolverInterval = "10s"
	// RFC 7518 §3.2: an HS256 key must be at least 256 bits long.
	minJWTSecretBytes = 32
)

// LoadConfig reads the configuration from environment variables,
// applying defaults for unset values and validating every value.
// Variables without a default (DATABASE_URL, WALLET_API_KEY, JWT_SECRET) are required.
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

	databaseURL, err := requireEnv("DATABASE_URL")
	if err != nil {
		return Config{}, err
	}
	cfg.DatabaseURL = databaseURL

	walletBaseURL, err := parseBaseURL(getEnv("WALLET_BASE_URL", defaultWalletBaseURL))
	if err != nil {
		return Config{}, fmt.Errorf("WALLET_BASE_URL: %w", err)
	}
	cfg.WalletBaseURL = walletBaseURL

	walletAPIKey, err := requireEnv("WALLET_API_KEY")
	if err != nil {
		return Config{}, err
	}
	cfg.WalletAPIKey = walletAPIKey

	operationTimeout, err := parsePositiveDuration(getEnv("WALLET_OPERATION_TIMEOUT", defaultWalletOperationTimeout))
	if err != nil {
		return Config{}, fmt.Errorf("WALLET_OPERATION_TIMEOUT: %w", err)
	}
	cfg.WalletOperationTimeout = operationTimeout

	balanceTimeout, err := parsePositiveDuration(getEnv("WALLET_BALANCE_TIMEOUT", defaultWalletBalanceTimeout))
	if err != nil {
		return Config{}, fmt.Errorf("WALLET_BALANCE_TIMEOUT: %w", err)
	}
	cfg.WalletBalanceTimeout = balanceTimeout

	jwtSecret, err := parseJWTSecret()
	if err != nil {
		return Config{}, err
	}
	cfg.JWTSecret = jwtSecret

	jwtTTL, err := parsePositiveDuration(getEnv("JWT_TTL", defaultJWTTTL))
	if err != nil {
		return Config{}, fmt.Errorf("JWT_TTL: %w", err)
	}
	cfg.JWTTTL = jwtTTL

	resolverInterval, err := parsePositiveDuration(getEnv("PENDING_RESOLVER_INTERVAL", defaultPendingResolverInterval))
	if err != nil {
		return Config{}, fmt.Errorf("PENDING_RESOLVER_INTERVAL: %w", err)
	}
	cfg.PendingResolverInterval = resolverInterval

	return cfg, nil
}

// parseJWTSecret reads the required JWT_SECRET and checks its length.
// The error never contains the value: it is a secret.
func parseJWTSecret() (string, error) {
	secret, err := requireEnv("JWT_SECRET")
	if err != nil {
		return "", err
	}
	if len(secret) < minJWTSecretBytes {
		return "", fmt.Errorf("JWT_SECRET must be at least %d bytes long, got %d", minJWTSecretBytes, len(secret))
	}
	return secret, nil
}

// parseBaseURL accepts an absolute http or https URL and removes any trailing
// slash, so paths can be appended as baseURL + "/api/v1/...".
func parseBaseURL(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("%q is not a valid URL", value)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%q must start with http:// or https://", value)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("%q has no host", value)
	}
	return strings.TrimRight(value, "/"), nil
}

// requireEnv returns the value of a variable that has no default.
func requireEnv(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
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
