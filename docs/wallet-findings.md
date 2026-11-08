# Wallet findings

Empirical behaviour of the mock Wallet (`kentechsp/wallet-client`), produced in
Phase 01 ([plan](plans/01-wallet-spike.md)). Later phases (05, 08–11) treat this
document as the source of truth about the Wallet.

- Image tested: `kentechsp/wallet-client@sha256:682a98803dd117469759e5f10be788e4a0418de3fa5dd12819bb4fe2d1890abf`
  (`linux/arm64` only, run under emulation on amd64). How to run it:
  [`wallet-spike/README.md`](wallet-spike/README.md).
- Each answer states **what we did**, **what we saw** (real request/response or
  CSV in `wallet-spike/results/`) and **the conclusion**.

## Q1 — API surface

**What we did.** Saved the OpenAPI document (`GET /swagger/doc.json`) as
[`wallet-spike/swagger.json`](wallet-spike/swagger.json) and compared it with the
routes the server prints at startup (`docker logs wallet-spike`). Nothing below
has been tested yet — it is what the Wallet *claims*.

**What we saw.** Swagger 2.0, title `wallet-client`, version `1.0.0`.

| Method | Path | In Swagger | Auth | Documented responses |
|---|---|---|---|---|
| GET | `/api/v1/balance/{user_id}` | yes | `x-api-key` | 200 `balanceResponse`, 400, 500 |
| POST | `/api/v1/withdraw` | yes | `x-api-key` | 201 `operationResponse`, 400, 500 |
| POST | `/api/v1/deposit` | yes | `x-api-key` | 201 `operationResponse`, 400, 500 |
| GET | `/health` | no (startup log only) | — | — |
| GET | `/metrics` | no (startup log only) | — | Prometheus metrics |
| GET | `/swagger/*any` | — | — | Swagger UI / document |

Schemas (`withdrawRequest` and `depositRequest` are identical; values are
illustrative, only the types come from Swagger):

```jsonc
// POST /api/v1/withdraw | /api/v1/deposit — all fields required
{
  "userId": 34633089486,          // integer
  "currency": "USD",              // string
  "transactions": [               // the "list payload"
    { "amount": 10.5,             // number
      "betId": 1,                 // integer
      "reference": "..." }        // string
  ]
}

// 201 operationResponse
{ "balance": 89.5,                // number
  "transactions": [ { "id": 123, "reference": "..." } ] }  // id: integer

// 200 balanceResponse
{ "balance": 100.0, "currency": "USD" }

// 400 / 500 errorResponse
{ "code": "...", "msg": "..." }
```

**Conclusions (to be confirmed by the next steps).**

- **No rollback** and **no transaction lookup** endpoint. The only state we can
  read back is the **balance**. So, after an ambiguous failure, the only way to
  check whether an operation was applied is a balance comparison, which is only
  meaningful if nothing else touched that wallet in between (→ per-player
  serialisation, Q6, Q8).
- One request can carry **several transactions**; the response returns one
  `{id, reference}` per transaction. We will always send exactly one. Whether a
  multi-item request can be partially applied is not documented.
- The Wallet returns its own integer transaction `id` → worth storing (useful in
  logs and for support), but not usable for any query.
- Amounts are JSON `number` in both directions; real precision and format are
  checked in Q2. User IDs and `betId` are `integer` (we use `int64`).
- Error body uses `msg`, not `message` — different from **our** API's error
  body; the Wallet client (Phase 05) must not pass Wallet errors through as-is.
- Only 400 and 500 are documented; no 401/403 for a bad API key (checked in Q3)
  and no 402/409/422 for insufficient funds (checked in Q5).
- `/health` exists → usable for the Docker Compose healthcheck (Phase 12).
- `/metrics` is undocumented; we may peek at it during the failure probes in
  case it exposes request/error counters.

## Q2 — Happy path

**What we did.** With a freshly started Wallet: read the balance of the four
seeded users, then withdraw 10.50 / deposit 25.25 on the USD user and
withdraw 0.01 / deposit 0.1 / deposit 0.2 on the EUR user, reading the balance
in between. Requests: [`wallet-spike/requests.http`](wallet-spike/requests.http) (section Q2).

**What we saw.**

Initial balances (all `200 OK`):

| User | Response body |
|---|---|
| 34633089486 | `{"balance":"5000","currency":"USD"}` |
| 34679664254 | `{"balance":"9000000000","currency":"EUR"}` |
| 34616761765 | `{"balance":"750.5","currency":"KES"}` |
| 34673635133 | `{"balance":"31415.25","currency":"USD"}` |

Operations on 34633089486 (USD):

