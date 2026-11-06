package main

import (
	"log/slog"
	"testing"
	"time"
)

func TestLoadConfig_NoVariablesSet_UsesDefaults(t *testing.T) {
	expected := Config{
		HTTPPort:        8080,
		LogLevel:        slog.LevelInfo,
		ShutdownTimeout: 10 * time.Second,
	}
	setEnv(t, map[string]string{})

	got, err := LoadConfig()

	assertConfigEqual(t, got, err, expected)
}

func TestLoadConfig_HTTPPortSet_UsesValue(t *testing.T) {
	expected := Config{
		HTTPPort:        9090,
		LogLevel:        slog.LevelInfo,
		ShutdownTimeout: 10 * time.Second,
	}
	setEnv(t, map[string]string{"HTTP_PORT": "9090"})

	got, err := LoadConfig()

	assertConfigEqual(t, got, err, expected)
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
			want: Config{HTTPPort: 9090, LogLevel: slog.LevelInfo, ShutdownTimeout: 10 * time.Second},
		},
		{
			name: "custom log level",
			env:  map[string]string{"LOG_LEVEL": "DEBUG"},
			want: Config{HTTPPort: 8080, LogLevel: slog.LevelDebug, ShutdownTimeout: 10 * time.Second},
		},
		{
			name: "custom shutdown timeout",
			env:  map[string]string{"SHUTDOWN_TIMEOUT": "5s"},
			want: Config{HTTPPort: 8080, LogLevel: slog.LevelInfo, ShutdownTimeout: 5 * time.Second},
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
			name: "port zero fails",
			env:  map[string]string{"HTTP_PORT": "0"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			setEnv(t, testCase.env)

			got, err := LoadConfig()

			if err == nil {
				t.Fatalf("expected an error for test case %q, got config %+v", testCase.name, got)
			}
		})
	}
}

func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	// Start every case from a clean environment: empty means "unset".
	// t.Setenv restores the previous value when the test ends.
	for _, key := range []string{"HTTP_PORT", "LOG_LEVEL", "SHUTDOWN_TIMEOUT"} {
		t.Setenv(key, "")
	}
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
