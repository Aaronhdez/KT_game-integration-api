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