```http
POST /api/v1/withdraw
{"userId":34633089486,"currency":"USD","transactions":[{"amount":10.50,"betId":1001,"reference":"spike-step4-w1"}]}

HTTP/1.1 200 OK
{"balance":"4989.5","transactions":[{"id":1,"reference":"spike-step4-w1"}]}
```

```http
POST /api/v1/deposit
{"userId":34633089486,"currency":"USD","transactions":[{"amount":25.25,"betId":1001,"reference":"spike-step4-d1"}]}

HTTP/1.1 200 OK
{"balance":"5014.75","transactions":[{"id":2,"reference":"spike-step4-d1"}]}
```

Balance re-read after each one matched the `balance` in the response.

Precision on 34679664254 (EUR, 9 000 000 000): deposits of 0.1 and 0.2 returned
`"9000000000.09"` and then `"9000000000.29"` — exact decimals, no float noise
(the starting point was 8 999 999 999.99, see the note below).

Every response, success or error, is `Content-Type: application/json; charset=utf-8`
and carries permissive CORS headers. The Wallet logs show it accepts/generates an
`X-TRACE-ID` header per request.

**Conclusions.**

- **Balances are JSON strings, not numbers** (`"4989.5"`), contrary to Swagger.
  Our client must decode `balance` as a string and parse it with
  `decimal.NewFromString`. Good news: no float conversion on the way in.
- **Balances are not padded**: `"5000"`, `"750.5"`, `"9000000000.09"`. Never
  compare them as strings; always parse to `Money`.
- **The Wallet computes with exact decimals**, even with 10+ significant digits.
- **Success is `200`, not `201`** as Swagger says. The client should accept any
  `2xx` as success.
- Request amounts are accepted as JSON numbers (`10.50`, `0.01`). Whether a
  string amount or 3 decimals is accepted is checked in Q4. We will serialise
  `Money` as a JSON number with exactly 2 decimals.
- User IDs (11 digits, > 2³¹) are accepted as JSON integers → `int64` is fine.
- Transaction `id`s are a **global sequence** across users and operations
  (1, 2, … ), and a failed request also consumed one (see below).
- `X-TRACE-ID`: we could send our `transactionId` in it so our logs and the
  Wallet's logs correlate (Phase 05).
- `/health` → `{"status":"OK"}`, no API key needed.

**Preliminary failure observations (to be measured in Q6/Q7).**

- `POST /withdraw` of 0.01 on the EUR user answered
  `500 {"msg":"Internal server response.","code":"666"}` — **but it was applied**:
  the next deposit of 0.1 returned `9000000000.09`, which only adds up if the
  0.01 was withdrawn. It also consumed transaction `id` 3. → *error after applying exists*.
- `GET /balance` also fails randomly with the same 500 body; a retry succeeded.
- One `GET /balance` got **no HTTP response at all** after ~5.5 s (curl status
  `000`) while the Wallet logged a 500 → the connection can drop without a response.
- Every call takes **~0.6–4.8 s** (even `/health` took 1.3 s), most around 3 s.

## Q3 — `x-api-key`

**What we did.** `GET /balance/34673635133` with: no key (×3), a wrong key (×3),
an empty key, the right key in lowercase, the right key under the header name
`X-API-KEY`, and the key in `Authorization`. Then `POST /withdraw` of 1.00 with a
wrong key (×2), followed by three balance reads with the right key.

**What we saw.**

| Case | Status | Body |
|---|---|---|
| No key / wrong / empty / lowercase value / in `Authorization` | `401` | `{"msg":"Authentication error, see logs for more details.","code":"AuthenticatonError"}` |
| Right key, header name `X-API-KEY` | `200` | balance |
| `POST /withdraw` with a wrong key | `401` | same body; balance unchanged afterwards (`"31415.25"`) |

- `401` is returned although Swagger does not document it. The code is spelled
  `AuthenticatonError` (sic).
- The key **value** is case-sensitive; the header **name** is not (normal HTTP).
- Latency is applied even to rejected requests (0.4–4.5 s).
- One wrong-key request got **no response at all** (curl `000` after 5.0 s) while
  the Wallet logged `401 … latency:5000 ms`. Together with the `500 … 5552 ms`
  seen in Q2, this suggests the Wallet's HTTP server closes the connection when
  a response takes ≥ ~5 s (a server write timeout). *Hypothesis, checked in Q7.*

**Conclusions.**

- A `401` from the Wallet is **definitive and nothing was applied** → *not applied*.
  But it is a **configuration error on our side** (bad `WALLET_API_KEY`), not a
  player problem: map it to `502 WALLET_ERROR` and log it at `error` level, never
  logging the key itself.
