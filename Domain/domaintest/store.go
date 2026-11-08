package domaintest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// FakeStore is an in-memory database of transactions and bets. It implements
// domain.PlayerLocker and domain.UnitOfWork, and its repositories follow the
// same rules as the PostgreSQL ones (Phase 04):
//   - reads and updates are scoped by player: another player's row is "not found";
//   - Update writes only the fields the real repositories write;
//   - the unique indexes are enforced with the same domain errors
//     (ErrTransactionExists, ErrBetAlreadySettled, ErrAlreadyCancelled);
//   - Atomic commits only if fn returns nil, and fails if ctx is already done;
//   - bet IDs come from a counter that, like a sequence, is never rolled back.
//
// Atomic calls run one at a time. It is safe for concurrent use.
type FakeStore struct {
	// Set before use to make every Insert / Update of a transaction fail with
	// it (e.g. to simulate the database going down). The Atomic call that
	// receives the error rolls back, as a real one would.
	InsertTransactionErr error
	UpdateTransactionErr error

	locksMutex sync.Mutex
	locks      map[domain.UserID]chan struct{}

	mutex     sync.Mutex // guards the fields below; held for a whole Atomic call
	state     storeState
	lastBetID domain.BetID
}

var (
	_ domain.PlayerLocker = (*FakeStore)(nil)
	_ domain.UnitOfWork   = (*FakeStore)(nil)
)

// storeState is the committed data. Atomic works on a copy of it.
type storeState struct {
	transactions map[domain.TransactionID]domain.Transaction
	bets         map[domain.BetID]domain.Bet
}

func (state storeState) clone() storeState {
	return storeState{
		transactions: maps.Clone(state.transactions),
		bets:         maps.Clone(state.bets),
	}
}

// NewFakeStore returns an empty store.
func NewFakeStore() *FakeStore {
	return &FakeStore{
		locks: make(map[domain.UserID]chan struct{}),
		state: storeState{
			transactions: make(map[domain.TransactionID]domain.Transaction),
			bets:         make(map[domain.BetID]domain.Bet),
		},
	}
}

// WithPlayerLock runs fn while no other operation of userID runs. Waiting for
// the lock stops when ctx is done.
func (store *FakeStore) WithPlayerLock(ctx context.Context, userID domain.UserID, fn func(ctx context.Context, uow domain.UnitOfWork) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("fake store: lock player %d: %w", userID, err)
	}

	// A channel with room for one value works as a lock that can be abandoned:
	// sending takes it, receiving releases it.
	lock := store.lockOf(userID)
	select {
	case lock <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("fake store: lock player %d: %w", userID, ctx.Err())
	}
	defer func() { <-lock }()

	return fn(ctx, store)
}

func (store *FakeStore) lockOf(userID domain.UserID) chan struct{} {
	store.locksMutex.Lock()
	defer store.locksMutex.Unlock()

	lock, found := store.locks[userID]
	if !found {
		lock = make(chan struct{}, 1)
		store.locks[userID] = lock
	}
	return lock
}

// Atomic runs fn on a copy of the data and keeps the copy only if fn returns nil.
func (store *FakeStore) Atomic(ctx context.Context, fn func(ctx context.Context, repos domain.Repositories) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("fake store: begin: %w", err)
	}

	store.mutex.Lock()
	defer store.mutex.Unlock()

	working := store.state.clone()
	repos := domain.Repositories{
		Transactions: &fakeTransactionRepository{store: store, state: &working},
		Bets:         &fakeBetRepository{store: store, state: &working},
	}
	if err := fn(ctx, repos); err != nil {
		return err
	}

	store.state = working
	return nil
}

// PutTransaction stores tx as it is, replacing any with the same ID and
// skipping the unique rules. For arranging tests.
func (store *FakeStore) PutTransaction(tx domain.Transaction) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.state.transactions[tx.ID] = tx
}

// PutBet stores bet as it is, replacing any with the same ID. For arranging
// tests (e.g. a bet settled after its withdraw).
func (store *FakeStore) PutBet(bet domain.Bet) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.state.bets[bet.ID] = bet
}

// Transactions returns the committed transactions ordered by ID.
func (store *FakeStore) Transactions() []domain.Transaction {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return slices.SortedFunc(maps.Values(store.state.transactions), func(a, b domain.Transaction) int {
		return cmp.Compare(a.ID, b.ID)
	})
}

// Bets returns the committed bets ordered by ID.
func (store *FakeStore) Bets() []domain.Bet {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return slices.SortedFunc(maps.Values(store.state.bets), func(a, b domain.Bet) int {
		return cmp.Compare(a.ID, b.ID)
	})
}

/////////////////////////////////////////////////////////////////////////////////////

// fakeTransactionRepository works on the copy of one Atomic call, which
// already holds the store's mutex.
type fakeTransactionRepository struct {
	store *FakeStore
	state *storeState
}

var _ domain.TransactionRepository = (*fakeTransactionRepository)(nil)

func (repository *fakeTransactionRepository) Insert(_ context.Context, tx domain.Transaction) error {
	if repository.store.InsertTransactionErr != nil {
		return repository.store.InsertTransactionErr
	}
	if _, found := repository.state.transactions[tx.ID]; found {
		return fmt.Errorf("fake store: duplicate transaction id %s", tx.ID)
	}
	for _, existing := range repository.state.transactions {
		if err := uniqueViolation(existing, tx); err != nil {
			return err
		}
	}
	repository.state.transactions[tx.ID] = tx
	return nil
}

