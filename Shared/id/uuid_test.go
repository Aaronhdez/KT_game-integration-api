package id

import (
	"regexp"
	"testing"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// Lowercase 8-4-4-4-12 hex, version 4 (third group starts with 4) and RFC 9562
// variant (fourth group starts with 8, 9, a or b).
var uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// Several IDs, not one: a wrong version or variant would still match the
// pattern by chance for a single random ID (1 in 16 or 1 in 4).
func TestUUIDNewTransactionID_AreLowercaseVersion4UUIDs(t *testing.T) {
	result := generateIDs(100)

	assertAllUUIDv4(t, result)
}

func TestUUIDNewTransactionID_ManyCalls_ReturnsDistinctIDs(t *testing.T) {
	const calls = 1000

	result := generateIDs(calls)

	assertDistinct(t, result, calls)
}

/////////////////////////////////////////////////////////////////////////////////////

func generateIDs(count int) []domain.TransactionID {
	ids := make([]domain.TransactionID, 0, count)
	for range count {
		ids = append(ids, UUID{}.NewTransactionID())
	}
	return ids
}

func assertAllUUIDv4(t *testing.T, result []domain.TransactionID) {
	t.Helper()
	for _, id := range result {
		if !uuidV4Pattern.MatchString(string(id)) {
			t.Fatalf("expected a lowercase version 4 UUID, got %q", id)
		}
	}
}

func assertDistinct(t *testing.T, result []domain.TransactionID, want int) {
	t.Helper()
	seen := make(map[domain.TransactionID]bool, len(result))
	for _, id := range result {
		seen[id] = true
	}
	if len(seen) != want {
		t.Errorf("expected %d distinct IDs, got %d", want, len(seen))
	}
}
