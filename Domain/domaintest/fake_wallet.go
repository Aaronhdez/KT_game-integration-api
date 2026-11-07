// Package domaintest provides test doubles of the domain ports, so use cases
// can be tested without PostgreSQL or the real Wallet.
package domaintest

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	domain "github.com/aaronhdez/kt_game-integration-api/Domain"
)

// Behaviour is the scripted outcome of one call to the FakeWallet.
type Behaviour int

const (
	Succeed         Behaviour = iota // behaves like the real Wallet on a good day
	FailBeforeApply                  // not applied, outcome reported as unknown
	FailAfterApply                   // applied, outcome reported as unknown (the Wallet's 500)
	Reject                           // not applied, definitive rejection
	TimeOut                          // not applied, the call ran out of time
	Slow                             // waits SlowDelay (or until ctx is done), then Succeed
)

// DefaultSlowDelay is how long a Slow call waits unless SlowDelay is changed.
const DefaultSlowDelay = 100 * time.Millisecond

// Call is one call received by the FakeWallet, for assertions.
type Call struct {
	Method    string // "Withdraw", "Deposit" or "Balance"
	UserID    domain.UserID
	Operation domain.WalletOperation // zero value for Balance
}

// FakeWallet is an in-memory domain.WalletClient that simulates the real
// Wallet's failures deterministically (docs/wallet-findings.md):
//   - insufficient funds comes from the balance, with the Wallet's strict rule
//     (a withdraw needs amount < balance);
//   - a currency that is not the player's is rejected;
//   - a re-send of an applied reference is not applied again and returns
//     ErrWalletAppliedBalanceUnknown, like the client does for a 409;
//   - other failures are scripted with Next.
//
// It is safe for concurrent use; a Slow call does not block the others.
type FakeWallet struct {
	SlowDelay time.Duration

	mutex    sync.Mutex
	balances map[domain.UserID]domain.WalletBalance
	applied  map[domain.TransactionID]bool
	script   []Behaviour
	calls    []Call
}

var _ domain.WalletClient = (*FakeWallet)(nil)

// NewFakeWallet returns a FakeWallet holding a copy of balances.
func NewFakeWallet(balances map[domain.UserID]domain.WalletBalance) *FakeWallet {
	wallet := &FakeWallet{
		SlowDelay: DefaultSlowDelay,
		balances:  make(map[domain.UserID]domain.WalletBalance),
		applied:   make(map[domain.TransactionID]bool),
	}
	for userID, balance := range balances {
		wallet.balances[userID] = balance
	}
	return wallet
}

// Next scripts the behaviour of the next calls, in order. With no script left,
// calls Succeed.
func (wallet *FakeWallet) Next(behaviours ...Behaviour) {
	wallet.mutex.Lock()
	defer wallet.mutex.Unlock()
	wallet.script = append(wallet.script, behaviours...)
}

// Calls returns the calls received so far, in order.
func (wallet *FakeWallet) Calls() []Call {
	wallet.mutex.Lock()
	defer wallet.mutex.Unlock()
	return slices.Clone(wallet.calls)
}

// BalanceOf returns the current balance of userID, without recording a call.
func (wallet *FakeWallet) BalanceOf(userID domain.UserID) domain.Money {
	wallet.mutex.Lock()
	defer wallet.mutex.Unlock()
	return wallet.balances[userID].Amount
}

// Balance returns the player's balance. Scripted failures return
// ErrWalletFailure, and TimeOut (or a Slow call cut by ctx) ErrWalletTimeout.
func (wallet *FakeWallet) Balance(ctx context.Context, userID domain.UserID) (domain.WalletBalance, error) {
	behaviour := wallet.start(Call{Method: "Balance", UserID: userID})
	switch behaviour {
	case Slow:
		if !wallet.wait(ctx) {
			return domain.WalletBalance{}, fmt.Errorf("fake wallet: caller gave up: %w", domain.ErrWalletTimeout)
		}
	case TimeOut:
		return domain.WalletBalance{}, fmt.Errorf("fake wallet: scripted timeout: %w", domain.ErrWalletTimeout)
	case FailBeforeApply, FailAfterApply, Reject:
		return domain.WalletBalance{}, fmt.Errorf("fake wallet: scripted failure: %w", domain.ErrWalletFailure)
	}

	wallet.mutex.Lock()
	defer wallet.mutex.Unlock()
	balance, found := wallet.balances[userID]
	if !found {
		return domain.WalletBalance{}, fmt.Errorf("fake wallet: unknown user %d: %w", userID, domain.ErrWalletFailure)
	}
	return balance, nil
}

