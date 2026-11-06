package domain

import (
	"strings"
	"testing"
)

const maxLength = 128

func TestCasesIds_ParseProviderTransactionID_ValidCases(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{"assessment example", "prov-tx-0001"},
		{"exactly 128 characters", strings.Repeat("a", maxLength)},
		{"maxLength non-ASCII characters", strings.Repeat("あ", maxLength)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ParseProviderTransactionID(testCase.input)

			assertNoError(t, err)
		})
	}
}

func TestCasesIds_ParseProviderTransactionID_InvalidCases(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
		{"ASCII too long", strings.Repeat("a", maxLength+1)},
		{"non-ASCII too long", strings.Repeat("あ", maxLength+1)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ParseProviderTransactionID(testCase.input)

			assertErrorIs(t, err, ErrInvalidProviderTransactionID)
		})
	}
}
