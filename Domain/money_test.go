package domain

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestMoney_ValidValues(t *testing.T) {
	testCases := []struct {
		name  string
		value decimal.Decimal
	}{
		{name: "zero amount", value: decimal.NewFromInt(0)},
		{name: "valid amount", value: decimal.NewFromInt(1)},
		{name: "valid amount with decimal", value: decimal.RequireFromString("1.12")},
		{name: "valid amount with maximum value", value: decimal.RequireFromString("999999999999999999")},
		{name: "valid amount with minimum value", value: decimal.RequireFromString("0.01")},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := MoneyFromDecimal(testCase.value)

			assertNoError(t, err)
		})
	}
}

func TestMoney_NegativeAmount(t *testing.T) {
	negative := decimal.NewFromInt(-1)

	_, err := MoneyFromDecimal(negative)

	assertErrorIs(t, err, ErrInvalidAmount)
}

func TestMoney_ExceedsUpperLimit(t *testing.T) {

	_, err := MoneyFromDecimal(decimal.RequireFromString("1000000000000000000"))

	assertErrorIs(t, err, ErrInvalidAmount)
}

func TestMoney_TooManyDecimalPlaces(t *testing.T) {
	tooManyDecimals := decimal.RequireFromString("1.123")

	_, err := MoneyFromDecimal(tooManyDecimals)

	assertErrorIs(t, err, ErrInvalidAmount)
}
