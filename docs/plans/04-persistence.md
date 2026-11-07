# 04 — Persistence

## Goal

Give the service a **PostgreSQL store whose constraints carry part of the
correctness**, and the ports the use cases of Phases 06–11 need to use it. At the
end of the phase:

- the schema (players, bets, transactions) exists as embedded SQL migrations that
  run at startup;
- the four players of the Wallet mock are seeded with bcrypt password hashes;
- idempotency and "one settlement per bet" are guaranteed by **unique indexes**,
  not only by checks in Go code (§9.2);
- operations of the same player can be **serialised** across requests (§9.6) with
  a lock that survives the intent-first commit (persist `PENDING`, *then* call the
  Wallet);
- repositories for players, bets and transactions are implemented and covered by
  integration tests against a real PostgreSQL.

No endpoint changes in this phase: the first consumer is login (Phase 06).

## Requirements covered (assessment sections)

- **5.2** — IDs above 32 bits (`BIGINT`), very large and fractional balances
  (`NUMERIC(20,2)`, read back exactly).
- **8.1** — seeded players with bcrypt hashes; credentials documented.
- **9.1** — the `PENDING` record is committed before the Wallet call (the lock
  design below makes this possible while still serialising the player).
- **9.2** — no duplicate effects: unique indexes for idempotency keys, single
  active settlement per bet, single cancel per transaction.
- **9.6** — per-player serialisation primitive.
- **10.1** — repositories behind interfaces owned by `Domain`; parameterised SQL.

## Design decisions (and alternatives rejected)

