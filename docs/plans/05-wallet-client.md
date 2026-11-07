# 05 — Wallet client

## Goal

Implement the `domain.WalletClient` port (declared in Phase 03) as an **HTTP
client for the mock Wallet** in `Shared/wallet`, and give the use cases of
Phases 07–11 a **deterministic fake** of it in `Domain/domaintest`. At the end of
the phase:

- every Wallet call is bounded by a timeout and sends `x-api-key`;
- every operation result is classified as **applied / not applied / unknown**
  exactly as the port contract says, using the evidence of
  [`docs/wallet-findings.md`](../wallet-findings.md) (classification table in
  "Implications for later phases");
- balance reads have their own, longer timeout and one retry;
- each Wallet call produces one structured log line, without secrets;
- the Wallet settings are read from the environment and validated at startup.

No endpoint changes in this phase: the first consumer is `GET /api/v1/player`
(Phase 07), which is also where the client gets wired into `Startup`.

## Requirements covered (assessment sections)

- **5.1** — `x-api-key` on every call.
- **5.3** — list payload (always one item), integer `betId`, unreliable Wallet.
- **9.3** — unknown outcomes are explicit (`ErrWalletOutcomeUnknown`), never `FAILED`.
- **9.5** — bounded waiting: per-call timeouts, bounded retries (balance only).
- **9.7** — one log line per Wallet call; API key never logged.
- **10.1** — tests simulate every Wallet failure deterministically, no Docker needed.

## Design decisions (and alternatives rejected)

