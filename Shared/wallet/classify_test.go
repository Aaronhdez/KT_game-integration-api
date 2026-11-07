package wallet

import (
	"errors"
	"strings"
	"testing"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

func TestClassifyOperation_SuccessStatus_IsApplied(t *testing.T) {
	testCases := []struct {
		name   string
		status int
	}{
		{name: "200 returned by the mock", status: 200},
		{name: "201 documented by swagger", status: 201},
		{name: "last 2xx", status: 299},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := classifyOperation(testCase.status, "")

			assertNoError(t, result)
		})
	}
}

func TestClassifyOperation_ConflictDuplicatedTransaction_IsAppliedWithUnknownBalance(t *testing.T) {
	result := classifyOperation(409, "DuplicatedTransaction")

	assertErrorIs(t, result, domain.ErrWalletAppliedBalanceUnknown)
}

func TestClassifyOperation_BadRequestInsufficientBalance_IsInsufficientFunds(t *testing.T) {
	result := classifyOperation(400, "InsufficientBalance")

	assertErrorIs(t, result, domain.ErrInsufficientFunds)
}

func TestClassifyOperation_DefinitiveRejection_IsRejected(t *testing.T) {
	testCases := []struct {
		name   string
		status int
		code   string
	}{
		{name: "400 invalid request", status: 400, code: "InvalidRequest"},
		{name: "400 user not found", status: 400, code: "UserNotFound"},
		{name: "400 invalid currency", status: 400, code: "InvalidCurrency"},
		{name: "400 without code", status: 400, code: ""},
		{name: "401 wrong api key", status: 401, code: "AuthenticatonError"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := classifyOperation(testCase.status, testCase.code)

			assertErrorIs(t, result, domain.ErrWalletRejected)
		})
	}
}

func TestClassifyOperation_AnythingElse_IsUnknown(t *testing.T) {
	testCases := []struct {
		name   string
		status int
		code   string
	}{
		{name: "500 error after applying", status: 500, code: "666"},
		{name: "503", status: 503, code: ""},
		{name: "409 with another code", status: 409, code: "SomethingElse"},
		{name: "404", status: 404, code: ""},
		{name: "403", status: 403, code: ""},
		{name: "3xx", status: 302, code: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := classifyOperation(testCase.status, testCase.code)

			assertErrorIs(t, result, domain.ErrWalletOutcomeUnknown)
		})
	}
}

func TestClassifyOperation_Error_DescribesTheWalletAnswer(t *testing.T) {
	result := classifyOperation(500, "666")

	assertErrorContains(t, result, `status 500, code "666"`)
}

/////////////////////////////////////////////////////////////////////////////////////

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func assertErrorIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("expected %v to be %q", err, target)
	}
}

func assertErrorContains(t *testing.T, err error, text string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), text) {
		t.Errorf("expected an error containing %q, got %v", text, err)
	}
}
