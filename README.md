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

Needs Go (64-bit). Every variable has a default, so no setup is required:

```bash
go run ./Startup
curl http://localhost:8080/health      # {"status":"ok"}
```

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
go test ./...                     # unit tests, no Docker needed
go test -tags=integration ./...   # needs PostgreSQL (from Phase 04)
```

## Configuration

All configuration comes from environment variables. See [`.env.example`](.env.example).

An invalid value stops the service at startup with an error naming the variable.

| Variable | Description | Default | Read from |
|---|---|---|---|
| `HTTP_PORT` | API listen port (1–65535) | `8080` | Phase 02 |
| `LOG_LEVEL` | `debug`, `info`, `warn`, `error` (case-insensitive) | `info` | Phase 02 |
| `SHUTDOWN_TIMEOUT` | Max time to finish in-flight requests on shutdown (Go duration: `500ms`, `10s`, `1m`) | `10s` | Phase 02 |
| `DATABASE_URL` | PostgreSQL connection string | — | Phase 04 |
| `WALLET_BASE_URL` | Wallet base URL | `http://wallet:8000` | Phase 05 |
| `WALLET_API_KEY` | Wallet `x-api-key` header | — | Phase 05 |
| `WALLET_TIMEOUT` | Timeout per Wallet call | `3s` | Phase 05 |
| `JWT_SECRET` | HMAC secret to sign JWTs | — | Phase 06 |
| `JWT_TTL` | Token lifetime | `1h` | Phase 06 |

Variables of later phases are listed in advance; the service ignores them until
the phase that reads them is implemented.

## Seeded players

*TBD (Phase 04).*

| Username | Password | Wallet user ID | Currency |
|---|---|---|---|
| *TBD* | *TBD* | 34633089486 | USD |
| *TBD* | *TBD* | 34679664254 | EUR |
| *TBD* | *TBD* | 34616761765 | KES |
| *TBD* | *TBD* | 34673635133 | USD |

## API

Base path `/api/v1`. All endpoints except login require `Authorization: Bearer <JWT>`.

| Method | Path | Description |
|---|---|---|
| POST | `/auth/login` | Get a JWT |
| GET | `/player` | Player info and live balance |
| POST | `/withdraw` | Place a bet (take the stake) |
| POST | `/deposit` | Settle a bet (pay the outcome) |
| POST | `/cancel` | Revert a withdraw or deposit |

*Examples with `curl` and OpenAPI spec: TBD (Phase 13).*

## Resetting the state

The Wallet is in-memory and resets to its initial balances on restart, while our
database persists. To reset both:

```bash
docker compose down -v
```

*Confirmed in Phase 12.*
