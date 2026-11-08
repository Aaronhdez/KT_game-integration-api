# 11 — Pending resolution

## Goal

Give every `PENDING` transaction a way to reach its final state (§9.4), and
settle the retry policy of a `FAILED` one (§7.1, general plan open
questions 3 and 4). At the end of the phase:

- a `PENDING` withdraw, deposit or cancel is resolved by **re-sending the
  identical Wallet request** (same `reference` = our transaction ID, same
  `betId`, amount, currency and direction). The Wallet de-duplicates it
  (findings Q8): `409 DuplicatedTransaction` → it had been applied; `2xx` → it
  had not, and is applied now (exactly once); a definitive rejection → `FAILED`;
  another unknown → stays `PENDING`;
- the re-send runs from **two triggers sharing one code path**:
  1. **the provider retries** the same request: instead of answering `202`
     straight away, the handler re-sends (if the last attempt is old enough)
     and answers the final result (`200` / `422` / `502`), or `202` if it is
     still unknown;
  2. **a background resolver** started by `Startup` scans the `PENDING`
     transactions of every player at a fixed interval and re-sends the ones
     that are due, so a transaction whose provider never retries does not
     stay `PENDING` forever;
- a re-send is never made while an earlier attempt may still be running inside
  the Wallet: it waits at least `resendDelay` (10 s) after the last attempt
  started (findings Q8: the Wallet finishes slow requests at ~5.7 s);
- a retry of a request whose earlier attempt ended `FAILED` **gets the same
  error as the first time** (`422 INSUFFICIENT_FUNDS` or `502 WALLET_ERROR`),
  with no Wallet call. The failure reason is stored on the transaction
  (migration `00006`).

New: one migration, one port (`PendingTransactionFinder`), one use case
(`ResolvePendingCommandHandler`), one goroutine in `Startup`, one environment
variable (`PENDING_RESOLVER_INTERVAL`). No new dependency, no endpoint change
other than the status codes above.

## Requirements covered (assessment sections)

- **9.4** — "every `PENDING` transaction must have a path to resolution": on
  provider retry and in the background.
- **9.1 / 9.2** — no lost and no duplicated Wallet calls: the re-send reuses
  the stored reference, so the Wallet applies it at most once; every re-send
  runs under the player's lock, after re-reading the transaction.
- **9.3** — the re-send's result goes through the same `classifyWalletResult`:
  an unknown outcome is never `FAILED`.
- **9.5** — every re-send is bounded by the Wallet client's timeouts; the
  resolver handles a bounded batch per run.
- **9.6** — the resolver takes the same per-player lock as the endpoints.
- **9.7** — one log line per re-send (operation, IDs, attempt outcome).
- **7.1 / 7.2** — same key → same result: a resolved retry is `200` (replay),
  a `FAILED` retry repeats its `422` / `502`; `202` only while still unknown.
- **11** — `DESIGN.md` §2 ("How a PENDING transaction gets resolved") and §3
  ("Retry of a request whose previous attempt ended FAILED").

## Design decisions (and alternatives rejected)

