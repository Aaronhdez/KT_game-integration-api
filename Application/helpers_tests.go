package application

import (
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
