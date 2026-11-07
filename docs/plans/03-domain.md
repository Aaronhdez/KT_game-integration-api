# 03 — Domain

## Goal

Build the **business core** in `Domain/`: the types and rules that every later
phase uses, with no dependency on HTTP, the database or the Wallet client
(assessment §10.1 "Clean Architecture"). At the end of the phase:

- money is represented exactly and validated in one place (`Money`, `Currency`);
- bets and transactions exist with their statuses, and **every status change of
  the assessment's tables is a method that refuses invalid transitions**;
- the **Cancel table of §8.5** is a pure, unit-tested decision function;
- domain errors exist, grouped so that `Application` can map them to the HTTP
  codes of §7.3 with a small table;
- the ports (interfaces) that the use cases of Phases 06–10 need from the outside
  world are declared.

Everything here is plain Go plus `shopspring/decimal`, tested without Docker.

## Requirements covered (assessment sections)

- **5.2 / 7** — IDs above 32 bits (`int64`), very large and fractional balances,
  currencies `USD`/`EUR`/`KES`, amounts with at most two decimals, never `float64`.
- **6.1** — transaction types (`WITHDRAW`, `DEPOSIT`, `CANCEL`), transaction
  statuses (`COMPLETED`, `PENDING`, `FAILED`, `CANCELLED`) and the bet status table
  (`PLACED`, `WON`, `LOST`, `CANCELLED`).
- **7.3** — error categories behind every HTTP error code.
- **8.3 / 8.4** — amount rules (withdraw `> 0`, deposit `>= 0`; zero ⇒ `LOST`).
- **8.5** — every row of the Cancel table except "unknown / another player"
  (that one needs a repository lookup: Phase 10).
- **10.1** — business rules in a layer reached through interfaces it defines.
- **12** — "Money handling" and "Cancel semantics" evaluation criteria.

## Design decisions (and alternatives rejected)

