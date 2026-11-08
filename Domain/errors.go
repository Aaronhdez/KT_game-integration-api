package domain

import (
	"errors"
	"fmt"
)

// Error categories. Each one corresponds to one error code of the API.
// Mapped by the application layer
var (
	ErrInvalidInput      = errors.New("invalid input")      // 400 INVALID_REQUEST
	ErrUnauthorized      = errors.New("unauthorized")       // 401 UNAUTHORIZED
	ErrNotFound          = errors.New("not found")          // 404 NOT_FOUND
	ErrConflict          = errors.New("conflict")           // 409 CONFLICT
	ErrInsufficientFunds = errors.New("insufficient funds") // 422 INSUFFICIENT_FUNDS
	ErrWalletFailure     = errors.New("wallet failure")     // 502 WALLET_ERROR
	ErrWalletTimeout     = errors.New("wallet timeout")     // 504 WALLET_TIMEOUT
)

// Specific errors. Each one wraps exactly one category.
var (
	ErrInvalidAmount                = fmt.Errorf("%w: invalid amount", ErrInvalidInput)
	ErrInvalidCurrency              = fmt.Errorf("%w: invalid currency", ErrInvalidInput)
	ErrCurrencyMismatch             = fmt.Errorf("%w: currency does not match", ErrInvalidInput)
	ErrInvalidProviderTransactionID = fmt.Errorf("%w: invalid provider transaction id", ErrInvalidInput)
	ErrMissingCredentials           = fmt.Errorf("%w: username and password are required", ErrInvalidInput)
	ErrInvalidCredentials           = fmt.Errorf("%w: invalid credentials", ErrUnauthorized)
	ErrInvalidToken                 = fmt.Errorf("%w: invalid token", ErrUnauthorized)
	ErrTransactionNotFound          = fmt.Errorf("%w: transaction not found", ErrNotFound)
	ErrPlayerNotFound               = fmt.Errorf("%w: player not found", ErrNotFound)
	ErrBetNotFound                  = fmt.Errorf("%w: bet not found", ErrNotFound)
	ErrIdempotencyConflict          = fmt.Errorf("%w: same provider transaction id with a different payload", ErrConflict)
	ErrTransactionExists            = fmt.Errorf("%w: a transaction with this provider transaction id already exists", ErrConflict)
	ErrPreviousAttemptFailed        = fmt.Errorf("%w: previous attempt failed", ErrConflict)
	ErrBetAlreadySettled            = fmt.Errorf("%w: bet already settled", ErrConflict)
	ErrBetNotSettled                = fmt.Errorf("%w: bet is not settled", ErrConflict)
	ErrTransactionPending           = fmt.Errorf("%w: transaction outcome is still pending", ErrConflict)
	ErrNothingToRevert              = fmt.Errorf("%w: transaction failed, nothing to revert", ErrConflict)
	ErrWithdrawNotCompleted         = fmt.Errorf("%w: withdraw is not completed", ErrConflict)
	ErrInvalidTransition            = fmt.Errorf("%w: invalid status transition", ErrConflict)
	ErrWalletRejected               = fmt.Errorf("%w: wallet rejected the operation", ErrWalletFailure)
)

// Outcomes that are not errors for the API client. They never reach the HTTP error mapper:
// the use case turns them into a success response.
var (
	// ErrWalletOutcomeUnknown means the Wallet may or may not have applied the
	// operation. The transaction stays PENDING.
	ErrWalletOutcomeUnknown = errors.New("wallet outcome unknown")

	// ErrWalletAppliedBalanceUnknown means the Wallet applied the operation but
	// did not report a usable new balance (e.g. it answered that an earlier
	// attempt with the same reference was already applied). The transaction is
	// COMPLETED, never FAILED.
	ErrWalletAppliedBalanceUnknown = errors.New("wallet applied the operation, new balance unknown")

	// ErrAlreadyCancelled means the transaction was already cancelled. The use
	// case returns the result of the earlier cancel with 200.
	ErrAlreadyCancelled = errors.New("transaction already cancelled")
)