| Decision | Why | Rejected alternative |
|---|---|---|
| **`github.com/jackc/pgx/v5`** (+ `pgxpool`), plain SQL with `$1, $2…` | The standard PostgreSQL driver for Go; exposes PostgreSQL error codes (needed to map unique violations) and advisory locks naturally. Explicit SQL is easy to defend. *(Dependency reason, as AGENTS.md requires.)* | `database/sql` + `lib/pq` (maintenance mode); an ORM like `gorm` (magic, harder to explain); `sqlc` (code generation: one more tool to learn). |
| **`github.com/pressly/goose/v3`** for migrations, SQL files embedded with `//go:embed`, run at startup through `goose.NewProvider` | Well-known, SQL-only migrations; embedding means the binary carries its schema (no files to copy in Docker). `NewProvider` avoids goose's package-level globals (AGENTS.md: no globals). *(Dependency reason.)* | A hand-written migrator (~50 lines of our code to test and defend); running SQL from `docker-entrypoint-initdb.d` (only on the first start of an empty volume, never upgrades). See open question 4. |
| **Seed is a migration** (`00002_seed_players.sql`) with **precomputed bcrypt hashes** (cost 10) | Same mechanism, same order, every environment. The service never hashes passwords (Phase 03: `PasswordHasher` only verifies), so the hashes are generated once with a throwaway program and pasted in. | Seeding from Go code at startup (needs bcrypt in this phase and plaintext passwords in the binary). |
| Money columns are `NUMERIC(20,2)`; Go ↔ DB conversion goes **through text**: write `money.String()`, read `amount::text` → `domain.ParseMoney` | Exact in both directions with no extra dependency, and every value read from the DB passes the `Money` rules again (a corrupted value surfaces as an error, not as a wrong balance). | `pgx-shopspring-decimal` type registration (another dependency, invisible conversion); `float64` scanning (forbidden). |
| IDs: `wallet_user_id BIGINT`, `bets.id BIGINT` from the sequence `bet_id_seq` (starts at 1), `transactions.id UUID` | §5.2 / §7; Phase 03 open question 4 decided the sequence. The bet ID is reserved (`nextval`) **before** the bet row exists, because the `PENDING` withdraw must already store the `betId` it sends to the Wallet. | `bets.id BIGSERIAL` (the ID only exists after the bet row is inserted, too late). |
| `transactions.bet_id` has **no foreign key** to `bets` | A `PENDING` withdraw points to a bet that does not exist yet (Phase 03: a bet is created when its withdraw completes). | A FK (would force creating a bet in a status the spec does not define). |
| Idempotency index: **`UNIQUE (wallet_user_id, provider_transaction_id)` for `WITHDRAW`/`DEPOSIT`** (partial index) | §7.1. Scoped per player so one player's key can never collide with — or reveal anything about — another player's transaction. `CANCEL` rows are excluded because they copy the original's provider ID (Phase 03, `NewCancel`). See open question 3. | Global uniqueness (a collision between two players would answer 409 or, worse, replay someone else's transaction). |
| **Single active settlement:** `UNIQUE (bet_id)` for `DEPOSIT` rows with status `PENDING` or `COMPLETED` | §8.4: a bet is settled once. A cancelled (`CANCELLED`) or `FAILED` deposit frees the slot, so the bet can be settled again after its deposit is cancelled. | Checking only in Go (two concurrent deposits from two instances would both pass). |
| **Single cancel per transaction:** `UNIQUE (cancelled_transaction_id)` for `CANCEL` rows | §8.5 / §7.1: cancelling the same transaction twice returns the first cancel. Strict for now, including `FAILED` cancels; Phase 11 (FAILED-retry policy) may relax it with a new migration. | Leaving duplicates to Go code. |
| `CHECK` constraints mirror the domain: valid `type`/`status`/`currency` values, `amount >= 0`, withdraw `amount > 0`, `cancelled_transaction_id` present **iff** `type = 'CANCEL'` | Defence in depth: a bug in Go cannot store a state the domain forbids. | No checks; or a `new_balance IS NOT NULL` ⇔ `COMPLETED` check (left out: Phase 05 may complete a transaction after a `409 DuplicatedTransaction` without knowing the new balance — Phase 03 open question 5). |
| **Per-player lock = PostgreSQL session advisory lock** (`pg_advisory_lock(wallet_user_id)`) held on **one dedicated connection**, and **every DB access of that operation runs on that same connection** | The lock must outlive the commit of the `PENDING` row (intent-first), so a transaction-scoped lock (`FOR UPDATE`, `pg_advisory_xact_lock`) cannot be used. Using one connection for lock + work avoids pool starvation (N operations each holding a lock connection while waiting for a second one). If the process dies, PostgreSQL drops the connection and releases the lock. Works with several API instances. | `SELECT … FOR UPDATE` on the player row (would need a DB transaction open across the Wallet call, contradicting intent-first); an in-process `map[UserID]*sync.Mutex` (simpler, but only correct with a single instance and never shrinks). |
| Ports (in `Domain/ports.go`): **`PlayerLocker.WithPlayerLock(ctx, userID, fn)`**, whose `fn` receives a **`UnitOfWork`** with **`Atomic(ctx, fn)`**, whose `fn` receives **`Repositories{Transactions, Bets}`** | Callback style guarantees the lock is released and the DB transaction is committed/rolled back on every path (≈ `using` + `TransactionScope`). Use cases see exactly two levels: "while I own this player" and "these writes together". Sketch below. | Returning an `unlock func()` (easy to forget); hiding the DB transaction inside `ctx` (magic); exposing `pgx.Tx` to `Actions` (breaks the dependency rule). |
| Repository reads are **scoped by `UserID`** (`FindByID(ctx, userID, id)`) | AGENTS.md: a player only sees their own bets/transactions, otherwise 404. Enforcing it in the `WHERE` clause means a use case cannot forget it. | Loading by ID and comparing the owner in Go. |
| Unique violations (`SQLSTATE 23505`) are mapped **by constraint name** to domain errors | Use cases keep working with domain errors only (table below). | Returning raw `pgconn.PgError` (leaks the DB into `Actions`). |
| Repositories run against a small **`querier` interface** (`Exec`, `Query`, `QueryRow`) satisfied by both `*pgxpool.Conn` and `pgx.Tx` | One implementation works inside and outside a DB transaction. A good example of implicit interfaces. | Two copies of each query. |
| `DATABASE_URL` becomes **required**; the service connects, pings and migrates at startup and fails fast otherwise | A service that starts without its database would fail on every request later. Pool size uses pgx defaults, overridable with `pool_max_conns=` in the URL. | A default URL with a password in the code. |
| `/health` stays **liveness only** | Phase 12 decides the Compose healthcheck; a readiness check can be added then. | Pinging the DB on every health probe now. |
| **Integration tests**: build tag `integration`, database from `TEST_DATABASE_URL`, PostgreSQL started with one `docker run` command; the test **fails** (not skips) if the variable is missing | `go test ./...` stays Docker-free (AGENTS.md). Running with the tag is an explicit opt-in, so a silent skip would hide that nothing ran. | `testcontainers-go` (heavy dependency tree, Docker API from tests); skipping when unset. See open question 5. |
| **No in-memory fakes in `Domain/domaintest` yet** | Same rule as Phase 03: fakes arrive with their first consumer (login fake player repo in 06, lock/unit-of-work/repos fakes in 08). | Writing them now, before their shape is exercised by a use case. |
| Payload hash column and a `PENDING` index are **not** created now | Their content/need is decided in Phases 08 and 11, each adding its own migration. | Guessing them now. |