| Decision | Why | Rejected alternative |
|---|---|---|
| Package **`Shared/wallet`** (`package wallet`), type **`Client`** built with **`NewClient(cfg Config, logger *slog.Logger) *Client`**; `Config{BaseURL, APIKey, OperationTimeout, BalanceTimeout}` | Follows the layout of `ARCHITECTURE.md`; `Shared` imports only `Domain`. Its own small `Config` keeps the package independent from `Startup`. | Passing `Startup.Config` (would import `main`, impossible) or loose parameters. |
| **Standard library only** (`net/http`, `encoding/json`, `net/http/httptest` for tests) | No new dependency to justify. | `resty`, `go-retryablehttp` (magic retries, harder to explain what is re-sent). |
| **No `http.Client.Timeout`**; each call uses **`context.WithTimeout`** derived from the caller's `ctx` | One mechanism for both "our timeout" and "the request was cancelled"; the caller's deadline still applies. | `http.Client{Timeout: …}` (a second, hidden timeout that ignores the caller's context). |
| **Two timeouts:** operations **6 s** (`WALLET_OPERATION_TIMEOUT`), balance **10 s** (`WALLET_BALANCE_TIMEOUT`) | Findings Q7: no POST is ever answered after 5 s, so 6 s cuts no successful call; `GET /balance` takes up to ~15 s. The old `WALLET_TIMEOUT=3s` would turn ~⅓ of successful operations into `PENDING`. | One shared timeout (either too short for balance or needlessly long for operations). |
| **Operations are never retried by the client** | Re-sending is safe only with the Wallet's de-duplication, and findings Q8 warn that an immediate re-send after a dropped connection may race the original. Re-sending is the resolution mechanism of Phase 11, done later and deliberately. | A bounded in-client retry (risk of a double apply in the race window). |
| **Balance: one retry** after a short pause (~200 ms) on `5xx`, transport error or timeout; never on `4xx` or an invalid body | A read is always safe to retry; findings Q7 recommend 10 s + 1 retry (worst case ≈ 20 s, still bounded). No retry if the caller's `ctx` is already done. | No retry (≈ 9 % of reads fail randomly); several retries with backoff (unbounded-feeling latency for `GET /player`). |
| **Classification by HTTP status *and* body `code`** (pure function `classifyOperation`, table below) | Findings Q5: insufficient funds and validation errors are both `400`; only `code` tells them apart. A pure function is trivial to test row by row. | Status-only classification (would turn insufficient funds into `502`). |
| **Anything not recognised is *unknown***: other `4xx`, `5xx`, `409` with another code, transport errors, timeouts | AGENTS.md: only a *definitive* "not applied" may become `FAILED`. When in doubt we keep `PENDING`, which is recoverable; a wrong `FAILED` is not. | Treating every `4xx` as not applied. |
| **New outcome `domain.ErrWalletAppliedBalanceUnknown`** (in the "not errors for the API client" group): the operation **was applied** but the client cannot report the new balance. Returned for `409 DuplicatedTransaction` and for a `2xx` whose body or `balance` cannot be read as `Money` | Resolves Phase 03 open questions 3 and 5 with one concept: a `409` on a re-send has no balance in its body (Q8), and an applied operation must never become `FAILED` because of a 3-decimal balance. The port signature `(Money, error)` stays as it is; Phase 08/11 decide how to complete such a transaction (e.g. read the balance under the player lock). See open question 2. | Changing the return type to `(*Money, error)` (nil-pointer traps) or a result struct; the client calling `GET /balance` itself after a `409` (adds up to 20 s inside an operation and mixes two calls in one). |
| Request DTO with explicit wire types: `userId int64`, `currency string`, `transactions: [{amount, betId int64, reference string}]`, **amount as `json.Number(op.Amount.String())`** | Exactly one transaction per request (Q4, Q8); amount sent as a JSON number with 2 decimals, never through `float64`. Using `json.Number` decouples the Wallet wire format from our API's `Money` JSON. | Embedding `domain.Money` in the DTO (our API format would silently become the Wallet's). |
| Response `balance` decoded into **`json.Number`**, then `domain.ParseMoney` | The Wallet sends a JSON **string** (`"4989.5"`) although Swagger says number (Q2); `json.Number` accepts both, and the value never becomes a float. | `string` field (breaks if the Wallet ever sends a number); `float64` (forbidden). |
| `GET /balance`: empty or unknown `currency` → `ErrWalletFailure` | Q4: an unknown user gets `200 {"balance":"0","currency":""}`; we must not show a zero balance. | Trusting any `200`. |
| `X-TRACE-ID: <reference>` on operations | Correlates our logs with the Wallet's (Q2) at no cost; the reference is our transaction ID, not a secret. | Nothing. |
| Response bodies read through `io.LimitReader` (1 MiB) and always closed (`defer resp.Body.Close()`) | A broken Wallet cannot make us read an unbounded body; closing lets the connection be reused. | `io.ReadAll` without a limit. |
| **One log line per call**: `operation`, `user_id`, `bet_id`, `reference`, `attempt`, `status`, `wallet_code`, `wallet_transaction_id`, `outcome`, `duration_ms`. Level: `info` applied / insufficient funds, `warn` unknown, `error` rejected (`400`/`401`: our bug or bad `WALLET_API_KEY`) | §9.7. The `providerTransactionId` is not known to the client; the use case adds it in its own log (Phase 08). The API key, request headers and bodies are never logged. | Logging the whole request/response (would include the key header). |
| Wallet transaction `id` is **logged, not returned** | Findings Q10: it restarts at 1 with the Wallet, so it is information for support, not a key. No column exists for it (Phase 04). | Extending the port and schema now for a value nobody reads. |
| **Config now, wiring later:** `WALLET_BASE_URL` (default `http://wallet:8000`, must be an absolute `http(s)` URL), `WALLET_API_KEY` (**required**), both timeouts (positive durations) are loaded and validated in this phase; `Startup` builds the client in Phase 07 | The README already says these variables are read from Phase 05; validating them now fails fast on a bad `.env`. Building a client nobody uses would be dead code in `main`. | Deferring the configuration to Phase 07 too. |
| **Fake wallet `domaintest.FakeWallet`** built now (see sketch) | Unlike the repository fakes (deferred in Phase 04), its shape is fully fixed by the port contract, and AGENTS.md lists the behaviours it must simulate. Phases 07–11 all need it. See open question 3. | Deferring it to Phase 07. |
| **No automated tests against the real Wallet**; a manual `curl` check when the client is wired in Phase 07 | The real behaviour was already measured in Phase 01 (findings) and the image is pinned by digest; `httptest` covers every classification row. Real-Wallet tests would need Docker + arm64 emulation, be slow (~3 s/call) and need a re-send loop to cope with random failures. The re-send itself belongs to Phase 11. See open question 4. | Opt-in tests behind a `wallet` build tag. |

### Classification of an operation response (`classifyOperation`)

