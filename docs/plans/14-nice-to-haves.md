# 14 — Nice-to-haves (after delivery)

> **Status: written point by point.** Point 1 is **built** (branch
> `refactor_code_clean_up`); points 2–4 are only outlined and will be
> detailed one at a time, each one approved before the next is written.

## Goal

Improve the delivered `v1.0.0` without changing what the API promises
(status codes, bodies, the reliability rules of `DESIGN.md`). Four
independent improvements, each one its own point of this plan and its own
series of commits:

1. **Readability refactors** — make `Actions` shorter and easier to read
   aloud, with no behaviour change.
2. **Swagger UI** — browse and try the API from a browser, on top of the
   existing `docs/openapi.yaml`.
3. **River job queue** — replace the pending resolver's periodic scan with a
   PostgreSQL job queue (candidate raised in Phase 11).
4. **OpenTelemetry + Grafana** — traces and metrics exported to a local
   Grafana stack, to see each request, each Wallet call and the `PENDING`
   backlog.

Order: 1 first, because 3 and 4 touch the same handlers and are easier to
review on top of the simpler code. 2, 3 and 4 do not depend on each other.

## Requirements covered (assessment sections)

| Section | Point |
|---|---|
| 10.1 / 12 — readable, maintainable code | 1 |
| 10.2 — nice-to-haves (API docs, observability) | 2, 4 |
| 9.4 — pending resolution | 3 |
| 9.7 — observability without secrets | 4 |

---

## Point 1 — Readability refactors

### Goal

Remove the noise that is left in `Actions/withdraw.go`, `deposit.go` and
`cancel.go` after Phases 10b and 11: long parameter lists, the same lock
boilerplate three times, stringly-typed log outcomes and decimal arithmetic
spelled out by hand. **No behaviour change**: no HTTP response, log field or
database write changes, and the existing tests stay green; a test file is
only touched where a signature it calls changes (said per refactor).

### Candidates

Each row is independent. **Chosen by the developer: R1–R5.**

| # | Refactor | Today | After | Tests touched |
|---|---|---|---|---|
| R1 | **Validated request structs** per operation: `withdrawRequest{player, providerID, amount}`, `depositRequest{player, providerID, withdrawnID, amount, currency}`, `cancelRequest{player, providerID}`, built by `Execute` after parsing | `depositLocked` takes 7 parameters, deposit's `replayIfProcessed` 7, `replayAfterDuplicateInsert` 8; every call repeats `player.UserID, providerID, withdrawnID, amount, currency` | `depositLocked(ctx, uow, request)`, `replayIfProcessed(ctx, uow, request)`… The parsed, trusted values travel together; the `Command` stays the raw input from `Application` | None (unexported) |
| R2 | **`operation.runLocked`**: `func (o operation) runLocked(ctx, userID, fn func(ctx, uow) (TransactionResult, error)) (TransactionResult, error)` | The three `Execute` end with the same 6 lines: `var result`, `WithPlayerLock`, `var lockedErr error`, assign, return | `return handler.runLocked(ctx, player.UserID, func(ctx, uow) (TransactionResult, error) { return handler.depositLocked(ctx, uow, request) })` | None |
| R3 | **Typed log outcomes**: `type outcome string` with constants `outcomeCompleted`, `outcomeCompletedWithoutWallet`, `outcomePending`, `outcomePendingOverdue`, `outcomeBalanceUnknown`, `outcomeNotStored`, `outcomeRejected`, `outcomeInsufficientFunds` | 8 string literals spread over 4 files; `leavePending` compares `outcome == "pending"` — a typo compiles | The compiler checks every outcome; the list of possible values is in one place (also useful for Point 4, where outcomes become metric labels). Same strings in the log | None (log values unchanged) |
| R4 | **`Money.Add` / `Money.Sub`** in `Domain/money.go`, returning `(Money, error)` like `MoneyFromDecimal` | `domain.MoneyFromDecimal(pending.OldBalance.Decimal().Sub(pending.Amount.Decimal()))` in withdraw, deposit and `balanceAfterCompensation` | `pending.OldBalance.Sub(pending.Amount)` — reads like the rule it implements | New unit tests in `money_test.go` |
| R5 | **`replayOrResend`** helper per handler, wrapping the first block of each `xxxLocked`: look up the previous request; if `PENDING`, re-send it as a replay | The same 7-line `if result, found, err := …; found \|\| err != nil { if … PENDING { return asReplay(resendPending…) } … }` in the three handlers | `if result, handled, err := handler.replayOrResend(ctx, uow, request); handled { return result, err }` — the `xxxLocked` body starts with the new-request path | None |
### Not proposed / not chosen (and why)

