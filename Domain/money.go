package domain

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// Money represents a monetary amount with a fixed number of decimal places.
type Money struct {
	amount decimal.Decimal
}

const moneyMaxDecimals = 2
const moneyMaxIntegers = 18

var moneyUpperLimit = decimal.New(1, moneyMaxIntegers)

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