| Decision | Why | Rejected alternative |
|---|---|---|
| Add **`github.com/shopspring/decimal` v1.6.0** (first dependency) | Exact decimal arithmetic, the closest thing to .NET `decimal`; parses the Wallet's string balances (`"9000000000.09"`) exactly; widely used and stable. AGENTS.md requires a reason for every dependency: this is it. | `float64` (forbidden by §7); `math/big.Rat` (stdlib, but no decimal formatting/parsing and much clunkier); integer cents `int64` (exact, but every conversion to/from JSON and the Wallet becomes our code and our bugs). |
| `Money` is a **struct with an unexported `decimal.Decimal` field**, built only through constructors that validate it | The invariants (≥ 0, ≤ 2 decimals, ≤ 18 integer digits) can then never be broken from outside the package — like a C# type with a private constructor and factory methods. | Exposing `decimal.Decimal` directly (every caller must remember to validate). |
| `Money` does **not** carry a currency; currency travels next to it (`Transaction.Currency`, `Bet.Currency`) | The service never adds amounts of different transactions: balances come from the Wallet. The only currency rule is "must match", checked explicitly where it applies (§7.3, §8.4). Keeps `Money` small. | `Money{Amount, Currency}` with currency checks in every operation (more code, no rule needs it). |
| `Money` invariants: **non-negative**, **at most 2 decimals**, **at most 18 integer digits** | §7 (two decimals); amounts and balances are never negative in this domain; 18 integer digits = the DB type `NUMERIC(20,2)` chosen in the general plan, so a value that passes validation always fits the column. "> 0" for withdraw is a rule of the withdraw (below), not of `Money`. | Allowing negatives (no use case); no upper bound (a huge amount would fail later, at the DB, with an obscure error). |
| `Money` implements **`MarshalJSON` / `UnmarshalJSON`** (in `Domain`) | §7 "amounts are JSON numbers with at most two decimal places": decoding goes straight from the JSON text to `decimal` (never through `float64`) and rejects strings, `null`, negatives and 3+ decimals; encoding always writes a number with 2 decimals (`10.50`). One place enforces the money format for every DTO. `encoding/json` is stdlib, allowed in `Domain`. | A separate DTO money type in `Application` (duplicates the rules); decoding into `float64` and converting (precision loss — exactly what reviewers look for). |
| `Currency` is a named `string` type with three constants; **only the exact uppercase codes are valid** | The Wallet rejects `usd` (findings Q4), and §7.3 treats an invalid currency as `400`. Being strict is simpler and honest. | Normalising `usd` → `USD` (silently accepts input the spec does not define). |
| **Named types for identifiers**: `UserID`, `BetID` (`int64`), `TransactionID`, `ProviderTransactionID` (`string`) | The compiler then refuses to pass a bet ID where a user ID is expected — both are `int64` and both reach the Wallet. Costs a conversion at the edges only. | Plain `int64`/`string` everywhere (a swapped argument compiles and corrupts data). |
| `ProviderTransactionID` must be non-empty and ≤ 128 characters | It is the idempotency key and will be a `UNIQUE` DB column; empty would collide, unbounded length is an abuse vector. | No validation (an empty key would make every "empty" request a replay of the first one). |
| **Bet and Transaction status changes are methods** (`bet.Settle`, `tx.Complete`, …) that return an error for an invalid transition | The tables of §6.1 become code that cannot be bypassed; each row and each forbidden transition is a unit test. | Setting `Status` fields directly in the use cases (rules scattered across Phases 08–10). |
| A **`Bet` is created only when its withdraw completes**; the `BetID` is reserved earlier and stored on the `PENDING` withdraw | §8.3 "a completed withdraw creates a bet"; §7.4 `betStatus` is `null` while the withdraw is pending. The Wallet needs the `betId` in the first call, so it is reserved before (intent-first, general plan §3.1). | Creating a bet in a "pending" status (a status the spec does not define). |
| **`DecideCancel(original, bet)`** — a pure function that returns *what to do* for a cancel (credit back, debit back, no money movement) or the domain error | Turns the §8.5 table into one function with one test per row, without Wallet or DB. Phase 10 only executes the decision. | Encoding the table inside the Cancel use case (only testable with fakes of everything). |
| **Errors: 7 category errors** mirroring §7.3 (`ErrInvalidInput`, `ErrUnauthorized`, `ErrNotFound`, `ErrConflict`, `ErrInsufficientFunds`, `ErrWalletFailure`, `ErrWalletTimeout`) and **specific errors that wrap one category** (`ErrBetAlreadySettled` wraps `ErrConflict`, …) | `Application` maps 7 categories with `errors.Is`, while use cases and tests can still check the precise reason. No custom error struct is needed. | One error type with a `Kind` field (more code); only specific errors (the HTTP mapping table would grow with every new error). |
| Ports declared now: **`WalletClient`, `PlayerRepository`, `Clock`, `IDGenerator`, `PasswordHasher`, `TokenIssuer`** | Their shape is already known (Wallet findings, §8.1). | — |
| **Bet/Transaction repositories and the unit-of-work/locking port are deferred to Phase 04** | Their shape depends on how idempotency and per-player locking are implemented in PostgreSQL; designing them now would be guesswork. *This narrows the Phase 03 row of the general plan — updated in the last commit.* | Declaring them now and rewriting them in 04/08. |
| **No fakes in `Domain/domaintest` yet** | Nothing in this phase needs them: domain functions receive `now time.Time` and IDs as parameters instead of calling a `Clock`/`IDGenerator`. The fake Wallet arrives with the client in Phase 05. | Writing fakes before anything uses them. |
| Tests follow the project style: one test per behaviour, Arrange/Act/one assert, logic in `t.Helper()` helpers; **tables only for cases of identical shape** (e.g. the list of invalid money strings) | Same as Phase 02 (see `02-bootstrap.md`, As built). | — |

## Files to create / modify