- The client sends the header as `x-api-key` with the exact value from config.
- Even a request that can only be rejected can end without a response → a
  missing response never tells us what happened (*unknown*).

## Q4 — Validation errors

**What we did.** After `docker restart`, sent invalid or unusual requests for
user 34673635133 (USD, 31415.25). Each request was retried while the answer
was a random `500`/no response, and the balance was read before and after.
Representative requests: [`wallet-spike/requests.http`](wallet-spike/requests.http) (section Q4).

> Note: in the first run a script bug sent the same `reference` in every request.
> The ambiguous cases were repeated with unique references; the table shows the
> clean result. The bug itself revealed something about duplicates (see Q8).

**What we saw** (withdraw unless stated).

| Input | Status | Body `code` | Balance |
|---|---|---|---|
| `amount: -5` (withdraw and deposit) | 400 | `InvalidRequest` | unchanged |
| `amount: 0` (withdraw and deposit) | **200** | — | unchanged; consumes a Wallet `id` |
| `amount` missing | **200** | — | unchanged (treated as 0) |
| `amount: 1.005` / `0.001` (3 decimals) | **200** | — | **applied with 3 decimals** (`31414.245`) |
| `amount: "1.00"` (string) | **200** | — | applied |
| `amount: 1e2` (exponent) | **200** | — | applied (−100) |
| unknown `userId` 12345 | 400 | `UserNotFound` | — |
| currency `EUR` for a USD user | 400 | `InvalidCurrency` (*Currency mismatch for the user.*) | unchanged |
| currency `GBP` / `usd` (lowercase) | 400 | `InvalidCurrency` | unchanged |
| malformed JSON | 400 | `InvalidRequest` | unchanged |
| `transactions` missing or `[]` | 400 | `InvalidRequest` | unchanged |
| `betId` missing | **200** | — | applied (treated as `betId` 0) |
| `betId`: `"3012"` / `1.5` / `-7` | 400 | `InvalidRequest` | unchanged |
| `betId: 0` | 200 | — | applied |
| `reference` missing or `""` | **200** | — | applied; response echoes `"reference":""` |
| 2 transactions, the 2nd invalid (`-1`) | 400 | `InvalidRequest` | **unchanged** (first one not applied either) |
| `GET /balance/12345` (unknown user) | **200** | `{"balance":"0","currency":""}` | — |
| `GET /balance/abc` | 400 | `InvalidRequest` | — |

All 400 bodies have the shape `{"msg":"…","code":"…"}`; `msg` is a fixed text per code.

**Conclusions.**

- **The Wallet validates very little.** It accepts 3+ decimals, string amounts,
  zero amounts and missing `betId`/`reference`. **All validation of money and
  identifiers must happen on our side, before the call**; we must never send a
  request we have not validated (Phase 03 `Money`, Phases 08–10 handlers).
- What it does reject (`400 InvalidRequest`, `UserNotFound`, `InvalidCurrency`)
  is **definitive and nothing is applied** → *not applied*. With our own
  validation in front, any 400 from the Wallet means a bug or a data mismatch on
  our side (e.g. seeded currency ≠ Wallet currency) → `502 WALLET_ERROR`, logged.
- A list with an invalid item is rejected **as a whole** (validation is atomic).
  We still send exactly one transaction per request.
- **`GET /balance` of an unknown user returns `200` with `"0"` and an empty
  currency.** Our client must treat an empty `currency` as an error, otherwise we
  would show a player a zero balance.
- **Zero amounts are accepted by the Wallet and consume an `id`**, so the
  zero-amount deposit decision (Phase 09) is ours. Forwarding it is possible.
- Always send a non-empty `reference` and a positive `betId`: the Wallet would
  silently use `""` and `0`.

## Q5 — Insufficient funds

**What we did.** On user 34616761765 (KES, 750.5, untouched since the restart):
withdraws above, equal to and just below the balance, a two-item withdraw whose
items fit individually but not together, and small amounts on a nearly empty
wallet. Balance read before and after each one; retries only on `500`/no response.

**What we saw.**

| Balance | Withdraw | Status | Balance after |
|---|---|---|---|
| 750.5 | 750.51 | 400 `InsufficientBalance` | 750.5 |
| 750.5 | 400 + 400 (two items) | 400 `InsufficientBalance` | 750.5 (neither applied) |
| 750.5 | **750.5 (exact)** | **400 `InsufficientBalance`** | 750.5 |
| 750.5 | 0.01 | 200 | 750.49 |
| 750.49 | **750.49 (exact)** | **400 `InsufficientBalance`** | 750.49 |
| 750.49 | 750.48 | 200 | 0.01 |
| 0.01 | **0.01 (exact)** | **400 `InsufficientBalance`** | 0.01 |
| 0.01 | 0.001 | 200 | 0.009 |