| Wallet result | Returned by `Withdraw` / `Deposit` | Use case will… |
|---|---|---|
| `2xx` with a valid `balance` | `newBalance, nil` | `COMPLETED` |
| `2xx` with unreadable body / invalid `balance` | `ErrWalletAppliedBalanceUnknown` | `COMPLETED` (Phase 08/11 decide the balance) |
| `409` + `DuplicatedTransaction` | `ErrWalletAppliedBalanceUnknown` | `COMPLETED` (earlier attempt applied) |
| `400` + `InsufficientBalance` | `ErrInsufficientFunds` | `FAILED`, `422` |
| `400` other code, `401` | `ErrWalletRejected` | `FAILED`, `502` |
| `5xx`, other `4xx`, `409` other code | `ErrWalletOutcomeUnknown` | `PENDING`, `202` |
| timeout, dropped connection, any transport error | `ErrWalletOutcomeUnknown` | `PENDING`, `202` |

Returned errors wrap the domain error and add context, e.g.
`fmt.Errorf("wallet withdraw: status 500, code %q: %w", code, domain.ErrWalletOutcomeUnknown)`,
so callers use `errors.Is` and logs keep the detail.

### Balance read

| Wallet result | Returned by `Balance` |
|---|---|
| `200` with valid `balance` (≤ 2 decimals) and known `currency` | `WalletBalance, nil` |
| `200` with invalid `balance`, empty/unknown `currency` or unreadable body | `ErrWalletFailure` (no retry) |
| `400`, `401`, other `4xx` | `ErrWalletFailure` (no retry) |
| `5xx` or transport error, twice | `ErrWalletFailure` |
| timeout on the last attempt | `ErrWalletTimeout` |

### Fake wallet sketch (`Domain/domaintest/fake_wallet.go`)

```go
type FakeWallet struct { /* mutex, balances, applied references, script, calls */ }

func NewFakeWallet(balances map[domain.UserID]domain.WalletBalance) *FakeWallet

// Next scripts the outcome of the next Withdraw/Deposit/Balance call(s), in order.
// With no script left, calls behave like the real Wallet on a good day.
func (w *FakeWallet) Next(behaviours ...Behaviour)

const (
    Succeed           Behaviour = iota // applies, returns the new balance
    FailBeforeApply                    // not applied, returns ErrWalletOutcomeUnknown
    FailAfterApply                     // applied,     returns ErrWalletOutcomeUnknown
    Reject                             // not applied, returns ErrWalletRejected
    TimeOut                            // operations: ErrWalletOutcomeUnknown (not applied); Balance: ErrWalletTimeout
    Slow                               // waits SlowDelay (or until ctx is done), then Succeed
)

func (w *FakeWallet) Calls() []Call                       // what was sent, for assertions
func (w *FakeWallet) BalanceOf(userID domain.UserID) domain.Money
```

- Insufficient funds is **not scripted**: it comes from the balance, with the
  real Wallet's strict rule (`amount < balance`, findings Q5).
- A re-send of an **already applied reference** returns
  `ErrWalletAppliedBalanceUnknown` without applying again (the real client's
  `409`), so Phase 11 can test resolution.
- Deterministic: no randomness; `Slow` honours `ctx` so tests never hang.

## Files to create / modify

| File | Content |
|---|---|
| `Domain/errors.go`, `Domain/errors_test.go` | `ErrWalletAppliedBalanceUnknown` (+ test that it wraps no API category). |
| `Domain/ports.go` | `WalletClient` doc updated with the new outcome. |
| `Startup/config.go`, `Startup/config_test.go` | `WalletBaseURL`, `WalletAPIKey`, `WalletOperationTimeout`, `WalletBalanceTimeout`. |
| `Startup/main.go` | Startup log adds wallet base URL and timeouts (never the key). |
| `Shared/wallet/config.go` | `Config`. |
| `Shared/wallet/dto.go` | Request/response/error body structs (unexported). |
| `Shared/wallet/classify.go`, `classify_test.go` | `classifyOperation(status int, code string) error` — pure. |
| `Shared/wallet/client.go`, `client_test.go` | `Client`, `NewClient`, `Withdraw`, `Deposit`, `Balance`, shared `do` helper, logging. Tests with `httptest.Server`. |
| `Domain/domaintest/fake_wallet.go`, `fake_wallet_test.go` | `FakeWallet`. |
| `.env.example`, `README.md` | `WALLET_TIMEOUT=3s` → `WALLET_OPERATION_TIMEOUT=6s` + `WALLET_BALANCE_TIMEOUT=10s`; `WALLET_API_KEY` required. |
| `ARCHITECTURE.md` | `Shared/wallet` and `Domain/domaintest` described. |
| `DESIGN.md` §1, §5 | "Error before applying" and "Wallet times out" rows filled; Wallet client notes. |
| `docs/plans/00-general-plan.md`, `03-domain.md` | Phase 05 row; Phase 03 open questions 3 and 5 marked resolved. |

