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

*TBD (step 4).* JSON encoding of amounts and IDs, response fields.

## Q3 — `x-api-key`

*TBD (step 5).* Missing / wrong key.

## Q4 — Validation errors

*TBD (step 6).* Status and body per invalid input.

## Q5 — Insufficient funds

*TBD (step 7).* Status and body; balance untouched?

## Q6 — Random failures

*TBD (steps 9–10).* Kinds, frequencies, error before vs after applying, can we
tell them apart?

## Q7 — Latency and timeout

*TBD (step 11).* Distribution, hangs, recommended `WALLET_TIMEOUT`.

## Q8 — Duplicate requests

*TBD (step 12).* De-duplication by `reference` and/or `betId`?

## Q9 — Reverting an operation

*TBD (step 13).* Compensating operations and their rules.

## Q10 — Restart

*TBD (step 14).* Is the state lost? Initial balances?

## Implications for later phases

*TBD (step 15).* Classification table (*applied / not applied / unknown*),
recommended timeout, retry policy.
