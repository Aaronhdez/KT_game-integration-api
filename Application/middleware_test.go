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

	_, output := serveWithRequestLogger(teapotHandler(), http.MethodGet, "/test")

	logEntry := parseLogLine(t, output)

	assertLogField(t, logEntry, "status", float64(http.StatusTeapot))
}

func TestRequestLogger_HandlerWritesOnlyBody_LogsStatusOK(t *testing.T) {
	t.Parallel()

	_, output := serveWithRequestLogger(bodyOnlyHandler(), http.MethodGet, "/test")

	logEntry := parseLogLine(t, output)
	assertLogField(t, logEntry, "status", float64(http.StatusOK))
}

func TestRequestLogger_LogsMethod(t *testing.T) {
	t.Parallel()

	_, output := serveWithRequestLogger(teapotHandler(), http.MethodPost, "/test")

	logEntry := parseLogLine(t, output)
	assertLogField(t, logEntry, "method", "POST")
}

func TestRequestLogger_LogsPath(t *testing.T) {
	t.Parallel()

	_, output := serveWithRequestLogger(teapotHandler(), http.MethodGet, "/my-path")

	logEntry := parseLogLine(t, output)
	assertLogField(t, logEntry, "path", "/my-path")
}

func TestRequestLogger_DoesNotLogQueryString(t *testing.T) {
	t.Parallel()

	_, output := serveWithRequestLogger(teapotHandler(), http.MethodGet, "/my-path?query=string")

	assertOutputNotContains(t, output, "query=string")
}

func TestRequestLogger_PassesResponseThrough(t *testing.T) {
	t.Parallel()

	recorder, _ := serveWithRequestLogger(teapotHandler(), http.MethodGet, "/test")

	assertStatusCode(t, recorder, http.StatusTeapot)
}

/////////////////////////////////////////////////////////////////////////////////////

func serveWithRequestLogger(next http.Handler, method, target string) (*httptest.ResponseRecorder, *bytes.Buffer) {
	output := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(output, nil))
	handler := RequestLogger(logger, next)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, nil)

	handler.ServeHTTP(recorder, request)
	return recorder, output
}

func teapotHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
}

func bodyOnlyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("body"))
	})
}