## Commit sequence

**Working mode** — same loop as Phases 02–04. Proposed split (open question 1):

| Steps | Who writes |
|---|---|
| 2, 3, 7, 8 and the tests of 4–6 | Claude writes and explains |
| **4, 5, 6 (production code)** | **Developer**, from step-by-step instructions; Claude reviews |

For every step: `gofmt -l .` empty, `go vet ./...`, `go build ./...`,
`go test ./...` green; Claude proposes the commit message.

---

1. **`docs(plan): add phase 05 wallet client plan`** — this file.

2. **`feat(domain): add applied-with-unknown-balance wallet outcome`**
   - `ErrWalletAppliedBalanceUnknown` + test; `WalletClient` doc updated.

3. **`feat(startup): read and validate wallet configuration`**
   - Four `Config` fields + tests (defaults, required key, invalid URL, invalid
     timeouts); `.env.example` and README configuration table updated; startup
     log without the key.
   - *Go concepts:* `net/url.Parse` and why it accepts almost anything (check
     `Scheme` and `Host`).

4. **`feat(wallet): classify wallet operation responses`**
   - `Shared/wallet/classify.go` + one test per table row.
   - *Go concepts:* `switch` without a condition (≈ `if/else if` chain);
     wrapping sentinel errors so `errors.Is` still works.

5. **`feat(wallet): add withdraw and deposit calls`**
   - `config.go`, `dto.go`, `client.go`: build request, headers, per-call
     timeout, decode, classify, log.
   - Tests (`httptest.Server`): headers (`x-api-key`, `Content-Type`,
     `X-TRACE-ID`) and body (one transaction, amount `10.50`, `betId`,
     `reference`); success parses a string and a numeric `balance`; each row of
     the table via HTTP; **dropped connection** (handler hijacks and closes the
     connection) → unknown; **timeout** (handler slower than a 50 ms timeout) →
     unknown; `2xx` with bad body / 3-decimal balance → applied-balance-unknown;
     the API key never appears in the log output.
   - *Go concepts:* `context.WithTimeout` + `http.NewRequestWithContext`;
     `defer resp.Body.Close()`; `json.Number`; `httptest.Server` (≈ an
     in-process `TestServer`); `http.Hijacker` to simulate a dropped connection.

6. **`feat(wallet): add balance read with one retry`**
   - `Balance` with per-attempt timeout and one retry.
   - Tests: success; empty currency → failure; 3-decimal balance → failure;
     `500` then `200` → success (2 calls); `500` twice → failure; `401` →
     failure after **1** call; timeout twice → `ErrWalletTimeout`; caller `ctx`
     cancelled → no retry.
   - *Go concepts:* `select` on `time.After` and `ctx.Done()` for a cancellable pause.

7. **`test(domain): add fake wallet for use-case tests`**
   - `Domain/domaintest/fake_wallet.go` + tests for each behaviour, the strict
     insufficient-funds rule and the re-send of an applied reference.
   - *Go concepts:* `sync.Mutex` + `defer Unlock()` (≈ `lock`); compile-time
     interface check `var _ domain.WalletClient = (*FakeWallet)(nil)`.

8. **`docs(wallet): document wallet client behaviour and configuration`**
   - `DESIGN.md`, `ARCHITECTURE.md`, `README.md`, `00-general-plan.md`,
     `03-domain.md`, and an "As built" section here.

## Test plan

- **Unit (`go test ./...`, no Docker):** domain outcome; config; classification
  table; client against `httptest.Server` (every failure kind, headers, body,
  logs); balance retry; fake wallet.