| File | Content |
|---|---|
| `go.mod`, `go.sum` | `require github.com/shopspring/decimal v1.6.0`; `go.sum` holds its checksum. |
| `Domain/errors.go` | Category errors and specific errors (list below). |
| `Domain/errors_test.go` | A specific error is recognised as its category through `errors.Is`, also after extra wrapping. |
| `Domain/currency.go` | `Currency`, `USD`/`EUR`/`KES`, `ParseCurrency`. |
| `Domain/currency_test.go` | Valid codes; lowercase, empty and unknown codes fail with `ErrInvalidCurrency`. |
| `Domain/money.go` | `Money`, `ParseMoney`, `MoneyFromDecimal`, `Zero`, `IsZero`, `IsPositive`, `Equal`, `Decimal`, `String`, `MarshalJSON`, `UnmarshalJSON`. |
| `Domain/money_test.go` | Parsing rules, formatting, JSON round-trip, rejected JSON inputs. |
| `Domain/ids.go` | `UserID`, `BetID`, `TransactionID`, `ProviderTransactionID` + `ParseProviderTransactionID`. |
| `Domain/player.go` | `Player` entity (`UserID`, `Username`, `PasswordHash`, `Currency`). |
| `Domain/bet.go` | `BetStatus`, `Bet`, `NewPlacedBet`, `Settle`, `Cancel`, `RevertSettlement`. |
| `Domain/bet_test.go` | One test per row of the §6.1 bet table and per forbidden transition. |
| `Domain/transaction.go` | `TransactionType`, `TransactionStatus`, `Transaction`, `NewWithdraw`, `NewDeposit`, `NewCancel`, `Complete`, `Fail`, `MarkCancelled`. |
| `Domain/transaction_test.go` | Constructors' amount rules; allowed and forbidden status transitions. |
| `Domain/cancel.go` | `CancelAction`, `CancelDecision`, `DecideCancel`. |
| `Domain/cancel_test.go` | One test per row of the §8.5 table (except the 404 row). |
| `Domain/ports.go` | `WalletClient` (+ `WalletOperation`, documented error contract), `PlayerRepository`, `Clock`, `IDGenerator`, `PasswordHasher`, `TokenIssuer`. |
| `ARCHITECTURE.md` | Domain contents and statuses; repositories now listed under Phase 04. |
| `DESIGN.md` §5 | Money rules, strict currencies, cancel of `PENDING`/`FAILED`, named IDs. |
| `docs/plans/00-general-plan.md` | Phase 03 row: repositories/unit of work moved to Phase 04. |

### Errors (content of `Domain/errors.go`)

| Category (→ HTTP in `Application`) | Specific errors wrapping it |
|---|---|
| `ErrInvalidInput` (400) | `ErrInvalidAmount`, `ErrInvalidCurrency`, `ErrCurrencyMismatch`, `ErrInvalidProviderTransactionID` |
| `ErrUnauthorized` (401) | `ErrInvalidCredentials` (Phase 06 may add more) |
| `ErrNotFound` (404) | `ErrTransactionNotFound` |
| `ErrConflict` (409) | `ErrIdempotencyConflict`, `ErrBetAlreadySettled`, `ErrBetNotSettled`, `ErrTransactionPending`, `ErrNothingToRevert`, `ErrWithdrawNotCompleted`, `ErrInvalidTransition` |
| `ErrInsufficientFunds` (422) | — (used directly) |
| `ErrWalletFailure` (502) | `ErrWalletRejected` |
| `ErrWalletTimeout` (504) | — (used directly; reads only, §7.3) |
| *(no HTTP code — handled by the use case)* | `ErrWalletOutcomeUnknown` → `PENDING` + `202`; `ErrAlreadyCancelled` → replay `200` |

The last row matters: an unknown Wallet outcome is **not an error for the
client** (§7.2 says `202`), so it must never reach the HTTP error mapper.

### Bet transitions (§6.1)

| Method | From | To | Otherwise |
|---|---|---|---|
| `NewPlacedBet(id, userID, currency, stake, now)` | — | `PLACED` | — |
| `bet.Settle(amount, now)` | `PLACED` | `WON` if `amount > 0`, `LOST` if `amount = 0` | `ErrBetAlreadySettled` |
| `bet.Cancel(now)` (cancel of the withdraw) | `PLACED` | `CANCELLED` | `ErrBetAlreadySettled` if `WON`/`LOST` ("the deposit has to be cancelled first"), `ErrInvalidTransition` if `CANCELLED` |
| `bet.RevertSettlement(now)` (cancel of the deposit) | `WON`, `LOST` | `PLACED` | `ErrBetNotSettled` |

