package domaintest

import (
	"testing"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

func TestFakeIDGenerator_ReturnsSequentialIDs(t *testing.T) {
	generator := &FakeIDGenerator{}

	result := []domain.TransactionID{generator.NewTransactionID(), generator.NewTransactionID()}

	assertTransactionIDs(t, result, []domain.TransactionID{
		"00000000-0000-4000-8000-000000000001",
		"00000000-0000-4000-8000-000000000002",
	})
}

/////////////////////////////////////////////////////////////////////////////////////

func assertTransactionIDs(t *testing.T, result, want []domain.TransactionID) {
	t.Helper()
	if len(result) != len(want) {
		t.Fatalf("expected IDs %v, got %v", want, result)
	}
	for i := range want {
		if result[i] != want[i] {
			t.Errorf("expected IDs %v, got %v", want, result)
		}
	}
}