| Decision | Why | Rejected alternative |
|---|---|---|
| **Resolution = re-send the identical request** built only from the stored transaction: user, currency, amount, `betId`, `reference` = transaction ID; direction from the type (withdraw → `Withdraw`, deposit → `Deposit`, cancel of a withdraw → `Deposit`, cancel of a deposit → `Withdraw`) | Findings Q8: the Wallet de-duplicates by (`betId`, `reference`) for withdraws and by `reference` for deposits, so a re-send can never apply twice, and its answer tells us what happened. Everything needed was stored before the first call (intent-first). | Comparing balances (meaningless once other operations happened, Q1). Marking old `PENDING` ones `FAILED` (contradicts the Wallet: most of them were applied, Phase 08 measurement). |
| **Two triggers, one path**: each command handler gets `resendPending(ctx, uow, pending)`; the provider retry (inside its `…Locked` method, where the replay finds a `PENDING`) and the background resolver both call it, always under the player's lock | Retry gives the provider the final answer as soon as possible; the resolver covers providers that never retry (a withdraw that took the money but created no bet). Sharing one method means one set of rules and tests per operation. | Background only (a retry keeps getting `202` for up to one interval). Retry only (a `PENDING` without retry stays forever). |
| **Wait `resendDelay = 10 s` after the last attempt started**, checked in `resendPending` (so in both triggers). A constant in `Actions`, not configuration | Findings Q8: a re-send that reaches the Wallet while the original is still being processed could be applied twice. The Wallet never answers a POST after ~5 s and logs slow ones finishing at ~5.7 s; our timeout is 6 s. 10 s leaves a margin. Not an env var: it depends on the Wallet's behaviour, not on the deployment. | Re-sending immediately on retry (the race of Q8). A config knob (one more thing to explain, with no real reason to change it). |
| **"Last attempt started" = the transaction's `UpdatedAt`**: before each re-send, one database transaction re-reads it and, if it is still `PENDING` and due, writes `UpdatedAt = now` (new domain method `Transaction.RecordAttempt(now)`, `PENDING` only), *then* calls the Wallet | The first attempt starts right after the insert (`UpdatedAt = CreatedAt`), and every re-send moves it forward, so the 10 s rule holds across triggers, instances and crashes. Written **before** the call (intent-first again): if the process dies during the re-send, the next one still waits. No new column. | A `last_attempt_at` / `attempts` column (more schema for the same information). Writing it after the call (a crash during the call would let the next re-send race it). |
| **Re-read under the lock** before re-sending: no longer `PENDING` → return its current state, no Wallet call | Between the scan and the lock, a provider retry may have resolved it (or the resolver, from another instance). | Trusting the scanned copy (two re-sends of a resolved transaction; harmless for the Wallet, but a wrong `Update`). |
| **Outcome of a re-send** — same rules as the first attempt, through the same code: applied → `COMPLETED` with the balance the Wallet reports, plus the operation's own effect (withdraw: insert the `PLACED` bet; deposit: bet `WON`; cancel: original `CANCELLED`, bet `CANCELLED` / `PLACED`); applied, balance unknown (`409`) → `COMPLETED` with `newBalance = oldBalance ± amount`; insufficient funds / rejected → `FAILED` with its reason; unknown → stays `PENDING` | One rule (§9.3). To reuse the first attempt's code exactly, step 5 extracts each handler's outcome `switch` into a `finish` method (refactor, no behaviour change). | A separate "resolution" implementation per operation (two copies of each operation's effects). |
| **`newBalance` of a resolved transaction**: the Wallet's balance on a `2xx`; `oldBalance ± amount` on a `409` | A `2xx` re-send was applied *now*, so the Wallet's balance is the true one after it (other operations may have run in between, so `oldBalance − amount` may differ from it; documented). A `409` gives no balance; `oldBalance ± amount` is the balance right after the first attempt, when the Wallet most likely applied it. | Reading the balance again after a `409` (another Wallet call that can fail, and still not "the balance after this operation"). |
| **Provider retry answers**: re-send completed it → `200` (`Replayed`), with the bet's current status; re-send failed it → the stored failure (`422` / `502`); still unknown, or not due yet → `202` as today | §7.2: the retry is a replay of a request we already accepted; only the first request is `201`. | `201` for the retry that completes it (the transaction was created by the first request). |
| **Background resolver** = `actions.ResolvePendingCommandHandler.Execute(ctx)`: `FindPendingBefore(now − resendDelay, limit 100)` (oldest first) → for each: `WithPlayerLock(user)` → the handler of its type → `resendPending`. One transaction's error is logged and does not stop the batch. Returns a summary (checked / completed / failed / still pending) for the run's log line | A use case in `Actions` (testable with fakes, no goroutines); dispatch by `Type` to the three existing handlers, which already have the Wallet, clock and logger. A batch limit bounds one run (≈ 100 × 11 s worst case). | The scan and dispatch inside `Startup` (business rules out of `Actions`). One goroutine per transaction (no need; the Wallet is slow per call, not overloaded). |
| **New port `PendingTransactionFinder`** in `Domain/ports_persistence.go`: `FindPendingBefore(ctx, updatedBefore time.Time, limit int) ([]Transaction, error)`, implemented by `persistence.NewPendingTransactionFinder(pool)` (plain pool, no lock) | The resolver has no player yet when it scans, so it cannot use the per-player `UnitOfWork`. It is **not scoped by owner** — the one exception to the repository rule, justified because it is only called by a system process, never with a player's input (said in its comment). | Adding it to `TransactionRepository` (every player-scoped method would sit next to one that is not). |
| **Partial index** `transactions_pending ON transactions (updated_at) WHERE status = 'PENDING'` (migration `00006`) | The scan runs every interval; the index holds only the few `PENDING` rows. | A full scan (fine today, but it grows with every completed transaction). |
| **Resolver loop in `Startup/resolver.go`**: a goroutine with a `time.Ticker` (`PENDING_RESOLVER_INTERVAL`, default `10s`), started after migrations, stopped by the shutdown signal; `run` waits for it to finish before returning. It depends on a one-method interface declared in `Startup` (so it can be tested with a stub) | Operations happen in `Actions`; the loop is wiring and lifecycle, which is `Startup`'s job. Cancelling its context cancels an in-flight Wallet call: that is an unknown outcome, so the transaction stays `PENDING` (the outcome writes use `outcomeContext`, as today). It also runs once **at startup**, so `PENDING` transactions left by a crash are resolved without waiting for an interval. | A cron/external job (one more container). `time.Sleep` in a loop (cannot be stopped promptly on shutdown). |
| **No cap on re-sends**; a transaction still `PENDING` after `overdueAfter = 5 min` is logged at `error` (`pending_overdue`) on every attempt | The Wallet de-duplicates, so re-sending is safe while it keeps its memory; giving up would leave the transaction wrong forever. The `error` line is the operator's signal (e.g. the Wallet keeps answering `500`). | Giving up after N attempts or an age (still needs a human, and hides the problem). |
| **Wallet restart** stays an accepted risk (`DESIGN.md` §1): after a restart a re-send of an applied request is applied again | The Wallet has no way to tell us (findings Q10), and the documented reset also wipes our database. Documented next to the resolver: the resolver makes the risk concrete for transactions that were `PENDING` across a Wallet restart. | Detecting restarts (no API for it). |
| **`FAILED` retry: replay the stored failure.** New column `failure_reason TEXT NULL CHECK (failure_reason IN ('INSUFFICIENT_FUNDS','WALLET_REJECTED'))`, plus `CHECK (failure_reason IS NULL OR status = 'FAILED')`. Domain: `FailureReason` type, `Transaction.FailureReason`, `Fail(reason, now)`; `Transaction.FailureError()` returns `ErrInsufficientFunds` / `ErrWalletRejected`, or `ErrPreviousAttemptFailed` when there is no reason (rows written before the migration) | §7.1: same key → same result. The first attempt answered `422` or `502`, so the retry does too. Without storing the reason we could only answer a generic `409` (the interim policy). The key stays used: a `FAILED` attempt was definitive, and the provider makes a new request (new ID) if it wants to try again, which is the usual contract with game providers. | Keeping `409` (a different answer than the first time). Allowing a new attempt with the same ID (relaxing two unique indexes, and `FindCancelOf` would have to choose among several cancels; most code, and a retried withdraw could succeed later with a stale game state). |
| `Update` writes `failure_reason` too (with status, `NewBalance`, `UpdatedAt`) | `Fail` is the only place that sets it, through `Update`. | — |