### Transaction transitions (§6.1, §9.3)

| Method | From | To | Otherwise |
|---|---|---|---|
| `NewWithdraw(…)` | — | `PENDING` | `ErrInvalidAmount` if `amount = 0` |
| `NewDeposit(…)` | — | `PENDING` | — (zero allowed) |
| `NewCancel(original, …)` | — | `PENDING` | amount and currency copied from `original` |
| `tx.Complete(newBalance, now)` | `PENDING` | `COMPLETED` | `ErrInvalidTransition` |
| `tx.Fail(now)` | `PENDING` | `FAILED` | `ErrInvalidTransition` |
| `tx.MarkCancelled(now)` | `COMPLETED` (`WITHDRAW`/`DEPOSIT` only) | `CANCELLED` | `ErrInvalidTransition` |

`Transaction` fields: `ID`, `Type`, `Status`, `UserID`, `BetID`,
`ProviderTransactionID` (for a `CANCEL`: the one of the cancelled transaction,
as §7.4 returns it), `CancelledTransactionID` (internal ID of the original, only
for `CANCEL`), `Amount`, `Currency`, `OldBalance`, `NewBalance *Money` (`nil`
while not completed — §7.4 `null`), `CreatedAt`, `UpdatedAt`. Fields needed only
for idempotency (payload hash) are added in Phase 08, where their content is
decided.

### Cancel decision (§8.5)

| Original transaction | Bet | `DecideCancel` returns |
|---|---|---|
| `WITHDRAW` `COMPLETED` | `PLACED` | `CreditBack` the withdraw amount → afterwards original `CANCELLED`, bet `CANCELLED` |
| `WITHDRAW` `COMPLETED` | `WON` / `LOST` | `ErrBetAlreadySettled` (409) |
| `DEPOSIT` `COMPLETED`, amount > 0 | `WON` | `DebitBack` the deposit amount → original `CANCELLED`, bet `PLACED` (Wallet may answer insufficient funds → 422, nothing changes: Phase 10) |
| `DEPOSIT` `COMPLETED`, amount = 0 | `LOST` | `NoWalletCall` → original `CANCELLED`, bet `PLACED` |
| any, `CANCELLED` | — | `ErrAlreadyCancelled` (use case returns the earlier cancel, 200) |
| any, `PENDING` | — | `ErrTransactionPending` (409) |
| any, `FAILED` | — | `ErrNothingToRevert` (409) |
| *unknown or another player's* | — | *not decided here: repository lookup in Phase 10 → 404* |

## Commit sequence

**Working mode** — same loop as Phase 02. Proposed split (see open question 1):

| Steps | Who writes |
|---|---|
| 2, 10, 12 | Claude writes and explains |
| **3, 4, 5, 6, 7, 8, 9, 11** | **Developer**, from step-by-step instructions; Claude reviews |

For every step: `gofmt -l .` empty, `go vet ./...`, `go build ./...`,
`go test ./...` green; Claude proposes the commit message.

---

1. **`docs(plan): add phase 03 domain plan`** — this file.

2. **`feat(domain): add domain error categories`**
   - `Domain/errors.go` with the table above; `Domain/errors_test.go`.
   - *Go concepts:* errors are values; sentinel errors (`var ErrX = errors.New(…)`,
     ≈ well-known exception types); wrapping with `fmt.Errorf("%w: …", ErrCategory)`;
     `errors.Is` walks the chain (≈ `catch (ConflictException)` matching a subclass);
     `errors.As` for typed errors (mentioned, not used yet).

3. **`feat(domain): add currency type`**
   - `Currency` (`type Currency string`), constants, `ParseCurrency(s string) (Currency, error)`.
   - *Go concepts:* named types over built-ins; typed constants (Go's closest
     thing to an `enum`, and its limits: any string can still be converted);
     why a `Parse…` function is the real guard.

