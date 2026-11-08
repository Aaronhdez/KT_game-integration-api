// Package id provides the real domain.IDGenerator.
package id

import (
	"uuid"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// UUID generates transaction IDs as random (version 4) UUIDs with the standard
// library. Version 4 is requested explicitly, so the format of our IDs does not
// change if uuid.New ever switches to another version.
type UUID struct{}

var _ domain.IDGenerator = UUID{}

func (UUID) NewTransactionID() domain.TransactionID {
	return domain.TransactionID(uuid.NewV4().String())
}
