package application

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteError_SetsStatusCode(t *testing.T) {
	recorder := httptest.NewRecorder()

	WriteError(recorder, http.StatusBadRequest, CodeInvalidRequest, "Invalid request")

	assertStatusCode(t, recorder, http.StatusBadRequest)
}

func TestWriteError_SetsJSONContentType(t *testing.T) {
	recorder := httptest.NewRecorder()

	WriteError(recorder, http.StatusBadRequest, CodeInvalidRequest, "Invalid request")

	assertContentTypeJSON(t, recorder)
}

func TestWriteError_WritesCodeAndMessage(t *testing.T) {
	recorder := httptest.NewRecorder()

	WriteError(recorder, http.StatusBadRequest, CodeInvalidRequest, "Invalid request")

	result := decodeErrorResponse(t, recorder)
	assertErrorResponse(t, result, ErrorResponse{Code: CodeInvalidRequest, Message: "Invalid request"})
}

/////////////////////////////////////////////////////////////////////////////////////

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

func decodeErrorResponse(t *testing.T, recorder *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()
	var result ErrorResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	return result
}

func assertErrorResponse(t *testing.T, result, expected ErrorResponse) {
	t.Helper()
	if result != expected {
		t.Errorf("expected error response %+v, got %+v", expected, result)
	}
}
