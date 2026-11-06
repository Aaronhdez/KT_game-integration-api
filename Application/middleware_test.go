package application

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestLogger_LogsStatus(t *testing.T) {
	output := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(output, nil))
	handler := RequestLogger(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/test", nil)

	handler.ServeHTTP(recorder, request)

	logEntry := parseLogLine(t, output)
	assertLogField(t, logEntry, "status", float64(http.StatusTeapot))
}
