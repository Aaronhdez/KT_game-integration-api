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

## Running the tests

*TBD (Phase 02).*

```bash
go test ./...                     # unit tests, no Docker needed
go test -tags=integration ./...   # needs PostgreSQL
```

## Configuration

All configuration comes from environment variables. See [`.env.example`](.env.example).

| Variable | Description | Default |
|---|---|---|
| `HTTP_PORT` | API listen port | `8080` |
| `DATABASE_URL` | PostgreSQL connection string | — |
| `WALLET_BASE_URL` | Wallet base URL | `http://wallet:8000` |
| `WALLET_API_KEY` | Wallet `x-api-key` header | — |
| `WALLET_TIMEOUT` | Timeout per Wallet call | `3s` |
| `JWT_SECRET` | HMAC secret to sign JWTs | — |
| `JWT_TTL` | Token lifetime | `1h` |
| `LOG_LEVEL` | `debug`, `info`, `warn`, `error` | `info` |

*Final list confirmed in Phases 02, 05, 06.*

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
