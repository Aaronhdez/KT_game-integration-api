# 10b — Actions refactor (between Phases 10 and 11)

## Goal

Remove the code that Withdraw, Deposit and Cancel repeat word for word, now
that the three exist and before Phase 11 touches them again to resolve
`PENDING` transactions. At the end:

- an `operation` struct, embedded in the three command handlers, holds their
  shared dependencies and the helpers that are identical in all three
  (`fail`, `leavePending`, `log`, `readOldBalance`, the outcome-write context);
- one function, `classifyWalletResult`, turns a Wallet error into one of four
  outcomes (§9.3), so the rule "only a definitive rejection is `FAILED`" is
  written once;
- **no behaviour change**: no test is modified, and every existing test stays
  green. Constructors keep their signatures, so `Startup` does not change.

No new dependency, no migration, no change outside `Actions/` (plus docs).

## Requirements covered (assessment sections)

- **10.1** — "simple, readable and maintainable code"; **12** — architecture
  and code quality ("clarity, proportion and testability").
- **9.3** — the classification of Wallet outcomes lives in one place.

## Design decisions (and alternatives rejected)

| Decision | Why | Rejected alternative |
|---|---|---|
| **`operation` struct embedded** in `WithdrawCommandHandler`, `DepositCommandHandler`, `CancelCommandHandler`. It holds the six dependencies (`playerRepository`, `locker`, `wallet`, `ids`, `clock`, `logger`) plus `name` (`"withdraw"`, `"deposit"`, `"cancel"`), used as log message and error prefix | The six fields and their constructor code are identical in the three handlers. Embedding promotes the fields and methods, so the handlers keep writing `h.wallet`, `h.clock`, `h.fail(...)`: the bodies barely change. | A base "class" with overridable steps (Go has no inheritance; a template method with callbacks was already rejected in Phase 09 as too clever to explain live). A package-level helper per function with six parameters each (longer calls, same duplication of arguments). |
| **Shared methods** on `operation`: `fail(ctx, uow, pending, betStatus, walletErr)`, `leavePending(pending, betStatus, outcome, cause)`, `log(level, tx, outcome, cause)`, `readOldBalance(ctx, player)`, `outcomeContext(ctx)` | They are the same code in the three files today. `betStatus *domain.BetStatus` covers the only difference: `nil` for a withdraw (no bet yet, §7.4), the bet's current status for deposit and cancel. | Keeping three copies (they already drifted once: only the withdraw's comments mention §9.3). |
| `readOldBalance` and `logTransaction` (today free functions in `operation.go`) become methods of `operation` | They already take the Wallet / logger and the operation name as arguments: exactly what the struct holds. | Leaving them as free functions next to the struct (two ways to do the same thing). |
| **`classifyWalletResult(err) walletOutcome`** with four values: `walletApplied`, `walletAppliedBalanceUnknown`, `walletNotApplied`, `walletOutcomeUnknown`; each handler's `switch` becomes `switch classifyWalletResult(walletErr)` | §9.3 and the `WalletClient` contract are one rule; today it is three `switch` statements with the same `errors.Is` cases. Phase 11 will need the same classification when it re-sends a `PENDING` transaction. | A method that also *acts* on each outcome (each operation writes different things on success, so the actions stay in the handlers). |
| **What stays per handler**: `Execute` (each validates different input), `complete` (create / settle / revert a bet), `replayIfProcessed` / `replayIfCancelled`, the business checks, the derived balance on `walletAppliedBalanceUnknown` (`−` for withdraw, `+` for deposit, `±` for cancel) | Their differences are business rules, not duplication; side by side they are easier to read than behind parameters. | A generic "money operation engine" (rejected in Phase 09). |
| `login.go` is not touched | It is not a money operation and shares none of these helpers. | — |

### Shape after the refactor