### Ports sketch (`Domain/ports.go`)

```go
type BetRepository interface {
    NextID(ctx context.Context) (BetID, error)                        // reserves a bet ID (sequence)
    Insert(ctx context.Context, bet Bet) error
    FindByID(ctx context.Context, userID UserID, id BetID) (Bet, error) // ErrBetNotFound
    Update(ctx context.Context, bet Bet) error                         // status, updated_at
}

type TransactionRepository interface {
    Insert(ctx context.Context, tx Transaction) error                  // unique violations → table below
    FindByID(ctx context.Context, userID UserID, id TransactionID) (Transaction, error)
    // WITHDRAW/DEPOSIT only (CANCEL rows share the original's provider ID).
    FindByProviderTransactionID(ctx context.Context, userID UserID, id ProviderTransactionID) (Transaction, error)
    FindCancelOf(ctx context.Context, userID UserID, originalID TransactionID) (Transaction, error)
    Update(ctx context.Context, tx Transaction) error                  // status, new_balance, updated_at
}
// All Find… return ErrTransactionNotFound when there is no match.

type Repositories struct {
    Transactions TransactionRepository
    Bets         BetRepository
}

type UnitOfWork interface {
    // Atomic runs fn in one DB transaction: committed if fn returns nil,
    // rolled back otherwise.
    Atomic(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
}

type PlayerLocker interface {
    // WithPlayerLock runs fn while no other operation of userID can run.
    WithPlayerLock(ctx context.Context, userID UserID, fn func(ctx context.Context, uow UnitOfWork) error) error
}
```

How Phase 08 will use it (illustration only):

```go
locker.WithPlayerLock(ctx, userID, func(ctx context.Context, uow UnitOfWork) error {
    uow.Atomic(ctx, …) // idempotency lookup, NextID, Insert PENDING  → commit #1
    wallet.Withdraw(…)  // outside any DB transaction, still under the lock
    uow.Atomic(ctx, …) // Update transaction, Insert bet               → commit #2
})
```

Later phases may add repository methods (e.g. listing `PENDING` in Phase 11);
each one comes with its integration test.

### Schema (`00001_schema.sql`)

| Table | Columns | Constraints |
|---|---|---|
| `players` | `wallet_user_id BIGINT PK`, `username TEXT NOT NULL`, `password_hash TEXT NOT NULL`, `currency TEXT NOT NULL`, `created_at TIMESTAMPTZ` | `UNIQUE(username)`, currency check |
| `bets` | `id BIGINT PK`, `wallet_user_id BIGINT NOT NULL → players`, `currency`, `stake NUMERIC(20,2)`, `status`, `created_at`, `updated_at` | status/currency checks, `stake > 0` |
| `transactions` | `id UUID PK`, `type`, `status`, `wallet_user_id → players`, `bet_id BIGINT NOT NULL`, `provider_transaction_id TEXT NOT NULL`, `cancelled_transaction_id UUID NULL → transactions`, `amount NUMERIC(20,2)`, `currency`, `old_balance NUMERIC(20,2) NOT NULL`, `new_balance NUMERIC(20,2) NULL`, `created_at`, `updated_at` | checks above + the three partial unique indexes |
| `bet_id_seq` | sequence | — |

### Unique violations → domain errors

| Index | Domain error | Meaning for the use case |
|---|---|---|
| `transactions_idempotency_key` | `ErrTransactionExists` *(new, wraps `ErrConflict`)* | Another request with this key won; re-read it (replay or 409). |
| `transactions_active_settlement` | `ErrBetAlreadySettled` | The bet already has a pending/completed deposit (409). |
| `transactions_single_cancel` | `ErrAlreadyCancelled` | Return the earlier cancel (200). |

Also new: `ErrBetNotFound` (wraps `ErrNotFound`).

## Files to create / modify

