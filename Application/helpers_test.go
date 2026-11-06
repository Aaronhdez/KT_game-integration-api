package application

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func assertContentTypeJSON(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", recorder.Header().Get("Content-Type"))
	}
}

func assertStatusCode(t *testing.T, recorder *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if recorder.Code != expected {
		t.Errorf("expected status %d, got %d", expected, recorder.Code)
	}
}

func parseLogLine(t *testing.T, output *bytes.Buffer) map[string]any {
	t.Helper()
	var result map[string]any
	err := json.Unmarshal(output.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to parse log line: %v", err)
	}
	return result
}

func assertLogField(t *testing.T, entry map[string]any, key string, want any) {
	t.Helper()
	got, ok := entry[key]
	if !ok {
		t.Errorf("expected log to contain key %q, but it was missing", key)
		return
	}
	if got != want {
		t.Errorf("expected log field %q to be %v (%T), got %v (%T)", key, want, want, got, got)
	}
}

func assertOutputNotContains(t *testing.T, output *bytes.Buffer, text string) {
	t.Helper()
	if bytes.Contains(output.Bytes(), []byte(text)) {
		t.Errorf("expected log not to contain %q, got %s", text, output.String())
	}
}