### Flow of `resendPending` (same for both triggers; caller holds the player's lock)

```
resendPending(ctx, uow, pending)
  Atomic: FindByID(user, id)
          not PENDING                → its current state (no Wallet call)
          now − UpdatedAt < 10 s     → still PENDING, not due (no Wallet call)
          load what the outcome needs (deposit: bet; cancel: original + bet)
          RecordAttempt(now) → Update                       ← committed before the call
  wallet.Withdraw / Deposit (same reference, betId, amount)
  finish(outcomeContext, …)           ← the same method as the first attempt
    applied                → COMPLETED (+ bet / original effects)    → 200 on retry
    applied, balance unknown → COMPLETED, newBalance = old ± amount   → 200
    insufficient / rejected  → FAILED + reason                         → 422 / 502
    unknown                  → stays PENDING                           → 202
```

```
Background (Startup goroutine, every PENDING_RESOLVER_INTERVAL, and once at start)
  ResolvePendingCommandHandler.Execute
    FindPendingBefore(now − 10 s, 100)
    for each: WithPlayerLock(user) → handler[type].resendPending
    log: checked, completed, failed, still_pending
```

## Files to create / modify

| File | Change |
|---|---|
| `Persistence/migrations/00006_pending_resolution.sql` | `failure_reason` column + its two `CHECK`s, `transactions_pending` index |
| `Domain/transaction.go`, `transaction_test.go` | `FailureReason`, `Transaction.FailureReason`, `Fail(reason, now)`, `FailureError()`, `RecordAttempt(now)` |
| `Domain/ports_persistence.go` | `PendingTransactionFinder` |
| `Persistence/transaction_repository.go`, `…_integration_test.go` | `failure_reason` in insert / update / scan; `PendingTransactionFinder` implementation and tests |
| `Domain/domaintest/store.go`, `store_test.go` | The fake keeps the reason; `FindPendingBefore` |
| `Actions/operation.go` | `fail` stores the reason; `resendDelay`, `overdueAfter`; shared re-send helpers (re-read + `RecordAttempt`, the Wallet operation of a stored transaction) |
| `Actions/withdraw.go`, `deposit.go`, `cancel.go` (+ tests) | `finish` extracted (step 5); `FAILED` replay with `FailureError()` (step 3); `resendPending` and its call from the replay of a `PENDING` (steps 6–8) |
| `Actions/resolve_pending.go`, `resolve_pending_test.go` | `ResolvePendingCommandHandler`, `ResolveSummary` |
| `Application/*_test.go` | Retry of a `FAILED` → `422` / `502`; retry that resolves → `200` |
| `Startup/config.go`, `config_test.go`, `resolver.go`, `resolver_test.go`, `main.go` | `PENDING_RESOLVER_INTERVAL`; the loop; wiring and shutdown |
| `.env.example`, `README.md`, `DESIGN.md`, `ARCHITECTURE.md`, `docs/plans/00-general-plan.md` | Variable, status codes, §1/§2/§3, layout, phase row, open questions 3–4 |

