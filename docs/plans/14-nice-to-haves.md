# 14 — Nice-to-haves (after delivery)

> **Status:** Point 1 is **built** (branch `refactor_code_clean_up`, released
> as `v1.0.1`). Points 2–4 are **documented as future improvements and not
> planned for this version**: each one is ready to be picked up, after its
> open questions are answered.

## Goal

Improve the delivered `v1.0.0` without changing what the API promises
(status codes, bodies, the reliability rules of `DESIGN.md`). Four
independent improvements, each one its own point of this plan and its own
series of commits:

1. **Readability refactors** — make `Actions` shorter and easier to read
   aloud, with no behaviour change.
2. **Swagger UI** — browse and try the API from a browser, on top of the
   existing `docs/openapi.yaml`.
3. **River job queue** — replace the pending resolver's periodic scan with a
   PostgreSQL job queue (candidate raised in Phase 11).
4. **OpenTelemetry + Grafana** — traces and metrics exported to a local
   Grafana stack, to see each request, each Wallet call and the `PENDING`
   backlog.

Order: 1 first, because 3 and 4 touch the same handlers and are easier to
review on top of the simpler code. 2, 3 and 4 do not depend on each other;
4 is easier after 3 (the River job results become one more metric).

## Requirements covered (assessment sections)

| Section | Point |
|---|---|
| 10.1 / 12 — readable, maintainable code | 1 |
| 10.2 — nice-to-haves (API docs, observability) | 2, 4 |
| 9.4 — pending resolution | 3 |
| 9.7 — observability without secrets | 4 |

---

## Point 1 — Readability refactors

### Goal

Remove the noise that is left in `Actions/withdraw.go`, `deposit.go` and
`cancel.go` after Phases 10b and 11: long parameter lists, the same lock
boilerplate three times, stringly-typed log outcomes and decimal arithmetic
spelled out by hand. **No behaviour change**: no HTTP response, log field or
database write changes, and the existing tests stay green; a test file is
only touched where a signature it calls changes (said per refactor).

### Candidates

Each row is independent. **Chosen by the developer: R1–R5.**

| # | Refactor | Today | After | Tests touched |
|---|---|---|---|---|
| R1 | **Validated request structs** per operation: `withdrawRequest{player, providerID, amount}`, `depositRequest{player, providerID, withdrawnID, amount, currency}`, `cancelRequest{player, providerID}`, built by `Execute` after parsing | `depositLocked` takes 7 parameters, deposit's `replayIfProcessed` 7, `replayAfterDuplicateInsert` 8; every call repeats `player.UserID, providerID, withdrawnID, amount, currency` | `depositLocked(ctx, uow, request)`, `replayIfProcessed(ctx, uow, request)`… The parsed, trusted values travel together; the `Command` stays the raw input from `Application` | None (unexported) |
| R2 | **`operation.runLocked`**: `func (o operation) runLocked(ctx, userID, fn func(ctx, uow) (TransactionResult, error)) (TransactionResult, error)` | The three `Execute` end with the same 6 lines: `var result`, `WithPlayerLock`, `var lockedErr error`, assign, return | `return handler.runLocked(ctx, player.UserID, func(ctx, uow) (TransactionResult, error) { return handler.depositLocked(ctx, uow, request) })` | None |
| R3 | **Typed log outcomes**: `type outcome string` with constants `outcomeCompleted`, `outcomeCompletedWithoutWallet`, `outcomePending`, `outcomePendingOverdue`, `outcomeBalanceUnknown`, `outcomeNotStored`, `outcomeRejected`, `outcomeInsufficientFunds` | 8 string literals spread over 4 files; `leavePending` compares `outcome == "pending"` — a typo compiles | The compiler checks every outcome; the list of possible values is in one place (also useful for Point 4, where outcomes become metric labels). Same strings in the log | None (log values unchanged) |
| R4 | **`Money.Add` / `Money.Sub`** in `Domain/money.go`, returning `(Money, error)` like `MoneyFromDecimal` | `domain.MoneyFromDecimal(pending.OldBalance.Decimal().Sub(pending.Amount.Decimal()))` in withdraw, deposit and `balanceAfterCompensation` | `pending.OldBalance.Sub(pending.Amount)` — reads like the rule it implements | New unit tests in `money_test.go` |
| R5 | **`replayOrResend`** helper per handler, wrapping the first block of each `xxxLocked`: look up the previous request; if `PENDING`, re-send it as a replay | The same 7-line `if result, found, err := …; found \|\| err != nil { if … PENDING { return asReplay(resendPending…) } … }` in the three handlers | `if result, handled, err := handler.replayOrResend(ctx, uow, request); handled { return result, err }` — the `xxxLocked` body starts with the new-request path | None |
### Not proposed / not chosen (and why)