| File | Content |
|---|---|
| `go.mod`, `go.sum` | `require` pgx v5 and goose v3 (exact versions recorded when added). |
| `Domain/ports.go` | `BetRepository`, `TransactionRepository`, `Repositories`, `UnitOfWork`, `PlayerLocker`. |
| `Domain/errors.go`, `Domain/errors_test.go` | `ErrBetNotFound`, `ErrTransactionExists` + their category tests. |
| `Persistence/db.go` | `Connect(ctx, url) (*pgxpool.Pool, error)` (parse, create, ping); `querier` interface. |
| `Persistence/migrate.go` | `//go:embed migrations/*.sql`; `Migrate(ctx, pool) error` via `goose.NewProvider`. |
| `Persistence/migrations/00001_schema.sql` | Tables, sequence, checks, indexes (goose `-- +goose Up/Down`). |
| `Persistence/migrations/00002_seed_players.sql` | The four players with bcrypt hashes. |
| `Persistence/errors.go` | `mapUniqueViolation(err) error` (constraint name → domain error). |
| `Persistence/player_repository.go` | Implements `domain.PlayerRepository`. |
| `Persistence/bet_repository.go` | Implements `domain.BetRepository`. |
| `Persistence/transaction_repository.go` | Implements `domain.TransactionRepository`. |
| `Persistence/player_locker.go` | Implements `domain.PlayerLocker` and `domain.UnitOfWork` on one pooled connection. |
| `Persistence/*_integration_test.go` | `//go:build integration` tests (one file per repository + migrations + locker). |
| `Persistence/helpers_integration_test.go` | Connect to `TEST_DATABASE_URL`, migrate, empty `transactions`/`bets` between tests, compile-time port checks. |
| `Startup/config.go`, `Startup/config_test.go` | `DatabaseURL` (required). |
| `Startup/main.go` | Connect + migrate before starting the HTTP server; close the pool on shutdown; log DB host/name only. |
| `README.md` | Seeded players table, local PostgreSQL (`docker run …`), running integration tests, `DATABASE_URL` now read. |
| `.env.example` | `TEST_DATABASE_URL` example (commented). |
| `AGENTS.md` | Commands: integration tests now available. |
| `ARCHITECTURE.md` | Final data model + ports; "Data model (initial)" replaced. |
| `DESIGN.md` §1 / §5 | Concurrent duplicates and per-player lock; money as `NUMERIC` via text; constraints. |
| `docs/plans/00-general-plan.md` | Phase 04 row (no testcontainers, fakes deferred); open question 5 resolved. |

## Commit sequence

**Working mode** — same loop as Phases 02–03. Proposed split (open question 1):

| Steps | Who writes |
|---|---|
| 2, 3, 5, 9, 11 | Claude writes and explains |
| **4 (SQL), 6, 7, 8** | **Developer**, from step-by-step instructions; Claude reviews |

For every step: `gofmt -l .` empty, `go vet ./...`, `go build ./...`,
`go test ./...` green and, from step 4, `go test -tags=integration ./Persistence/...`
green; Claude proposes the commit message.

---

1. **`docs(plan): add phase 04 persistence plan`** — this file.

2. **`feat(domain): add repository, unit-of-work and player-lock ports`**
   - Ports sketch above in `Domain/ports.go`; `ErrBetNotFound`,
     `ErrTransactionExists` in `Domain/errors.go` + tests.
   - *Go concepts:* function values as parameters (callbacks ≈ `Func<…>`);
     why the callback style makes "always release" impossible to forget
     (combined with `defer` inside the implementation).

3. **`feat(persistence): connect to PostgreSQL at startup`**
   - `go get github.com/jackc/pgx/v5`; `Persistence/db.go` (`Connect`).
   - `Config.DatabaseURL` (required) + config tests; `run()` connects with a
     timeout, `defer pool.Close()`; startup log shows host and database name only.
   - README: start PostgreSQL locally with `docker run` and set `DATABASE_URL`.
   - Manual check: `go run ./Startup` with/without PostgreSQL running.
   - *Go concepts:* `defer` for cleanup in `run()` (≈ `using`/`finally`); why
     `main` exits through `run` so `defer`s execute; connection pools in Go.

4. **`feat(persistence): add schema migrations run at startup`**
   - `go get github.com/pressly/goose/v3`; `Persistence/migrate.go`;
     `00001_schema.sql` (developer writes the SQL from the schema table above).
   - `run()` calls `persistence.Migrate` after connecting.
   - Integration tests + helpers: migrations apply on an empty DB; running them
     twice is a no-op; a withdraw with `amount = 0` and a `CANCEL` without
     `cancelled_transaction_id` are rejected by checks.
   - *Go concepts:* `//go:embed` and `embed.FS` (≈ embedded resources);
     build tags (`//go:build integration`) to keep slow tests out of `go test ./...`.