| Idea | Why not |
|---|---|
| R6 — an `actions.Dependencies` struct for the six shared constructor parameters | Not chosen: it changes `Startup/main.go` and every test that builds a handler, for a list that is only written in a few places. |
| R7 — splitting the largest test files (`deposit_test.go` 1 335 lines, `cancel_test.go` 1 101, `withdraw_test.go` 1 000) by concern | Not chosen: it moves code without making any test easier to read. |
| Moving action-specific errors to `Actions/errors.go` | Already decided against in Phase 13 (`docs/decisions.md`, "Where errors live"). |
| A generic "money operation engine" / template method | Rejected in Phases 09 and 10b: too clever to explain live. |
| Refactoring `resolve_pending.go`'s `switch tx.Type` into an interface map | Point 3 (River) rewrites that file; refactor it there or not at all. |
| Controllers in `Application` (withdraw/deposit share ~15 lines) | They are short and read top to bottom; a shared helper would hide the request decoding of each endpoint. |

### Files to modify

| File | Refactors |
|---|---|
| `Actions/operation.go` | R2, R3 |
| `Actions/withdraw.go`, `deposit.go`, `cancel.go` | R1, R2, R3, R4, R5 |
| `Domain/money.go`, `money_test.go` | R4 |
| `ARCHITECTURE.md` | Actions section, if R1/R2/R5 change what it describes |

### Commit sequence (Point 1)

