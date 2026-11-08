# Design decisions

> Skeleton. Each section is filled in by the phase indicated. Required content
> per section 11 of the assessment. Target length: 1–2 pages.

## 1. Behaviour under failure

| Situation | Behaviour | Why |
|---|---|---|
| Wallet returns an error **before** applying the operation | Only a *definitive* rejection counts as "not applied": `400 InsufficientBalance` → `FAILED`, `422 INSUFFICIENT_FUNDS`; any other `400` or a `401` → `FAILED`, `502 WALLET_ERROR`, logged at `error` (it means a bug on our side or a wrong `WALLET_API_KEY`). The client classifies by HTTP status **and** body `code`; anything it does not recognise is *unknown*, never `FAILED`. A withdraw keeps its transaction as `FAILED` (for audit) and creates no bet; a deposit keeps it as `FAILED` and leaves its bet `PLACED`, so the bet can still be settled; a cancel keeps it as `FAILED` and leaves the original and its bet unchanged (§8.5: "if funds are insufficient, nothing changes"). | Insufficient funds and validation errors are both `400`; only `code` tells them apart (findings Q3–Q5). A wrong `FAILED` cannot be undone, a `PENDING` can be resolved. |
| Wallet returns an error **after** applying it | A `500` (and any response we never receive) is *unknown*: the transaction stays `PENDING` and the API answers `202`. A withdraw answers `202` with `status: PENDING` and `newBalance`/`betStatus` `null`; a deposit answers `202` with `newBalance` `null` and `betStatus: PLACED` (the bet exists and is not settled until the deposit is confirmed); a cancel answers `202` with the original still `COMPLETED` and the bet unchanged. It is resolved by re-sending the identical request (§2): `409 DuplicatedTransaction` means it had been applied. | In the Phase 01 spike every Wallet `500` had been applied (36 of 36), and dropped connections were sometimes applied too. Marking them `FAILED` would contradict what the Wallet actually did. See [`docs/wallet-findings.md`](docs/wallet-findings.md) Q6, Q8. |
| Wallet times out | **Operations:** each call is bounded by `WALLET_OPERATION_TIMEOUT` (6 s). No answer in time is *unknown* → `PENDING`, `202`. The client never re-sends an operation by itself. **Balance reads:** `WALLET_BALANCE_TIMEOUT` (10 s) per attempt, one retry after 200 ms on a timeout, a network error or a `5xx`; the read then fails with `504 WALLET_TIMEOUT` or `502 WALLET_ERROR`. Worst case ≈ 20 s, still bounded. A withdraw or deposit reads the balance first (its `oldBalance`): if that read fails or times out, nothing has been sent, so it is a `502` (never `504`, which §7.3 reserves for reads) and nothing is stored. Their worst case is ≈ 20 s + 6 s ≈ 27 s, under the server's 30 s write timeout. | The Wallet never answers a POST after ~5 s but a balance read can take ~15 s (findings Q7); one shared 3 s timeout would have turned a third of successful operations into `PENDING`. A read is safe to repeat; an operation re-sent immediately could race the original (Q8), so re-sending waits 10 s after the last attempt (§2). |
| Process stops between the Wallet call and the DB write | The transaction was committed as `PENDING` (with its `betId` and reference) **before** the Wallet call, so it is not lost; the player's lock is released by PostgreSQL when the connection closes. The outcome is written with a context that is not cancelled with the request (bounded by 5 s), so a provider hanging up does not leave an applied withdraw or deposit unrecorded; if the write itself fails, the transaction stays `PENDING` and the API answers `202`. The resolver runs once at startup, so whatever a crash left `PENDING` is re-sent straight away (§2). | Intent-first (§9.1): the record of what we sent must exist before we send it, or a crash would leave a Wallet movement nobody knows about. |
| Provider retries / sends the same request concurrently | Operations of one player are serialised by a lock, and the idempotency lookup runs inside it, before any Wallet call: a retry sees the first request's transaction. Same provider ID and payload → the stored result (`200`; for a `FAILED` one its `422`/`502`, §3), without calling the Wallet, except a `PENDING` one whose last attempt is at least 10 s old, which is re-sent (§2); another amount → `409`. A cancel's key is the cancelled transaction, so a repeated cancel returns the first one. The unique indexes on `provider_transaction_id`, on "one active deposit per bet" and on "one cancel per transaction" are the safety net if two requests ever race past the lock (e.g. two API instances): the insert fails and the lookup runs again. Tested with 10 simultaneous identical requests → one Wallet call (withdraw, deposit and cancel), and with 10 simultaneous deposits on one bet → one Wallet call, nine `409`. | §9.2 asks for no duplicate effects even for simultaneous duplicates; a check in Go alone has a race window, a unique index does not. |
| Wallet restarts and loses its state | Not detected. After a restart the Wallet is back at its seeded balances and has forgotten which requests it applied, so re-sending a `PENDING` request would apply it again. Accepted risk, and the resolver (§2) makes it concrete: a transaction left `PENDING` across a Wallet restart is applied by the restarted Wallet when it is re-sent. No mitigation is possible from our side. | The Wallet keeps everything in memory and has no endpoint to query past transactions (findings Q1, Q10). In this setup it only restarts on purpose, and the documented reset (`docker compose down -v`) also wipes our database. Stopping the whole Compose stack without `-v` does restart it while keeping our data; the README warns about it and recommends `docker compose restart api`. |

