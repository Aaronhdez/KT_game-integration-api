# Game Integration API

Backend service (Go) that lets third-party casino games place, settle and cancel
bets on behalf of players, keeping its records consistent with an unreliable
Wallet service.

> 🚧 Work in progress. Sections marked *TBD* are completed in the phase indicated
> (see [`docs/plans/00-general-plan.md`](docs/plans/00-general-plan.md)).

## Documentation

- [`ARCHITECTURE.md`](ARCHITECTURE.md) — layout, layers and dependency rules.
- [`DESIGN.md`](DESIGN.md) — failure handling, idempotency and other decisions.
- [`docs/plans/`](docs/plans/) — master plan and per-phase plans.
- [`AGENTS.md`](AGENTS.md) — conventions for AI coding agents.

## Requirements

- Docker + Docker Compose
- Go (latest stable) — only to run tests or the service outside Docker

## Quick start

*TBD (Phase 12).* Target:

```bash
cp .env.example .env
docker compose up --build
```

Services: API on `http://localhost:8080`, mock Wallet on `http://localhost:8000`
(Swagger at `/swagger/index.html`), PostgreSQL on `localhost:5432`.

## Running locally (without Docker)

Needs Go (64-bit) and a PostgreSQL 16 database. Start one with Docker:

```bash
docker run --rm -d --name gameapi-db -p 5432:5432 \
  -e POSTGRES_USER=gameapi -e POSTGRES_PASSWORD=gameapi -e POSTGRES_DB=gameapi \
  postgres:16-alpine
```

`DATABASE_URL`, `WALLET_API_KEY` and `JWT_SECRET` are the only required
variables; every other one has a default:

```bash
export DATABASE_URL="postgres://gameapi:gameapi@localhost:5432/gameapi?sslmode=disable"
export WALLET_API_KEY="<the key in .env.example>"
export JWT_SECRET="any-local-secret-of-at-least-32-bytes"
go run ./Startup
curl http://localhost:8080/health      # {"status":"ok"}
```

In PowerShell: `$env:DATABASE_URL="postgres://gameapi:gameapi@localhost:5432/gameapi?sslmode=disable"`,
`$env:WALLET_API_KEY="<the key in .env.example>"` and
`$env:JWT_SECRET="any-local-secret-of-at-least-32-bytes"`, then `go run ./Startup`.
To reach a Wallet started on your machine also set
`WALLET_BASE_URL=http://localhost:8000` (the default is the Docker Compose host name). If the database cannot be reached within 10 seconds the
service exits with code 1. Stop the container with `docker stop gameapi-db`
(`--rm` deletes it, and its data, when it stops).

On Windows PowerShell use `curl.exe` (plain `curl` is an alias of
`Invoke-WebRequest`). Stop the service with Ctrl+C: it finishes in-flight
requests (up to `SHUTDOWN_TIMEOUT`) and exits with code 0.

To override a setting, set the variable in the shell before running, e.g.
`$env:HTTP_PORT="9090"` (PowerShell) or `HTTP_PORT=9090 go run ./Startup` (bash).
The program does not read `.env` itself; Docker Compose does (Phase 12).

## Running the tests

```bash
gofmt -l .                        # must print nothing
go vet ./...
go vet -tags=integration ./...    # also checks the integration test files
go test ./...                     # unit tests, no Docker needed
```

### Integration tests (PostgreSQL)

Files named `*_integration_test.go` carry the build tag `integration`: they only
compile with `-tags=integration`, so `go test ./...` never needs a database.
They run against the database in `TEST_DATABASE_URL`; without it they fail
instead of being skipped. With the container from "Running locally", create a
separate test database once:

```bash
docker exec gameapi-db createdb -U gameapi gameapi_test
TEST_DATABASE_URL="postgres://gameapi:gameapi@localhost:5432/gameapi_test?sslmode=disable" \
  go test -count=1 -tags=integration ./Persistence/...
```

In PowerShell set `$env:TEST_DATABASE_URL=...` first. The tests migrate the
database themselves, drop and recreate its schema in one test, and create rows
with fresh IDs, so the test database must not be the one the service uses.
`-count=1` disables Go's test result cache.