Body: `{"msg":"Insufficient balance for the user.","code":"InsufficientBalance"}`.
Status is **400**, the same as validation errors; only `code` distinguishes them.

**Conclusions.**

- `InsufficientBalance` is **definitive and nothing is applied** (also for
  multi-item requests) → *not applied* → our `422 INSUFFICIENT_FUNDS`, transaction `FAILED`.
- The client must classify Wallet errors by **`code`**, not by HTTP status:
  `400 + InsufficientBalance` → insufficient funds; any other `400` → our bug /
  data mismatch (Q4).
- **The Wallet requires `amount < balance` (strict)**: a player can never
  withdraw their whole balance. We do not re-implement this rule (we never cache
  the balance, and the Wallet is the authority); we just report what it says.
  Recorded in `docs/decisions.md` §5 as a known Wallet behaviour (step 15).
- Only withdraws were tested: deposits add money, so they cannot run out of it.

## Q6 — Random failures

### Withdraw (step 9)

**What we did.** After `docker restart`, ran
`probe.ps1 -Operation withdraw -Iterations 200` (1.00 each, user 34633089486,
client timeout 30 s) → [`results/withdraw.csv`](wallet-spike/results/withdraw.csv).
The first run recorded a PowerShell error instead of the network error for calls
without a response (script bug, fixed), so a second run of 20 on user 34673635133
captured the real error → [`results/withdraw-errors.csv`](wallet-spike/results/withdraw-errors.csv).

**What we saw.**

| Outcome | Run 1 (200) | Run 2 (20) | Total | % | Applied? | Latency (run 1) |
|---|---|---|---|---|---|---|
| `200` | 150 | 14 | 164 | 74.5 % | **always** | 29 ms – 4 968 ms (p50 2 277) |
| `500 {"code":"666"}` | 17 | 1 | 18 | 8.2 % | **always** | 622 ms – 4 858 ms (p50 2 903) |
| No response (connection closed) | 33 | 5 | 38 | 17.3 % | **never** | 5 046 ms – 5 997 ms (p50 5 479) |

- No `other` or `unreadable` rows: every call either moved the balance by
  exactly −1.00 or not at all. Check: 5000 − 167 applied = 4833 = final balance.
- The `500` body is always `{"msg":"Internal server response.","code":"666"}`.
- "No response" is the server **closing the keep-alive connection without
  sending anything** (.NET: `WebException … KeepAliveFailure`). It is not our
  timeout (30 s).
- **Latency splits cleanly at ~5 s**: every answered call took < 5 s, every
  unanswered one took 5.0–6.0 s. It looks like the Wallet draws a random delay
  of 0–6 s and, when it exceeds ~5 s, drops the request *before* applying it.
- We never saw an explicit error *before* applying (an error status with the
  balance unchanged).

**Conclusions.**

- Three failure kinds, and in this sample each one maps to a single effect:

  | What the client sees | Observed effect | Rate |
  |---|---|---|
  | `200` | applied | 74.5 % |
  | `500` `666` | **applied** (error after applying) | 8.2 % |
  | connection closed after ~5 s, no response | **not applied** | 17.3 % |

- **A `500` must never be treated as *not applied*** — in this sample it was
  always applied. Marking it `FAILED` would lose money from the player's point of
  view (the Wallet took it, we say it failed).
- A dropped connection was never applied in this sample, but **the client
  cannot know that**: it is a property of this mock, not a contract, and a real
  network could drop the response of an applied request. Both stay *unknown*
  (`PENDING`) unless Q8 gives us a way to confirm (re-send → `409`).
  *(Step 12 confirmed it: dropped connections can be applied — see the
  correction at the end of Q6.)*
- About a quarter of the calls do not return a clean success → `PENDING` and its
  resolution (Phase 11) are a normal path, not an edge case.
- Phase 05 note: Go's `http.Transport` silently retries a request when a reused
  connection is closed before any response, **but only for idempotent requests**
  (GET, or with an `Idempotency-Key` header). Our `POST`s will not be retried
  behind our back; `GET /balance` may be.

### Deposit (step 10)

**What we did.** After `docker restart`, ran
`probe.ps1 -Operation deposit -Iterations 200` (1.00 each, user 34633089486,
client timeout 30 s, fixed script) → [`results/deposit.csv`](wallet-spike/results/deposit.csv).