```go
// Actions/operation.go
type operation struct {
    name             string
    playerRepository domain.PlayerRepository
    locker           domain.PlayerLocker
    wallet           domain.WalletClient
    ids              domain.IDGenerator
    clock            domain.Clock
    logger           *slog.Logger
}

func (o operation) readOldBalance(ctx, player) (domain.Money, error)
func (o operation) outcomeContext(ctx) (context.Context, context.CancelFunc)
func (o operation) fail(ctx, uow, pending, betStatus *domain.BetStatus, walletErr error) (TransactionResult, error)
func (o operation) leavePending(pending, betStatus *domain.BetStatus, outcome string, cause error) TransactionResult
func (o operation) log(level, tx, outcome string, cause error)

// Actions/wallet_outcome.go
type walletOutcome int
const ( walletApplied walletOutcome = iota; walletAppliedBalanceUnknown; walletNotApplied; walletOutcomeUnknown )
func classifyWalletResult(err error) walletOutcome

// Actions/cancel.go (same for withdraw and deposit)
type CancelCommandHandler struct {
    operation
}
```

## Files to create / modify

| File | Change |
|---|---|
| `Actions/operation.go` | `operation` struct and its methods (replaces the two free functions) |
| `Actions/wallet_outcome.go`, `wallet_outcome_test.go` | `walletOutcome`, `classifyWalletResult` and a table test of the four outcomes |
| `Actions/withdraw.go`, `deposit.go`, `cancel.go` | Embed `operation`; delete their `fail`, `leavePending`, `log`; use `outcomeContext` and `classifyWalletResult` |
| `ARCHITECTURE.md` | `Actions` tables of Phases 08–10: the shared struct and the classifier |
| `docs/plans/00-general-plan.md` | A line under Phase 10 pointing to this plan |

No `_test.go` file of `withdraw`, `deposit` or `cancel` changes.

## Commit sequence

Same working mode as Phases 09–10: Claude writes each step and explains the Go
concepts in the chat; the developer reviews before each commit. Branch:
`09_cancel` (the developer's choice). For every step: `gofmt -l .` empty,
`go vet ./...`, `go build ./...`, `go test ./...` green **without editing any
existing test**.

1. **`docs(plan): add actions refactor plan`** — this file.

2. **`refactor(actions): share the money operation helpers`**
   - `operation` struct + methods in `operation.go`; the three handlers embed
     it; their constructors fill it (same parameters as today).
   - Delete the three copies of `fail`, `leavePending` and `log`, and the
     `writeCtx` boilerplate (`outcomeContext`).
   - *Go concepts:* struct embedding and method promotion (and why it is not
     inheritance: no override, the embedded value does not know its outer
     struct); value vs pointer receivers for a struct that only holds
     interfaces.

3. **`refactor(actions): classify wallet results in one place`**
   - `classifyWalletResult` + table test (`nil`, `ErrWalletAppliedBalanceUnknown`,
     `ErrInsufficientFunds`, `ErrWalletRejected`, `ErrWalletOutcomeUnknown`,
     an unrecognised error, each also wrapped with `fmt.Errorf("…: %w")`).
   - The three `switch` statements use it.
   - *Go concepts:* a typed enum with `iota`; `errors.Is` through wrapping.

4. **`docs(architecture): describe the shared operation helpers`** —
   `ARCHITECTURE.md`, general plan, and an "As built" section here.

## Test plan

- **The existing tests are the safety net:** `go test ./...` green after
  steps 2 and 3 without touching any of them (withdraw, deposit, cancel, the
  controllers and the router).
- **New:** the table test of `classifyWalletResult` (step 3).
- **Check for leftovers:** `grep -n "func (h \*\(Withdraw\|Deposit\|Cancel\)CommandHandler) \(fail\|leavePending\|log\)"`
  prints nothing after step 2.
- No manual check needed: no behaviour or wiring change.

## Open questions

None.

## Definition of done

- `fail`, `leavePending`, `log`, `readOldBalance` and the outcome-write context
  exist once, in `operation.go`; the Wallet outcome rule exists once, in
  `wallet_outcome.go`.
- No existing test modified; `gofmt`, `go vet`, `go build`, `go test ./...`
  green; integration tests unaffected (no `Persistence` change).
- `Actions` still imports only stdlib and `Domain`.
- `ARCHITECTURE.md` and the general plan updated.
- The developer can explain embedding and method promotion.