5. **`feat(persistence): seed the four wallet players`**
   - Generate bcrypt hashes once with a throwaway program (not committed);
     `00002_seed_players.sql`; README "Seeded players" table filled.
   - Integration test: four players with the user IDs and currencies of
     `docs/wallet-findings.md` Q10.

6. **`feat(persistence): add player repository`**
   - `FindByUsername`, `FindByID`; `pgx.ErrNoRows` → `ErrPlayerNotFound`.
   - Integration tests: found by username / ID; unknown → `ErrPlayerNotFound`;
     user ID above 2³¹ round-trips.
   - *Go concepts:* `QueryRow(…).Scan(&a, &b)` (pointers as output parameters);
     the `querier` interface satisfied implicitly by pool connections and transactions.

7. **`feat(persistence): add bet repository`**
   - `NextID`, `Insert`, `FindByID`, `Update`; money read through `::text` + `ParseMoney`.
   - Integration tests: `NextID` increases; insert + find round-trip (stake
     `9000000000.09`); another player's bet → `ErrBetNotFound`; update changes status.
   - *Go concepts:* named-type conversions at the DB edge (`int64(id)`,
     `domain.BetID(n)`); PostgreSQL stores microseconds → tests truncate `now`.

8. **`feat(persistence): add transaction repository`**
   - `Insert`, `FindByID`, `FindByProviderTransactionID`, `FindCancelOf`,
     `Update`; `Persistence/errors.go` maps unique violations.
   - Integration tests: round-trip with `NewBalance` `nil` and set; same provider
     ID twice for one player → `ErrTransactionExists`; same provider ID for two
     players → allowed; a second pending/completed deposit on a bet →
     `ErrBetAlreadySettled`, allowed again after the first is `CANCELLED`; a second
     cancel of one transaction → `ErrAlreadyCancelled`; `FindByProviderTransactionID`
     ignores `CANCEL` rows; another player's transaction → `ErrTransactionNotFound`.
   - *Go concepts:* `errors.As` with `*pgconn.PgError` (≈ `catch (PostgresException e) when …`);
     scanning a nullable column into a pointer (`*string` → `*Money`).

9. **`feat(persistence): add per-player lock and unit of work`**
   - `Persistence/player_locker.go`: acquire a connection, `pg_advisory_lock`,
     run `fn`, `pg_advisory_unlock` with a fresh short context (the request's may
     be cancelled), destroy the connection if unlocking fails; `Atomic` = `Begin`
     / `Commit` / `Rollback` on that connection.
   - Integration tests: `Atomic` commits on `nil`, rolls back on error; a second
     `WithPlayerLock` on the same player waits until the first returns; a
     different player does not wait; the lock is released when `fn` returns an
     error; waiting for the lock stops when `ctx` is cancelled.
   - *Go concepts:* goroutines and channels to coordinate a concurrency test
     (≈ `Task` + `TaskCompletionSource`); `context.WithTimeout`; `defer` order.

10. **(developer) review pass** — explain, in your own words, how a withdraw would
    use the lock and the two `Atomic` blocks, and which index stops each
    duplicate. *(Commit only if tests are added: `test(persistence): …`.)*

11. **`docs(persistence): document schema, locking and local database`**
    - `ARCHITECTURE.md`, `DESIGN.md`, `README.md`, `AGENTS.md`, `.env.example`,
      `00-general-plan.md`, and an "As built" section in this plan.

## Test plan

- **Unit (`go test ./...`, no Docker):** new domain errors; `DATABASE_URL`
  required in config.
- **Integration (`go test -tags=integration ./Persistence/...`):**
  ```bash
  docker run --rm -d --name gameapi-test-db -p 5433:5432 \
    -e POSTGRES_USER=gameapi -e POSTGRES_PASSWORD=gameapi -e POSTGRES_DB=gameapi_test \
    postgres:16-alpine
  TEST_DATABASE_URL=postgres://gameapi:gameapi@localhost:5433/gameapi_test?sslmode=disable
  ```
  Migrations, seed, each repository (cases in steps 6–8), lock and unit of work (step 9).
  Tests in the package run sequentially and empty `transactions`/`bets` before each test.
- **Manual:** `go run ./Startup` against the local database → logs "migrations
  applied", `/health` 200; stopping PostgreSQL first → exit 1 with a clear error.