// Withdraw takes the amount from the player's balance.
func (wallet *FakeWallet) Withdraw(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	return wallet.operate(ctx, "Withdraw", operation, subtract)
}

// Deposit adds the amount to the player's balance.
func (wallet *FakeWallet) Deposit(ctx context.Context, operation domain.WalletOperation) (domain.Money, error) {
	return wallet.operate(ctx, "Deposit", operation, add)
}

// start records the call and takes the next scripted behaviour.
func (wallet *FakeWallet) start(call Call) Behaviour {
	wallet.mutex.Lock()
	defer wallet.mutex.Unlock()
	wallet.calls = append(wallet.calls, call)
	if len(wallet.script) == 0 {
		return Succeed
	}
	behaviour := wallet.script[0]
	wallet.script = wallet.script[1:]
	return behaviour
}

// wait sleeps SlowDelay and reports true, or reports false as soon as ctx is done.
func (wallet *FakeWallet) wait(ctx context.Context) bool {
	select {
	case <-time.After(wallet.SlowDelay):
		return true
	case <-ctx.Done():
		return false
	}
}

// operate runs a Withdraw or Deposit. move computes the new balance and
// reports false when the funds are insufficient.
func (wallet *FakeWallet) operate(ctx context.Context, method string, operation domain.WalletOperation, move func(balance, amount domain.Money) (domain.Money, bool)) (domain.Money, error) {
	behaviour := wallet.start(Call{Method: method, UserID: operation.UserID, Operation: operation})
	switch behaviour {
	case Slow:
		if !wallet.wait(ctx) {
			return domain.Money{}, fmt.Errorf("fake wallet: caller gave up: %w", domain.ErrWalletOutcomeUnknown)
		}
	case FailBeforeApply, TimeOut:
		return domain.Money{}, fmt.Errorf("fake wallet: scripted %s before applying: %w", method, domain.ErrWalletOutcomeUnknown)
	case Reject:
		return domain.Money{}, fmt.Errorf("fake wallet: scripted rejection: %w", domain.ErrWalletRejected)
	}

	wallet.mutex.Lock()
	defer wallet.mutex.Unlock()

	if wallet.applied[operation.Reference] {
		return domain.Money{}, fmt.Errorf("fake wallet: reference %s already applied: %w", operation.Reference, domain.ErrWalletAppliedBalanceUnknown)
	}
	balance, found := wallet.balances[operation.UserID]
	if !found || balance.Currency != operation.Currency {
		return domain.Money{}, fmt.Errorf("fake wallet: unknown user or currency mismatch: %w", domain.ErrWalletRejected)
	}
	newAmount, enough := move(balance.Amount, operation.Amount)
	if !enough {
		return domain.Money{}, fmt.Errorf("fake wallet: %w", domain.ErrInsufficientFunds)
	}

	wallet.balances[operation.UserID] = domain.WalletBalance{Amount: newAmount, Currency: balance.Currency}
	wallet.applied[operation.Reference] = true

	if behaviour == FailAfterApply {
		return domain.Money{}, fmt.Errorf("fake wallet: scripted %s after applying: %w", method, domain.ErrWalletOutcomeUnknown)
	}
	return newAmount, nil
}

// subtract applies the real Wallet's strict rule: the amount must be lower
// than the balance (findings Q5), so the whole balance cannot be withdrawn.
func subtract(balance, amount domain.Money) (domain.Money, bool) {
	if !amount.Decimal().LessThan(balance.Decimal()) {
		return domain.Money{}, false
	}
	return mustMoney(domain.MoneyFromDecimal(balance.Decimal().Sub(amount.Decimal()))), true
}

// add never runs out of funds.
func add(balance, amount domain.Money) (domain.Money, bool) {
	return mustMoney(domain.MoneyFromDecimal(balance.Decimal().Add(amount.Decimal()))), true
}

// mustMoney unwraps the result of the arithmetic above. It can only fail with
// absurd test data (a balance above 10^18), which is a bug in the test.
func mustMoney(money domain.Money, err error) domain.Money {
	if err != nil {
		panic(fmt.Sprintf("fake wallet: invalid balance: %v", err))
	}
	return money
}