**What we saw.**

| Outcome | Count | % | Applied? | Latency |
|---|---|---|---|---|
| `200` | 142 | 71 % | **always** | 10 ms – 4 971 ms (p50 2 239) |
| `500 {"code":"666"}` | 18 | 9 % | **always** | 709 ms – 4 797 ms (p50 2 815) |
| No response (connection closed, `KeepAliveFailure`) | 40 | 20 % | **never** | 5 004 ms – 5 985 ms (p50 5 478) |

- No `other` or `unreadable` rows. Check: 5000 + 160 applied = 5160 = final balance.
- Same bodies and the same ~5 s split as withdraw.

**Conclusions.**

- **Deposit fails exactly like withdraw**: same three kinds, same effects,
  similar rates. One classification rule serves both operations.
- Across the 420 probed calls (withdraw + deposit): `200` 72.9 %, `500` 8.6 %
  (all applied), dropped connection 18.6 % (none applied).
- For a deposit the risk of mis-classifying a `500` as *not applied* is the
  mirror image of withdraw: the player would be paid by the Wallet while we
  record the settlement as failed — and a provider retry could pay them twice
  unless the Wallet de-duplicates (Q8).

### Correction (step 12): a dropped connection *can* be applied

In Q8 three requests lost their connection **and were applied** (balance moved,
and the re-send answered `409`); two more had already happened in step 6.
The Wallet's own log explains the "no response" case: it records the real
result with its latency, and every POST slower than ~5 s loses its response.
Tally of every POST in the Wallet log during the spike:

| Wallet-side latency | Logged status | Count | Applied? | Client saw |
|---|---|---|---|---|
| < 5 s | 200 / 500 / 400 / 401 / 409 | 412 | 200 and 500: yes | the response |
| **≥ 5 s** | **400** | 82 | no | dropped connection |
| **≥ 5 s** | **200** | **3** | **yes** | dropped connection |
| ≥ 5 s | 409 | 1 | no | dropped connection |

So the Wallet's HTTP server cuts every response that takes ≥ ~5 s (a server
write timeout), **whatever the operation did**. Most slow requests are rejected
inside the Wallet (logged as 400, body not logged), but some are applied.
**"Not applied" in the step 9/10 samples was luck, not a rule**: a dropped
connection is genuinely *unknown*.

## Q7 — Latency and timeout

**What we did.** Reused the operation CSVs of Q6 (420 POSTs) and ran
`probe.ps1 -Operation balance -Iterations 100` on user 34616761765
→ [`results/balance.csv`](wallet-spike/results/balance.csv). Client timeout
30 s everywhere, so no call was cut by us.

**What we saw.**

| | Answered | p50 | p95 | p99 | max | Dropped (no response) |
|---|---|---|---|---|---|---|
| `POST` withdraw/deposit | 342 / 420 | 2 385 ms | 4 692 ms | 4 968 ms | **4 971 ms** | 78 (18.6 %), all at 5.0–6.0 s |
| `GET /balance` | 99 / 100 | 3 452 ms | 11 828 ms | 15 393 ms | **15 393 ms** | 1 (at 10.9 s) |

Share of **answered** calls that a client timeout would cut:

| Timeout | 2 s | 3 s | 4 s | 5 s | 6 s | 8 s | 10 s | 12 s | 16 s |
|---|---|---|---|---|---|---|---|---|---|
| `POST` | 59.4 % | 32.5 % | 17.8 % | 0 % | 0 % | — | — | — | — |
| `GET /balance` | — | 51.5 % | — | 18.2 % | — | 10.1 % | 7.1 % | 4.0 % | 0 % |

- Minimum latencies are 10–45 ms, so the delay is **artificial** (random per
  request); arm64 emulation adds little.
- The "~5 s" hypothesis of Q3 holds **for POSTs only**: no POST is answered
  after 5 s and every slower one is dropped. `GET /balance` has a different,
  longer delay (up to ~15 s) and is almost never dropped.
- No call ever hung until our 30 s timeout.

**Conclusions / recommendation.**

- **Operations (`POST`): timeout 6 s.** It cuts no answered call, and anything
  still waiting at 6 s will never be answered. The current
  `WALLET_TIMEOUT=3s` in `.env.example` would turn about **a third of the
  successful calls into `PENDING`** → change it in Phase 05.
- **Balance (`GET`): a separate, longer timeout, ~10 s**, plus **one retry** on
  `500`/dropped/timeout (a read is always safe to retry). Worst case ≈ 20 s,
  still bounded (assessment 9.5). With 16 s nothing would be cut, but a 16 s
  `GET /player` is a poor experience; 10 s + 1 retry is the proposed balance.
