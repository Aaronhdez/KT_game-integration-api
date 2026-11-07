package application

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
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

// Each error category of Domain maps to one status and one API code
// (assessment §7.3). One specific error per category is enough: the mapping
// only looks at the category.
func TestWriteDomainError_EachCategory_MapsToItsStatusAndCode(t *testing.T) {
	testCases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "invalid input", err: domain.ErrMissingCredentials, status: http.StatusBadRequest, code: CodeInvalidRequest},
		{name: "unauthorized", err: domain.ErrInvalidCredentials, status: http.StatusUnauthorized, code: CodeUnauthorized},
		{name: "not found", err: domain.ErrPlayerNotFound, status: http.StatusNotFound, code: CodeNotFound},
		{name: "conflict", err: domain.ErrIdempotencyConflict, status: http.StatusConflict, code: CodeConflict},
		{name: "insufficient funds", err: domain.ErrInsufficientFunds, status: http.StatusUnprocessableEntity, code: CodeInsufficientFunds},
		{name: "wallet failure", err: domain.ErrWalletRejected, status: http.StatusBadGateway, code: CodeWalletError},
		{name: "wallet timeout", err: domain.ErrWalletTimeout, status: http.StatusGatewayTimeout, code: CodeWalletTimeout},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder, _ := serveDomainError(testCase.err)

			assertStatusAndCode(t, recorder, testCase.status, testCase.code)
		})
	}
}

func TestWriteDomainError_ErrorWrappedWithContext_StillMapsToItsCategory(t *testing.T) {
	recorder, _ := serveDomainError(fmt.Errorf("login: %w", domain.ErrInvalidCredentials))

	assertStatusAndCode(t, recorder, http.StatusUnauthorized, CodeUnauthorized)
}

func TestWriteDomainError_SetsJSONContentType(t *testing.T) {
	recorder, _ := serveDomainError(domain.ErrInvalidCredentials)

	assertContentTypeJSON(t, recorder)
}

// For 400 and 409 the domain text tells the game provider what to fix, and
// domain errors never carry secrets.
func TestWriteDomainError_InvalidInputAndConflict_MessageIsTheErrorText(t *testing.T) {
	testCases := []struct {
		name string
		err  error
	}{
		{name: "invalid input", err: domain.ErrMissingCredentials},
		{name: "conflict", err: domain.ErrIdempotencyConflict},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder, _ := serveDomainError(testCase.err)

			result := decodeErrorResponse(t, recorder)
			assertMessage(t, result, testCase.err.Error())
		})
	}
}

// The other categories answer with a fixed text: e.g. a 401 must not say
// whether the user or the password was wrong.
func TestWriteDomainError_OtherCategories_MessageIsFixed(t *testing.T) {
	testCases := []struct {
		name    string
		err     error
		message string
	}{
		{name: "unauthorized", err: fmt.Errorf("login: %w", domain.ErrInvalidCredentials), message: "invalid credentials or token"},
		{name: "not found", err: domain.ErrBetNotFound, message: "resource not found"},
		{name: "insufficient funds", err: domain.ErrInsufficientFunds, message: "insufficient funds"},
		{name: "wallet failure", err: domain.ErrWalletRejected, message: "wallet error"},
		{name: "wallet timeout", err: domain.ErrWalletTimeout, message: "wallet did not answer in time"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder, _ := serveDomainError(testCase.err)

			result := decodeErrorResponse(t, recorder)
			assertMessage(t, result, testCase.message)
		})
	}
}

// Anything that is not a domain category is a bug or an infrastructure
// failure (database down, unusable hash…). The outcomes that are not errors
// for the client (e.g. ErrWalletOutcomeUnknown) must never reach the mapper
// either: if one does, it is a bug too.
func TestWriteDomainError_UnknownError_IsInternalError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
	}{
		{name: "plain error", err: errors.New("connection refused")},
		{name: "wrapped plain error", err: fmt.Errorf("login: %w", errors.New("connection refused"))},
		{name: "wallet outcome unknown", err: domain.ErrWalletOutcomeUnknown},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder, _ := serveDomainError(testCase.err)

			result := decodeErrorResponse(t, recorder)
			assertInternalError(t, recorder, result)
		})
	}
}

func TestWriteDomainError_UnknownError_DoesNotRevealItsText(t *testing.T) {
	recorder, _ := serveDomainError(errors.New("dial tcp postgres:5432: connection refused"))

	assertOutputNotContains(t, recorder.Body, "postgres")
}

func TestWriteDomainError_UnknownError_IsLoggedAtErrorLevel(t *testing.T) {
	_, output := serveDomainError(errors.New("connection refused"))

	logEntry := parseLogLine(t, output)
	assertLogField(t, logEntry, "level", "ERROR")
}

func TestWriteDomainError_UnknownError_LogsTheErrorText(t *testing.T) {
	_, output := serveDomainError(fmt.Errorf("login: %w", errors.New("connection refused")))

	logEntry := parseLogLine(t, output)
	assertLogField(t, logEntry, "error", "login: connection refused")
}

// Expected client errors are already visible in the request log (status);
// logging them again would only add noise.
func TestWriteDomainError_DomainError_IsNotLogged(t *testing.T) {
	_, output := serveDomainError(domain.ErrInvalidCredentials)

	assertNoOutput(t, output)
}

/////////////////////////////////////////////////////////////////////////////////////

func serveDomainError(err error) (*httptest.ResponseRecorder, *bytes.Buffer) {
	output := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(output, nil))
	recorder := httptest.NewRecorder()

	writeDomainError(recorder, logger, err)
	return recorder, output
}

func assertStatusAndCode(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	assertStatusCode(t, recorder, status)
	result := decodeErrorResponse(t, recorder)
	if result.Code != code {
		t.Errorf("expected code %q, got %q", code, result.Code)
	}
}

func assertMessage(t *testing.T, result ErrorResponse, want string) {
	t.Helper()
	if result.Message != want {
		t.Errorf("expected message %q, got %q", want, result.Message)
	}
}

func assertInternalError(t *testing.T, recorder *httptest.ResponseRecorder, result ErrorResponse) {
	t.Helper()
	assertStatusCode(t, recorder, http.StatusInternalServerError)
	assertErrorResponse(t, result, ErrorResponse{Code: CodeInternal, Message: "internal error"})
}

func assertNoOutput(t *testing.T, output *bytes.Buffer) {
	t.Helper()
	if output.Len() != 0 {
		t.Errorf("expected no log output, got %s", output.String())
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