## 2. How a PENDING transaction gets resolved

**By re-sending the identical Wallet request.** Everything needed was stored
before the first call (intent-first): user, currency, amount, `betId`, and our
transaction ID as `reference`. The Wallet de-duplicates by that reference
(findings Q8), so a re-send is never applied twice and its answer tells us
what happened: `409 DuplicatedTransaction` → it had been applied →
`COMPLETED`; `2xx` → it had not, and is applied now → `COMPLETED`; a
definitive rejection → `FAILED` (with its reason, §3); anything else → still
`PENDING`. The outcome goes through the same code as a first attempt
(`finish`), so the rules (§1) are written once. A cancel re-sends the same
compensation (credit back for a withdraw, debit back for a deposit): the
decision taken when it was accepted is repeated, not decided again.

**Two triggers, one path.** (1) The provider **retries** the same request:
instead of a plain `202`, the retry re-sends and answers the final result
(`200`, `422`/`502`, or `202` if still unknown). (2) A **background resolver**
started by `Startup` scans the `PENDING` transactions of every player once at
startup and then every `PENDING_RESOLVER_INTERVAL` (10 s), oldest first, 100
per run, so a transaction whose provider never retries does not stay
`PENDING` forever (e.g. a withdraw that took the money but has no bet). Both
take the player's lock and re-read the transaction before re-sending, so they
never re-send it twice; tested with a retry and the resolver at the same time,
and with 10 simultaneous retries → one re-send.

**Never while the last attempt may still be running.** A re-send that reached
the Wallet while the previous attempt was still being processed could be
applied twice (Q8): the Wallet never answers a POST after ~5 s but finishes
slow ones at ~5.7 s, and our timeout is 6 s. So a transaction is re-sent only
if its last attempt **started at least 10 s ago**. The start of the last
attempt is the transaction's `updated_at`, written and committed *before* each
re-send, so the rule holds across triggers, instances and crashes. Earlier,
a retry gets `202` without a Wallet call.

**`newBalance` of a resolved transaction:** the balance the Wallet reports on
a `2xx` (it was applied now, so this is the true balance after it, even if
other operations ran meanwhile); `oldBalance ± amount` on a `409`, which
reports no balance (the balance right after the first attempt, when the
Wallet most likely applied it).

**No cap on re-sends.** While the Wallet keeps its memory a re-send is safe,
and giving up would leave the transaction wrong forever. A transaction still
unknown 5 minutes after it was created is logged at `error` level
(`pending_overdue`) on every new attempt: that is the operator's signal (e.g.
the Wallet keeps answering `500`). The one case a re-send cannot handle is a
Wallet restart (§1, last row).

*Measured against the mock:* 12 withdraws → 3 `202`, all three resolved by the
resolver within ~20 s (the Wallet answered `409`: they had been applied); the
API killed right after a `202` → the transaction was resolved by the run at
the next startup.

*Rejected:* marking old `PENDING` transactions `FAILED` (most of them were
applied); comparing balances (meaningless once other operations ran, Q1);
re-sending immediately on a retry (the race of Q8); only one trigger (a retry
alone leaves silent providers `PENDING`; the resolver alone makes a retry wait
up to an interval for an answer it could get now).

## 3. Retry of a request whose previous attempt ended FAILED

**The retry gets the same error as the first attempt, without calling the
Wallet:** `422 INSUFFICIENT_FUNDS` or `502 WALLET_ERROR`. A `FAILED`
transaction stores why (`failure_reason`, migration `00006`), so the answer
can be repeated: same key, same result (§7.1). The provider transaction ID
stays used: a `FAILED` attempt was a definitive answer, and a provider that
wants to try again sends a new request with a new ID. Rows that failed before
the reason was stored answer `409 CONFLICT` ("previous attempt failed"), the
policy from Phase 08 to 10. *Rejected:* keeping that `409` for every retry (a
different answer than the first time); allowing a new attempt with the same
ID (it would need the unique indexes relaxed and several cancels per
transaction, and a withdraw retried later could succeed against a game state
that has moved on).

