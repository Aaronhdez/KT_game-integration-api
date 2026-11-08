package domaintest

import (
	"fmt"
	"sync"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// FakeIDGenerator is a domain.IDGenerator that returns FakeTransactionID(1),
// FakeTransactionID(2), … in order, so tests know every ID in advance. The
// zero value is ready to use. It is safe for concurrent use.
type FakeIDGenerator struct {
	mutex sync.Mutex
	last  int
}

var _ domain.IDGenerator = (*FakeIDGenerator)(nil)

func (generator *FakeIDGenerator) NewTransactionID() domain.TransactionID {
	generator.mutex.Lock()
	defer generator.mutex.Unlock()
	generator.last++
	return FakeTransactionID(generator.last)
}

// FakeTransactionID is the n-th ID returned by a FakeIDGenerator: a valid
// version 4 UUID (PostgreSQL would accept it) whose last group is n, e.g.
// 00000000-0000-4000-8000-000000000001.
func FakeTransactionID(n int) domain.TransactionID {
	return domain.TransactionID(fmt.Sprintf("00000000-0000-4000-8000-%012d", n))
}
