# 09 — Deposit endpoint

## Goal

Let a game settle a bet for the authenticated player: `POST /api/v1/deposit`
pays the outcome of a bet placed by a completed withdraw, and our records stay
consistent with the Wallet when it fails or its outcome is unknown. At the end
of the phase:

- `actions.DepositCommandHandler` exists and follows the same reliability path
  as the withdraw: per-player lock → idempotency lookup → checks on the
  referenced withdraw and its bet → `oldBalance` read → `PENDING` committed
  **before** the Wallet call → classify the Wallet result →
  `COMPLETED` / `FAILED` / stays `PENDING`;
- a completed deposit settles the bet: amount `> 0` → `WON`, amount `0` →
  `LOST` (§6.1), in the same database transaction as the deposit;
- a bet has **one active settlement**: a second deposit (other provider ID) on a
  bet that is settled, cancelled **or has a `PENDING` deposit** → `409`;
- **zero-amount deposits are not forwarded to the Wallet** (general-plan open
  question 2, recorded in `DESIGN.md` §4);
- the withdraw no longer replays a transaction of another type that happens to
  share its provider ID (bug found while preparing this phase, see step 2);
- status codes `201`, `200`, `202`, `400`, `401`, `404`, `409`, `422`, `502` —
  never `504`.

No new dependency, no new configuration and **no migration**: the
`transactions_active_settlement` index and the global provider ID index already
exist (Phases 04 and 08). Resolving a `PENDING` deposit is Phase 11.

## Requirements covered (assessment sections)