## 4. Zero-amount deposits

**Not forwarded to the Wallet.** A deposit of `0` settles the bet as `LOST` and
moves no money, so a Wallet call could only add risk: the mock accepts `0`, but
like any call it can fail, time out or answer `500`, which would leave a lost
bet `PENDING` for nothing. The balance is still read under the player's lock,
because `oldBalance` must be a balance we observed (§7.4); then one database
transaction stores the deposit as `COMPLETED` with `newBalance = oldBalance`
and the bet as `LOST` (`201`). There is no `PENDING` step because there is no
Wallet call to keep track of. If the balance read fails, the answer is `502`
and nothing is stored, so the provider can retry. Cancelling a zero deposit
moves no money either (§8.5), so both sides stay symmetric. *Rejected:*
forwarding it (one code path, but a `LOST` could end `PENDING` and need
resolution for a movement of 0).

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
In Docker (Phase 12) the binary is the container's main process (exec-form
`ENTRYPOINT`), so `docker compose stop` delivers `SIGTERM` to it directly, and
the `api` service has `stop_grace_period: 15s` — more than `SHUTDOWN_TIMEOUT` —
so Docker does not kill it while requests are still finishing.

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
`PENDING` ones had been applied by the Wallet, so resolving them (§2) is
a normal path, not an edge case.

**Deposit (Phase 09).** Same path as the withdraw, plus the rules of §8.4.
Before the lock: both provider IDs, the currency and "the currency is the
player's" (`400`); a missing amount is a `400`, never `0`, which would settle the
bet as `LOST`. Under the lock: idempotency lookup → the referenced withdraw,
looked up **among the player's own transactions** (unknown, another player's or
not a withdraw → `404`; `PENDING` or `FAILED` → `409`; a currency other than the
deposit's → `400`) and its bet (already `WON`, `LOST` or `CANCELLED` → `409`) →
balance read → `PENDING` deposit committed → Wallet deposit → the outcome:
applied → `COMPLETED` and the bet `WON` in one database transaction. **One
active settlement per bet** is a partial unique index (a `PENDING` or
`COMPLETED` deposit per bet): while a deposit is `PENDING` the bet is still
`PLACED`, so only the index sees that a second deposit (another provider ID) is
not allowed (`409`); a `FAILED` or cancelled deposit does not count. A deposit
is a replay only if the stored transaction is a deposit with the same amount,
currency **and bet** (every withdraw has its own bet, so "same bet" is "same
referenced withdraw"); a withdraw and a deposit never replay each other's
provider ID. When an insert is rejected by a unique index although the lookup
found nothing, the lookup runs again for **both** indexes (provider ID and
active settlement): PostgreSQL does not say which one it reports when a row
breaks the two, and an identical request must still end as a replay.

**Cancel (Phase 10).** The Wallet has no rollback, so a cancel is a
compensating operation with the same `betId` and the cancel's own ID as
`reference`: a withdraw is credited back with a Wallet deposit, a deposit is
debited back with a Wallet withdraw. It follows the same path as the other
operations: under the player's lock, the cancel of the original is looked up
first (the idempotency key, §7.1: `COMPLETED` → `200`, `PENDING` → re-sent if due (§2),
`FAILED` → its `422`/`502` (§3)); this must come before the §8.5 table, because while a
cancel is `PENDING` or `FAILED` its original is still `COMPLETED` and the table
alone would accept a second one. Then `DecideCancel` → balance read → `PENDING`
cancel committed → Wallet → outcome: applied → the cancel `COMPLETED`, the
original `CANCELLED` and the bet `CANCELLED` (withdraw) or back to `PLACED`
(deposit), in one database transaction. Insufficient funds on a debit back →
`422`, only the `FAILED` cancel is stored. Cancelling a **zero-amount deposit**
is not sent to the Wallet, symmetric with §4: the balance is read for
`oldBalance` and everything is stored `COMPLETED` at once. Two cross-checks
close gaps that bet statuses alone cannot see, both `409`: a withdraw cannot
be cancelled while its bet has a `PENDING` deposit (the Wallet may have paid
it; giving the stake back too would pay twice), and a deposit is not accepted
while the withdraw it settles has a cancel in progress (the stake may already
be back). A cancel that arrives before its withdraw (§10.2) is `404`.

*Future improvements (authentication):* refresh tokens and revocation (today a
leaked token is valid until it expires — keep `JWT_TTL` short), rate limiting or
lockout of repeated failed logins, and key rotation (`kid` header with several
accepted secrets).

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
