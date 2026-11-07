package wallet

import "time"

// Config holds the configuration for the wallet client, including the base URL, API key, and timeouts for operations and balance checks.
// APIKey is secret, and therefore, is never logged
type Config struct {
	BaseURL          string
	APIKey           string
	OperationTimeout time.Duration
	BalanceTimeout   time.Duration
}
