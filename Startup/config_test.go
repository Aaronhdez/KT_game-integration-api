package main

import (
	"log/slog"
	"testing"
	"time"
)

const testDatabaseURL = "postgres://user:secret@localhost:5432/db"

func TestLoadConfig_OnlyRequiredVariablesSet_UsesDefaults(t *testing.T) {
	expected := Config{
		HTTPPort:        8080,
		LogLevel:        slog.LevelInfo,
		ShutdownTimeout: 10 * time.Second,
		DatabaseURL:     testDatabaseURL,
	}
	setEnv(t, map[string]string{})

	result, err := LoadConfig()

	assertConfigEqual(t, result, err, expected)
}

func TestLoadConfig_ValidValues_AreUsed(t *testing.T) {
	testCases := []struct {
		name string
		env  map[string]string
		want Config
	}{
		{
			name: "custom HTTP port",
			env:  map[string]string{"HTTP_PORT": "9090"},
			want: Config{HTTPPort: 9090, LogLevel: slog.LevelInfo, ShutdownTimeout: 10 * time.Second, DatabaseURL: testDatabaseURL},
		},
		{
			name: "custom log level",
			env:  map[string]string{"LOG_LEVEL": "DEBUG"},
			want: Config{HTTPPort: 8080, LogLevel: slog.LevelDebug, ShutdownTimeout: 10 * time.Second, DatabaseURL: testDatabaseURL},
		},
		{
			name: "custom shutdown timeout",
			env:  map[string]string{"SHUTDOWN_TIMEOUT": "5s"},
			want: Config{HTTPPort: 8080, LogLevel: slog.LevelInfo, ShutdownTimeout: 5 * time.Second, DatabaseURL: testDatabaseURL},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			setEnv(t, testCase.env)

			result, err := LoadConfig()

			assertConfigEqual(t, result, err, testCase.want)
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
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			setEnv(t, testCase.env)

			result, err := LoadConfig()

			if err == nil {
				t.Fatalf("expected an error for test case %q, got config %+v", testCase.name, result)
			}
		})
	}
}

// setEnv starts every case from a clean environment that contains only the
// required variables, then applies env on top. Empty means "unset".
// t.Setenv restores the previous values when the test ends.
func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, key := range []string{"HTTP_PORT", "LOG_LEVEL", "SHUTDOWN_TIMEOUT"} {
		t.Setenv(key, "")
	}
	t.Setenv("DATABASE_URL", testDatabaseURL)
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
