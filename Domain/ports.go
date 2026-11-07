package domain

import "time"

// Clock tells the current time. Use cases ask it for "now" and pass that value
// to the domain (NewPlacedBet, tx.Complete, …), so tests can use a fixed time.
type Clock interface {
	Now() time.Time
}

// IDGenerator creates the IDs our service assigns to its transactions (UUIDs,
type IDGenerator interface {
	NewTransactionID() TransactionID
}
