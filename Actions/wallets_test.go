package actions

import (
	"context"
	"fmt"
	"testing"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
	"github.com/aaronhdez/kt_game-integration-api/Domain/domaintest"
)

// Wallet wrappers for the money operation tests: each one embeds the
// FakeWallet and changes a single thing of it.

// storeObservingWallet records what the store held when Withdraw or Deposit
// was called.
type storeObservingWallet struct {
	*domaintest.FakeWallet
	store *domaintest.FakeStore
	seen  []domain.Transaction
}

func (wallet *storeObservingWallet) Withdraw(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	wallet.seen = wallet.store.Transactions()
	return wallet.FakeWallet.Withdraw(ctx, operation)
}

func (wallet *storeObservingWallet) Deposit(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	wallet.seen = wallet.store.Transactions()
	return wallet.FakeWallet.Deposit(ctx, operation)
}

// balanceUnknownWallet applies the operation but reports that the new balance
// is unknown, as the real client does for a 2xx with an unusable body.
type balanceUnknownWallet struct {
	*domaintest.FakeWallet
}

func (wallet balanceUnknownWallet) Withdraw(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	if _, err := wallet.FakeWallet.Withdraw(ctx, operation); err != nil {
		return domain.Money{}, err
	}
	return domain.Money{}, fmt.Errorf("test wallet: %w", domain.ErrWalletAppliedBalanceUnknown)
}

func (wallet balanceUnknownWallet) Deposit(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	if _, err := wallet.FakeWallet.Deposit(ctx, operation); err != nil {
		return domain.Money{}, err
	}
	return domain.Money{}, fmt.Errorf("test wallet: %w", domain.ErrWalletAppliedBalanceUnknown)
}

// callerLeavingWallet cancels the caller's context while the Wallet applies
// the operation, as when the provider closes the connection.
type callerLeavingWallet struct {
	*domaintest.FakeWallet
	leave context.CancelFunc
}

func (wallet callerLeavingWallet) Withdraw(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	wallet.leave()
	return wallet.FakeWallet.Withdraw(ctx, operation)
}

func (wallet callerLeavingWallet) Deposit(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	wallet.leave()
	return wallet.FakeWallet.Deposit(ctx, operation)
}

// racingWallet stores an identical transaction when the old balance is read,
// i.e. after our idempotency lookup and before our insert, as if another
// API instance had processed the same request at the same time.
type racingWallet struct {
	*domaintest.FakeWallet
	store  *domaintest.FakeStore
	stored domain.Transaction
}

func (wallet *racingWallet) Balance(ctx context.Context, userID domain.UserID) (domain.WalletBalance, error) {
	wallet.store.PutTransaction(wallet.stored)
	return wallet.FakeWallet.Balance(ctx, userID)
}

func assertTransactionsSeenByTheWallet(t *testing.T, wallet *storeObservingWallet, want ...domain.Transaction) {
	t.Helper()
	assertTransactions(t, wallet.seen, want)
}
