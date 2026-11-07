// Package wallet is the HTTP client of the Wallet service.
package wallet

import (
	"fmt"
	"net/http"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

const (
	insufficientBalance   = "InsufficientBalance"
	duplicatedTransaction = "DuplicatedTransaction"
)

func classifyOperation(status int, code string) error {
	switch {
	case status >= http.StatusOK && status < http.StatusMultipleChoices:
		return nil
	case status == http.StatusConflict && code == duplicatedTransaction:
		return fmt.Errorf("wallet answered status %d, code %q: %w", status, code, domain.ErrWalletAppliedBalanceUnknown)
	case status == http.StatusBadRequest && code == insufficientBalance:
		return fmt.Errorf("wallet answered status %d, code %q: %w", status, code, domain.ErrInsufficientFunds)
	case status == http.StatusBadRequest || status == http.StatusUnauthorized:
		return fmt.Errorf("wallet answered status %d, code %q: %w", status, code, domain.ErrWalletRejected)
	default:
		return fmt.Errorf("wallet answered status %d, code %q: %w", status, code, domain.ErrWalletOutcomeUnknown)
	}
}