## Commit sequence

**Working mode** — same as Phases 09–10: **Claude writes every step** (tests
first) and explains the Go concepts in the chat; the developer reviews each step
before it is committed. A step may be split when that makes the history easier
to follow.

For every step: `gofmt -l .` empty, `go vet ./...`, `go build ./...`,
`go test ./...` green; steps touching `Persistence` also
`go vet -tags=integration ./...` and `go test -count=1 -tags=integration
./Persistence/...`. Claude proposes the commit message and commits only when the
developer asks. Branch: `10_pending_resolution` (following the existing numbering).

---

1. **`docs(plan): add phase 11 pending resolution plan`** — this file.

2. **`feat(domain): record why a transaction failed`**
   - `FailureReason` (`FailureInsufficientFunds`, `FailureWalletRejected`),
     `Transaction.FailureReason`, `Fail(reason, now)`, `FailureError()`.
   - `operation.fail` passes the reason taken from the Wallet error;
     `FakeStore` keeps it (it stores whole structs, so probably no change).
   - Tests: `Fail` sets status and reason; `FailureError` for each reason and
     for none (`ErrPreviousAttemptFailed`); existing withdraw/deposit/cancel
     tests of `FAILED` also assert the reason.
   - *Go concepts:* a named string type as an enum; changing a method signature
     and letting the compiler list every caller.

3. **`feat(persistence): store the failure reason`**
   - Migration `00006` (column, `CHECK`s, `transactions_pending` index);
     `failure_reason` in `Insert`, `Update`, `scanTransaction`.
   - Integration tests: round-trip of each reason; `NULL` for non-failed rows;
     the `CHECK` rejects a reason on a `COMPLETED` row; migration up/down.
   - *Go concepts:* scanning a nullable column (`*string`) into a typed value.

4. **`feat(actions): replay a failed attempt with its original error`**
   - The three replay functions return `previous.FailureError()` instead of
     `ErrPreviousAttemptFailed`.
   - Tests (each: **no Wallet call**, nothing new stored): withdraw / deposit /
     cancel retried after insufficient funds → `ErrInsufficientFunds`; after a
     rejection → `ErrWalletRejected`; a `FAILED` row without reason →
     `ErrPreviousAttemptFailed`. Controllers: `422` / `502` on the retry.

