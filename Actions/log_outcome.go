package actions

// logOutcome is what the log line of a money operation reports in its
// "outcome" field. It is an int, not a string, so a misspelt string literal
// does not compile where an outcome is expected.
type logOutcome int

const (
	outcomeCompleted              logOutcome = iota // the Wallet applied it; stored COMPLETED
	outcomeCompletedWithoutWallet                   // zero amount: stored COMPLETED, no Wallet call
	outcomePending                                  // the Wallet's answer is unknown; stays PENDING
	outcomePendingOverdue                           // still PENDING after overdueAfter
	outcomeBalanceUnknown                           // applied, but the new balance could not be derived
	outcomeNotStored                                // the Wallet answered, but the result could not be stored
	outcomeRejected                                 // the Wallet rejected it; stored FAILED
	outcomeInsufficientFunds                        // not enough balance; stored FAILED
)

// String is the value written in the log. Changing one changes what log
// searches and alerts match.
func (outcome logOutcome) String() string {
	switch outcome {
	case outcomeCompleted:
		return "completed"
	case outcomeCompletedWithoutWallet:
		return "completed_without_wallet"
	case outcomePending:
		return "pending"
	case outcomePendingOverdue:
		return "pending_overdue"
	case outcomeBalanceUnknown:
		return "balance_unknown"
	case outcomeNotStored:
		return "outcome_not_stored"
	case outcomeRejected:
		return "rejected"
	case outcomeInsufficientFunds:
		return "insufficient_funds"
	default:
		return "invalid_outcome"
	}
}
