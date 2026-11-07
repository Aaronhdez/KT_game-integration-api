package clock

import (
	"testing"
	"time"
)

func TestSystemNow_IsInUTC(t *testing.T) {
	result := System{}.Now()

	assertLocation(t, result, time.UTC)
}

/////////////////////////////////////////////////////////////////////////////////////

func assertLocation(t *testing.T, result time.Time, want *time.Location) {
	t.Helper()
	if result.Location() != want {
		t.Errorf("expected location %s, got %s", want, result.Location())
	}
}
