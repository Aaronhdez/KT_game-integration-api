package actions

import (
	"errors"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// walletOutcome is what the result of a Wallet withdraw or deposit means for
// our transaction.
type walletOutcome int

const (
	walletApplied               walletOutcome = iota // COMPLETED with the balance the Wallet reported
	walletAppliedBalanceUnknown                      // COMPLETED; the new balance must be derived
	walletNotApplied                                 // FAILED: a definitive rejection (422 or 502)
	walletOutcomeUnknown                             // stays PENDING (202): it may have been applied
)

// classifyWalletResult turns the error of a Wallet withdraw or deposit into
// its outcome. Only a definitive rejection is "not applied": any error the
// Wallet client did not classify is unknown, never FAILED, because a wrong
// FAILED cannot be undone and a PENDING can be resolved.
func classifyWalletResult(err error) walletOutcome {
	switch {
	case err == nil:
		return walletApplied
	case errors.Is(err, domain.ErrWalletAppliedBalanceUnknown):
		return walletAppliedBalanceUnknown
	case errors.Is(err, domain.ErrInsufficientFunds), errors.Is(err, domain.ErrWalletRejected):
		return walletNotApplied
	default:
		return walletOutcomeUnknown
	}
}

// String names the outcome, for test failures and debugging.
func (outcome walletOutcome) String() string {
	switch outcome {
	case walletApplied:
		return "applied"
	case walletAppliedBalanceUnknown:
		return "applied, balance unknown"
	case walletNotApplied:
		return "not applied"
	case walletOutcomeUnknown:
		return "unknown"
	default:
		return "invalid wallet outcome"
	}
}