| Idea | Why not |
|---|---|
| R6 — an `actions.Dependencies` struct for the six shared constructor parameters | Not chosen: it changes `Startup/main.go` and every test that builds a handler, for a list that is only written in a few places. |
| R7 — splitting the largest test files (`deposit_test.go` 1 335 lines, `cancel_test.go` 1 101, `withdraw_test.go` 1 000) by concern | Not chosen: it moves code without making any test easier to read. |
| Moving action-specific errors to `Actions/errors.go` | Already decided against in Phase 13 (`docs/decisions.md`, "Where errors live"). |
| A generic "money operation engine" / template method | Rejected in Phases 09 and 10b: too clever to explain live. |
| Refactoring `resolve_pending.go`'s `switch tx.Type` into an interface map | Point 3 (River) rewrites that file; refactor it there or not at all. |
| Controllers in `Application` (withdraw/deposit share ~15 lines) | They are short and read top to bottom; a shared helper would hide the request decoding of each endpoint. |

### Files to modify

| File | Refactors |
|---|---|
| `Actions/operation.go` | R2, R3 |
| `Actions/withdraw.go`, `deposit.go`, `cancel.go` | R1, R2, R3, R4, R5 |
| `Domain/money.go`, `money_test.go` | R4 |
| `ARCHITECTURE.md` | Actions section, if R1/R2/R5 change what it describes |

### Commit sequence (Point 1)

