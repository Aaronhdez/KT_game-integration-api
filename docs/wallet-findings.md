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

*TBD (step 7).* Status and body; balance untouched?

## Q6 — Random failures

*TBD (steps 9–10).* Kinds, frequencies, error before vs after applying, can we
tell them apart?

## Q7 — Latency and timeout

*TBD (step 11).* Distribution, hangs, recommended `WALLET_TIMEOUT`.

## Q8 — Duplicate requests

*TBD (step 12).* De-duplication by `reference` and/or `betId`?

**Preliminary observations (from step 6).**

- Several withdraws with the **same `reference` and different `betId`** were all
  accepted (`200`) and applied → `reference` alone is **not** a de-duplication key.
- Re-sending the **identical** request after a `500` or a dropped connection
  answered **`409 {"msg":"Transaction already exists.","code":"DuplicatedTransaction"}`**,
  and the balance proved the first attempt **had been applied** (deposit of 1 → +1;
  withdraw of 0.001 → −0.001). Nothing was applied twice.
- A withdraw with `betId 0` + an already-used `reference` → 409; `betId 0` + a new
  `reference` → 200.
- One deposit (amount 0, new `betId` 3016, `reference` already used by withdraws)
  answered 409 on its first attempt — not explained yet.
- Hypothesis: the key involves `betId` and `reference` together, maybe also the
  operation type. **If confirmed, a 409 on re-send means "already applied", which
  would turn an *unknown* outcome into a known one.** To be tested properly here.

## Q9 — Reverting an operation

*TBD (step 13).* Compensating operations and their rules.

## Q10 — Restart

*TBD (step 14).* Is the state lost? Initial balances?

## Implications for later phases

*TBD (step 15).* Classification table (*applied / not applied / unknown*),
recommended timeout, retry policy.
