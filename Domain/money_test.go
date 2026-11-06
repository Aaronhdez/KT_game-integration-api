package domain

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestMoneyFromDecimal_ValidAmount_IsAccepted(t *testing.T) {
	testCases := []struct {
		name  string
		value decimal.Decimal
	}{
		{name: "zero amount", value: decimal.NewFromInt(0)},
		{name: "valid amount", value: decimal.NewFromInt(1)},
		{name: "valid amount with decimal", value: decimal.RequireFromString("1.12")},
		{name: "maximum valid amount", value: decimal.RequireFromString("999999999999999999.99")},
		{name: "one cent", value: decimal.RequireFromString("0.01")},
		{name: "trailing zero beyond two decimals", value: decimal.RequireFromString("1.100")},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := MoneyFromDecimal(testCase.value)

			assertNoError(t, err)
		})
	}
}

func TestMoneyFromDecimal_InvalidAmount_ReturnsInvalidAmount(t *testing.T) {
	testCases := []struct {
		name  string
		value decimal.Decimal
	}{
		{name: "negative amount", value: decimal.NewFromInt(-1)},
		{name: "exceeds upper limit", value: decimal.RequireFromString("1000000000000000000")},
		{name: "too many decimal places", value: decimal.RequireFromString("1.123")},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := MoneyFromDecimal(testCase.value)

			assertErrorIs(t, err, ErrInvalidAmount)
		})
	}
}

func TestParseMoney_ValidText_IsAccepted(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{name: "integer", input: "5000"},
		{name: "two decimals", input: "4989.50"},
		{name: "largest seeded balance", input: "9000000000.09"},
		{name: "exponent notation", input: "1e3"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ParseMoney(testCase.input)

			assertNoError(t, err)
		})
	}
}

func TestParseMoney_InvalidText_ReturnsInvalidAmount(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{name: "too many decimal places", input: "1.123"},
		{name: "NAN", input: "NaN"},
		{name: "infinity", input: "Inf"},
		{name: "decimal comma", input: "-1,123"},
		{name: "non-numeric", input: "abc"},
		{name: "surrounding spaces", input: " 10 "},
		{name: "empty string", input: ""},
		{name: "only decimal point", input: "."},
		{name: "only decimal point with whitespace", input: " . "},
		{name: "only whitespace", input: "   "},
		{name: "only plus sign", input: "+"},
		{name: "only minus sign", input: "-"},
		{name: "negative amount", input: "-1"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ParseMoney(testCase.input)

			assertErrorIs(t, err, ErrInvalidAmount)
		})
	}
}