Working mode as in Phases 09–13: Claude writes each step and explains it
in the chat; the developer reviews it before it is committed. For every step: `gofmt -l .` empty, `go vet ./...`,
`go build ./...`, `go test ./...` green. Branch: `refactor_code_clean_up`
(the developer's choice; each later point gets its own branch).

1. `docs(plan): add phase 14 plan` — this file (Point 1 detailed).
2. `feat(domain): add Money Add and Sub` — R4 methods + table tests.
3. `refactor(actions): use Money arithmetic for derived balances` — R4 callers.
4. `refactor(actions): type the operation log outcomes` — R3.
5. `refactor(actions): run each operation under the lock with one helper` — R2.
6. `refactor(actions): pass validated requests instead of long parameter lists` — R1.
7. `refactor(actions): share the replay-or-resend step of each operation` — R5.
8. `docs(architecture): describe the refactored actions` — only if needed.

*Go concepts to explain in the chat:* methods on a value type returning
`(T, error)` (R4); a named string type with constants vs an `iota` enum, and
why a string type keeps the log unchanged (R3); passing a closure (`func`
literal) that captures variables, the Go equivalent of a C# lambda (R2).

### Test plan (Point 1)

- The existing unit tests are the safety net: no test of `withdraw`,
  `deposit`, `cancel`, `resolve_pending`, the controllers or the router is
  modified by R1–R5.
- New: `Money.Add` / `Money.Sub` tests (normal, result with > 2 decimals is
  impossible by construction, negative result allowed or not — see open
  question).
- `go test -count=1 -tags=integration ./Persistence/...` unaffected (no
  `Persistence` change) — run once at the end.

### Open questions (Point 1)

1. ~~Which candidates go in?~~ **Resolved:** R1–R5; R6 and R7 not chosen.
2. ~~`Money.Sub` with a negative result?~~ **Resolved:** `Add` and `Sub`
   delegate to `MoneyFromDecimal`, which decides what is valid (same
   behaviour as now).
3. ~~Working mode?~~ **Resolved:** Claude writes each step and the developer
   reviews it before it is committed (as in Phases 09–13).

### As built — deviations from this plan (Point 1)

| Planned | Built | Why |
|---|---|---|
| R3: `type outcome string` | `type logOutcome int` with `iota` and a `String()` method, in `Actions/log_outcome.go` | An untyped string literal converts silently to a named string type, so `"pendng"` would still compile; it cannot convert to an `int` type. Same shape as `walletOutcome`; the name avoids confusing the two. Log values unchanged (the tests that read the log pass untouched). |
| R1: `depositRequest` with a `currency` field | No `currency` field: the code uses `request.player.Currency` | `Execute` has already checked they are equal; two fields could contradict each other. |
| R5: `replayOrResend` makes the handlers shorter | It adds ~45 lines (one 12-line helper per handler) | Each handler has its own lookup and `resendPending`; sharing the helper would need function parameters or generics, rejected in Phases 09/10b. Kept for readability: each `xxxLocked` now starts with one `if … handled` and the re-send rule has a name. |
| — | Typo fixed in a `cancel.go` comment ("findWhatToRevertthen") | Found while editing; committed with R1. |

Commits: `docs(plan)` → `feat(domain)` R4 → `refactor(actions)` R4, R3, R2,
R1, R5 → `docs(architecture)`. No existing test was modified; `go test ./...`
green after every step. `resolve_pending.go` untouched (Point 3).

---

## Point 2 — Swagger UI *(documented, deferred: not in v1.0.x)*

### Goal

Try the API from a browser: open `http://localhost:8080/docs`, call login,
paste the token in *Authorize*, and call the other four endpoints with
*Try it out*. The spec stays the hand-written `docs/openapi.yaml`, linted in
CI; no endpoint changes.

### Design decisions (and alternatives rejected)

| Decision | Why | Rejected alternative |
|---|---|---|
| **Served by the API**: `GET /docs` (an HTML page) and `GET /docs/openapi.yaml` (the spec) | Same origin as the API, so *Try it out* needs no CORS; works with `go run` and with Docker | A `swaggerapi/swagger-ui` container in Compose (no Go change, but another origin → CORS headers on the API, a security change to defend; and nothing without Docker) |
| The spec is embedded with `//go:embed` from a tiny package next to it, `docs/openapi.go` (`package docs`, `var OpenAPI []byte`), imported by `Startup` | `go:embed` cannot reach parent folders (`../docs`), so the file must be embedded by a package in its own folder; the binary carries the spec and cannot drift from the commit | Copying the YAML into `Startup/` (two copies to keep in sync) |
| The HTML page loads `swagger-ui-dist` from a CDN allowed by AGENTS conventions (`cdn.jsdelivr.net` / `unpkg.com`) at an **exact pinned version**, with a Subresource Integrity hash | One small HTML file (~20 lines) instead of ~1.5 MB of vendored JavaScript; pinned + SRI = the browser refuses a changed file | Vendoring `swagger-ui-dist` into the repo (large diff nobody reviews); a floating `@latest` URL |
| **`DOCS_ENABLED`** env var, default `false`; `docker-compose.yml` and `.env.example` set it to `true` | Secure by default: a production deployment does not advertise its API unless asked; locally it is one line | Always on (exposes the contract in every environment); a build tag (needs two binaries) |
| `servers` in the spec becomes a relative URL (`/`) | *Try it out* then calls whatever host served the page (`localhost:8080`, another port, a container name) | Keeping `http://localhost:8080` (breaks when the port changes) |
| Routes outside `RequireAuth`, and not under `/api/v1` | Documentation is public when enabled; the token is entered in the UI, not needed to load it | — |
| Not generated from code | Decided in Phase 13: `swaggo/swag` adds a dependency and annotations on every handler for five endpoints | — |

Reverses a Phase 13 decision ("the spec is not served by the API"): it was
taken when the only reader was a reviewer on GitHub; a browsable UI is a new
use, and the flag keeps it off by default.

### Files to create / modify

| File | Change |
|---|---|
| `docs/openapi.go` | New: `package docs`, embeds `openapi.yaml` |
| `Application/docs.go`, `docs_test.go` | New: `DocsController` serving the HTML page and the spec (`Content-Type` `text/html` / `application/yaml`) |
| `Startup/config.go`, `config_test.go` | `DocsEnabled` from `DOCS_ENABLED` (bool, default `false`) |
| `Startup/router.go`, `router_test.go` | The two routes, only when enabled; `404` otherwise |
| `docs/openapi.yaml` | `servers: - url: /` |
| `docker-compose.yml`, `.env.example` | `DOCS_ENABLED=true` |
| `README.md`, `docs/try-it.md`, `ARCHITECTURE.md`, `AGENTS.md` | How to open the UI; the new variable; the folder table (`docs` package) |

### Commit sequence

1. `docs(plan): detail point 2 (Swagger UI)` — this section, approved.
2. `feat(startup): add the DOCS_ENABLED setting` — config + tests.
3. `feat(application): serve the OpenAPI spec and Swagger UI` — embed
   package, controller + tests (page contains the pinned script URL; spec
   bytes equal the file).
4. `feat(startup): route /docs when enabled` — router + tests (enabled →
   `200`, disabled → `404`).
5. `docs(docs): explain how to open Swagger UI` — README, try-it, compose,
   `.env.example`, architecture.

### Test plan

- Unit: controller and router tests above; config parsing (`true`, `false`,
  missing, invalid).
- CI: Redocly lint still passes with the relative `servers` URL.
- Manual: `docker compose up --build -d --wait`, open `/docs`, login →
  *Authorize* → withdraw → `201`.

### Open questions

1. Default of `DOCS_ENABLED`: `false` (recommended) or `true`?
2. CDN with pinned version + SRI (recommended) or vendored assets?

### Definition of done

`/docs` works from Compose and from `go run` with the flag on, is `404` with
it off; no change to the five endpoints; checks and CI green.

## Point 3 — River job queue replacing the pending resolver *(documented, deferred: not in v1.0.x)*

### Goal

Today a ticker in `Startup/resolver.go` runs `ResolvePendingCommandHandler`
every `PENDING_RESOLVER_INTERVAL`: it scans up to 100 `PENDING` rows and
re-sends the due ones, one after another. Replace the scan with a
**PostgreSQL job queue (`riverqueue/river`)**: each `PENDING` transaction
gets its own "resend" job, created in the same database transaction as the
row, so:

- a crash can never leave a `PENDING` row without its job (atomic, like the
  intent-first record itself);
- each transaction has its own schedule, retries and maximum attempts,
  instead of one global interval and batch;
- several API instances can run workers safely (River locks each job row,
  `FOR UPDATE SKIP LOCKED`), which the current ticker only tolerates thanks
  to the player lock.

The rules of `DESIGN.md` do not change: the database row is still the source
of truth, a re-send is still the identical request, under the player's lock,
at least `resendDelay` (10 s) after the last attempt, through the same
`resendPending` → `finish` path.

### Design decisions (and alternatives rejected)

| Decision | Why | Rejected alternative |
|---|---|---|
| **River** (`github.com/riverqueue/river` + `riverdriver/riverpgxv5`) | Uses the PostgreSQL we already have and our `pgx` pool; jobs are rows, so a job can be inserted inside our own DB transaction (`InsertTx`) | RabbitMQ / Redis queue (already judged worse in Phase 11: a second system, and an outbox table + relay to stay atomic with the `PENDING` row); keeping the ticker |
| **New port in `Domain`**: `PendingResendScheduler` with `Schedule(ctx, tx Transaction, at time.Time) error`, reachable as `repos.Resends` inside `UnitOfWork.Atomic` | `Actions` must not import River (dependency rule); exposing it through `Repositories` puts the job insert in the **same** DB transaction as the `PENDING` insert, which is the whole point | A scheduler called after the commit (a crash between the two loses the job); `Actions` importing River |
| Adapter in `Persistence` (`resend_scheduler.go`), using `river.Client.InsertTx` with the unit of work's `pgx.Tx` | `Persistence` already owns the transaction; River is a persistence detail | Adapter in `Shared` (would need the `pgx.Tx` passed through `Domain`) |
| Job args `{transaction_id, user_id}` only; `ScheduledAt = attempt start + resendDelay`; `UniqueOpts{ByArgs: true}` | Identifiers only (no amounts or secrets in the job table); the delay keeps the 10 s rule; uniqueness means a provider retry and the worker cannot queue two jobs for one transaction | Putting the whole transaction in the job (a second copy that can disagree with the row) |
| **Worker in `Startup`** (`resend_worker.go`), calling a new `ResolvePendingCommandHandler.ResolveOne(ctx, userID, transactionID)` | Only `Startup` may import both River and `Actions`; `ResolveOne` reuses today's per-transaction `resolve` under the player's lock | Worker in `Actions` (imports River) |
| Still `PENDING` after a re-send → the worker returns an error, River retries with backoff (a retry policy never shorter than `resendDelay`), `MaxAttempts` ≈ 25 | Retries and backoff become River's job; the overdue log line (`pending_overdue`) stays | Infinite attempts (a stuck job would retry forever) |
| **Keep a slow startup reconcile**: at start, any `PENDING` row without a job gets one (same `PendingTransactionFinder`) | Covers rows created before the upgrade and jobs discarded after `MaxAttempts`; the scan becomes a safety net, not the mechanism | Trusting jobs alone (an upgrade would strand existing `PENDING` rows) |
| River's tables created by **`rivermigrate`** at startup, right after goose | River ships its own versioned migrations; copying them into goose files means maintaining them by hand on every River upgrade | Hand-copied SQL in `Persistence/migrations` |
| `PENDING_RESOLVER_INTERVAL` removed; new `RESEND_MAX_ATTEMPTS` (optional) | The interval no longer exists | — |
| River UI: not included | A separate binary/container; the logs and Point 4's metrics cover the operator's needs | `riverui` container in Compose (possible later) |

### Files to create / modify

| File | Change |
|---|---|
| `go.mod` | `riverqueue/river`, `riverdriver/riverpgxv5` (reason: this point) |
| `Domain/ports.go` | `PendingResendScheduler`; `Repositories.Resends` |
| `Domain/domaintest/store.go` (+ test) | Fake scheduler recording scheduled jobs inside `FakeStore` |
| `Persistence/resend_scheduler.go` (+ integration test) | River `InsertTx` adapter; wired into the unit of work |
| `Persistence/migrate.go` | Run `rivermigrate` after goose |
| `Actions/withdraw.go`, `deposit.go`, `cancel.go`, `operation.go` | Schedule the resend job in the same `Atomic` as the `PENDING` insert and in `startResend` |
| `Actions/resolve_pending.go` (+ test) | `ResolveOne`; the batch `Execute` becomes the startup reconcile |
| `Startup/resend_worker.go` (+ test), `main.go`, `resolver.go`, `config.go` | River client + worker; start/stop with the server (graceful: `client.Stop`); ticker removed |
| `DESIGN.md`, `docs/decisions.md`, `ARCHITECTURE.md`, `README.md`, `.env.example` | PENDING resolution section, new variable, diagram |

### Commit sequence

1. `docs(plan): detail point 3 (River)` — this section, approved.
2. `build(persistence): add River and run its migrations` — dependency,
   `rivermigrate` after goose, integration test that River tables exist.
3. `feat(domain): add the pending resend scheduler port` — port + fake.
4. `feat(persistence): schedule resend jobs in the unit of work` — adapter +
   integration test (rollback of the unit of work → no job).
5. `feat(actions): schedule a resend job with every pending transaction` —
   unit tests with the fake: `PENDING` → one job at `+10 s`; `COMPLETED` /
   `FAILED` → none; duplicate → still one.
6. `feat(actions): resolve one pending transaction` — `ResolveOne` + tests.
7. `feat(startup): run the resend worker instead of the ticker` — worker,
   lifecycle, startup reconcile, config.
8. `docs(design): pending resolution with a job queue`.

### Test plan

- Unit (no Docker): scheduling rules with the fake scheduler; `ResolveOne`
  outcomes (completed, failed, still pending, not due, no longer pending).
- Integration (`-tags=integration`): job inserted iff the transaction commits;
  River tables migrated.
- Manual: `docker compose up`, force a Wallet timeout, watch the job retry in
  the logs until `COMPLETED`; stop the API mid-flight and check the job is
  still there at restart.

### Open questions

1. `MaxAttempts` and the retry curve (how long before a transaction is left
   to the operator).
2. Keep the startup reconcile permanently, or only for the upgrade?
3. Add River UI to Compose?

### Definition of done

No `PENDING` row without a job (checked by an integration test and by the
reconcile log); same `DESIGN.md` guarantees; ticker and
`PENDING_RESOLVER_INTERVAL` gone; checks, integration tests and CI green.

## Point 4 — OpenTelemetry + Grafana *(documented, deferred: not in v1.0.x)*

### Goal

See what the service is doing without reading JSON logs: one trace per
request (HTTP → lock → each SQL statement → each Wallet call), metrics for
the things an operator watches (error rate, Wallet latency and outcomes,
`PENDING` backlog), and the existing log lines linked to their trace — all in
a local Grafana started by Compose.

### Design decisions (and alternatives rejected)

| Decision | Why | Rejected alternative |
|---|---|---|
| **OpenTelemetry Go SDK**, exported with **OTLP** (`go.opentelemetry.io/otel`, `sdk`, `otlptrace`/`otlpmetric` exporters) | Vendor-neutral: the same code sends to Grafana, Jaeger, Datadog… by changing an endpoint | Prometheus client only (metrics, no traces); a vendor SDK |
| Setup in a new `Shared/telemetry` package, called from `Startup`; configured by the **standard env vars** (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`) | No custom config to document; **off when no endpoint is set** (no-op providers), so `go test`, `go run` and CI need nothing | A custom `TELEMETRY_*` set of variables |
| **HTTP server**: `otelhttp.NewHandler` around the router in `Startup` | One wrapper gives a span and the standard HTTP metrics (duration, status) per route | Hand-written middleware |
| **Wallet client**: `otelhttp.NewTransport` as the `http.Client` transport in `Shared/wallet` | A client span per Wallet call (with status and duration); `traceparent` is sent but the mock ignores it | Spans written by hand in the client |
| **PostgreSQL**: `github.com/exaring/otelpgx` as the pgx tracer in `Persistence.Connect` | A span per query, including the advisory lock wait — the most useful number when operations of one player queue up | No DB spans |
| **`Actions` stays free of OpenTelemetry**: a new port `domain.OperationMetrics` with `Record(ctx, operation, outcome string)`, called from `operation.log`; adapter in `Shared/telemetry` (a counter), no-op fake in tests | The dependency rule; and `logOutcome` (Point 1, R3) already lists every outcome, so the metric labels are exactly the log values | Importing the SDK in `Actions`; parsing log lines into metrics |
| **Metrics** (names to confirm): `http.server.request.duration` (from `otelhttp`), `wallet.client.request.duration` by operation and outcome (applied / not applied / unknown), `game.operations` by operation and `logOutcome`, `game.pending.count` and `game.pending.oldest_age` (observable gauges read through `PendingTransactionFinder`) | Answers: are we failing? is the Wallet slow? are transactions stuck `PENDING`? | Per-player or per-transaction metrics |
| **Labels never carry identifiers or money**: no user ID, transaction ID, amount, token, API key; IDs go only on spans as attributes (as they already go in logs) | §9.7 (no secrets) and cardinality: one label value per user would explode the metric store | — |
| **Logs**: keep `slog` to stdout; add `trace_id` / `span_id` to each line with a small `slog.Handler` wrapper | Grafana links a trace to its log lines; no change to what is logged | Sending logs via the OTel logs bridge (newer API, a second log path to explain) |
| **Local stack**: one `grafana/otel-lgtm` container in Compose (OTel Collector + Tempo + Prometheus + Loki + Grafana), under a Compose **profile** `observability`, Grafana on `127.0.0.1:3000` | One image for a demo; the profile keeps `docker compose up` as light as today (`docker compose --profile observability up` to add it) | Five separate services (more YAML than the API itself); always on |
| A **provisioned dashboard** JSON (`deploy/grafana/dashboards/game-api.json`): request rate/errors/latency, Wallet outcomes, `PENDING` backlog, operations by outcome | Opens ready to use; versioned with the code | Building it by hand after each `down -v` |

### Files to create / modify

| File | Change |
|---|---|
| `go.mod` | OTel SDK, OTLP exporters, `otelhttp`, `otelpgx` (reason: this point) |
| `Shared/telemetry/` (+ tests) | Provider setup/shutdown, `OperationMetrics` adapter, `slog` trace handler |
| `Domain/ports.go`, `Domain/domaintest/` | `OperationMetrics` port + no-op/recording fake |
| `Actions/operation.go` (+ tests) | `log` also records the outcome metric |
| `Shared/wallet/client.go` | `otelhttp` transport |
| `Persistence/connect.go` | `otelpgx` tracer |
| `Startup/main.go`, `router.go` | Init/shutdown telemetry (flush on graceful stop), wrap the router, pending gauges |
| `docker-compose.yml`, `deploy/grafana/` | `otel-lgtm` service under a profile, dashboard provisioning; `OTEL_*` env for `api` |
| `README.md`, `DESIGN.md`, `ARCHITECTURE.md`, `AGENTS.md`, `.env.example` | How to start and open Grafana; what is measured and what is never recorded |

### Commit sequence

1. `docs(plan): detail point 4 (OpenTelemetry)` — this section, approved.
2. `feat(shared): set up OpenTelemetry, off without an endpoint` —
   `Shared/telemetry`, init/shutdown in `Startup`.
3. `feat(startup): trace incoming HTTP requests`.
4. `feat(wallet): trace Wallet calls`.
5. `feat(persistence): trace database queries`.
6. `feat(actions): count operations by outcome` — port, fake, `operation.log`.
7. `feat(startup): expose the pending backlog as gauges`.
8. `feat(shared): add trace ids to log lines`.
9. `chore(docker): add the Grafana LGTM stack and dashboard`.
10. `docs(docs): document telemetry`.

### Test plan

- Unit: with telemetry off, every existing test passes unchanged; the
  recording fake asserts one `Record` per operation with the right outcome;
  the `slog` handler adds `trace_id` only when a span exists.
- Security check: a test that the recorded label sets never contain a user
  ID, amount or token (only operation and outcome).
- Manual: `docker compose --profile observability up --build -d --wait`, run
  the `docs/try-it.md` walkthrough, open Grafana → a withdraw trace shows
  HTTP → lock query → INSERT → Wallet POST → UPDATE; force a timeout and see
  `game.pending.count` rise and fall.

### Open questions

1. Compose profile (recommended) or always-on observability stack?
2. Sampling: everything (fine locally) or a ratio, configurable with the
   standard `OTEL_TRACES_SAMPLER` variables?
3. Should the metric names follow a project prefix (`game.`) or the
   OpenTelemetry semantic conventions only?

### Definition of done

With the profile on, Grafana shows traces, metrics and linked logs for the
walkthrough; with no endpoint configured, the service behaves and tests run
exactly as before; no identifier, amount or secret in any metric label;
checks and CI green.

---

## Definition of done (whole phase)

- Each point approved here before its code starts; each one merged
  separately with `gofmt`, `go vet`, `go build`, `go test ./...` green and
  CI green.
- No change to the API contract of `README.md` / `docs/openapi.yaml`
  unless a point says so explicitly.
- `README.md`, `DESIGN.md`, `ARCHITECTURE.md`, `AGENTS.md` and the general
  plan updated by the point that changes them.
- The developer can explain every new line and every new dependency.
