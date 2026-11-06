package application

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth_ReturnsStatusOK(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)

	Health(recorder, request)

	assertStatusCode(t, recorder, http.StatusOK)
}

func TestHealth_SetsJSONContentType(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)

	Health(recorder, request)

	assertContentTypeJSON(t, recorder)
}

func TestHealth_WritesStatusOk(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)

	Health(recorder, request)

	result := decodeHealthResponse(t, recorder)
	assertHealthResponse(t, result, healthResponse{Status: "ok"})
}

func decodeHealthResponse(t *testing.T, recorder *httptest.ResponseRecorder) healthResponse {
	t.Helper()
	var result healthResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	return result
}

func assertHealthResponse(t *testing.T, result, expected healthResponse) {
	t.Helper()
	if result != expected {
		t.Errorf("expected health response %+v, got %+v", expected, result)
	}
}
