package domain

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const maxStringLength = 128

// ID types for various entities in the domain

// UserID represents the unique identifier for a user.
type UserID int64

// BetID represents the unique identifier for a bet.
type BetID int64

// TransactionID represents the unique identifier for a transaction.
type TransactionID string

// ProviderTransactionID represents the unique identifier for a provider transaction.
type ProviderTransactionID string

func ParseProviderTransactionID(value string) (ProviderTransactionID, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%w: must not be empty", ErrInvalidProviderTransactionID)
	}
	if utf8.RuneCountInString(value) > maxStringLength {
		return "", fmt.Errorf("%w: too long", ErrInvalidProviderTransactionID)
	}
	return ProviderTransactionID(value), nil
}