Working mode as in Phases 09–13: Claude writes each step and explains it
in the chat; the developer reviews it before it is committed. For every step: `gofmt -l .` empty, `go vet ./...`,
`go build ./...`, `go test ./...` green. Branch: `refactor_code_clean_up`
(the developer's choice; each later point gets its own branch).

1. `docs(plan): add phase 14 plan` — this file (Point 1 detailed).
2. `feat(domain): add Money Add and Sub` — R4 methods + table tests.
3. `refactor(actions): use Money arithmetic for derived balances` — R4 callers.
4. `refactor(actions): type the operation log outcomes` — R3.
5. `refactor(actions): run each operation under the lock with one helper` — R2.
6. `refactor(actions): pass validated requests instead of long parameter lists` — R1.
7. `refactor(actions): share the replay-or-resend step of each operation` — R5.
8. `docs(architecture): describe the refactored actions` — only if needed.

*Go concepts to explain in the chat:* methods on a value type returning
`(T, error)` (R4); a named string type with constants vs an `iota` enum, and
why a string type keeps the log unchanged (R3); passing a closure (`func`
literal) that captures variables, the Go equivalent of a C# lambda (R2).

### Test plan (Point 1)

- The existing unit tests are the safety net: no test of `withdraw`,
  `deposit`, `cancel`, `resolve_pending`, the controllers or the router is
  modified by R1–R5.
- New: `Money.Add` / `Money.Sub` tests (normal, result with > 2 decimals is
  impossible by construction, negative result allowed or not — see open
  question).
- `go test -count=1 -tags=integration ./Persistence/...` unaffected (no
  `Persistence` change) — run once at the end.

### Open questions (Point 1)

1. ~~Which candidates go in?~~ **Resolved:** R1–R5; R6 and R7 not chosen.
2. ~~`Money.Sub` with a negative result?~~ **Resolved:** `Add` and `Sub`
   delegate to `MoneyFromDecimal`, which decides what is valid (same
   behaviour as now).
3. ~~Working mode?~~ **Resolved:** Claude writes each step and the developer
   reviews it before it is committed (as in Phases 09–13).

### As built — deviations from this plan (Point 1)

| Planned | Built | Why |
|---|---|---|
| R3: `type outcome string` | `type logOutcome int` with `iota` and a `String()` method, in `Actions/log_outcome.go` | An untyped string literal converts silently to a named string type, so `"pendng"` would still compile; it cannot convert to an `int` type. Same shape as `walletOutcome`; the name avoids confusing the two. Log values unchanged (the tests that read the log pass untouched). |
| R1: `depositRequest` with a `currency` field | No `currency` field: the code uses `request.player.Currency` | `Execute` has already checked they are equal; two fields could contradict each other. |
| R5: `replayOrResend` makes the handlers shorter | It adds ~45 lines (one 12-line helper per handler) | Each handler has its own lookup and `resendPending`; sharing the helper would need function parameters or generics, rejected in Phases 09/10b. Kept for readability: each `xxxLocked` now starts with one `if … handled` and the re-send rule has a name. |
| — | Typo fixed in a `cancel.go` comment ("findWhatToRevertthen") | Found while editing; committed with R1. |

Commits: `docs(plan)` → `feat(domain)` R4 → `refactor(actions)` R4, R3, R2,
R1, R5 → `docs(architecture)`. No existing test was modified; `go test ./...`
green after every step. `resolve_pending.go` untouched (Point 3).

---

## Point 2 — Swagger UI *(outline, to be detailed)*

- Serve `docs/openapi.yaml` and a Swagger UI page so the API can be tried
  from a browser (login → *Authorize* with the token → call the endpoints).
- Reverses a Phase 13 decision ("the spec is not served by the API"): to be
  justified here.
- Options to compare: a `swaggerapi/swagger-ui` container in
  `docker-compose.yml` pointed at the file (no Go change) vs `GET /docs`
  served by the API with `embed` (spec + static UI assets) vs generating the
  spec from code (`swaggo/swag`, already rejected in Phase 13).
- Points to settle: CORS if the UI runs on another origin; whether `/docs`
  is enabled in every environment or behind a config flag.

## Point 3 — River job queue replacing the pending resolver *(outline, to be detailed)*

- Replace `Startup/resolver.go` (ticker) + `ResolvePendingCommandHandler`'s
  scan with `riverqueue/river` on the same PostgreSQL: a "resend pending"
  job inserted **in the same DB transaction** as the `PENDING` row (River's
  `InsertTx` with pgx), so a crash cannot lose it.
- Per-transaction backoff and max attempts instead of a global interval;
  safe with several API instances (River locks jobs in the database).
- Points to settle: River's own migrations next to goose; how `Actions`
  enqueues a job without importing River (a new port in `Domain`, e.g.
  `PendingScheduler`, implemented in `Persistence` or `Shared`); what
  happens to the existing `PENDING` rows at upgrade; keeping the 10 s
  `resendDelay` rule; the overdue alert; River UI (optional).

## Point 4 — OpenTelemetry + Grafana *(outline, to be detailed)*

- Instrument with the OpenTelemetry Go SDK: HTTP server spans
  (`otelhttp`), outgoing Wallet calls (`otelhttp` transport), PostgreSQL
  (`otelpgx`), and a span per operation in `Actions` (through a port, so
  `Actions` does not import the SDK — to decide).
- Metrics: requests by route and status, Wallet call duration and outcome
  (applied / not applied / unknown), `PENDING` count and age, resolver or
  River job results.
- Export via OTLP to a local stack added to `docker-compose.yml` (e.g.
  `grafana/otel-lgtm`: collector + Tempo + Prometheus/Mimir + Loki +
  Grafana in one image) with a provisioned dashboard.
- Points to settle: correlate `slog` lines with trace IDs; never put tokens,
  API key or amounts-as-labels in telemetry (§9.7); telemetry off by default
  when no endpoint is configured; one image vs separate services.

---

## Definition of done (whole phase)

- Each point approved here before its code starts; each one merged
  separately with `gofmt`, `go vet`, `go build`, `go test ./...` green and
  CI green.
- No change to the API contract of `README.md` / `docs/openapi.yaml`
  unless a point says so explicitly.
- `README.md`, `DESIGN.md`, `ARCHITECTURE.md`, `AGENTS.md` and the general
  plan updated by the point that changes them.
- The developer can explain every new line and every new dependency.
