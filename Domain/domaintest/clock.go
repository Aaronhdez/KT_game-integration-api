package domaintest

import (
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// FixedClock is a domain.Clock that always returns Time, so tests know
// exactly which "now" the code under test used.
type FixedClock struct {
	Time time.Time
}

var _ domain.Clock = FixedClock{}

func (clock FixedClock) Now() time.Time {
	return clock.Time
}
