// Package clock provides the real domain.Clock.
package clock

import (
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// System is the domain.Clock of the running service: the machine's time in UTC,
// so every timestamp we store or return has the same time zone.
type System struct{}

var _ domain.Clock = System{}

func (System) Now() time.Time {
	return time.Now().UTC()
}
