# Design decisions

> Skeleton. Each section is filled in by the phase indicated. Required content
> per section 11 of the assessment. Target length: 1–2 pages.

## 1. Behaviour under failure

| Situation | Behaviour | Why |
|---|---|---|
| Wallet returns an error **before** applying the operation | Only a *definitive* rejection counts as "not applied": `400 InsufficientBalance` → `FAILED`, `422 INSUFFICIENT_FUNDS`; any other `400` or a `401` → `FAILED`, `502 WALLET_ERROR`, logged at `error` (it means a bug on our side or a wrong `WALLET_API_KEY`). The client classifies by HTTP status **and** body `code`; anything it does not recognise is *unknown*, never `FAILED`. A withdraw keeps its transaction as `FAILED` (for audit) and creates no bet. | Insufficient funds and validation errors are both `400`; only `code` tells them apart (findings Q3–Q5). A wrong `FAILED` cannot be undone, a `PENDING` can be resolved. |
| Wallet returns an error **after** applying it | A `500` (and any response we never receive) is *unknown*: the transaction stays `PENDING` and the API answers `202`. A withdraw answers `202` with `status: PENDING` and `newBalance`/`betStatus` `null`. It is resolved by re-sending the identical request: `409 DuplicatedTransaction` means it had been applied. *Resolution: 11.* | In the Phase 01 spike every Wallet `500` had been applied (36 of 36), and dropped connections were sometimes applied too. Marking them `FAILED` would contradict what the Wallet actually did. See [`docs/wallet-findings.md`](docs/wallet-findings.md) Q6, Q8. |
| Wallet times out | **Operations:** each call is bounded by `WALLET_OPERATION_TIMEOUT` (6 s). No answer in time is *unknown* → `PENDING`, `202`. The client never re-sends an operation by itself. **Balance reads:** `WALLET_BALANCE_TIMEOUT` (10 s) per attempt, one retry after 200 ms on a timeout, a network error or a `5xx`; the read then fails with `504 WALLET_TIMEOUT` or `502 WALLET_ERROR`. Worst case ≈ 20 s, still bounded. A withdraw reads the balance first (its `oldBalance`): if that read fails or times out, nothing has been sent, so it is a `502` (never `504`, which §7.3 reserves for reads) and nothing is stored. A withdraw's worst case is ≈ 20 s + 6 s ≈ 27 s, under the server's 30 s write timeout. | The Wallet never answers a POST after ~5 s but a balance read can take ~15 s (findings Q7); one shared 3 s timeout would have turned a third of successful operations into `PENDING`. A read is safe to repeat; an operation re-sent immediately could race the original (Q8), so re-sending is left to the resolution of Phase 11. |
| Process stops between the Wallet call and the DB write | The transaction was committed as `PENDING` (with its `betId` and reference) **before** the Wallet call, so it is not lost; the player's lock is released by PostgreSQL when the connection closes. The outcome is written with a context that is not cancelled with the request (bounded by 5 s), so a provider hanging up does not leave an applied withdraw unrecorded; if the write itself fails, the transaction stays `PENDING` and the API answers `202`. *Resolution: 11.* | Intent-first (§9.1): the record of what we sent must exist before we send it, or a crash would leave a Wallet movement nobody knows about. |
| Provider retries / sends the same request concurrently | Operations of one player are serialised by a lock, and the idempotency lookup runs inside it, before any Wallet call: a retry sees the first request's transaction. Same provider ID and payload → the stored result (`200`, or `202` while `PENDING`), without calling the Wallet; another amount → `409`. The unique index on `provider_transaction_id` is the safety net if two requests ever race past the lock (e.g. two API instances): the insert fails and the lookup runs again. Tested with 10 simultaneous identical requests → one Wallet call. | §9.2 asks for no duplicate effects even for simultaneous duplicates; a check in Go alone has a race window, a unique index does not. |
| Wallet restarts and loses its state | Not detected. After a restart the Wallet is back at its seeded balances and has forgotten which requests it applied, so re-sending a `PENDING` request would apply it again. Accepted risk. *Mitigations, if any: 11.* | The Wallet keeps everything in memory and has no endpoint to query past transactions (findings Q1, Q10). In this setup it only restarts on purpose, and the documented reset (`docker compose down -v`) also wipes our database. |

## 2. How a PENDING transaction gets resolved

*TBD (11).*

## 3. Retry of a request whose previous attempt ended FAILED

*Interim (Phase 08), final decision in Phase 11:* the retry gets
`409 CONFLICT` ("previous attempt failed") and the Wallet is not called. The
provider transaction ID is already used by a transaction that did not succeed;
repeating the original `422`/`502` would need its reason, which is not stored.

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

**Per-player lock (Phase 04).** Each operation takes a PostgreSQL session
advisory lock keyed by the player's wallet user ID and runs all its queries on
that same connection. A session lock (not a transaction lock or
`SELECT … FOR UPDATE`) is needed because the lock must survive the commit of the
`PENDING` transaction and the Wallet call that follows. Using one connection
for lock and work means an operation never waits for a second connection while
holding the first, so the pool cannot deadlock. It works with several API
instances, and a crash releases the lock automatically. Cost: each in-flight
operation holds one pooled connection for the duration of its Wallet call, so
the pool size (`pool_max_conns` in `DATABASE_URL`) bounds how many different
players can operate at once. If the lock cannot be taken or released cleanly,
the connection is closed instead of reused, which releases any lock it holds.
*Rejected:* an in-process mutex per player (simplest, but only correct with a
single instance) and a lease column with expiry (more code, overlapping
operations if a call outlives the lease).