- So the client needs **two timeouts** (e.g. `WALLET_TIMEOUT` for operations and
  `WALLET_BALANCE_TIMEOUT` for reads). Final names and values: Phase 05 plan.
- **No automatic retry of operations** in the client for now: whether re-sending
  is safe depends on Q8.
- Operations of the same player are serialised, so a slow Wallet call holds that
  player's lock for up to the timeout; this bounds how long a concurrent request
  of the same player can wait (Phase 04/08).

## Q8 — Duplicate requests

**What we did.** After `docker restart`, a sequence of withdraws and deposits on
user 34673635133 (and one on 34616761765) combining repeated and new `betId` /
`reference` values. Each request was re-sent while the answer was `500` or a
dropped connection, and the balance was read before and after. Wallet logs were
used to see the real result of dropped requests. Replayable cases:
[`wallet-spike/requests.http`](wallet-spike/requests.http) (section Q8).

**What we saw** (`Bn` = betId, `Rn` = reference; "applied" = balance moved).

| # | Request | Already existing | Result |
|---|---|---|---|
| A2 | withdraw B1/R1 (identical re-send) | withdraw B1/R1 | **409**, not applied |
| D | withdraw B1/R1, **amount 2** instead of 1 | withdraw B1/R1 | **409**, not applied |
| B | withdraw B1/**R2** | withdraw B1/R1 | applied (connection dropped; re-send → 409) |
| C | withdraw **B3**/R1 | withdraw B1/R1 | applied (500; re-send → 409) |
| K | withdraw B1/R1 for **another user** | withdraw B1/R1 (user 1) | **409**, not applied |
| N | withdraw [B1/R1, B13/R13] | withdraw B1/R1 | **409**, B13/R13 not applied either |
| E | **deposit** B1/R1 | withdraw B1/R1 | 200, applied |
| F, H | deposit B1/R5, then deposit B1/**R6** | — | both applied (two deposits on one bet) |
| G | deposit B1/R5 (identical re-send) | deposit B1/R5 | **409**, not applied |
| L | deposit **B11**/R1 | deposit B1/R1 | **409**, not applied |
| Q5 | deposit **B63**/R62 | deposit B62/R62 | **409**, not applied |
| P2 | deposit **B31**/R30 | withdraw B30/R30 only | applied (dropped; re-send → 409) |
| P3 | deposit B30/R30 | deposit B31/R30 | **409** |
| I | deposit B9/R9 with no withdraw for B9 | — | applied |
| J | withdraw B9/R10 after that deposit | deposit B9/R9 | applied |
| M | withdraw B12/R5 | deposit B1/R5 | applied |

The 409 body is always
`{"msg":"Transaction already exists.","code":"DuplicatedTransaction"}`: no
balance, no original `id`.

**Conclusions.**

- **The Wallet does de-duplicate**, with a different key per operation:

  | Operation | De-duplication key | Scope |
  |---|---|---|
  | withdraw | (`betId`, `reference`) | all users, withdraws only |
  | deposit | `reference` | deposits only (cross-user not tested) |

  Withdraws and deposits are separate namespaces (E, M, P2). The amount is
  **not** part of the key (D).
- **The Wallet has no bet state**: several withdraws or deposits on one bet,
  a deposit without a withdraw, and a withdraw after a deposit are all accepted.
  **Every bet rule of the assessment (one settlement per bet, cancel rules…) is
  ours to enforce.**
- **A re-send resolves an unknown outcome.** If we re-send the *identical*
  request (same `betId`, `reference` and amount):
  - `409 DuplicatedTransaction` → the earlier attempt **was applied**;
  - `200` → it was not, and it is now applied (exactly once);
  - nothing is ever applied twice.

  This is the mechanism for resolving `PENDING` (Phase 11), and it also makes a
  bounded client-side retry of operations safe. **Condition: every request we
  send uses a `reference` that is unique to that transaction of ours** — our
  `transactionId` (UUID) is the natural choice, stored before the call
  (intent-first) so a re-send is byte-for-byte the same request.
- Because the amount is not in the key, a 409 cannot detect a *different*
  payload under the same key. That is fine: payload conflicts are checked by us
  (payload hash) before anything reaches the Wallet.
- A multi-item request with one duplicate is rejected as a whole (N): another
  reason to send one transaction per request.
- **Do not re-send immediately after a dropped connection.** The Wallet logs
  slow requests finishing at ~5.7 s, after the client already saw the drop. If a
  re-send reached the Wallet while the original was still being processed, the
  duplicate check could run before the original is recorded and the operation
  could be **applied twice** (not observed, but nothing rules it out).
  Resolution should run a little later (seconds), not in the same instant.
