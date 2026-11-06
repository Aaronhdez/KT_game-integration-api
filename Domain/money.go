package domain

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// Money represents a non-negative monetary amount with up to two decimal places.
type Money struct {
	amount decimal.Decimal
}

const moneyMaxDecimals = 2
const moneyMaxIntegerDigits = 18

var moneyUpperLimit = decimal.New(1, moneyMaxIntegerDigits)

// MoneyFromDecimal creates a Money instance from a decimal.Decimal value.
func MoneyFromDecimal(amount decimal.Decimal) (Money, error) {
	if amount.GreaterThanOrEqual(moneyUpperLimit) {
		return Money{}, fmt.Errorf("%w: %s exceeds upper limit", ErrInvalidAmount, amount)
	}
	if amount.LessThan(decimal.Zero) {
		return Money{}, fmt.Errorf("%w: %s is negative", ErrInvalidAmount, amount)
	}
	if !amount.Truncate(moneyMaxDecimals).Equal(amount) {
		return Money{}, fmt.Errorf("%w: %s has too many decimal places", ErrInvalidAmount, amount)
	}
	return Money{amount: amount}, nil
}

func ParseMoney(value string) (Money, error) {
	amount, err := decimal.NewFromString(value)
	if err != nil {
		return Money{}, fmt.Errorf("%w: %q is not a valid decimal", ErrInvalidAmount, value)
	}
	return MoneyFromDecimal(amount)
}
