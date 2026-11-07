package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	application "github.com/aaronhdez/kt_game-integration-api/Application"
)

func TestRouter_GetHealth_ReturnsStatusOK(t *testing.T) {
	recorder := aGET("/health")

	assertStatusCode(t, recorder, http.StatusOK)
}

func TestRouter_UnknownPath_ReturnsNotFound(t *testing.T) {
	recorder := aGET("/nope")

	assertStatusCode(t, recorder, http.StatusNotFound)
}

func TestRouter_UnknownPath_WritesNotFoundCode(t *testing.T) {
	recorder := aGET("/nope")

	result := decodeErrorResponse(t, recorder)
	assertErrorCode(t, result, application.CodeNotFound)
}

func TestRouter_PostHealth_ReturnsNotFound(t *testing.T) {
	recorder := aPOST("/health")

	assertStatusCode(t, recorder, http.StatusNotFound)
}

func TestRouter_PostLogin_ReachesTheLoginHandler(t *testing.T) {
	recorder := aPOST("/api/v1/auth/login")

	assertStatusCode(t, recorder, loginStubStatus)
}

func TestRouter_GetLogin_ReturnsNotFound(t *testing.T) {
	recorder := aGET("/api/v1/auth/login")

	assertStatusCode(t, recorder, http.StatusNotFound)
}

/////////////////////////////////////////////////////////////////////////////////////

// loginStubStatus is an unusual status, so a test can tell that the request
// reached the login stub and not another route.
const loginStubStatus = http.StatusTeapot

func stubHandlers() Handlers {
	return Handlers{
		Login: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(loginStubStatus)
		}),
	}
}

func aPOST(path string) *httptest.ResponseRecorder {
	return serve(http.MethodPost, path)
}

func aGET(path string) *httptest.ResponseRecorder {
	return serve(http.MethodGet, path)
}

func serve(method, path string) *httptest.ResponseRecorder {
	router := NewRouter(slog.New(slog.DiscardHandler), stubHandlers())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	router.ServeHTTP(recorder, request)
	return recorder
}

func assertStatusCode(t *testing.T, recorder *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if recorder.Code != expected {
		t.Errorf("expected status %d, got %d", expected, recorder.Code)
	}
}

func assertErrorCode(t *testing.T, result application.ErrorResponse, expected string) {
	t.Helper()
	if result.Code != expected {
		t.Errorf("expected error code %s, got %s", expected, result.Code)
	}
}

func decodeErrorResponse(t *testing.T, recorder *httptest.ResponseRecorder) application.ErrorResponse {
	t.Helper()
	var result application.ErrorResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	return result
}