4. **`feat(domain): add money value object`**
   - `go get github.com/shopspring/decimal@v1.6.0`.
   - `Money` with `ParseMoney(s string)`, `MoneyFromDecimal(d decimal.Decimal)`
     (both validate), `Zero()`, `IsZero`, `IsPositive`, `Equal`, `Decimal()`,
     `String()` (always 2 decimals: `"10.50"`).
   - *Go concepts:* third-party modules (`go get`, `require` in `go.mod`, `go.sum`
     ≈ NuGet lock); unexported fields as the only way to protect an invariant;
     value receivers (`func (m Money) …`) for immutable values; why `Equal` and
     not `==` on `decimal` (`10.5` and `10.50` have different internal scale).

5. **`feat(domain): encode money as JSON numbers`**
   - `MarshalJSON` writes a JSON number with 2 decimals; `UnmarshalJSON` accepts
     only a JSON number token and applies the `Money` rules.
   - Tests: `10.5` → `10.50`; rejects `"10.50"` (string), `null`, `-1`, `1.005`,
     `1e400`; round-trip of `9000000000.09`.
   - *Go concepts:* implementing `json.Marshaler`/`json.Unmarshaler` implicitly;
     pointer receiver on `UnmarshalJSON` (it must modify the value); `[]byte`.

6. **`feat(domain): add identifier types and player`**
   - `Domain/ids.go`, `Domain/player.go`; `ParseProviderTransactionID` (non-empty,
     ≤ 128 characters). Tests for the parser.
   - *Go concepts:* conversions between named types (`domain.UserID(n)`); `len`
     counts bytes, `utf8.RuneCountInString` counts characters (which one we use and why).

7. **`feat(domain): add bet with status transitions`**
   - `Domain/bet.go` + tests per the bet table.
   - *Go concepts:* pointer receivers for methods that change state
     (`func (b *Bet) Settle(…) error`); returning an error instead of throwing;
     `switch` on a status.

8. **`feat(domain): add transaction with status transitions`**
   - `Domain/transaction.go` + tests per the transaction table.
   - *Go concepts:* pointer fields as "nullable" (`*Money` ≈ `Money?`); passing
     `now time.Time` instead of calling `time.Now()` so tests are deterministic.

9. **`feat(domain): add cancel decision rules`**
   - `Domain/cancel.go` + one test per row of the cancel table.
   - *Go concepts:* small result structs; `errors.Is` in tests; why pure functions
     are the cheapest code to test.

