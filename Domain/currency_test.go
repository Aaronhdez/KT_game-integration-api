package domain

import (
	"testing"
)

func TestParseCurrency_SupportedCode_ReturnsCurrency(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Currency
	}{
		{name: "US Dollar", input: "USD", expected: USD},
		{name: "Euro", input: "EUR", expected: EUR},
		{name: "Kenyan Shilling", input: "KES", expected: KES},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCurrency(tt.input)

			assertCurrency(t, got, err, tt.expected)
		})
	}
}

func TestParseCurrency_UnsupportedCode_ReturnsInvalidCurrency(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "unsupported code: JAP", input: "JAP"},
		{name: "unsupported code: GBP", input: "GBP"},
		{name: "unsupported code: ZLO", input: "ZLO"},
		{name: "lowercase", input: "usd"},
		{name: "leading spaces", input: "  USD"},
		{name: "trailing spaces", input: "USD  "},
		{name: "empty string", input: ""},
		{name: "only spaces", input: "  "},
		{name: "numeric", input: "123"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {

			_, err := ParseCurrency(testCase.input)

			assertErrorIs(t, err, ErrInvalidCurrency)
		})
	}
}

func assertCurrency(t *testing.T, got Currency, err error, expected Currency) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != expected {
		t.Errorf("expected %v, got %v", expected, got)
	}
}
