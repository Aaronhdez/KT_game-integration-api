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
| 422 | `INSUFFICIENT_FUNDS` | The Wallet refused the withdraw |
| 502 | `WALLET_ERROR` | The Wallet failed or rejected the call |
| 504 | `WALLET_TIMEOUT` | The Wallet did not answer in time |
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

*More `curl` examples and OpenAPI spec: Phase 13.*

## Resetting the state

The Wallet is in-memory and resets to its initial balances on restart, while our
database persists. To reset both:

```bash
docker compose down -v
```

*Confirmed in Phase 12.*
