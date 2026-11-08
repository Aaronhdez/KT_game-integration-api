package main

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

const (
	testDatabaseURL  = "postgres://user:secret@localhost:5432/db"
	testWalletAPIKey = "test-wallet-key"
	testJWTSecret    = "test-jwt-secret-of-at-least-32-bytes"
)

func TestLoadConfig_OnlyRequiredVariablesSet_UsesDefaults(t *testing.T) {
	setEnv(t, map[string]string{})

	result, err := LoadConfig()

	assertConfigEqual(t, result, err, expectedDefaultConfig())
}

func TestLoadConfig_ValidValues_AreUsed(t *testing.T) {
	testCases := []struct {
		name   string
		env    map[string]string
		adjust func(want *Config) // changes the defaults into the expected config
	}{
		{
			name:   "custom HTTP port",
			env:    map[string]string{"HTTP_PORT": "9090"},
			adjust: func(want *Config) { want.HTTPPort = 9090 },
		},
		{
			name:   "custom log level",
			env:    map[string]string{"LOG_LEVEL": "DEBUG"},
			adjust: func(want *Config) { want.LogLevel = slog.LevelDebug },
		},
		{
			name:   "custom shutdown timeout",
			env:    map[string]string{"SHUTDOWN_TIMEOUT": "5s"},
			adjust: func(want *Config) { want.ShutdownTimeout = 5 * time.Second },
		},
		{
			name:   "custom wallet base url",
			env:    map[string]string{"WALLET_BASE_URL": "https://wallet.example.com:8443"},
			adjust: func(want *Config) { want.WalletBaseURL = "https://wallet.example.com:8443" },
		},
		{
			name:   "wallet base url with trailing slash",
			env:    map[string]string{"WALLET_BASE_URL": "http://localhost:8000/"},
			adjust: func(want *Config) { want.WalletBaseURL = "http://localhost:8000" },
		},
		{
			name:   "custom wallet operation timeout",
			env:    map[string]string{"WALLET_OPERATION_TIMEOUT": "4s"},
			adjust: func(want *Config) { want.WalletOperationTimeout = 4 * time.Second },
		},
		{
			name:   "custom wallet balance timeout",
			env:    map[string]string{"WALLET_BALANCE_TIMEOUT": "15s"},
			adjust: func(want *Config) { want.WalletBalanceTimeout = 15 * time.Second },
		},
		{
			name:   "custom JWT TTL",
			env:    map[string]string{"JWT_TTL": "15m"},
			adjust: func(want *Config) { want.JWTTTL = 15 * time.Minute },
		},
		{
			name:   "custom pending resolver interval",
			env:    map[string]string{"PENDING_RESOLVER_INTERVAL": "30s"},
			adjust: func(want *Config) { want.PendingResolverInterval = 30 * time.Second },
		},
		{
			name:   "JWT secret of exactly 32 bytes",
			env:    map[string]string{"JWT_SECRET": "0123456789abcdef0123456789abcdef"},
			adjust: func(want *Config) { want.JWTSecret = "0123456789abcdef0123456789abcdef" },
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			expected := expectedDefaultConfig()
			testCase.adjust(&expected)
			setEnv(t, testCase.env)

			result, err := LoadConfig()

			assertConfigEqual(t, result, err, expected)
		})
	}
}

