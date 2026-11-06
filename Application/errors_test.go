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