- **8.4** — request `{currency, amount, providerTransactionId,
  providerWithdrawnTransactionId}`; amount `≥ 0`; `0` → `LOST`, otherwise `WON`;
  the referenced ID must be a `COMPLETED` withdraw **of the same player**
  (unknown / another player's → `404`, not completed → `409`); currency = the
  withdraw's currency; one active settlement per bet (`409`); zero-amount
  decision recorded in `DESIGN.md`.
- **6.1** — bet state table (deposit completed → `WON`/`LOST`); provider IDs
  unique across the system; a player only acts on their own transactions.
- **7.1** — idempotency by `providerTransactionId`: replay vs `409`.
- **7.2** — `201` / `200` / `202`.
- **7.3** — `400` includes "a currency that does not match … the original
  transaction"; `404`; `409` "bet already settled"; `502` only when not applied.
- **7.4** — the six response fields; `betStatus` is `null` **only** while a
  *withdraw* is `PENDING`, so a `PENDING` deposit reports the bet's current
  status (`PLACED`).
- **9.1–9.3, 9.5–9.7** — same mechanisms as the withdraw, now for deposits.

## Design decisions (and alternatives rejected)

| Decision | Why | Rejected alternative |
|---|---|---|
| **Request** exactly as §8.4; the player only from the token. `amount` decoded into `*domain.Money`: a missing amount is `400 "amount is required"`, never `0` | For a deposit `0` is meaningful ("bet lost"); treating a missing field as `0` would settle a bet as `LOST` by accident. Already recorded in `DESIGN.md` §5 and covered for the withdraw by `MissingAmount_SaysItIsRequired`. | `Amount domain.Money` (zero value on a missing key). |
| **Response**: the shared `transactionResponse` / `writeTransactionResult` of Phase 08, unchanged | §8.4: "The response uses the fields of section 7.4". | A deposit-specific DTO. |
| **Validation before the lock** (no Wallet call, nothing stored, no lock held): both provider IDs (`ParseProviderTransactionID`), currency format (`ParseCurrency`), currency = player's currency (`ErrCurrencyMismatch`). Amount `≥ 0` is guaranteed by `Money` itself | Same pattern as the withdraw. A withdraw is always in its player's currency (Phase 08 enforces it), so this check already covers "the currency of the referenced withdraw" for every valid withdraw. | Checking the currency only after loading the withdraw (would take the lock for a bad request). |
| **Currency checked against the referenced withdraw too**, inside the lock (`ErrCurrencyMismatch`, `400`) | §8.4 states the rule against the withdraw, not the player; one comparison makes the code say exactly that, even if the first check ever changes. | Relying only on the player check (correct today, but implicit). |
| **Order inside the lock**: (1) idempotency lookup → (2) referenced withdraw + bet checks → (3) Wallet `Balance` (`oldBalance`) → (4) amount `0`: store `COMPLETED` + `LOST` and stop; amount `> 0`: insert `PENDING` (committed) → (5) Wallet `Deposit` → (6) store the outcome | (1) first: a replay must return the stored result even if the bet changed since (e.g. the deposit was later cancelled). (2) before (3): a deposit that cannot be accepted never touches the Wallet. Same reasons as Phase 08 for the rest. | Checking the withdraw before the idempotency lookup (a replay of a deposit whose bet was later cancelled would get `409` instead of `200`). |
| **Referenced withdraw** looked up with `FindByProviderTransactionID(player, providerWithdrawnTransactionId)` | Already scoped by player: another player's transaction is `ErrTransactionNotFound` → `404`, as §8.4 asks, without revealing it exists. No new repository method. | A global lookup + ownership check (leaks existence through timing/messages). |
| **What the referenced ID may be**: not found → `404 ErrTransactionNotFound`; found but it is a **deposit** → `404` too; withdraw `PENDING` or `FAILED` → `409 ErrWithdrawNotCompleted`; withdraw `COMPLETED` or `CANCELLED` → load its bet and let `bet.Settle` decide (`PLACED` → OK; `WON`/`LOST`/`CANCELLED` → `409 ErrBetAlreadySettled`) | §8.4: "must identify a COMPLETED withdraw … otherwise 404 (unknown …) or 409 (not completed)". A deposit's ID does not identify a withdraw, so it is "unknown". A cancelled withdraw means a cancelled bet, which §8.4 lists explicitly ("already settled or cancelled bet … is 409"); `Bet.Settle` already holds that rule (Phase 03). | `409` for a deposit's ID (it is not "a withdraw in the wrong state"). |
| **One active settlement** is enforced by the `transactions_active_settlement` index (`PENDING` or `COMPLETED` deposit per bet). Its violation (`ErrBetAlreadySettled`) on insert → `409` | While a deposit is `PENDING` the bet is still `PLACED`, so `bet.Settle` alone would accept a second deposit. The index covers that case, and also two API instances racing past the lock. A `FAILED` or `CANCELLED` deposit does not count, so the bet can be settled again (§8.5: cancel of a deposit returns the bet to `PLACED`). | A new `FindActiveDepositOf(bet)` repository method: duplicates what the index already guarantees. |
| **Idempotency = compare the stored fields**: the transaction found by `providerTransactionId` must be a **`DEPOSIT`** with the same `amount`, `currency` **and bet** (the referenced withdraw, looked up by `providerWithdrawnTransactionId`, must exist and have the deposit's `BetID`); anything else → `ErrIdempotencyConflict` (`409`) | Same approach as Phase 08 (no payload hash). The deposit row does not store `providerWithdrawnTransactionId`, but every withdraw has its own bet, so "same bet" is exactly "same withdraw". A withdraw with that provider ID is a different request → `409`. | A new column `provider_withdrawn_transaction_id` (a migration for information we can already derive). |
| **Replay rules** (no Wallet call): `COMPLETED` or `CANCELLED` → `200` with the stored deposit and the bet's **current** status; `PENDING` → `202` with the bet's current status (`PLACED`); `FAILED` → `409 ErrPreviousAttemptFailed` | Same as the withdraw (§7.2), except `betStatus`: §7.4 makes it `null` only for a pending *withdraw*; here the bet exists. Final `FAILED` policy: Phase 11. | `betStatus: null` for a pending deposit (contradicts §7.4). |
| **Zero-amount deposit: not forwarded to the Wallet.** Under the lock, after the checks, the balance is still read (`oldBalance`), then **one** database transaction stores the deposit as `COMPLETED` with `newBalance = oldBalance` and the bet as `LOST` → `201`. No `PENDING` step: there is no Wallet call to keep track of (§9.1) | No money moves, so a Wallet call can only add risk: the mock accepts `0` but it can fail, time out or answer `500` like any call (findings Q4, Q6), leaving a lost bet `PENDING` for nothing. Cancelling a zero deposit moves no money either (§8.5), so both sides stay symmetric. The balance is read because `oldBalance` is "as observed by your service" (§7.4) and the column is `NOT NULL`; if the read fails → `502`, nothing stored, the provider retries. | **Forwarding it** (one code path, but a `LOST` could end `PENDING` and need Phase 11 for a movement of 0). **Not reading the balance** (no value for `oldBalance` that we observed; reusing the withdraw's `newBalance` would be stale). |
| **Outcome classification** — same as the withdraw: `nil` → `COMPLETED` + `bet.Settle` (`WON`) in one transaction (`201`); `ErrWalletAppliedBalanceUnknown` → `COMPLETED` with `newBalance = oldBalance + amount`; `ErrInsufficientFunds` → `FAILED`, `422`; `ErrWalletRejected` → `FAILED`, `502`; anything else → `PENDING`, `202`, bet stays `PLACED` | The port contract is the same for both operations. Insufficient funds cannot really happen on a deposit, but following the contract keeps one rule instead of a special case. | A deposit-only switch that treats `ErrInsufficientFunds` as unknown. |
| After the Wallet call, DB writes use `context.WithoutCancel(ctx)` with `outcomeWriteTimeout`; if storing the outcome fails → `202` with the transaction as stored (`PENDING`), logged at `error` | Same reasoning as Phase 08 (`DESIGN.md` §1). | — |
| `ErrTransactionExists` on insert → own lookup again: found → replay rules; not found → the ID belongs to another player → `409` with the generic message | Same as Phase 08, including the fix that keeps internal IDs out of the message. | — |
| **Withdraw fix**: `replayIfProcessed` of the withdraw requires the found transaction to be a `WITHDRAW`; otherwise `ErrIdempotencyConflict` | `FindByProviderTransactionID` returns withdraws **and** deposits. Today no deposit exists, but from this phase on a withdraw reusing a deposit's provider ID with the same amount and currency would be answered as a replay of the deposit (wrong body, `200`, no bet placed). | Leaving it for Phase 10. |
| **Small shared helpers** in `Actions/operation.go`, extracted from the withdraw before writing the deposit: `readOldBalance(ctx, wallet, player)` (balance read + currency check → `ErrWalletFailure`) and `logTransaction(logger, level, operation, tx, outcome, cause)` | Both are identical for the two operations and Cancel (Phase 10) will need them too. Everything else (the flows, the replay rules, `complete`/`fail`) stays per use case: they differ in the details and are easier to read side by side than behind parameters. | A generic "money operation" engine with callbacks for withdraw/deposit/cancel (too clever to explain live). Copy-pasting the two helpers. |
| **Logging**: one line per deposit (`transaction_id`, `provider_transaction_id`, `user_id`, `bet_id`, `amount`, `outcome`), message `"deposit"`; outcomes `completed`, `completed_without_wallet` (amount 0), `pending`, `balance_unknown`, `insufficient_funds`, `rejected`, `outcome_not_stored` | §9.7, same levels as the withdraw. | — |
| **Time budget** unchanged: ≈ 20 s (balance) + 6 s (deposit) + DB ≈ 27 s worst case, under the 30 s `WriteTimeout`; a zero deposit ≈ 20 s | Same calls as a withdraw. | — |
| `DepositController` depends on the concrete `*actions.DepositCommandHandler`, wrapped in `RequireAuth` by `Startup` | Same pattern as Phases 06–08. | An interface for one implementation. |

### Flow

```
POST /api/v1/deposit  (RequireAuth → user ID)
  decode {currency, amount, providerTransactionId, providerWithdrawnTransactionId}
                                                            bad JSON / missing amount → 400
  DepositCommandHandler.Execute
    parse both provider IDs, currency                         → 400
    players.FindByID → currency == player currency            → 404 / 400
    WithPlayerLock(user):
      Atomic: FindByProviderTransactionID(providerTransactionId)
        found DEPOSIT, same amount+currency+bet → replay (200 / 202 / 409 if FAILED)
        found, anything else                    → 409 CONFLICT
      Atomic: FindByProviderTransactionID(providerWithdrawnTransactionId)
        not found / is a deposit           → 404
        withdraw PENDING / FAILED          → 409 (not completed)
        currency ≠ withdraw currency       → 400
        bets.FindByID → bet.Settle check   → 409 (settled / cancelled)
      wallet.Balance                                          → 502, nothing stored
      amount == 0:
        Atomic: NewDeposit → Complete(oldBalance) → Insert, bet.Settle(LOST), Update → 201
      amount > 0:
        Atomic: NewDeposit(PENDING, BetID = withdraw's bet) → Insert   ← committed
          ErrTransactionExists → own lookup again: replay, or 409
          ErrBetAlreadySettled → 409 (another deposit is active)
        wallet.Deposit(reference = transaction ID, betId)
        Atomic (ctx WithoutCancel):
          applied      → tx.Complete, bet.Settle(WON), Update both → 201
          insufficient → tx.Fail, Update                         → 422
          rejected     → tx.Fail, Update                         → 502
          unknown      → nothing to write                        → 202 (betStatus PLACED)
```

## Files to create / modify

| File | Change |
|---|---|
| `Actions/withdraw.go`, `withdraw_test.go` | Step 2: type check in `replayIfProcessed` + test. Step 3: use the shared helpers |
| `Actions/operation.go` | `readOldBalance`, `logTransaction` (step 3) |
| `Actions/deposit.go`, `deposit_test.go` | `DepositCommandHandler`, `NewDepositCommandHandler(players, locker, wallet, ids, clock, logger)`, `DepositCommand{UserID, ProviderTransactionID, ProviderWithdrawnTransactionID, Amount, Currency}` (raw strings for IDs/currency, parsed inside) |
| `Application/deposit.go`, `deposit_test.go` | `DepositController`, `depositRequest` |
| `Startup/main.go`, `router.go`, `router_test.go` | The handler, `Handlers.Deposit`, route `POST /api/v1/deposit` |
| `README.md`, `DESIGN.md`, `ARCHITECTURE.md`, `docs/plans/00-general-plan.md` | Endpoint, decisions (§4 zero-amount, §1 deposit side, §5 deposit paragraph; remove the "to handle in Phase 09" note), layout, phase row, open question 2 resolved |

`Domain`, `Persistence` and `domaintest` are expected to need **no change**:
`NewDeposit`, `Bet.Settle`, the errors, the active-settlement index and its
fake equivalent, and `FakeWallet.Deposit` already exist. If a test shows
otherwise, the change gets its own commit and an "As built" row.

## Commit sequence

**Working mode** — test-first; **Claude writes every step** (tests and
production code) and explains the Go concepts in the chat. One step at a time:
the developer reviews each step before it is committed, and a step may be split
into smaller commits when that makes the history easier to follow.

For every step: `gofmt -l .` empty, `go vet ./...`, `go build ./...`,
`go test ./...` green; Claude proposes the commit message and commits only when
the developer asks. Branch: `08_deposit` (following the existing `07_withdraw`
numbering).

---

1. **`docs(plan): add phase 09 deposit endpoint plan`** — this file.

2. **`fix(actions): treat another operation's provider id as a conflict in withdraw`**
   - `replayIfProcessed`: `previous.Type != TransactionWithdraw` →
     `ErrIdempotencyConflict`, checked before the amount comparison.
   - Test: a `COMPLETED` deposit put in the `FakeStore` with provider ID
     `prov-tx-9` and the same amount/currency; a withdraw with `prov-tx-9` →
     `ErrIdempotencyConflict`, no Wallet call, no new transaction.

3. **`refactor(actions): share the old balance read and the operation log`**
   - `Actions/operation.go`: `readOldBalance` and `logTransaction`, moved from
     `withdraw.go` with the operation name as a parameter. No behaviour change:
     the withdraw tests stay green untouched.

4. **`feat(actions): validate deposit and check the referenced withdraw`**
   - `Execute` up to and including the `oldBalance` read (returns before
     storing anything; the rest comes in steps 5–6).
   - Tests (`TestDeposit_…`, with `FakeStore`, `FakeWallet`, `FakeIDGenerator`,
     `FixedClock`; each one asserts **no Wallet `Deposit` call and nothing
     stored**):
     - invalid provider ID / invalid withdrawn provider ID / invalid currency /
       currency ≠ player's → `ErrInvalidInput` category;
     - unknown player → `ErrPlayerNotFound`;
     - withdrawn ID unknown / of **another player** / of a deposit →
       `ErrTransactionNotFound`;
     - withdraw `PENDING` / `FAILED` → `ErrWithdrawNotCompleted`;
     - bet `WON` / `LOST` / `CANCELLED` (withdraw `CANCELLED`) →
       `ErrBetAlreadySettled`;
     - withdraw in another currency (put directly in the store) →
       `ErrCurrencyMismatch`;
     - balance read fails **or times out** → `ErrWalletFailure` (never
       `ErrWalletTimeout`).
   - *Go concepts:* reusing the shared helpers from step 3; why the checks
     run inside the lock (another deposit of the same player could settle the
     bet between our check and our insert).

5. **`feat(actions): call the wallet and store the deposit outcome`**
   - `PENDING` insert, Wallet `Deposit`, outcome (amounts `> 0` only).
   - Tests:
     - success → deposit `COMPLETED`, `oldBalance`/`newBalance` from the
       Wallet, bet `WON`; Wallet called with `reference` = deposit ID and the
       **withdraw's** `betId`;
     - the `PENDING` deposit exists **when the Wallet is called**;
     - `FailAfterApply` / `FailBeforeApply` / `TimeOut` → `PENDING`, bet still
       `PLACED`, result `BetStatus` `PLACED`, `newBalance` `nil`;
     - `Reject` → `FAILED`, `ErrWalletRejected`, bet `PLACED`;
     - `ErrWalletAppliedBalanceUnknown` → `COMPLETED`, `newBalance = old + amount`;
     - outcome write fails (`UpdateTransactionErr`) → result `PENDING`, no error,
       bet `PLACED`;
     - request `ctx` cancelled during the Wallet call → outcome still stored;
     - a `PENDING` deposit already on the bet (other provider ID) →
       `ErrBetAlreadySettled`, no Wallet call;
     - a `FAILED` deposit on the bet → a new deposit is accepted (`WON`).

6. **`feat(actions): settle zero-amount deposits without the wallet`**
   - Tests:
     - amount `0` → `COMPLETED`, bet `LOST`, `oldBalance` = `newBalance` = the
       balance read, **no** Wallet `Deposit` call (one `Balance` call);
     - amount `0` and the balance read fails → `ErrWalletFailure`, nothing
       stored;
     - amount `0` on a settled bet → `ErrBetAlreadySettled`.

7. **`feat(actions): make deposit idempotent`**
   - Replay and conflict rules; `ErrTransactionExists` on insert → own lookup
     again.
   - Tests:
     - same request twice → second `Replayed`, **one** Wallet `Deposit`, one
       deposit, bet `WON`;
     - replay of a `PENDING` → `PENDING`, `Replayed`, `BetStatus` `PLACED`, no
       Wallet call;
     - replay of a `FAILED` → `ErrPreviousAttemptFailed`;
     - same provider ID with another amount / currency / withdrawn ID →
       `ErrIdempotencyConflict`;
     - provider ID of an existing **withdraw** (including the referenced one
       itself) → `ErrIdempotencyConflict`;
     - provider ID already used by **another player** → `409` category, no
       Wallet `Deposit`;
     - replay after the bet changed (deposit `CANCELLED`, bet `PLACED` in the
       store) → `200` data: deposit `CANCELLED`, `BetStatus` `PLACED`;
     - zero-amount replay → `Replayed`, no Wallet call;
     - **concurrent duplicates**: 10 goroutines with the same request → one
       Wallet `Deposit`, one non-replayed result, nine replays;
     - **concurrent different deposits on one bet**: 10 goroutines with
       different provider IDs → one `COMPLETED`, nine `ErrBetAlreadySettled`,
       one Wallet `Deposit`.

8. **`feat(application): add deposit endpoint`**
   - `depositRequest` (`Amount *domain.Money`), `DepositController`.
   - Tests (`TestDepositController_…`, real handler with fakes):
     - `201` with the exact §7.4 body and `"betStatus":"WON"`;
     - amount `0` → `201`, `"betStatus":"LOST"`, `oldBalance` = `newBalance`;
     - replay → `200`; unknown outcome → `202`, `"newBalance":null`,
       `"betStatus":"PLACED"`;
     - missing `amount` → `400` "amount is required"; `"amount":"10.00"`,
       `10.005`, `-1`, missing `providerTransactionId`, missing
       `providerWithdrawnTransactionId`, `"currency":"usd"` → `400`;
     - unknown withdrawn ID → `404`; bet already settled → `409`; conflict →
       `409`; rejected → `502`; balance timeout → `502` (not `504`);
     - through `RequireAuth`: no token → `401`, no Wallet call.

9. **`feat(startup): wire deposit endpoint`**
   - `newHandlers`: `actions.NewDepositCommandHandler(...)`,
     `RequireAuth(...)(DepositController)`; `Handlers.Deposit`, route
     `POST /api/v1/deposit`; router tests.
   - Manual check (see Test plan).

10. **`docs(deposit): document deposit endpoint`**
    - `README.md` (request/response, status codes, curl), `DESIGN.md` (§1
      deposit side, §4 zero-amount decision, §5 deposit paragraph — one active
      settlement, `PENDING` deposit keeps the bet `PLACED`, idempotency by bet;
      remove the "Missing amount … Phase 09" note), `ARCHITECTURE.md`
      (deposit section, `operation.go`), general plan (row 09, open question 2),
      and an "As built" section here.

## Test plan

- **Unit (`go test ./...`, no Docker):** the withdraw type fix; the deposit use
  case with every `FakeWallet` behaviour; every §8.4 rule; idempotency
  including concurrent duplicates, concurrent different deposits on one bet and
  another player's ID; controller and router.
- **Integration:** no persistence change. The active-settlement index is
  already covered by Phase 04 (`…SecondActiveDepositOnSameBet_ReturnsBetAlreadySettled`,
  `…DepositAfterTheFirstWasCancelled_IsStored`).
- **Manual (`curl`)**, local PostgreSQL + Wallet mock:
  ```bash
  TOKEN=…   # login as player_usd (see Phase 08)
  curl -i -X POST localhost:8080/api/v1/withdraw -H "Authorization: Bearer $TOKEN" \
       -d '{"currency":"USD","amount":10.50,"providerTransactionId":"prov-tx-0001"}'      # 201
  DEP='{"currency":"USD","amount":25.00,"providerTransactionId":"prov-tx-0002","providerWithdrawnTransactionId":"prov-tx-0001"}'
  curl -i -X POST localhost:8080/api/v1/deposit -H "Authorization: Bearer $TOKEN" -d "$DEP"  # 201 WON (or 202)
  curl -i -X POST localhost:8080/api/v1/deposit -H "Authorization: Bearer $TOKEN" -d "$DEP"  # 200 (or 202)
  # other provider ID on the same bet → 409; amount 26 with prov-tx-0002 → 409
  # unknown withdrawn ID → 404; same withdrawn ID with player_usd_2's token → 404
  # new withdraw + deposit amount 0 → 201 LOST, oldBalance == newBalance, no Wallet deposit in the logs
  # currency EUR → 400; missing amount → 400
  # ~10 deposits on new bets: expect some 202
  psql … -c "SELECT b.status, t.status, count(*) FROM bets b JOIN transactions t ON t.bet_id = b.id AND t.type='DEPOSIT' GROUP BY 1,2"
  ```
  Check: every `201`/`200` deposit has a `WON`/`LOST` bet; every `202` deposit
  is `PENDING` with a `PLACED` bet; one log line per deposit; no secrets.
- **Not covered:** `go test -race` (no C compiler on Windows; CI in Phase 13).

## Open questions

1. ~~**Zero-amount deposit.**~~ **Resolved:** not forwarded to the Wallet (it
   adds or removes nothing); the balance is still read for `oldBalance`
   (`newBalance = oldBalance`).
2. ~~**Who writes which step?**~~ **Resolved:** Claude writes every step, one
   small commit at a time, reviewed by the developer before each commit.
3. ~~**Withdrawn ID that belongs to a deposit.**~~ **Resolved:** `404` (it does
   not identify a withdraw).

## As built — deviations from this plan

| Planned | Built | Why |
|---|---|---|
| Step 5: one commit | Two commits (`78a342a` use case, `1268aaf` the `Deposit` side of the test Wallet wrappers) | The developer's choice when committing. |
| `ErrTransactionExists` on insert → own lookup again | The lookup runs again for `ErrTransactionExists` **and** `ErrBetAlreadySettled` (`isDuplicateDeposit`, `replayAfterDuplicateInsert`) | An identical request racing past the lock breaks both unique indexes; PostgreSQL does not say which one it reports, so checking only one could turn a replay into a `409`. Not covered by a test: the `FakeStore` always reports the provider ID index first. |
| Pending insert rejected by the active-settlement index → `409` | Same, through `insertDepositError`, shared by the positive and the zero-amount paths | The message ("the bet already has a pending or completed deposit") is the same in both. |
| Referenced withdraw checks in the order of the table | Not found / not a withdraw → currency → status → bet | Any order gives the same codes for a single problem; the currency is checked before the status so a deposit in the wrong currency is always `400`. |
| `FakeStore` / `Domain` changes "if a test shows otherwise" | None needed | — |
| Manual `curl` check of step 9 | **Not done yet** | The Wallet mock and PostgreSQL were not running during the phase. Pending: run the Test plan and record the results here. |

## Definition of done

- `Actions` imports only stdlib and `Domain`; `Application` does not import
  `Shared` or `Persistence`; `domaintest` is not imported by production code.
- `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` green without
  Docker; integration tests still green with `TEST_DATABASE_URL`.
- Every case of steps 2 and 4–8 has a test, in particular: `PENDING` exists
  before the Wallet call, unknown outcome never `FAILED`, zero amount never
  reaches the Wallet, one active settlement (including a `PENDING` deposit and
  concurrent different deposits), replay without a Wallet call, conflict,
  another player's withdraw → `404`, no `504` from the endpoint.
- No `float` anywhere in the request/response path.
- Manual `curl` check of step 9 done against the real Wallet mock.
- `README.md`, `DESIGN.md` (§4 filled), `ARCHITECTURE.md`, `00-general-plan.md`
  updated.
- The developer can explain each Go concept listed in the steps.
