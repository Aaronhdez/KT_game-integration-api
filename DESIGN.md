# Design decisions

> Skeleton. Each section is filled in by the phase indicated. Required content
> per section 11 of the assessment. Target length: 1–2 pages.

## 1. Behaviour under failure

| Situation | Behaviour | Why |
|---|---|---|
| Wallet returns an error **before** applying the operation | *TBD (05, 08)* | |
| Wallet returns an error **after** applying it | A `500` (and any response we never receive) is *unknown*: the transaction stays `PENDING` and the API answers `202`. It is resolved by re-sending the identical request: `409 DuplicatedTransaction` means it had been applied. *Details: 05, 08, 11.* | In the Phase 01 spike every Wallet `500` had been applied (36 of 36), and dropped connections were sometimes applied too. Marking them `FAILED` would contradict what the Wallet actually did. See [`docs/wallet-findings.md`](docs/wallet-findings.md) Q6, Q8. |
| Wallet times out | *TBD (05, 08)* | |
| Process stops between the Wallet call and the DB write | *TBD (08, 11)* | |
| Provider retries / sends the same request concurrently | *TBD (08)* | |
| Wallet restarts and loses its state | Not detected. After a restart the Wallet is back at its seeded balances and has forgotten which requests it applied, so re-sending a `PENDING` request would apply it again. Accepted risk. *Mitigations, if any: 11.* | The Wallet keeps everything in memory and has no endpoint to query past transactions (findings Q1, Q10). In this setup it only restarts on purpose, and the documented reset (`docker compose down -v`) also wipes our database. |

## 2. How a PENDING transaction gets resolved

*TBD (11).*

## 3. Retry of a request whose previous attempt ended FAILED

*TBD (11).*

## 4. Zero-amount deposits

*TBD (09).*

## 5. Other assumptions, decisions and future improvements

*Filled in progressively.*

**Logging (Phase 02).** Logs are JSON lines on stdout (`log/slog`), level from
`LOG_LEVEL`. Every HTTP request produces one line with `method`, `path`,
`status` and `duration_ms`. The request log deliberately records only the URL
**path**: never the query string, headers or bodies, because those carry the JWT,
passwords and other secrets. Configuration is logged at startup without secrets.

**Unknown routes and wrong methods (Phase 02).** Both return
`404 {"code":"NOT_FOUND",…}`, so every error keeps the same JSON shape. The
standard router would answer a wrong method with a plain-text `405`, but the
catch-all route that produces the JSON `404` also captures wrong methods, and the
assessment defines no "method not allowed" error code.

**Graceful shutdown (Phase 02).** On Ctrl+C / `SIGTERM` the server stops
accepting connections and waits up to `SHUTDOWN_TIMEOUT` for in-flight requests,
so an operation is not cut in the middle of a Wallet call by a normal restart.

**Money (Phase 03).** Amounts are `shopspring/decimal` values wrapped in a
`Money` type that can only be built through validating constructors: never
negative, at most 2 decimals and at most 18 integer digits (what fits in
`NUMERIC(20,2)`). Amounts with more decimals are **rejected, not rounded**
(`1.005` is an error, never `1.01`). JSON amounts are decoded straight from the
number's text into a decimal — never through `float64` — and a string (`"10.50"`),
`null`, a negative or a 3-decimal number is `400 INVALID_REQUEST`. Responses
write amounts as numbers with two decimals (`4989.50`). Zero is a valid amount
(a lost bet's deposit); "greater than zero" is a rule of the withdraw only.

**Currencies (Phase 03).** Only the exact codes `USD`, `EUR`, `KES` are accepted;
`usd` or `" USD"` are rejected instead of normalised, as the Wallet itself rejects
them. A request's currency is checked against the player's account currency
before any Wallet call, so a mismatch is our `400`, not a Wallet error.

**Cancel rules (Phase 03).** The §8.5 table lives in one pure function,
`DecideCancel`, tested row by row. Cancelling a `PENDING` transaction is `409`
(its outcome must be resolved first), a `FAILED` one is `409` (nothing to revert),
an already `CANCELLED` one returns the earlier cancel (`200`), and a `CANCEL`
transaction itself cannot be cancelled.

**Missing `amount` in a deposit (to handle in Phase 09).** If the field is absent,
JSON decoding leaves the amount at zero without calling our decoder — which for a
deposit would silently mean "bet lost". The deposit request DTO must detect the
missing field (e.g. decode into `*Money`) and answer `400`.

Known Wallet behaviours we rely on or work around (from
[`docs/wallet-findings.md`](docs/wallet-findings.md)):

- **The Wallet validates very little** (it accepts 3 decimals, string or zero
  amounts, missing `betId`/`reference`). All validation happens in our API
  before any Wallet call.
- **The Wallet has no notion of a bet**: it accepts several withdraws or
  deposits on one `betId`. Every bet rule (one settlement per bet, cancel rules)
  is enforced by us.
- **A withdraw of the whole balance is rejected** by the Wallet
  (`amount < balance` is required). We do not re-implement this rule; we report
  the Wallet's `InsufficientBalance` as `422 INSUFFICIENT_FUNDS`.
- **Reverts are compensating operations** (deposit to undo a withdraw, withdraw
  to undo a deposit), because the Wallet has no rollback. Undoing a deposit can
  fail with insufficient funds.