10. **`feat(domain): add ports for the outside world`**
    - `Domain/ports.go`. `WalletClient` methods take `ctx context.Context` first;
      its doc comment states the **error contract** used by Phases 05 and 08:
      `nil` = applied; `ErrInsufficientFunds` / `ErrWalletRejected` = not applied;
      `ErrWalletOutcomeUnknown` = unknown. `Balance` read errors:
      `ErrWalletFailure` / `ErrWalletTimeout`.
    - *Go concepts:* interfaces declared where they are used, not where they are
      implemented (opposite of the usual C# habit); implicit satisfaction;
      `context.Context` as first parameter; small interfaces.

11. **(developer) review pass and exercise** — no new production code: the developer
    explains in their own words the bet and cancel tables against the tests, and
    adds any missing test found while doing so. *(Commit only if tests are added:
    `test(domain): …`.)*

12. **`docs(domain): document domain model and money rules`**
    - `ARCHITECTURE.md`, `DESIGN.md` §5, `00-general-plan.md` row 03, and an
      "As built" section in this plan.

## Test plan

- **Unit (`go test ./...`, no Docker):**
  - errors: every specific error `Is` its category;
  - currency: valid / invalid codes;
  - money: valid strings (`0`, `0.01`, `10.5`, `9000000000.09`, 18 integer
    digits), invalid ones (negative, 3 decimals, 19 integer digits, empty,
    `abc`, `NaN`); `String()` formatting; JSON encode/decode and rejected JSON;
  - bet: each transition of the table, each forbidden transition;
  - transaction: constructors' amount rules, each allowed/forbidden transition,
    `NewBalance` is `nil` until `Complete`;
  - cancel: each row of the §8.5 table (7 rows).
- **Integration / manual:** none (no I/O in this phase).

## Open questions

1. ~~**Who writes which step?**~~ **Resolved:** as proposed, plus steps 4 and 5 (`Money` and its JSON),
   which the developer asked to write too — developer writes
   3, 6, 7, 8, 9; Claude writes 2, 4, 5, 10, 12.
2. ~~**Upper bound for amounts**~~ **Resolved:** 18 integer digits.
3. ~~**Wallet balance with more than 2 decimals**~~ **Resolved in Phase 05:** a
   balance read with 3 decimals → `ErrWalletFailure`; an applied operation whose
   balance has 3 decimals → `ErrWalletAppliedBalanceUnknown` (completed, never
   `FAILED`). Original note: cannot happen through our API
   (we validate every amount and always send 2 decimals; seeded balances have
   ≤ 2), only if someone operates on the Wallet directly. `Money` stays strict.
   How the Wallet client handles it is decided in Phase 05, with one constraint
   fixed now: **a balance read** (`GET /player`, `oldBalance`) that is not a valid
   `Money` → `ErrWalletFailure` (502 / the operation is not started); **an applied
   operation must never become `FAILED`** just because its returned balance has 3
   decimals.
4. ~~**Who generates `BetID`**~~ **Resolved:** a PostgreSQL sequence (Phase 04);
   `IDGenerator` only generates `TransactionID` (UUID).
5. ~~**New balance after a `409 DuplicatedTransaction`**~~ **Resolved in Phase
   05:** the signature stays `(Money, error)`; a `409` returns the new sentinel
   `ErrWalletAppliedBalanceUnknown` (applied, balance unknown). How such a
   transaction is completed is decided in Phases 08/11.

## As built — deviations from this plan

| Planned | Built | Why |
|---|---|---|
| Claude writes steps 2, 4, 5, 10, 12 | Developer wrote 3–9 (business code); Claude wrote 2, 10, 12 and, on request, the tests of 7–9 and the code of 7 and 9 | The developer chose to write `Money` and its JSON too, and asked for help on the larger steps. |
| `Money.Zero()` | No `Zero()`; the zero value `Money{}` is 0.00 | A function returning the zero value adds nothing. |
| `ParseMoney` / `MoneyFromDecimal` reject in that order | Upper limit checked before the sign | Same outcome; all three rules always apply. |
| `Domain/cancel.go` | `Domain/cancel_decision.go` | Developer's choice; Go file names are lowercase snake_case. |
| `IDGenerator` | `NewTransactionID()` only | Bet IDs come from a DB sequence (open question 4). |
| — | `ErrPlayerNotFound` (wraps `ErrNotFound`) | Needed by `PlayerRepository`; login maps it to `ErrInvalidCredentials` (§8.1). |
| `PasswordHasher` | `Verify` only, returns `error` | No use case hashes passwords (seed is precomputed); an error distinguishes "wrong password" from "unusable hash". |
| `TokenIssuer` | Returns `IssuedToken{Value, ExpiresAt}` | Login must return `expiresAt` (§8.1). Token *verification* is declared with the auth middleware (Phase 06). |
| — | `WalletOperation`, `WalletBalance` | Parameter/result types of `WalletClient`. |
| Table-driven tests | Tables only for same-shape cases (currency, money, IDs, errors); one test per transition / §8.5 row | Project test style (`02-bootstrap.md`, As built). |
| — | Found while writing `Money` JSON: a missing `amount` decodes to 0 | Recorded in `DESIGN.md` §5; the deposit DTO handles it in Phase 09. |

Step 11 (review pass) was done partially, for the bet table (§6.1).

## Definition of done

- `Domain/` imports only the standard library and `shopspring/decimal`
  (`go list -f '{{.Imports}}' ./Domain`).
- No `float32`/`float64` anywhere in `Domain/`.
- `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` green.
- Every row of the §6.1 bet table and of the §8.5 cancel table (except 404) has a
  test whose name says which row it covers.
- `ARCHITECTURE.md`, `DESIGN.md`, `00-general-plan.md` updated (step 12).
- The developer can explain each Go concept listed in steps 2–10.