**Database constraints as the last line of defence (Phase 04).** Idempotency
(`(wallet_user_id, provider_transaction_id)`), "one active settlement per bet"
and "one cancel per transaction" are partial unique indexes, and the domain's
amount and status rules are `CHECK` constraints. The repositories translate
violations of the three business indexes into domain errors, so use cases never
see PostgreSQL errors. *Changed in Phase 08:* the idempotency index was per
player; §6.1 requires provider transaction IDs to be unique across the whole
system, so migration `00005` made it global. Lookups stay scoped by player, so
another player's transaction is never returned: an ID they already used is a
`409` that only says the ID is taken.

**Money in PostgreSQL (Phase 04).** Amounts are `NUMERIC(20,2)` and cross the
Go/SQL boundary as text (`Money.String()` in, `amount::text` out, then
`ParseMoney`): exact in both directions, and every value read is validated
again by the domain.

**Wallet client (Phase 05).** `Shared/wallet` implements `domain.WalletClient`
with the standard library only. Each operation sends exactly one transaction,
with our transaction ID as `reference` (and as `X-TRACE-ID`, to correlate logs)
and the amount as a JSON number with two decimals. Any `2xx` is success. A `2xx`
whose body or balance cannot be read, and a `409 DuplicatedTransaction`, mean
*applied, new balance unknown* (`ErrWalletAppliedBalanceUnknown`): the
transaction completes, it never fails. A balance read with an empty or unknown
currency (what the Wallet answers for an unknown user) or an invalid amount is a
`502`, never a zero balance. Money and currency errors coming from the Wallet are
wrapped with `%v`, so they can never be mistaken for an invalid request (`400`).
Every call (every attempt, for balance reads) writes one log line with the
operation, user, bet, reference, attempt, Wallet status and code, outcome and
duration; the API key, headers and bodies are never logged (tested). The client
is wired into `Startup` with its first consumer, `GET /player` (Phase 07).

**Authentication (Phase 06).** Login checks the password against the seeded
bcrypt hash and returns an HS256 JWT (`sub` = wallet user ID as a string, `iat`,
`exp`) plus `tokenType: "Bearer"` and `expiresAt` (§8.1), with `Cache-Control: no-store`. An unknown username and
a wrong password return the **same `401`**, and an unknown username still runs
bcrypt against a fixed dummy hash, so neither the body nor the response time
reveals which usernames exist (measured: ≈ 55 ms both). Empty fields are a
`400`, and a database failure is a `500`, never "wrong password". Tokens are
verified strictly: only HS256 (no `alg: none`), `exp` required, no clock-skew
leeway, positive numeric subject. `JWT_SECRET` is required and must be at least
32 bytes (RFC 7518 §3.2); the service refuses to start otherwise. The
middleware takes the player only from the verified token (never from the body),
answers every failure with the same `401` plus `WWW-Authenticate: Bearer`, and
does not look the player up in the database. Passwords, hashes, tokens and the
secret are never logged.

**Errors → HTTP (Phase 06).** One function in `Application` maps the seven
domain error categories to status + code with `errors.Is`, so wrapping errors
with context never changes the status. For `400` and `409` the message is the
domain error's text (it tells the provider what to fix and never carries
secrets); every other category has a fixed message. Anything unrecognised is
`500 INTERNAL_ERROR` — a code the assessment does not define — logged with its
cause and never shown to the client. Request bodies are limited to 1 MiB, must
be exactly one JSON object, and unknown fields are ignored.

**Player information (Phase 07).** `GET /player` answers exactly the §8.2 shape
`{userId, balance, currency}`. The player comes only from the token. The
balance is read from the Wallet on every request and never cached
(`Cache-Control: no-store` too), so a Wallet failure is a `502` and a Wallet
that does not answer in time a `504` — never a zero or stale balance. Our
database is read first: a token whose player does not exist is a `404` before
any Wallet call, and if the Wallet reports a currency different from the
player's account the answer is a `502` (the two systems disagree; no currency
would make that balance correct). The read takes **no per-player lock**: it
changes nothing, and locking would make it wait behind a slow withdraw. A read
during an in-flight operation shows the Wallet's value at that moment, which is
still the truth. The query does not retry: the client already retries a balance
read once, so the worst case is about 2 × `WALLET_BALANCE_TIMEOUT` (≈ 20 s), under
the server's 30 s write timeout. Measured against the mock: answers took 0.5–15 s
and the Wallet dropped 3 of 11 balance attempts, so the retry matters in practice.

**Withdraw (Phase 08).** Invalid input (provider ID, amount ≤ 0, more than 2
decimals, currency, a currency that is not the player's) is a `400` before the
lock and before any Wallet call; a missing amount is reported as missing, not
treated as zero. Under the player's lock: idempotency lookup → balance read
(`oldBalance`, "as observed by our service", §7.4) → the `PENDING` transaction
with its reserved `betId` is committed → Wallet withdraw with our transaction
ID as `reference` → the outcome. Applied → `COMPLETED` and the `PLACED` bet in
one database transaction (`201`); if the Wallet applied it but its new balance
is unknown, `newBalance` is `oldBalance − amount`, exact because the balance
was read under the lock just before. Insufficient funds → `FAILED`, `422`;
another definitive rejection → `FAILED`, `502`; anything else stays `PENDING`
(`202`). One log line per withdraw with the transaction and provider IDs, the
bet ID, the amount and the outcome. Measured against the mock (10 withdraws):
6 × `201`, 3 × `202`, 1 × `502` (balance read failed twice); the three
`PENDING` ones had been applied by the Wallet, so resolving them (Phase 11) is
a normal path, not an edge case.

*Future improvements (authentication):* refresh tokens and revocation (today a
leaked token is valid until it expires — keep `JWT_TTL` short), rate limiting or
lockout of repeated failed logins, and key rotation (`kid` header with several
accepted secrets).

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