- **Manual:** none in this phase (the client is not wired yet); a `curl` check
  against the real mock comes with Phase 07.
- **Not covered:** `go test -race` (no C compiler on Windows; CI in Phase 13).

## Open questions

1. ~~**Who writes which step?**~~ **Resolved:** as proposed — developer writes
   the production code of 4–6 (classification, operations, balance); Claude
   writes the domain outcome, config, all tests, the fake wallet, the `wallet`
   tests and docs.
2. ~~**`ErrWalletAppliedBalanceUnknown` vs. changing the port's return type.**~~
   **Resolved:** the new sentinel (signature unchanged). Consequence for Phase
   08/11: completing such a transaction needs a balance from somewhere (a
   `GET /balance` under the lock, or allowing `COMPLETED` without `newBalance`,
   which the schema already permits).
3. ~~**Fake wallet now or in Phase 07?**~~ **Resolved:** now (step 7).
4. ~~**Real-Wallet tests: keep or drop?**~~ **Resolved:** dropped, to keep the
   solution minimal (reasons in the design decisions table).

## As built — deviations from this plan

| Planned | Built | Why |
|---|---|---|
| Developer writes the production code of 4–6; Claude the tests | Developer wrote it from step-by-step instructions; Claude wrote all tests and, on request, parts of the code (the `switch` of `classifyOperation`, the response handling of `sendOperation`, the end of both loggers) and the two readability refactors | The developer asked for help on the larger pieces. |
| Steps 5 and 6 as one commit each | Built **test-first in small increments**, one commit each: withdraw success → balance formats → non-2xx → unusable body → payload and headers → timeout → dropped connection → log → deposit; then balance success → request and invalid answers → retry → per-attempt timeout → log → compile-time port check | The developer wanted every piece of production code to be demanded by a failing test. |
| `sendOperation` in one function | Split into `newRequest` / `buildRequestBody` / `setHeaders` and `decodeSuccess` / `decodeFailure`; `isSuccess` shared with `classifyOperation` | Readability; a request that was never sent maps to `ErrWalletRejected` in one place. |
| `Balance` in one function | `Balance` (retry loop) → `readBalance` → `newBalanceRequest` / `networkFailure` / `statusFailure` / `decodeBalance`, at the bottom of `client.go` in call order | Each helper owns one kind of failure and says whether it is retried. |
| One logger | `logOperation` + `describeOutcome` for operations, `logBalanceRead` + `describeBalanceOutcome` for reads (`ok` / `timeout` / `failure`, all failures at `warn`) | Different fields (`attempt` vs `bet_id`, `reference`, `wallet_code`) and outcomes. |
| — | `Accept` header dropped | No test or Wallet behaviour needs it. |
| — | Test helpers drain the request body before waiting | Found while testing: Go's server only notices that the client has gone (and cancels `r.Context()`) once the handler has read the whole body; without it a timeout test took 2 s instead of 50 ms. |
| — | `go vet` forced the `Do` error check from the first increment | Its `httpresponse` analyzer rejects using a response before checking the error. |
| `Startup/config_test.go` with full `Config` literals | Cases start from `expectedDefaultConfig()` and change one field (`adjust func(*Config)`) | Eight fields made each literal too long. |
| — | `AGENTS.md`: `go run ./Startup` needs `WALLET_API_KEY` too | The comment had become wrong in step 3. |
| Real-Wallet tests (`wallet` build tag) | Dropped before implementation | Already measured in Phase 01; slow, needs Docker and a re-send loop. A manual `curl` check comes with Phase 07. |

No new dependencies.

## Definition of done

- `Shared/wallet` imports only the standard library and `Domain` from this repo;
  `Domain` still imports only stdlib + `decimal`; `domaintest` is not imported by
  any production package (`go list -f '{{.Imports}}' ./...`).
- No `float32`/`float64` in `Shared/wallet`.
- `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` green without Docker.
- Every row of both classification tables has a test whose name says which row.
- No log line or error message contains the API key (tested).
- `.env.example`, `README.md`, `DESIGN.md`, `ARCHITECTURE.md`,
  `00-general-plan.md` updated (step 8).
- The developer can explain each Go concept listed in steps 3–7.
