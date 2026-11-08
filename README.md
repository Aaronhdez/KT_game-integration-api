# Game Integration API

[![CI](https://github.com/Aaronhdez/KT_game-integration-api/actions/workflows/ci.yml/badge.svg)](https://github.com/Aaronhdez/KT_game-integration-api/actions/workflows/ci.yml)

Backend service (Go) that lets third-party casino games place, settle and cancel
bets on behalf of players, keeping its records consistent with an unreliable
Wallet service.

Developed with the assistance of Claude Opus 5.5 as a coding assistant; see
the disclaimer in [`DESIGN.md`](DESIGN.md).

## Contents

- [Documentation](#documentation)
- [Project layout](#project-layout)
- [Requirements](#requirements)
- [Quick start](#quick-start)
  - [Troubleshooting](#troubleshooting)
- [Seeded players](#seeded-players)
- [API](#api)
  - [Pending transactions](#pending-transactions)
- [Configuration](#configuration)
- [Resetting the state](#resetting-the-state)
- [Running locally (without Docker for the API)](#running-locally-without-docker-for-the-api)
- [Running the tests](#running-the-tests)
  - [Integration tests (PostgreSQL)](#integration-tests-postgresql)
  - [Continuous integration](#continuous-integration)
- [Annex: API examples](#annex-api-examples)
  - [Login](#login)
  - [Player information](#player-information)
  - [Withdraw (bet placement)](#withdraw-bet-placement)
  - [Deposit (bet settlement)](#deposit-bet-settlement)
  - [Cancel](#cancel)

## Documentation

- [`docs/openapi.yaml`](docs/openapi.yaml) — OpenAPI 3.1 specification of the API.
- [`DESIGN.md`](DESIGN.md) — how the service behaves when the Wallet fails, and why.
- [`ARCHITECTURE.md`](ARCHITECTURE.md) — layout, layers and dependency rules.
- [`docs/try-it.md`](docs/try-it.md) — a complete round trip with `curl` (bash and PowerShell).
- [`docs/wallet-findings.md`](docs/wallet-findings.md) — how the mock Wallet actually behaves (spike).
- [`docs/decisions.md`](docs/decisions.md) — the full reasoning, measurements and per-topic decisions.

- [`AGENTS.md`](AGENTS.md) — conventions for AI coding agents.
- [`docs/plans/`](docs/plans/) — master plan and per-phase plans.

## Project layout

```
Startup/       main, configuration, wiring, router, background resolver
Application/   HTTP handlers, DTOs, auth middleware, error → HTTP mapping
Actions/       state-changing use cases: Login, Withdraw, Deposit, Cancel, ResolvePending
Queries/       read-only use cases: GetPlayer
Domain/        entities, money, rules, errors and ports (interfaces); domaintest/ fakes
Persistence/   PostgreSQL repositories, per-player lock, migrations and seed
Shared/        Wallet HTTP client, JWT, bcrypt, clock, IDs
docs/          design details, try-it walkthrough, OpenAPI spec, Wallet findings, plans
```

Dependency rules and the request flow are in [`ARCHITECTURE.md`](ARCHITECTURE.md).

## Requirements

- Docker + Docker Compose
- Go (latest stable, 64-bit) — only to run the tests or the service outside Docker

## Quick start

```bash
cp .env.example .env        # PowerShell: Copy-Item .env.example .env
# edit .env: set JWT_SECRET to a random value of at least 32 bytes
#            (e.g. the output of `openssl rand -hex 32`)
docker compose up --build
```

The API starts once PostgreSQL and the Wallet report healthy (the first build
takes a minute or two). Then:

```bash
curl http://localhost:8080/health      # {"status":"ok"}
```

To see it working end to end — log in, place a bet, replay it, settle it and
cancel it — follow [`docs/try-it.md`](docs/try-it.md).

| Service | URL (localhost only) | Notes |
|---|---|---|
| `api` | `http://localhost:8080` | This service. Migrations and seed run at startup |
| `wallet` | `http://localhost:8000` | Mock Wallet; Swagger at `/swagger/index.html` |
| `postgres` | `localhost:5432` | PostgreSQL 16; databases `gameapi` (the service) and `gameapi_test` (integration tests) |

Useful commands:

```bash
docker compose up --build -d --wait   # start in the background, return when healthy
docker compose ps                     # status and health of each service
docker compose logs -f api            # API logs (JSON, one line per event)
docker compose restart api            # restart only the API (keeps the database and the Wallet's state)
docker compose down -v                # stop and reset everything (see "Resetting the state")
```

Under Compose, `DATABASE_URL` and `WALLET_BASE_URL` are set in
`docker-compose.yml` so they always point at the other containers; the values
in `.env` are only for running the service outside Docker. Everything else is
read from `.env`.

### Troubleshooting

- **Port already in use** (`8080`, `8000` or `5432`): stop whatever uses it
  (e.g. a local PostgreSQL) and run `docker compose up` again.
- **Platform warning for `wallet`** (`linux/arm64` … `linux/amd64`): expected.
  The Wallet image is only built for arm64 and Docker emulates it on other
  hosts; it works, just a bit slower.
- **The API exits with "password authentication failed"** after changing
  `POSTGRES_PASSWORD`: PostgreSQL reads it only when its volume is first
  created. Run `docker compose down -v` (deletes the data) and start again.
- **Use only letters, digits, `-` and `_` in `POSTGRES_PASSWORD`**: it is put
  into `DATABASE_URL` without URL-encoding.

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
The full contract is in [`docs/openapi.yaml`](docs/openapi.yaml) (open it in
e.g. https://editor.swagger.io).

| Method | Path | Description | Example |
|---|---|---|---|
| POST | `/auth/login` | Get a JWT | [Annex](#login) |
| GET | `/player` | Player info and live balance | [Annex](#player-information) |
| POST | `/withdraw` | Place a bet (take the stake) | [Annex](#withdraw-bet-placement) |
| POST | `/deposit` | Settle a bet (pay the outcome) | [Annex](#deposit-bet-settlement) |
| POST | `/cancel` | Revert a withdraw or deposit | [Annex](#cancel) |

`GET /health` (no token) reports that the process is up. The
[annex](#annex-api-examples) has a `curl` call, the response and every status
code of each endpoint.

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
in [`DESIGN.md`](DESIGN.md) §3.

## Configuration

All configuration comes from environment variables; [`.env.example`](.env.example)
lists them with working values for Docker Compose. An invalid value stops the
service at startup with an error naming the variable.

| Variable | Description | Default |
|---|---|---|
| `HTTP_PORT` | API listen port (1–65535) | `8080` |
| `LOG_LEVEL` | `debug`, `info`, `warn`, `error` (case-insensitive) | `info` |
| `SHUTDOWN_TIMEOUT` | Max time to finish in-flight requests on shutdown (Go duration: `500ms`, `10s`, `1m`) | `10s` |
| `DATABASE_URL` | PostgreSQL connection string (**required**). Pool options such as `pool_max_conns=10` can be added as URL parameters. Set by Compose | — |
| `WALLET_BASE_URL` | Wallet base URL (absolute `http`/`https` URL). Set by Compose | `http://wallet:8000` |
| `WALLET_API_KEY` | Wallet `x-api-key` header (**required**; the mock's key is in `.env.example`) | — |
| `WALLET_OPERATION_TIMEOUT` | Timeout per withdraw/deposit call to the Wallet | `6s` |
| `WALLET_BALANCE_TIMEOUT` | Timeout per balance read attempt (retried once) | `10s` |
| `JWT_SECRET` | HMAC-SHA256 key that signs the JWTs (**required**, at least 32 bytes) | — |
| `JWT_TTL` | Token lifetime (Go duration) | `1h` |
| `PENDING_RESOLVER_INTERVAL` | How often `PENDING` transactions are re-sent to the Wallet in the background (see [Pending transactions](#pending-transactions)) | `10s` |
| `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` | Read by the compose `postgres` service (and used to build `DATABASE_URL`) | — |

## Resetting the state

The Wallet is in-memory and resets to its initial balances whenever its
container stops, while our database persists in the `pgdata` volume. To reset
both:

```bash
docker compose down -v      # removes the containers and the pgdata volume
docker compose up -d --wait # fresh database (migrations + seed) and fresh Wallet
```

Always reset both together: resetting only one of them leaves our records and
the Wallet's balances out of step (see `DESIGN.md` §2, "Wallet restarts and
loses its state"). For the same reason, `docker compose stop`, `restart` or
`down` **without** `-v` are not a clean state: they keep our database but
reset the Wallet. To restart only the API, use `docker compose restart api`.

## Running locally (without Docker for the API)

Needs Go (64-bit). Start only PostgreSQL and the Wallet with Compose (needs the
`.env` of the Quick start):

```bash
docker compose up -d --wait postgres wallet
```

`DATABASE_URL`, `WALLET_API_KEY` and `JWT_SECRET` are the only required
variables; every other one has a default. The program does not read `.env`
itself, so set them in the shell (the values match `.env.example`):

```bash
export DATABASE_URL="postgres://gameapi:change-me@localhost:5432/gameapi?sslmode=disable"
export WALLET_BASE_URL="http://localhost:8000"
export WALLET_API_KEY="<the key in .env.example>"
export JWT_SECRET="any-local-secret-of-at-least-32-bytes"
go run ./Startup
curl http://localhost:8080/health      # {"status":"ok"}
```

In PowerShell: `$env:DATABASE_URL="postgres://gameapi:change-me@localhost:5432/gameapi?sslmode=disable"`,
`$env:WALLET_BASE_URL="http://localhost:8000"`,
`$env:WALLET_API_KEY="<the key in .env.example>"` and
`$env:JWT_SECRET="any-local-secret-of-at-least-32-bytes"`, then `go run ./Startup`.
Use the password of your `.env` if you changed it. `WALLET_BASE_URL` is needed
because its default is the Docker Compose host name (`http://wallet:8000`).
If the database cannot be reached within 10 seconds the service exits with
code 1. Stop the compose `api` service first (`docker compose stop api`), or
set `HTTP_PORT` to another port, since both listen on `8080`.

On Windows PowerShell use `curl.exe` (plain `curl` is an alias of
`Invoke-WebRequest`). Stop the service with Ctrl+C: it finishes in-flight
requests (up to `SHUTDOWN_TIMEOUT`) and exits with code 0.

## Running the tests

```bash
gofmt -l .                        # must print nothing
go vet ./...
go vet -tags=integration ./...    # also checks the integration test files
go test ./...                     # unit tests, no Docker needed
```

The unit tests cover the business rules with a fake Wallet that simulates
success, insufficient funds, errors before and after applying, timeouts and
slow answers deterministically, including idempotency (replays, conflicts,
concurrent duplicates) and every cancel case of the assessment.

### Integration tests (PostgreSQL)

Files named `*_integration_test.go` carry the build tag `integration`: they only
compile with `-tags=integration`, so `go test ./...` never needs a database.
They run against the database in `TEST_DATABASE_URL`; without it they fail
instead of being skipped. The compose `postgres` service creates the test
database `gameapi_test` on its first start (script in `docker/postgres/init/`):

```bash
docker compose up -d --wait postgres
TEST_DATABASE_URL="postgres://gameapi:change-me@localhost:5432/gameapi_test?sslmode=disable" \
  go test -count=1 -tags=integration ./Persistence/...
```

In PowerShell set `$env:TEST_DATABASE_URL=...` first. The tests migrate the
database themselves, drop and recreate its schema in one test, and create rows
with fresh IDs, so the test database must not be the one the service uses.
`-count=1` disables Go's test result cache.

To make VS Code (gopls) analyse these files, add
`"gopls": { "buildFlags": ["-tags=integration"] }` to your user settings.

### Continuous integration

[`.github/workflows/ci.yml`](.github/workflows/ci.yml) runs on every push and
pull request, in four parallel jobs: formatting, `go vet`, build and unit
tests with the race detector (`go test -race`); the integration tests against
a PostgreSQL 16 service container; the Docker image build; and the OpenAPI
lint (`npx @redocly/cli lint docs/openapi.yaml`).

## Annex: API examples

Every endpoint with a `curl` call, its response and every status code it
can return. The same contract is in [`docs/openapi.yaml`](docs/openapi.yaml).

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
`oldBalance` = `newBalance` (see `DESIGN.md` §5).

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