// uniqueViolation applies the three partial unique indexes of the
// transactions table to a new row and an existing one.
func uniqueViolation(existing, tx domain.Transaction) error {
	switch {
	case hasIdempotencyKey(existing) && hasIdempotencyKey(tx) &&
		existing.ProviderTransactionID == tx.ProviderTransactionID:
		return domain.ErrTransactionExists
	case isActiveSettlement(existing) && isActiveSettlement(tx) &&
		existing.BetID == tx.BetID:
		return domain.ErrBetAlreadySettled
	case existing.Type == domain.TransactionCancel && tx.Type == domain.TransactionCancel &&
		existing.CancelledTransactionID == tx.CancelledTransactionID:
		return domain.ErrAlreadyCancelled
	default:
		return nil
	}
}

func hasIdempotencyKey(tx domain.Transaction) bool {
	return tx.Type == domain.TransactionWithdraw || tx.Type == domain.TransactionDeposit
}

func isActiveSettlement(tx domain.Transaction) bool {
	return tx.Type == domain.TransactionDeposit &&
		(tx.Status == domain.TransactionPending || tx.Status == domain.TransactionCompleted)
}

func (repository *fakeTransactionRepository) FindByID(_ context.Context, userID domain.UserID, id domain.TransactionID) (domain.Transaction, error) {
	tx, found := repository.state.transactions[id]
	if !found || tx.UserID != userID {
		return domain.Transaction{}, domain.ErrTransactionNotFound
	}
	return tx, nil
}

func (repository *fakeTransactionRepository) FindByProviderTransactionID(_ context.Context, userID domain.UserID, id domain.ProviderTransactionID) (domain.Transaction, error) {
	for _, tx := range repository.state.transactions {
		if tx.UserID == userID && hasIdempotencyKey(tx) && tx.ProviderTransactionID == id {
			return tx, nil
		}
	}
	return domain.Transaction{}, domain.ErrTransactionNotFound
}

func (repository *fakeTransactionRepository) FindCancelOf(_ context.Context, userID domain.UserID, originalID domain.TransactionID) (domain.Transaction, error) {
	for _, tx := range repository.state.transactions {
		if tx.UserID == userID && tx.Type == domain.TransactionCancel && tx.CancelledTransactionID == originalID {
			return tx, nil
		}
	}
	return domain.Transaction{}, domain.ErrTransactionNotFound
}

func (repository *fakeTransactionRepository) FindActiveDeposit(_ context.Context, userID domain.UserID, betID domain.BetID) (domain.Transaction, error) {
	for _, tx := range repository.state.transactions {
		if tx.UserID == userID && tx.BetID == betID && isActiveSettlement(tx) {
			return tx, nil
		}
	}
	return domain.Transaction{}, domain.ErrTransactionNotFound
}

func (repository *fakeTransactionRepository) Update(_ context.Context, tx domain.Transaction) error {
	if repository.store.UpdateTransactionErr != nil {
		return repository.store.UpdateTransactionErr
	}
	stored, found := repository.state.transactions[tx.ID]
	if !found || stored.UserID != tx.UserID {
		return domain.ErrTransactionNotFound
	}
	stored.Status = tx.Status
	stored.FailureReason = tx.FailureReason
	stored.NewBalance = tx.NewBalance
	stored.UpdatedAt = tx.UpdatedAt
	repository.state.transactions[tx.ID] = stored
	return nil
}

/////////////////////////////////////////////////////////////////////////////////////

// fakeBetRepository works on the copy of one Atomic call, which already holds
// the store's mutex.
type fakeBetRepository struct {
	store *FakeStore
	state *storeState
}

var _ domain.BetRepository = (*fakeBetRepository)(nil)

// NextID changes the store directly, not the copy: like a PostgreSQL
// sequence, an ID handed out is never given again, even after a rollback.
func (repository *fakeBetRepository) NextID(_ context.Context) (domain.BetID, error) {
	repository.store.lastBetID++
	return repository.store.lastBetID, nil
}

func (repository *fakeBetRepository) Insert(_ context.Context, bet domain.Bet) error {
	if _, found := repository.state.bets[bet.ID]; found {
		return fmt.Errorf("fake store: duplicate bet id %d", bet.ID)
	}
	repository.state.bets[bet.ID] = bet
	return nil
}

func (repository *fakeBetRepository) FindByID(_ context.Context, userID domain.UserID, id domain.BetID) (domain.Bet, error) {
	bet, found := repository.state.bets[id]
	if !found || bet.UserID != userID {
		return domain.Bet{}, domain.ErrBetNotFound
	}
	return bet, nil
}

func (repository *fakeBetRepository) Update(_ context.Context, bet domain.Bet) error {
	stored, found := repository.state.bets[bet.ID]
	if !found || stored.UserID != bet.UserID {
		return domain.ErrBetNotFound
	}
	stored.Status = bet.Status
	stored.UpdatedAt = bet.UpdatedAt
	repository.state.bets[bet.ID] = stored
	return nil
}
