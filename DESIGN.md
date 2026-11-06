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
