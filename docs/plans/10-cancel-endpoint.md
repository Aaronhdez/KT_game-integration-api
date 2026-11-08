# 10 — Cancel endpoint

## Goal

Let a game revert a completed withdraw or deposit of the authenticated player:
`POST /api/v1/cancel` executes the §8.5 table through **compensating Wallet
operations** (the Wallet has no rollback, findings Q9), and our records stay
consistent with the Wallet when it fails or its outcome is unknown. At the end
of the phase:

- `actions.CancelCommandHandler` exists and follows the same reliability path
  as withdraw and deposit: per-player lock → find the original transaction →
  idempotency lookup (its cancel) → `DecideCancel` → `oldBalance` read →
  `PENDING` cancel committed **before** the Wallet call → classify the Wallet
  result → `COMPLETED` / `FAILED` / stays `PENDING`;
- a completed cancel updates **three rows in one database transaction**: the
  cancel (`COMPLETED`), the original (`CANCELLED`) and the bet (withdraw cancel
  → `CANCELLED`; deposit cancel → back to `PLACED`);
- cancelling a **zero-amount deposit** does not call the Wallet (symmetric with
  Phase 09, `DESIGN.md` §4);
- two holes between bets, deposits and cancels are closed: a withdraw cannot be
  cancelled while its bet has a `PENDING` deposit, and a deposit is not
  accepted while the withdraw it settles has a cancel in progress;
- status codes `201`, `200`, `202`, `400`, `401`, `404`, `409`, `422`, `502` —
  never `504`.

No new dependency, no new configuration and **no migration**: the
`transactions_single_cancel` index (Phase 04) already guarantees one cancel per
transaction. **One new repository method** (`FindActiveDeposit`, step 2).
Resolving a `PENDING` cancel and the `FAILED`-retry policy are Phase 11.

## Requirements covered (assessment sections)

- **8.5** — every row of the Cancel table: withdraw `COMPLETED` + bet `PLACED`
  → credit back, bet `CANCELLED`; withdraw of a settled bet → `409` (cancel the
  deposit first); deposit `COMPLETED` → debit back (insufficient funds → `422`,
  nothing changes), bet `PLACED`; zero deposit → no money moves, bet `PLACED`;
  already cancelled → the earlier cancel (`200`); `PENDING` → `409`; `FAILED` →
  `409`; unknown or another player's → `404`.
- **5.3** — no rollback endpoint: reverts are compensating operations with the
  same `betId` and a new `reference`.
- **6.1** — `CANCEL` transaction type, `CANCELLED` status, bet table (cancel of
  withdraw / of deposit); a player only acts on their own transactions.
- **7.1** — idempotency: the same cancel twice returns the first one.
- **7.2 / 7.3 / 7.4** — `201` / `200` / `202`, error codes, the six response
  fields (`providerTransactionId` = the cancelled transaction's, as `NewCancel`
  already stores it).
- **9.1–9.3, 9.5–9.7** — same mechanisms as withdraw/deposit, now for cancels.

## Design decisions (and alternatives rejected)

