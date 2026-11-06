package domain

import (
	"fmt"
)

type Currency string

const (
	USD Currency = "USD"
	EUR Currency = "EUR"
	KES Currency = "KES"
)

// ParseCurrency accepts only the exact codes USD, EUR and KES.
func ParseCurrency(s string) (Currency, error) {
	switch value := s; value {
	case "USD":
		return USD, nil
	case "EUR":
		return EUR, nil
	case "KES":
		return KES, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidCurrency, value)
	}
}
