package application

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

type sampleRequest struct {
	Username string `json:"username"`
}

func TestDecodeJSON_ValidBody_FillsTheDestination(t *testing.T) {
	var result sampleRequest

	err := decodeJSON(httptest.NewRecorder(), aJSONRequest(`{"username":"player_usd"}`), &result)

	assertDecoded(t, result, err, sampleRequest{Username: "player_usd"})
}

// Providers often send extra fields; rejecting them would break integrations
// for no benefit (same default as ASP.NET).
func TestDecodeJSON_UnknownFields_AreIgnored(t *testing.T) {
	var result sampleRequest

	err := decodeJSON(httptest.NewRecorder(), aJSONRequest(`{"username":"player_usd","extra":1}`), &result)

	assertDecoded(t, result, err, sampleRequest{Username: "player_usd"})
}

func TestDecodeJSON_InvalidBody_IsInvalidInput(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "malformed JSON", body: `{"username":`},
		{name: "not JSON", body: `username=player_usd`},
		{name: "array instead of object", body: `[]`},
		{name: "wrong field type", body: `{"username":5}`},
		{name: "second object after the first", body: `{"username":"a"}{"username":"b"}`},
		{name: "garbage after the object", body: `{"username":"a"} x`},
		{name: "body over 1 MiB", body: `{"username":"` + strings.Repeat("a", 1<<20) + `"}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var destination sampleRequest

			err := decodeJSON(httptest.NewRecorder(), aJSONRequest(testCase.body), &destination)

			assertErrorIs(t, err, domain.ErrInvalidInput)
		})
	}
}

/////////////////////////////////////////////////////////////////////////////////////

func aJSONRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func assertDecoded(t *testing.T, result sampleRequest, err error, want sampleRequest) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != want {
		t.Errorf("expected %+v, got %+v", want, result)
	}
}