| Decision | Why | Rejected alternative |
|---|---|---|
| **Request** `{ "providerTransactionId": "<the withdraw or deposit to cancel>" }`; the player only from the token (§8.5, open question 1) | §7.1: "for Cancel, the ID of the transaction being cancelled" is the key. The domain was built for it in Phase 03: `NewCancel` copies the original's provider ID, `FindByProviderTransactionID` ignores `CANCEL` rows, and idempotency is "one cancel per original" (`transactions_single_cancel`), so the cancel needs no ID of its own. | A cancel with its own provider ID (would need a new column and a second idempotency key). |
| **Response**: the shared `transactionResponse` / `writeTransactionResult`, unchanged. `transactionId` = the **cancel's** ID, `providerTransactionId` = the original's, `amount` = the reverted amount, `betStatus` = the bet after the cancel | §7.4; same shape as withdraw/deposit. | A cancel-specific DTO. |
| **Validation before the lock**: `ParseProviderTransactionID`, `players.FindByID` (`404`) | Same pattern as Phases 08–09. There is no amount or currency to validate: both come from the stored original. | — |
| **Order inside the lock**: (1) original by provider ID → (2) its cancel (`FindCancelOf`) → replay rules → (3) bet + `DecideCancel` (+ pending-deposit check for a withdraw) → (4) Wallet `Balance` (`oldBalance`) → (5) no Wallet call: store everything `COMPLETED` and stop; otherwise insert `PENDING` cancel (committed) → (6) Wallet `Deposit` (credit back) or `Withdraw` (debit back) → (7) store the outcome | (1) first: the idempotency key is the original's internal ID, unknown until it is found. (2) before (3): while a cancel is `PENDING` or `FAILED` the original is still `COMPLETED`, so `DecideCancel` would accept a second cancel; the lookup answers it, the index is the safety net. (3) before (4): a cancel that cannot be accepted never touches the Wallet. | Relying only on `DecideCancel` + the index (a retry of a `PENDING` cancel would reach the insert and need special handling there). |
| **Original lookup**: `FindByProviderTransactionID(player, id)`; not found → `404 ErrTransactionNotFound` | Already scoped by player: another player's transaction is `404` without revealing it exists (§8.5). No new method. | A global lookup + ownership check. |
| **Replay rules** (existing cancel, no Wallet call): `COMPLETED` → `200` with the stored cancel and the bet's **current** status; `PENDING` → `202` with the bet's current status; `FAILED` → `409 ErrPreviousAttemptFailed` | Same as withdraw/deposit (§7.2). A `FAILED` cancel still occupies the single-cancel index, so it cannot be re-attempted today; the final policy is Phase 11 (e.g. cancel of a deposit that failed for insufficient funds, retried after the player topped up). | Allowing a new attempt now (needs a migration that relaxes the index: Phase 11's decision). |
| **Decision**: `DecideCancel(original, bet)` as written in Phase 03. Its `ErrAlreadyCancelled` (original `CANCELLED` but no cancel found — should not happen) → `409` with a generic message, logged at `error` | The table lives in the domain; the use case only executes it. A `CANCELLED` original always has a `COMPLETED` cancel, so step (2) normally answers first. | Re-implementing the table in the use case. |
| **Withdraw with a `PENDING` deposit on its bet → `409 ErrTransactionPending`** ("the bet has a deposit in progress"). New port method `TransactionRepository.FindActiveDeposit(ctx, userID, betID)`: the `PENDING` or `COMPLETED` deposit of the bet, `ErrTransactionNotFound` otherwise | While the deposit is `PENDING` the bet is still `PLACED`, so `DecideCancel` would credit the stake back and cancel the bet — and if the Wallet had paid that deposit, the player would keep both the stake and the winnings, and resolving the deposit (Phase 11) would hit a `CANCELLED` bet. The query matches the `transactions_active_settlement` index exactly (at most one row). | A bet column `pending_deposit` (denormalised state to keep in sync). Ignoring it (a real money hole). |
| **Deposit fix: the withdraw has a non-`FAILED` cancel → `409 ErrTransactionPending`** ("the withdraw has a cancel in progress"), checked in `findBetToSettle` with `FindCancelOf` | While the cancel is `PENDING` the withdraw is still `COMPLETED` and the bet `PLACED`, so today a deposit would be accepted on a bet whose stake may already be back in the player's wallet. A `COMPLETED` cancel is already rejected (withdraw `CANCELLED` → bet `CANCELLED`), a `FAILED` one does not block. | Leaving it for Phase 11. |
| **Compensating operation**: credit back = `wallet.Deposit`, debit back = `wallet.Withdraw`, `amount` = the original's, `betId` = the original's bet, `reference` = **the cancel's own ID** | Findings Q9: the Wallet de-duplicates by `reference`; a new reference makes it a new movement, and re-sending the cancel (Phase 11) reuses it. The link to the original exists only in our database. | Reusing the original's reference (the Wallet would answer `409 DuplicatedTransaction`). |
| **Outcome classification** — the port contract: `nil` → `COMPLETED` (`newBalance` from the Wallet) + original `MarkCancelled` + bet `Cancel` / `RevertSettlement`, one database transaction (`201`); `ErrWalletAppliedBalanceUnknown` → `COMPLETED`, `newBalance = oldBalance ± amount`; `ErrInsufficientFunds` → cancel `FAILED`, original and bet unchanged, `422`; `ErrWalletRejected` → `FAILED`, `502`; anything else → cancel `PENDING`, original `COMPLETED`, bet unchanged, `202` | Same rules as withdraw/deposit. Insufficient funds is a real case for a debit back (the player spent the winnings, or the Wallet's strict `amount < balance`, findings Q5, Q9) and §8.5 asks for `422` with nothing changed. | A partial revert (findings Q9: leaves the bet half-cancelled). |
| **Zero-amount deposit cancel (`CancelNoWalletCall`)**: under the lock, the balance is still read (`oldBalance`), then **one** database transaction inserts the cancel `COMPLETED` with `newBalance = oldBalance`, marks the deposit `CANCELLED` and the bet `PLACED` → `201`. No `PENDING` step | Mirror of the zero-amount deposit (`DESIGN.md` §4): no money moves, so a Wallet call could only fail; `oldBalance` must be a balance we observed (§7.4, `NOT NULL`). Balance read fails → `502`, nothing stored. | Not reading the balance (no observed value for `oldBalance`). |
| **Race past the lock**: insert rejected with `ErrAlreadyCancelled` (single-cancel index) → run the cancel lookup again → replay rules | Same safety net as Phases 08–09 (two API instances). | — |
| After the Wallet call, DB writes use `context.WithoutCancel(ctx)` with `outcomeWriteTimeout`; if storing the outcome fails → `202` with the cancel as stored (`PENDING`), logged at `error` | Same reasoning as Phase 08 (`DESIGN.md` §1). | — |
| **Logging**: one line per cancel (`logTransaction`, message `"cancel"`) plus `cancelled_transaction_id` when the transaction is a `CANCEL`; outcomes `completed`, `completed_without_wallet`, `pending`, `balance_unknown`, `insufficient_funds`, `rejected`, `outcome_not_stored` | §9.7; the extra attribute links the cancel to the original in the logs. Added inside `logTransaction`, so withdraw/deposit lines do not change. | — |
| **Time budget** unchanged: ≈ 20 s (balance) + 6 s (operation) + DB ≈ 27 s, under the 30 s `WriteTimeout` | Same calls as a withdraw. | — |
| `CancelController` depends on the concrete `*actions.CancelCommandHandler`, wrapped in `RequireAuth` by `Startup` | Same pattern as Phases 06–09. | An interface for one implementation. |

### Flow

```
POST /api/v1/cancel  (RequireAuth → user ID)
  decode {providerTransactionId}                        bad JSON / missing ID → 400
  CancelCommandHandler.Execute
    parse provider ID                                     → 400
    players.FindByID                                      → 404
    WithPlayerLock(user):
      Atomic: FindByProviderTransactionID                 → 404 (unknown / another player's)
              FindCancelOf(original.ID)
                COMPLETED → replay 200 · PENDING → replay 202 · FAILED → 409
              bets.FindByID(original.BetID)
              DecideCancel(original, bet)                 → 409 (PENDING / FAILED / settled bet)
              withdraw: FindActiveDeposit(bet) PENDING    → 409 (deposit in progress)
      wallet.Balance                                      → 502, nothing stored
      NO_WALLET_CALL:
        Atomic: Insert cancel COMPLETED, deposit CANCELLED, bet PLACED → 201
      CREDIT_BACK / DEBIT_BACK:
        Atomic: NewCancel(PENDING) → Insert              ← committed
          ErrAlreadyCancelled → cancel lookup again → replay
        wallet.Deposit (credit back) / wallet.Withdraw (debit back)
                    (reference = cancel ID, betId = original's bet)
        Atomic (ctx WithoutCancel):
          applied      → cancel COMPLETED, original CANCELLED,
                         bet CANCELLED (withdraw) / PLACED (deposit) → 201
          insufficient → cancel FAILED                              → 422
          rejected     → cancel FAILED                              → 502
          unknown      → nothing to write                           → 202
```

## Files to create / modify

| File | Change |
|---|---|
| `Domain/ports_persistence.go` | `FindActiveDeposit` in `TransactionRepository` (step 2) |
| `Persistence/transaction_repository.go`, `…_integration_test.go` | Its SQL implementation + integration tests (step 2) |
| `Domain/domaintest/store.go`, `store_test.go` | Its fake (step 2) |
| `Actions/deposit.go`, `deposit_test.go` | Cancel-in-progress check (step 3) |
| `Actions/operation.go` | `cancelled_transaction_id` in `logTransaction` (step 4) |
| `Actions/cancel.go`, `cancel_test.go` | `CancelCommandHandler`, `NewCancelCommandHandler(players, locker, wallet, ids, clock, logger)`, `CancelCommand{UserID, ProviderTransactionID string}` |
| `Actions/wallets_test.go` | Test Wallet wrappers, if the cancel tests need new ones |
| `Application/cancel.go`, `cancel_test.go` | `CancelController`, `cancelRequest` |
| `Startup/main.go`, `router.go`, `router_test.go` | The handler, `Handlers.Cancel`, route `POST /api/v1/cancel` |
| `README.md`, `DESIGN.md`, `ARCHITECTURE.md`, `docs/plans/00-general-plan.md` | Endpoint, decisions (§1 cancel side, §5 cancel paragraph, deposit fix), layout, phase row |

`Domain` entities (`DecideCancel`, `NewCancel`, `MarkCancelled`, `Bet.Cancel`,
`Bet.RevertSettlement`) and `FakeWallet` are expected to need **no change**. If
a test shows otherwise, the change gets its own commit and an "As built" row.

## Commit sequence

**Working mode** — test-first, same as Phase 09: **Claude writes every step**
(tests and production code) and explains the Go concepts in the chat; the
developer reviews each step before it is committed. A step
may be split into smaller commits when that makes the history easier to follow.

For every step: `gofmt -l .` empty, `go vet ./...`, `go build ./...`,
`go test ./...` green (step 2 also `go test -count=1 -tags=integration
./Persistence/...`); Claude proposes the commit message and commits only when
the developer asks. Branch: `09_cancel` (following the existing numbering).

---

1. **`docs(plan): add phase 10 cancel endpoint plan`** — this file.

2. **`feat(persistence): find the active deposit of a bet`**
   - Port: `FindActiveDeposit(ctx, userID, betID) (Transaction, error)`.
   - PostgreSQL: `… WHERE wallet_user_id = $1 AND bet_id = $2 AND type =
     'DEPOSIT' AND status IN ('PENDING','COMPLETED')`.
   - Fake in `FakeStore` with the same rule.
   - Tests (integration + fake): `PENDING` deposit found; `COMPLETED` found;
     only `FAILED` / `CANCELLED` deposits → `ErrTransactionNotFound`; another
     player's bet → `ErrTransactionNotFound`; a withdraw on the bet is ignored.
   - *Go concepts:* adding a method to an interface breaks every implementation
     at compile time (no `implements` keyword; the compiler checks it where the
     concrete type is used as the interface).

3. **`fix(actions): reject a deposit while its withdraw is being cancelled`**
   - `findBetToSettle`: `FindCancelOf(withdraw.ID)`; found and not `FAILED` →
     `ErrTransactionPending` with "the withdraw has a cancel in progress".
   - Tests: withdraw with a `PENDING` cancel (put in the store) → `409`
     category, no Wallet call, nothing stored; with a `FAILED` cancel → the
     deposit is accepted (`WON`).

4. **`feat(actions): validate cancel and decide what to revert`**
   - `Execute` up to and including the `oldBalance` read (returns before
     storing anything; the rest comes in steps 5–6). `logTransaction` gains
     `cancelled_transaction_id`.
   - Tests (`TestCancel_…`, each one asserts **no Wallet operation and nothing
     stored**):
     - invalid provider ID → `ErrInvalidInput` category; unknown player →
       `ErrPlayerNotFound`;
     - unknown provider ID / **another player's** transaction →
       `ErrTransactionNotFound`;
     - original `PENDING` → `ErrTransactionPending`; `FAILED` →
       `ErrNothingToRevert`;
     - withdraw whose bet is `WON` / `LOST` → `ErrBetAlreadySettled`;
     - withdraw whose bet has a `PENDING` deposit → `ErrTransactionPending`;
     - balance read fails **or times out** → `ErrWalletFailure` (never
       `ErrWalletTimeout`).

5. **`feat(actions): revert through the wallet and store the cancel outcome`**
   - `PENDING` insert, compensating call, outcome.
   - Tests:
     - cancel of withdraw → `wallet.Deposit` with the withdraw's amount, its
       `betId` and `reference` = cancel ID; cancel `COMPLETED`
       (`oldBalance`/`newBalance` from the Wallet), withdraw `CANCELLED`, bet
       `CANCELLED`; result `providerTransactionId` = the withdraw's;
     - cancel of deposit (`WON`) → `wallet.Withdraw`; deposit `CANCELLED`, bet
       `PLACED`; afterwards a **new deposit on the same bet is accepted**;
     - the `PENDING` cancel exists **when the Wallet is called**;
     - `FailAfterApply` / `FailBeforeApply` / `TimeOut` → cancel `PENDING`,
       original `COMPLETED`, bet unchanged, `newBalance` `nil`;
     - debit back with insufficient funds → cancel `FAILED`,
       `ErrInsufficientFunds`, deposit `COMPLETED`, bet `WON`;
     - `Reject` → `FAILED`, `ErrWalletRejected`;
     - `ErrWalletAppliedBalanceUnknown` → `COMPLETED`, `newBalance = old + amount`
       (withdraw cancel) / `old − amount` (deposit cancel);
     - outcome write fails → result `PENDING`, no error, nothing else changed;
     - request `ctx` cancelled during the Wallet call → outcome still stored.

6. **`feat(actions): cancel zero-amount deposits without the wallet`**
   - Tests: zero deposit (`LOST`) → cancel `COMPLETED`, `oldBalance` =
     `newBalance`, deposit `CANCELLED`, bet `PLACED`, **no** Wallet operation
     (one `Balance` call); balance read fails → `ErrWalletFailure`, nothing
     stored.

7. **`feat(actions): make cancel idempotent`**
   - Replay rules; `ErrAlreadyCancelled` on insert → lookup again.
   - Tests:
     - same cancel twice → second `Replayed` (`200` data), **one** Wallet
       operation, one `CANCEL` row;
     - replay of a `PENDING` cancel → `PENDING`, `Replayed`, no Wallet call;
     - replay of a `FAILED` cancel → `ErrPreviousAttemptFailed`;
     - replay after the bet changed (deposit cancelled, then a new deposit
       settled the bet `WON`) → stored cancel, bet's **current** status
       (`WON`);
     - zero-amount replay → `Replayed`, no Wallet call;
     - **concurrent duplicates**: 10 goroutines cancelling the same transaction
       → one Wallet operation, one non-replayed result, nine replays.

8. **`feat(application): add cancel endpoint`**
   - `cancelRequest{ProviderTransactionID string}`, `CancelController`.
   - Tests (`TestCancelController_…`, real handler with fakes):
     - `201` with the exact §7.4 body (`"betStatus":"CANCELLED"` for a withdraw,
       `"PLACED"` for a deposit);
     - replay → `200`; unknown outcome → `202`, `"newBalance":null`;
     - missing / empty `providerTransactionId`, bad JSON → `400`;
     - unknown ID → `404`; `PENDING` / `FAILED` original / settled bet → `409`;
       insufficient funds → `422`; rejected → `502`; balance timeout → `502`
       (not `504`);
     - through `RequireAuth`: no token → `401`, no Wallet call.

9. **`feat(startup): wire cancel endpoint`**
   - `newHandlers`: `actions.NewCancelCommandHandler(...)`,
     `RequireAuth(...)(CancelController)`; `Handlers.Cancel`, route
     `POST /api/v1/cancel`; router tests. Manual check (see Test plan).

10. **`docs(cancel): document cancel endpoint`**
    - `README.md` (request/response, status codes, curl), `DESIGN.md` (§1
      cancel side incl. insufficient funds on a debit back, §5 cancel
      paragraph: order, single cancel, pending-deposit / pending-cancel checks,
      zero-deposit cancel), `ARCHITECTURE.md` (cancel section, new port
      method), general plan (row 10), and an "As built" section here.

## Test plan

- **Unit (`go test ./...`, no Docker):** every §8.5 row; every `FakeWallet`
  behaviour for both compensating directions; the two cross-checks
  (pending deposit / pending cancel); idempotency including concurrent
  duplicates; controller and router.
- **Integration:** `FindActiveDeposit` (step 2). The single-cancel index is
  already covered by Phase 04.
- **Manual (`curl`)**, local PostgreSQL + Wallet mock:
  ```bash
  TOKEN=…   # login as player_usd
  curl -i -X POST localhost:8080/api/v1/withdraw -H "Authorization: Bearer $TOKEN" \
       -d '{"currency":"USD","amount":10.50,"providerTransactionId":"prov-tx-0101"}'   # 201
  curl -i -X POST localhost:8080/api/v1/cancel -H "Authorization: Bearer $TOKEN" \
       -d '{"providerTransactionId":"prov-tx-0101"}'    # 201 CANCELLED, balance back (or 202)
  # same cancel again → 200; deposit on that bet → 409
  # withdraw + deposit 25 (WON) → cancel the withdraw → 409; cancel the deposit → 201 PLACED
  #   → new deposit on the same bet → 201
  # withdraw + deposit 0 (LOST) → cancel the deposit → 201, no Wallet call in the logs
  # cancel with player_usd_2's token → 404; unknown ID → 404
  # deposit a large win, GET /player, withdraw almost all, cancel the win → 422
  psql … -c "SELECT type, status, count(*) FROM transactions GROUP BY 1,2"
  ```
  Check: balances in `GET /player` match the reverts; every `202` cancel is
  `PENDING` with its original `COMPLETED`; one log line per cancel; no secrets.
  Also run the pending manual check of Phase 09.
- **Not covered:** `go test -race` (no C compiler on Windows; CI in Phase 13).

## Open questions

1. ~~**Request shape of §8.5.**~~ **Resolved (checked against the assessment
   PDF):** `{ "providerTransactionId": "prov-tx-0001" }`, the ID of the
   transaction being cancelled; §7.1 makes it the idempotency key and §7.4
   returns it as `providerTransactionId`. The payload has no other field, so
   "same key, different payload → 409" cannot happen for a cancel. A cancel
   that arrives before its withdraw (§10.2 nice-to-have) is `404` here;
   Phase 14.
2. ~~**Who writes which step?**~~ **Resolved:** same as Phase 09 — Claude
   writes every step, the developer reviews each commit.
3. ~~**Where errors live**~~ (general-plan open question 6). **Resolved:** out
   of this phase; decided once Cancel is merged, as a separate `refactor` if
   accepted.

## As built — deviations from this plan

| Planned | Built | Why |
|---|---|---|
| Step 2 commit `feat(persistence): find the active deposit of a bet` | Committed as `f9b65f8` with the message `ocs(plan): add phase 10 cancel endpoint plan` | Wrong message when committing; the content is step 2. Left as is (rewriting history was not requested). |
| Step 6: one commit | Two commits with the same message (`165f28b` zero-deposit cancel, `69b55aa` `applyCancel` helper shared by `complete` and `cancelWithoutWallet`) | The developer asked to remove the duplication after reviewing step 6. |
| `ErrAlreadyCancelled` on insert → lookup again | Also for the zero-deposit path (its `COMPLETED` cancel is inserted directly) | Both paths insert a cancel row, so both can hit the single-cancel index. |
| `DecideCancel`'s `ErrAlreadyCancelled` → `409`, logged at `error` | As planned; message "the transaction is already cancelled" | Only reachable with inconsistent data (a `CANCELLED` original without its cancel). |
| `Domain`, `FakeWallet` unchanged | As planned | — |
| Step 3: a deposit is refused while its withdraw has a non-`FAILED` cancel | Only a **`PENDING`** cancel counts as "in progress" (separate `fix(actions)` commit, with a test) | Found by the manual check: a deposit on a bet whose withdraw was already cancelled got `409` "the withdraw has a cancel in progress" instead of "bet already settled". A `COMPLETED` cancel does reach the check (its withdraw is `CANCELLED`, which `findBetToSettle` lets through to the bet), contrary to the step 3 comment. Same status code, wrong reason. |
| Integration tests of `FindActiveDeposit` (step 2) | Run against PostgreSQL 16: green | — |
| Manual `curl` check (step 9) | Done against the real Wallet mock (scratch script, not committed), see below | — |

**Manual check (2026-11-08, PostgreSQL 16 + `kentechsp/wallet-client`).**
Withdraw cancel → `201` `CANCELLED`, balance back; same cancel → `200`;
deposit on the cancelled bet → `409` (after the fix: "bet … is CANCELLED").
Withdraw + deposit 25 → cancel the withdraw `409` ("cancel the deposit … first"),
cancel the deposit `201` `PLACED` with the 25 taken back, a new deposit on the
same bet `201` `WON`, replay of the first cancel `200` with `betStatus` `WON`.
Zero deposit `201` `LOST` → its cancel `201` `PLACED`, `oldBalance` =
`newBalance`, no Wallet call in the logs. Another player's / unknown ID `404`,
missing ID or malformed JSON `400`, no token `401`. Deposit of 50 000, then
spend all but 100 → cancelling the deposit `422`, cancel `FAILED`, deposit
still `COMPLETED`, bet `WON`; retry `409`. Database: every `COMPLETED` cancel
has its original `CANCELLED` and the right bet status; 7 cancel log lines (4
`completed`, 2 `completed_without_wallet`, 1 `insufficient_funds`); no secret
in the logs. The Wallet left 2 of the ~14 withdraws `PENDING` (`202`), which
blocked the scenarios built on them (`409` "withdraw is not completed", as
expected); they were re-run.

The developer then repeated the main cases with `curl` (cancel of a withdraw
and its replay, the settled-bet `409`, cancel of a deposit and a new deposit
on the bet, `404`/`400`/`401`). Two Wallet `500 "666"` answers produced the
unknown-outcome path for real: a cancel stayed `PENDING` (its retry answered
`202` without a new Wallet call, and a deposit on that bet got `409` "cancel
in progress"), and a deposit stayed `PENDING` with its bet `PLACED`. No
secret in the logs.

These runs also exercised Phase 09's deposit cases (`201` `WON`/`LOST`,
settled bet `409`, withdraw not completed `409`); its validation cases
(`EUR`, missing `amount`) were not run, so Phase 09's own manual check stays
pending.

## Definition of done

- `Actions` imports only stdlib and `Domain`; `Application` does not import
  `Shared` or `Persistence`; `domaintest` is not imported by production code.
- `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` green without
  Docker; integration tests green with `TEST_DATABASE_URL`.
- Every §8.5 row has a test, in particular: `PENDING` cancel exists before the
  Wallet call, unknown outcome never `FAILED`, insufficient funds on a debit
  back changes nothing but the cancel, zero deposit cancel never reaches the
  Wallet, replay without a Wallet call, concurrent duplicates → one Wallet
  operation, another player's transaction → `404`, no `504` from the endpoint.
- Withdraw cancel blocked by a `PENDING` deposit; deposit blocked by a
  cancel in progress.
- No `float` anywhere in the request/response path.
- Manual `curl` check done against the real Wallet mock. ✔ (see As built)
- `README.md`, `DESIGN.md`, `ARCHITECTURE.md`, `00-general-plan.md` updated.
- The developer can explain each Go concept listed in the steps.