To make VS Code (gopls) analyse these files, add
`"gopls": { "buildFlags": ["-tags=integration"] }` to your user settings.

## Configuration

All configuration comes from environment variables. See [`.env.example`](.env.example).

An invalid value stops the service at startup with an error naming the variable.

| Variable | Description | Default | Read from |
|---|---|---|---|
| `HTTP_PORT` | API listen port (1–65535) | `8080` | Phase 02 |
| `LOG_LEVEL` | `debug`, `info`, `warn`, `error` (case-insensitive) | `info` | Phase 02 |
| `SHUTDOWN_TIMEOUT` | Max time to finish in-flight requests on shutdown (Go duration: `500ms`, `10s`, `1m`) | `10s` | Phase 02 |
| `DATABASE_URL` | PostgreSQL connection string (**required**). Pool options such as `pool_max_conns=10` can be added as URL parameters | — | Phase 04 |
| `WALLET_BASE_URL` | Wallet base URL (absolute `http`/`https` URL) | `http://wallet:8000` | Phase 05 |
| `WALLET_API_KEY` | Wallet `x-api-key` header (**required**; the mock's key is in `.env.example`) | — | Phase 05 |
| `WALLET_OPERATION_TIMEOUT` | Timeout per withdraw/deposit call to the Wallet | `6s` | Phase 05 |
| `WALLET_BALANCE_TIMEOUT` | Timeout per balance read attempt (retried once) | `10s` | Phase 05 |
| `JWT_SECRET` | HMAC-SHA256 key that signs the JWTs (**required**, at least 32 bytes) | — | Phase 06 |
| `JWT_TTL` | Token lifetime (Go duration) | `1h` | Phase 06 |
| `PENDING_RESOLVER_INTERVAL` | How often `PENDING` transactions are re-sent to the Wallet in the background (see [Pending transactions](#pending-transactions)) | `10s` | Phase 11 |

Variables of later phases are listed in advance; the service ignores them until
the phase that reads them is implemented.

## Seeded players

Created by the migration `Persistence/migrations/00004_seed_players.sql` when the
service starts. They match the users of the Wallet mock; the database stores
only bcrypt hashes of these passwords (test credentials, valid only locally).

| Username | Password | Wallet user ID | Currency | Initial Wallet balance |
|---|---|---|---|---|
| `player_usd` | `usd-pass-2026` | 34633089486 | USD | 5000 |
| `player_eur` | `eur-pass-2026` | 34679664254 | EUR | 9000000000 |
| `player_kes` | `kes-pass-2026` | 34616761765 | KES | 750.5 |
| `player_usd_2` | `usd2-pass-2026` | 34673635133 | USD | 31415.25 |

## API

Base path `/api/v1`. All endpoints except login require `Authorization: Bearer <JWT>`.

| Method | Path | Description |
|---|---|---|
| POST | `/auth/login` | Get a JWT |
| GET | `/player` | Player info and live balance |
| POST | `/withdraw` | Place a bet (take the stake) |
| POST | `/deposit` | Settle a bet (pay the outcome) |
| POST | `/cancel` | Revert a withdraw or deposit |

Errors always have the body `{"code": "...", "message": "..."}`:

| Status | Code | When |
|---|---|---|
| 400 | `INVALID_REQUEST` | Malformed JSON, missing or invalid fields (the message says which) |
| 401 | `UNAUTHORIZED` | Wrong credentials, or missing / invalid / expired token (same message for all) |
| 404 | `NOT_FOUND` | Unknown route or method, or a resource that does not exist or belongs to another player |
| 409 | `CONFLICT` | Idempotency conflict or invalid state (the message says which) |
| 422 | `INSUFFICIENT_FUNDS` | The Wallet refused the operation for insufficient funds |
| 502 | `WALLET_ERROR` | The Wallet failed or rejected the call, and nothing was applied |
| 504 | `WALLET_TIMEOUT` | The Wallet did not answer a read in time (`GET /player` only; an operation with an unknown outcome is a `202`) |
| 500 | `INTERNAL_ERROR` | Unexpected failure (e.g. database down). Not in the assessment; details only in the logs |

### Login

```bash
curl -i -X POST http://localhost:8080/api/v1/auth/login \
     -H 'Content-Type: application/json' \
     -d '{"username":"player_usd","password":"usd-pass-2026"}'
```

`200` with `Cache-Control: no-store`:

```json
{"token":"eyJhbGciOiJIUzI1NiIs…","tokenType":"Bearer","expiresAt":"2026-11-07T22:57:53Z"}
```

The token is an HS256 JWT whose `sub` is the player's wallet user ID; it
expires after `JWT_TTL`. Send it as `Authorization: Bearer <token>` to the
other endpoints. An unknown username and a wrong password get the same `401`.
In PowerShell use `curl.exe` and escape the inner quotes
(`-d '{\"username\":\"player_usd\",\"password\":\"usd-pass-2026\"}'`).

### Player information

```bash
curl -i http://localhost:8080/api/v1/player -H "Authorization: Bearer $TOKEN"
```

`200` with `Cache-Control: no-store`:

```json
{"userId":34633089486,"balance":5000.00,"currency":"USD"}
```

The player comes from the token. The balance is read from the Wallet on every
request (never cached), so the answer can take several seconds, and the
unreliable Wallet can make it a `502 WALLET_ERROR` or `504 WALLET_TIMEOUT` —
just retry. Without a valid token: `401`.

### Withdraw (bet placement)

```bash
curl -i -X POST http://localhost:8080/api/v1/withdraw \
     -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
     -d '{"currency":"USD","amount":10.50,"providerTransactionId":"prov-tx-0001"}'
```

`201` with `Cache-Control: no-store`:

```json
{"transactionId":"9bfc9dae-87f4-4270-83e1-fe8fbd2d7cee","providerTransactionId":"prov-tx-0001","oldBalance":5000.00,"newBalance":4989.50,"status":"COMPLETED","betStatus":"PLACED"}
```

| Status | When |
|---|---|
| `201` | Processed for the first time and `COMPLETED`; the bet is `PLACED` |
| `200` | Same `providerTransactionId` and payload again: the stored result in its current state, without calling the Wallet; or a retry that [resolved](#pending-transactions) a `PENDING` withdraw |
| `202` | The Wallet's outcome is unknown: `"status":"PENDING"`, `"newBalance":null`, `"betStatus":null`. It is [resolved](#pending-transactions) by a retry or in the background |
| `400` | Missing or invalid field, amount not greater than zero, more than 2 decimals, or a currency that is not the player's |
| `409` | Same `providerTransactionId` with another amount, or an ID already used by another player |
| `422` | Insufficient funds: the transaction is kept as `FAILED` and no bet is created. A retry gets `422` again, without calling the Wallet |
| `502` | The Wallet rejected the withdraw (a retry gets `502` again), or the balance before it could not be read (nothing was sent) |

The player comes from the token, and `providerTransactionId` is the
idempotency key. Expect a few seconds per call and roughly one `202` in four
from the unreliable Wallet.

### Deposit (bet settlement)

```bash
curl -i -X POST http://localhost:8080/api/v1/deposit \
     -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
     -d '{"currency":"USD","amount":25.00,"providerTransactionId":"prov-tx-0002","providerWithdrawnTransactionId":"prov-tx-0001"}'
```

`201` with `Cache-Control: no-store`:

```json
{"transactionId":"5d0c6f8e-2a41-4c1b-9f7e-3b8a6d2e1c40","providerTransactionId":"prov-tx-0002","oldBalance":4989.50,"newBalance":5014.50,"status":"COMPLETED","betStatus":"WON"}
```

`providerWithdrawnTransactionId` is the withdraw whose bet is settled. An
amount greater than zero settles it as `WON`; `0` settles it as `LOST`, is
**not sent to the Wallet** (no money moves) and answers `201` with
`oldBalance` = `newBalance` (see `DESIGN.md` §4).

| Status | When |
|---|---|
| `201` | Processed for the first time and `COMPLETED`; the bet is `WON` or `LOST` |
| `200` | Same `providerTransactionId` and payload again: the stored result in its current state, without calling the Wallet; or a retry that [resolved](#pending-transactions) a `PENDING` deposit |
| `202` | The Wallet's outcome is unknown: `"status":"PENDING"`, `"newBalance":null`, `"betStatus":"PLACED"`. It is [resolved](#pending-transactions) by a retry or in the background |
| `400` | Missing or invalid field (a missing `amount` is not `0`), negative amount, more than 2 decimals, or a currency that is not the player's / the withdraw's |
| `404` | The referenced withdraw does not exist, belongs to another player, or is not a withdraw |
| `409` | The withdraw is not `COMPLETED` or has a cancel in progress; the bet is already settled, cancelled or has another deposit in progress; or same `providerTransactionId` with another payload or used by another player |
| `502` | The Wallet rejected the deposit (a retry gets `502` again), or the balance before it could not be read (nothing was sent) |

### Cancel

```bash
curl -i -X POST http://localhost:8080/api/v1/cancel \
     -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
     -d '{"providerTransactionId":"prov-tx-0001"}'
```

`201` with `Cache-Control: no-store`:

```json
{"transactionId":"1f4b2c3d-6e7a-4b8c-9d0e-a1b2c3d4e5f6","providerTransactionId":"prov-tx-0001","oldBalance":4989.50,"newBalance":5000.00,"status":"COMPLETED","betStatus":"CANCELLED"}
```

`providerTransactionId` is the withdraw or deposit to cancel, and the
idempotency key. The Wallet has no rollback, so a cancel is a compensating
operation on the same bet: a withdraw is **credited back** (the bet becomes
`CANCELLED`), a deposit is **debited back** (the bet returns to `PLACED` and
can be settled again). Cancelling a zero-amount deposit moves no money and is
not sent to the Wallet. The response's `transactionId` is the cancel's own
ID; `providerTransactionId` is the cancelled transaction's.

| Status | When |
|---|---|
| `201` | Processed for the first time and `COMPLETED` |
| `200` | The transaction was already cancelled: the earlier cancel in its current state, without calling the Wallet; or a retry that [resolved](#pending-transactions) a `PENDING` cancel |
| `202` | The Wallet's outcome is unknown: `"status":"PENDING"`, `"newBalance":null`, `betStatus` unchanged. It is [resolved](#pending-transactions) by a retry or in the background |
| `400` | Missing or invalid `providerTransactionId` |
| `404` | The transaction does not exist or belongs to another player |
| `409` | The transaction is `PENDING` or `FAILED`; or a withdraw whose bet is settled (cancel the deposit first) or has a deposit in progress |
| `422` | Insufficient funds to take a deposit back: the cancel is kept as `FAILED`, nothing else changes. A retry gets `422` again, without calling the Wallet |
| `502` | The Wallet rejected the compensation (a retry gets `502` again), or the balance before it could not be read (nothing was sent) |

### Pending transactions

A `202` means the Wallet may or may not have applied the operation (a `500`, a
dropped connection or a timeout). The transaction stays `PENDING` until we
know, and is resolved by **sending the identical request to the Wallet
again**, which never applies it twice: the Wallet answers that it already has
it (it had been applied) or applies it now. Two things trigger that re-send:

- **A retry of the same request** by the provider. If the last attempt
  started at least 10 s ago, the retry re-sends it and answers the final
  result: `200` (completed), `422` / `502` (failed), or `202` again if the
  Wallet's answer is still unknown. Before 10 s it answers `202` straight away.
- **A background resolver**, run at startup and then every
  `PENDING_RESOLVER_INTERVAL`, for providers that never retry. A `PENDING`
  transaction is usually resolved within about 20 s.

The 10 s wait avoids re-sending while the first attempt may still be running
inside the Wallet. A transaction still unknown after 5 minutes is logged at
`error` level (`"outcome":"pending_overdue"`) on every new attempt. Details
in [`DESIGN.md`](DESIGN.md) §2.

*More `curl` examples and OpenAPI spec: Phase 13.*

## Resetting the state

The Wallet is in-memory and resets to its initial balances on restart, while our
database persists. To reset both:

```bash
docker compose down -v
```

*Confirmed in Phase 12.*