- **Not covered:** `go test -race` (needs cgo + a C compiler on Windows; can be
  run in CI in Phase 13).

## Open questions

1. ~~**Who writes which step?**~~ **Resolved:** as proposed — developer writes the
   schema SQL (step 4) and the three repositories (6–8); Claude writes ports,
   connection, seed, the lock/unit of work and docs.
2. ~~**Seeded usernames and passwords**~~ (general plan open question 5)
   **Resolved:** `player_usd` (34633089486), `player_eur` (34679664254),
   `player_kes` (34616761765), `player_usd_2` (34673635133), each with its own
   password, all documented in `README.md`.
3. ~~**Scope of the idempotency key**~~ **Resolved:** per player
   (`wallet_user_id, provider_transaction_id`).
4. ~~**goose or a hand-written migrator?**~~ **Resolved:** goose.
5. ~~**Integration database**~~ **Resolved:** `docker run` + `TEST_DATABASE_URL`
   for now. In Phase 12 the `postgres` service of `docker-compose.yml` can serve
   the integration tests too (published port + a separate `gameapi_test`
   database created by an init script), replacing the `docker run` command in
   `README.md`; decided in `12-dockerization.md`.

## As built — deviations from this plan

| Planned | Built | Why |
|---|---|---|
| Developer writes 4 (SQL), 6–8; Claude the rest | Developer wrote the schema SQL and the repository code of 6–8 step by step; Claude wrote, on request, the code of 8a, completed 6, `scanBet`, 8b, and **all the tests of 6–9** | The developer chose to focus on production code and review the tests. |
| `00001_schema.sql` + `00002_seed_players.sql` | `00001_create_players`, `00002_create_bets`, `00003_create_transactions`, `00004_seed_players` | Developer's choice: one table per migration, easier to read. Done before any database depended on it. |
| Test database in a second container on port 5433 | Database `gameapi_test` in the same `gameapi-db` container (`createdb`) | One container is enough; the README documents both commands. |
| `querier` in `db.go` | `Persistence/querier.go` | Developer's choice. |
| `PlayerRepository` built from a `querier` | Exported `PlayerRepository` + `NewPlayerRepository(*pgxpool.Pool)`; bet and transaction repositories unexported with `new…Repository(querier)` | Only the player repository is built by `Startup`; the other two are only created by the unit of work, on its database transaction. |
| Test helpers emptying `transactions`/`bets` between tests | No cleanup: every test uses fresh IDs (`bet_id_seq`, `gen_random_uuid()`) and provider IDs derived from them | Simpler, and tests cannot collide even with rows of earlier runs. |
| Repositories tested through each other | Each method's test prepares and checks rows with **plain SQL** helpers (`insertBetRow`/`readBetRow`, `insertTransactionRow`/`readTransactionRow`) | A failing test points at the method under test only. Helpers are ordered by first use in the tests (project convention). |
| — | Lock tests use goroutines + channels (`holdLock`, `runWithLock`), verified 5× in a row and with a deliberately broken lock (two tests fail) | Concurrency tests must not be flaky and must actually detect a missing lock. |
| — | Mutation check of the bcrypt hashes deferred | Verified against the documented passwords when generated; a login test in Phase 06 checks them end to end. |
| Alternatives to the advisory lock | Reviewed again during step 9 (in-process mutex, transaction lock, lease column, `FOR UPDATE`, optimistic) — advisory lock kept | Recorded in `DESIGN.md` §5. |

Dependencies added: `github.com/jackc/pgx/v5 v5.12.0`, `github.com/pressly/goose/v3 v3.29.0`.

Step 10 (review pass) was done partially: blocks A (withdraw flow) and C (the
lock) answered and corrected in the chat; block B (which mechanism stops each
duplicate) pending.

## Definition of done

- `Domain/` still imports only the standard library and `shopspring/decimal`;
  `Actions`/`Queries` untouched; `Persistence` imports only `Domain` from this repo.
- `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` green **without Docker**.
- `go test -tags=integration ./Persistence/...` green against PostgreSQL 16.
- `go run ./Startup` connects, migrates and serves `/health`; fails fast without a database.
- Every SQL statement is parameterised; no secret (DB password) is logged.
- Each unique index has a test proving it rejects the duplicate it exists for.
- `README.md`, `ARCHITECTURE.md`, `DESIGN.md`, `AGENTS.md`, `00-general-plan.md`
  updated (step 11).
- The developer can explain each Go concept listed in steps 2–9.
