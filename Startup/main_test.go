package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestNewLogger_InfoMessage_WritesMessage(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output, slog.LevelInfo)

	logger.Info("hello")

	result := parseLogLine(t, &output)
	assertLogField(t, result, "msg", "hello")
}

func TestNewLogger_InfoMessage_WritesLevel(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output, slog.LevelInfo)

	logger.Info("hello")

	result := parseLogLine(t, &output)
	assertLogField(t, result, "level", "INFO")
}

func TestNewLogger_InfoMessage_WritesPort(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output, slog.LevelInfo)

	logger.Info("hello", "port", 8080)

	result := parseLogLine(t, &output)
	assertLogField(t, result, "port", float64(8080))
}

func TestNewLogger_MessageAtLevel_IsWritten(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output, slog.LevelInfo)

	logger.Info("should be shown")

	assertOutputContains(t, &output, "should be shown")
}

func TestNewLogger_MessageBelowLevel_IsNotWritten(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output, slog.LevelWarn)

	logger.Info("should be hidden")

	assertOutputNotContains(t, &output, "should be hidden")
}

/////////////////////////////////////////////////////////////////////////////////////

func parseLogLine(t *testing.T, output *bytes.Buffer) map[string]any {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("log line is not valid JSON: %v (line: %q)", err, output.String())
	}
	return entry
}

func assertLogField(t *testing.T, entry map[string]any, key string, want any) {
	t.Helper()
	if entry[key] != want {
		t.Errorf("field %q = %v (%T), want %v (%T)", key, entry[key], entry[key], want, want)
	}
}

func assertOutputContains(t *testing.T, output *bytes.Buffer, text string) {
	t.Helper()
	if !strings.Contains(output.String(), text) {
		t.Errorf("output does not contain %q: %q", text, output.String())
	}
}

func assertOutputNotContains(t *testing.T, output *bytes.Buffer, text string) {
	t.Helper()
	if strings.Contains(output.String(), text) {
		t.Errorf("output contains %q but should not: %q", text, output.String())
	}
}