5. **`refactor(actions): share the outcome step of each operation`**
   - Extract each handler's `switch classifyWalletResult(…)` into
     `finish(ctx, uow, pending, …, newBalance, walletErr)`. No behaviour change,
     **no test modified**.

6. **`feat(persistence): find pending transactions due for a re-send`**
   - Port `PendingTransactionFinder`; `persistence.NewPendingTransactionFinder(pool)`;
     `FakeStore.FindPendingBefore`.
   - Tests (integration + fake): only `PENDING`; only `updated_at` ≤ cutoff;
     every player; oldest first; `limit` respected; empty → empty slice, no error.

7. **`feat(actions): re-send a pending withdraw`**
   - `Transaction.RecordAttempt(now)` (domain, with its unit test: `PENDING`
     only, moves `UpdatedAt`).
   - `resendDelay`, `overdueAfter` and the shared helpers in `operation.go`;
     `WithdrawCommandHandler.resendPending`; the replay of a `PENDING` withdraw
     calls it.
   - Tests (fake clock advanced past / not past 10 s):
     - first attempt `FailAfterApply`, retry after 10 s → re-send gets
       "applied, balance unknown" → `COMPLETED`, `newBalance = old − amount`,
       bet `PLACED`, result `Replayed`; **the Wallet balance moved once**;
     - first attempt `FailBeforeApply`, retry → re-send applied → `COMPLETED`
       with the Wallet's balance;
     - re-send `Reject` → `FAILED` + reason, `ErrWalletRejected`; insufficient
       funds → `ErrInsufficientFunds`;
     - re-send `TimeOut` → still `PENDING`, `202` data, `UpdatedAt` moved (the
       next retry waits again);
     - retry before 10 s → `PENDING`, **no Wallet call**;
     - the same `reference` and `betId` as the first attempt;
     - `UpdatedAt` is written **before** the Wallet call (asserted from inside
       the fake call, like the Phase 08 "PENDING exists when the Wallet is
       called" test);
     - outcome write fails → still `PENDING`, no error;
     - overdue (> 5 min) → `error` log line `pending_overdue`.
   - *Go concepts:* testing time with an injected clock instead of `time.Sleep`.

8. **`feat(actions): re-send a pending deposit`** — same cases; applied →
   bet `WON`; the active-settlement rule still holds (no second deposit while
   it is `PENDING`).

9. **`feat(actions): re-send a pending cancel`** — same cases for both
   directions: cancel of a withdraw re-sends a `Deposit` (applied → original
   `CANCELLED`, bet `CANCELLED`, `newBalance = old + amount` on a `409`);
   cancel of a deposit re-sends a `Withdraw` (applied → bet `PLACED`;
   insufficient funds → cancel `FAILED`, original and bet unchanged).

10. **`feat(actions): resolve due pending transactions in the background`**
    - `ResolvePendingCommandHandler` (`NewResolvePendingCommandHandler(finder,
      locker, withdraw, deposit, cancel, clock, logger)`), `ResolveSummary`.
    - Tests: one `PENDING` of each type → each resolved by its own rules; a
      young one is not touched; one transaction whose re-send fails does not
      stop the others; a transaction resolved between scan and lock → skipped
      (no Wallet call); summary counts; empty scan → no lock taken.
    - *Go concepts:* a `switch` on the type field to dispatch (instead of an
      interface per transaction type).

11. **`feat(startup): run the pending resolver`**
    - `PENDING_RESOLVER_INTERVAL` (default `10s`, must be > 0) in `Config`,
      startup log and `.env.example`; `Startup/resolver.go` with
      `runPendingResolver(ctx, resolver, interval, logger) <-chan struct{}`;
      `run` starts it after migrations and waits for it on shutdown.
    - Tests: runs once immediately and then on each tick (short interval,
      stub); stops when the context is cancelled; an `Execute` error is logged
      and the loop goes on; config default and invalid values.
    - Manual check (see Test plan).
    - *Go concepts:* `time.Ticker` + `select` on `ctx.Done()`; a goroutine that
      signals its end by closing a channel; why `run` must wait for it.

12. **`docs(pending): document pending resolution and the failed-retry policy`**
    - `DESIGN.md` §2 (both triggers, the 10 s rule, outcomes, `newBalance`, the
      overdue log, the Wallet-restart risk), §3 (replay of the stored failure),
      §1 rows that say "*Resolution: 11*"; `README.md` (status codes of a
      retry, `PENDING_RESOLVER_INTERVAL`); `ARCHITECTURE.md` (resolver use case,
      new port, loop in `Startup`); general plan (row 11, open questions 3 and
      4 resolved); "As built" section here.

## Test plan

- **Unit (`go test ./...`, no Docker):** every Wallet behaviour on a re-send,
  for the three operations and both cancel directions; the 10 s rule in both
  triggers; re-read under the lock; `UpdatedAt` written before the call; the
  Wallet applies each reference **at most once** across first attempt +
  re-sends (`FakeWallet` balance and `Calls()`); `FAILED` replay per reason;
  resolver batch behaviour; the `Startup` loop with a stub.
- **Concurrency:** a provider retry and the resolver on the same `PENDING`
  transaction at the same time → one re-send, one final state (10 goroutines,
  like the Phase 08–10 duplicate tests).
- **Integration:** migration `00006` up/down, `failure_reason` round-trip and
  `CHECK`, `FindPendingBefore`.
- **Manual (`curl`)**, local PostgreSQL + Wallet mock (about 1 in 4 operations
  ends `PENDING`, so loop until a `202` appears):
  ```bash
  TOKEN=…   # login as player_usd
  for i in $(seq 1 10); do
    curl -s -o /dev/null -w "%{http_code}\n" -X POST localhost:8080/api/v1/withdraw \
      -H "Authorization: Bearer $TOKEN" \
      -d "{\"currency\":\"USD\",\"amount\":1.00,\"providerTransactionId\":\"p11-$i\"}"
  done                                   # note the IDs that answered 202
  # retry one of them at once → 202 (not due); after 10 s → 200 COMPLETED (or 422/502)
  # leave another one alone → within ~20 s the resolver log line shows it completed
  # stop the API right after a 202, start it again → resolved by the run at startup
  # withdraw more than the balance → 422; same request again → 422, no Wallet call in the logs
  psql … -c "SELECT status, failure_reason, count(*) FROM transactions GROUP BY 1,2"
  ```
  Check: no `PENDING` row older than a minute unless the Wallet keeps failing;
  `GET /player` balance consistent with the `COMPLETED` rows; one log line per
  re-send; no secrets in the logs.
- **Not covered:** `go test -race` (no C compiler on Windows; CI in Phase 13).

## Open questions

1. ~~**Trigger(s) of the re-send.**~~ **Resolved:** both, provider retry and a
   background resolver, sharing `resendPending`.
2. ~~**Retry after `FAILED`**~~ (general plan open question 3). **Resolved:**
   replay the stored failure (`422` / `502`), reason stored by migration
   `00006`.
3. ~~**Same key while `PENDING`**~~ (general plan open question 4).
   **Resolved:** re-send if the last attempt started ≥ 10 s ago, otherwise
   `202`.
4. ~~**Who writes the code?**~~ **Resolved:** Claude writes every step, the
   developer reviews each commit.

## Definition of done

- Every `PENDING` transaction is re-sent by the resolver within about one
  interval after it becomes due, and by a provider retry; no re-send within
  10 s of the previous attempt; the Wallet never applies one transaction twice
  (while it keeps its memory).
- A re-send's unknown outcome stays `PENDING`; only a definitive rejection is
  `FAILED`, now with its reason; a `FAILED` retry answers `422` / `502` without
  a Wallet call.
- `Actions` imports only stdlib and `Domain`; the loop lives in `Startup`;
  `domaintest` is not imported by production code.
- `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` green without
  Docker; integration tests green with `TEST_DATABASE_URL`.
- Graceful shutdown waits for the resolver; an interrupted re-send leaves the
  transaction `PENDING`.
- Manual `curl` check done against the real Wallet mock.
- `README.md`, `DESIGN.md` (§1, §2, §3), `ARCHITECTURE.md`, `.env.example`,
  `00-general-plan.md` updated.
- The developer can explain each Go concept listed in the steps.