func TestLoadConfig_InvalidValues_ReturnError(t *testing.T) {
	testCases := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "invalid HTTP port",
			env:  map[string]string{"HTTP_PORT": "abc"},
		},
		{
			name: "unknown log level",
			env:  map[string]string{"LOG_LEVEL": "verbose"},
		},
		{
			name: "shutdown timeout without unit",
			env:  map[string]string{"SHUTDOWN_TIMEOUT": "10"},
		},
		{
			name: "negative shutdown timeout",
			env:  map[string]string{"SHUTDOWN_TIMEOUT": "-1s"},
		},
		{
			name: "port zero",
			env:  map[string]string{"HTTP_PORT": "0"},
		},
		{
			name: "missing database url",
			env:  map[string]string{"DATABASE_URL": ""},
		},
		{
			name: "missing wallet api key",
			env:  map[string]string{"WALLET_API_KEY": ""},
		},
		{
			name: "wallet base url without scheme",
			env:  map[string]string{"WALLET_BASE_URL": "localhost:8000"},
		},
		{
			name: "wallet base url with another scheme",
			env:  map[string]string{"WALLET_BASE_URL": "ftp://wallet:8000"},
		},
		{
			name: "wallet base url without host",
			env:  map[string]string{"WALLET_BASE_URL": "http://"},
		},
		{
			name: "wallet operation timeout without unit",
			env:  map[string]string{"WALLET_OPERATION_TIMEOUT": "6"},
		},
		{
			name: "zero wallet balance timeout",
			env:  map[string]string{"WALLET_BALANCE_TIMEOUT": "0s"},
		},
		{
			name: "missing JWT secret",
			env:  map[string]string{"JWT_SECRET": ""},
		},
		{
			name: "JWT secret of 31 bytes",
			env:  map[string]string{"JWT_SECRET": "0123456789abcdef0123456789abcde"},
		},
		{
			name: "JWT TTL without unit",
			env:  map[string]string{"JWT_TTL": "60"},
		},
		{
			name: "zero JWT TTL",
			env:  map[string]string{"JWT_TTL": "0s"},
		},
		{
			name: "pending resolver interval without unit",
			env:  map[string]string{"PENDING_RESOLVER_INTERVAL": "10"},
		},
		{
			name: "zero pending resolver interval",
			env:  map[string]string{"PENDING_RESOLVER_INTERVAL": "0s"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			setEnv(t, testCase.env)

			result, err := LoadConfig()

			assertConfigError(t, result, err)
		})
	}
}

func TestLoadConfig_ShortJWTSecret_ErrorDoesNotRevealTheSecret(t *testing.T) {
	setEnv(t, map[string]string{"JWT_SECRET": "short-secret-value"})

	_, err := LoadConfig()

	assertErrorNotContains(t, err, "short-secret-value")
}

// expectedDefaultConfig is the config produced when only the required
// variables are set (see setEnv).
func expectedDefaultConfig() Config {
	return Config{
		HTTPPort:                8080,
		LogLevel:                slog.LevelInfo,
		ShutdownTimeout:         10 * time.Second,
		DatabaseURL:             testDatabaseURL,
		WalletBaseURL:           "http://wallet:8000",
		WalletAPIKey:            testWalletAPIKey,
		WalletOperationTimeout:  6 * time.Second,
		WalletBalanceTimeout:    10 * time.Second,
		JWTSecret:               testJWTSecret,
		JWTTTL:                  time.Hour,
		PendingResolverInterval: 10 * time.Second,
	}
}

// setEnv starts every case from a clean environment that contains only the
// required variables, then applies env on top. Empty means "unset".
// t.Setenv restores the previous values when the test ends.
func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	optional := []string{
		"HTTP_PORT", "LOG_LEVEL", "SHUTDOWN_TIMEOUT",
		"WALLET_BASE_URL", "WALLET_OPERATION_TIMEOUT", "WALLET_BALANCE_TIMEOUT",
		"JWT_TTL", "PENDING_RESOLVER_INTERVAL",
	}
	for _, key := range optional {
		t.Setenv(key, "")
	}
	t.Setenv("DATABASE_URL", testDatabaseURL)
	t.Setenv("WALLET_API_KEY", testWalletAPIKey)
	t.Setenv("JWT_SECRET", testJWTSecret)
	for key, value := range env {
		t.Setenv(key, value)
	}
}

func assertConfigEqual(t *testing.T, got Config, err error, want Config) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func assertConfigError(t *testing.T, got Config, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error, got config %+v", got)
	}
}

func assertErrorNotContains(t *testing.T, err error, text string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error")
	}
	if strings.Contains(err.Error(), text) {
		t.Errorf("expected error not to contain %q, got %q", text, err)
	}
}
