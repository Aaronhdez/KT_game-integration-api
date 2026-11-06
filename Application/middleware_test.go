package application

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestLogger_LogsStatus(t *testing.T) {
	t.Parallel()
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

func TestRequestLogger_HandlerWritesOnlyBody_LogsStatusOK(t *testing.T) {
	t.Parallel()
	output := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(output, nil))
	handler := RequestLogger(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("body"))
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/test", nil)

	handler.ServeHTTP(recorder, request)

	logEntry := parseLogLine(t, output)
	assertLogField(t, logEntry, "status", float64(http.StatusOK))
}

func TestRequestLogger_LogsMethod(t *testing.T) {
	t.Parallel()
	output := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(output, nil))
	handler := RequestLogger(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/test", nil)

	handler.ServeHTTP(recorder, request)

	logEntry := parseLogLine(t, output)
	assertLogField(t, logEntry, "method", "POST")
}

func TestRequestLogger_LogsPath(t *testing.T) {
	t.Parallel()
	output := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(output, nil))
	handler := RequestLogger(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/my-path", nil)

	handler.ServeHTTP(recorder, request)

	logEntry := parseLogLine(t, output)
	assertLogField(t, logEntry, "path", "/my-path")
}

func TestRequestLogger_DoesNotLogQueryString(t *testing.T) {
	t.Parallel()
	output := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(output, nil))
	handler := RequestLogger(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/my-path?query=string", nil)

	handler.ServeHTTP(recorder, request)

	assertOutputNotContains(t, output, "query=string")
}

func TestRequestLogger_PassesResponseThrough(t *testing.T) {
	t.Parallel()
	output := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(output, nil))
	handler := RequestLogger(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/test", nil)

	handler.ServeHTTP(recorder, request)

	assertStatusCode(t, recorder, http.StatusTeapot)
}