- The de-duplication memory is in-memory like the balances: **after a Wallet
  restart a re-send is applied again** (confirmed in Q10).
- The step-6 deposit that got 409 on its first attempt (reference used only by
  withdraws and by an invalid deposit) is still not explained. It did not
  reproduce in Q8 (P2), and it errs on the safe side (rejecting, not applying twice).

## Q9 — Reverting an operation

**What we did.** After `docker restart`: reverted a withdraw with a deposit and a
deposit with a withdraw (same amount, same `betId`, new `reference`) on user
34673635133; then, on user 34616761765 (KES 750.5), tried to revert a deposit
after the player had spent the money. Requests:
[`wallet-spike/requests.http`](wallet-spike/requests.http) (section Q9).

**What we saw.**

| Step | Request | Result | Balance |
|---|---|---|---|
| R1a | withdraw 10, B1/`rev-w1` | 200 | 31415.25 → 31405.25 |
| R1b | **deposit 10, B1/`rev-c1`** (revert R1a) | 200 | → **31415.25** (restored) |
| R2a | deposit 10, B2/`rev-d2` | 200 | 31415.25 → 31425.25 |
| R2b | **withdraw 10, B2/`rev-c2`** (revert R2a) | 200 | → **31415.25** (restored) |
| R3a | KES deposit 10, B3 | 200 | 750.5 → 760.5 |
| R3b | KES withdraw 755, another bet | 200 | → 5.5 |
| R3c | **withdraw 10, B3** (revert R3a) | **400 `InsufficientBalance`** | 5.5 |
| R3d | withdraw 5.5, B3 (whole balance) | 400 `InsufficientBalance` | 5.5 |

**Conclusions.**

- **There is no rollback; a revert is a compensating operation** in the opposite
  direction for the same amount:
  - revert a **withdraw** → **deposit** (always possible: deposits have no
    balance check);
  - revert a **deposit** → **withdraw**, which **can fail with insufficient
    funds** if the player already spent the winnings (and, because of the strict
    `amount < balance` rule of Q5, even when the balance equals the amount).
- The compensating call uses the **same `betId`** and a **new `reference`**
  (the cancel transaction's own UUID). The Wallet imposes no rule linking it to
  the original (Q8: no bet state), so it cannot tell a revert from a normal
  operation — the link exists only in our database.
- A compensating call is just another Wallet operation: it can also return
  `500` or lose its connection, so a CANCEL goes through the same
  intent-first / `PENDING` / re-send flow as any other transaction.
- Reverting a deposit that cannot be paid back is a Cancel case the API must
  answer explicitly (`422 INSUFFICIENT_FUNDS`, cancel `FAILED`, bet unchanged) —
  to be decided against the table of assessment §8.5 in Phase 10.
- A partial revert (taking back only what is left) is technically possible but
  would leave the bet half-cancelled; not proposed.

## Q10 — Restart

**What we did.** With balances already changed by Q9: read all four balances,
withdrew 7 on user 34673635133 with a fixed `betId`/`reference` and re-sent it,
then `docker restart wallet-spike`, read the balances again and re-sent the same
withdraw twice. (The same effect had been visible at the start of every probe
run, each one preceded by a restart.)

**What we saw.**

| | USD 34633089486 | EUR 34679664254 | KES 34616761765 | USD 34673635133 |
|---|---|---|---|---|
| Before restart | 5000 | 9000000000 | 5.5 | 31408.25 |
| **After restart** | **5000** | **9000000000** | **750.5** | **31415.25** |

| Request (same `betId` 730001, `reference` `rst-R1`, amount 7) | Result |
|---|---|
| S1 first send | dropped connection, **applied** (31415.25 → 31408.25); re-send → 409 |
| S2 re-send before restart | `409 DuplicatedTransaction` |
| *`docker restart`* | |
| S3 re-send after restart | **`200`, applied again**, Wallet `id` **1** |
| S4 re-send | `409` |

**Conclusions.**

- **A restart wipes everything**: balances go back to the seeded values below,
  the de-duplication memory is lost, and the Wallet's transaction `id` sequence
  restarts at 1.

  | Wallet user ID | Currency | Initial balance |
  |---|---|---|
  | 34633089486 | USD | 5000 |
  | 34679664254 | EUR | 9000000000 |
  | 34616761765 | KES | 750.5 |
  | 34673635133 | USD | 31415.25 |

- **After a Wallet restart, a re-send of a request that had been applied is
  applied again.** Our "re-send to resolve `PENDING`" mechanism (Q8) is therefore
  only correct while the Wallet keeps its memory. In this assessment the Wallet
  restarts only when someone restarts it (and the documented reset is
  `docker compose down -v`, which also wipes our DB), so we accept the risk and
  document it in `DESIGN.md` ("Wallet restarts and loses its state").
- Wallet transaction `id`s are **not unique over time** → store them for
  information only, never as a key.
- A fourth case (S1) of a dropped connection that **was applied**, consistent
  with the Q6 correction.

## Implications for later phases

Summary of what Phases 03–12 should take from this document. These are
**inputs** for the phase plans, which make the final decisions.

### Classification of a Wallet operation result (Phase 05)

| Wallet result | Classification | Our transaction | Our API |
|---|---|---|---|
| `2xx` | applied | `COMPLETED` | `201` |
| `409 DuplicatedTransaction` on a **re-send** of our own request | applied (earlier attempt) | `COMPLETED` | `201`/`200` per phase |
| `400 InsufficientBalance` | not applied | `FAILED` | `422 INSUFFICIENT_FUNDS` |
| `400` any other code (`InvalidRequest`, `UserNotFound`, `InvalidCurrency`) | not applied | `FAILED` | `502 WALLET_ERROR` (our bug or data mismatch; log it) |
| `401 AuthenticatonError` | not applied | `FAILED` | `502 WALLET_ERROR` (bad `WALLET_API_KEY`; log it, never the key) |
| `500` (any body) | **unknown** | `PENDING` | `202` |
| connection dropped / reset, timeout, anything else | **unknown** | `PENDING` | `202` |

- Classify errors by the body's `code`, not only by HTTP status (Q5).
- `409` on the **first** send cannot happen if every `reference` is unique; if
  it ever does, it still means "a request with this key was applied".

### Wallet client (Phase 05)

- Send one transaction per request; `reference` = our `transactionId` (UUID),
  `betId` = our positive `int64` bet ID; never send `""` or `0` (Q4, Q8).
- Serialise amounts as JSON numbers with 2 decimals; parse `balance` from a
  **JSON string** with `decimal.NewFromString` (Q2).
- Treat an empty `currency` in `GET /balance` as an error (unknown user, Q4).
- Accept any `2xx` as success (the Wallet returns `200`, Swagger says `201`).
- Translate Wallet errors (`{code, msg}`) into domain errors; never pass them
  through to our API.
- Timeouts (Q7): **operations 6 s** (no POST is answered after 5 s);
  **balance ~10 s + 1 retry**. The current `WALLET_TIMEOUT=3s` in `.env.example`
  and `README.md` would turn a third of the successful calls into `PENDING` —
  update both in Phase 05.
- Optionally send our `transactionId` as `X-TRACE-ID` to correlate logs (Q2).

### Use cases (Phases 08–11)

- About **27 %** of operations end in `500` or a dropped connection → `PENDING`
  is a normal path, not an edge case (Q6).
- **Resolution of `PENDING`** (Phase 11): re-send the stored, identical request;
  `409` → `COMPLETED`, `2xx` → `COMPLETED`, definitive `4xx` → `FAILED`, another
  unknown → stays `PENDING`. Wait a few seconds after a dropped connection before
  re-sending (Q8). The same re-send makes a bounded in-request retry safe.
- **All validation and all bet rules are ours** (Q4, Q8): the Wallet accepts
  3 decimals, zero amounts, several settlements per bet, deposits without
  withdraws, etc.
- **Cancel = compensating operation** with the same `betId` and a new
  `reference`; cancelling a deposit can fail with `InsufficientBalance` (Q9).
- Zero-amount deposits are accepted by the Wallet (Q4): forwarding or not is
  purely our decision (Phase 09).

### Persistence and Docker (Phases 04, 12)

- Store the Wallet `id` for information only: it restarts at 1 with the Wallet (Q10).
- Seed must match the Wallet's users and currencies (table in Q10); a mismatch
  shows up as `400 InvalidCurrency`.
- Image is `linux/arm64` only: Compose needs `platform: linux/arm64` on amd64
  hosts. `GET /health` (no API key) works for the healthcheck (Q1, Q2).

### Comparison with §3 of the general plan

No contradictions. The findings **confirm** points 1–5 and 7 and **fill in** two
open items:

- §3.4 classification: confirmed, plus two cases it did not list
  (`401` → not applied; `409` on re-send → applied).
- §3.6 resolution of `PENDING`: the mechanism the plan left open is now known —
  re-sending the identical request, which the Wallet de-duplicates (valid while
  the Wallet does not restart).
