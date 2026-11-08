# 12 — Dockerization

## Goal

One command starts the whole system on a clean machine (§10.1, §5.1):

```bash
cp .env.example .env
docker compose up --build
```

At the end of the phase:

- a multi-stage `Dockerfile` builds the API into a small image that runs as a
  non-root user and stops gracefully on `docker compose stop` (`SIGTERM`);
- `docker-compose.yml` runs three services — `api`, `postgres`
  (PostgreSQL 16) and `wallet` (`kentechsp/wallet-client`) — each with a
  healthcheck; the API starts only when PostgreSQL and the Wallet are healthy;
- the configuration comes only from `.env` (git-ignored); the build context
  never contains `.env`;
- `docker compose down -v` resets everything: our database (volume removed)
  and the Wallet (in-memory, recreated) — the reset promised in `README.md`;
- the compose `postgres` service also serves the integration tests (a second
  database, `gameapi_test`, created on first start), so the README's manual
  `docker run postgres` instructions are replaced by `docker compose up -d postgres`.

No Go code changes are expected. If one turns out to be needed (e.g. a
healthcheck subcommand), stop and update this plan first.

## Requirements covered (assessment sections)

| Section | How |
|---|---|
| 5.1 Run the Wallet, `x-api-key`, reset state | `wallet` service; key from `.env`; `docker compose down -v` |
| 9.7 Observability without secrets | Logs go to stdout (`docker compose logs api`); `.env` kept out of the image and the build context |
| 10.1 Docker, env config | `Dockerfile`, `docker-compose.yml`, `.env.example`, `.dockerignore` |
| 10.2 Nice-to-haves (partly) | Healthchecks, non-root image, graceful stop |

## Design decisions (and alternatives rejected)

| Topic | Decision | Alternatives rejected |
|---|---|---|
| Build stage | `golang:1.27.3-alpine` (matches `go 1.27.3` in `go.mod`). `go.mod`/`go.sum` copied and `go mod download` run **before** copying the source, so dependencies are a cached layer. `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./Startup` | Building on the host and copying the binary (depends on the host OS/arch; the image is no longer reproducible) |
| Runtime stage | `alpine:3.22`, a non-root user (`adduser -D -u 10001 app`), only the binary. Migrations are embedded in the binary (`embed`), so no other files are copied | `scratch` / `distroless`: smaller, but they have no shell or `wget`, so the healthcheck would need a Go subcommand (new code to explain). `golang` as runtime: ~300 MB and a compiler in production |
| Process | `ENTRYPOINT ["/app/api"]` in **exec form**: the binary is PID 1 and receives `SIGTERM` directly, so the graceful shutdown of Phase 02 (and the resolver's wait of Phase 11) runs | Shell form (`ENTRYPOINT /app/api`): `sh` is PID 1 and does not forward the signal → killed after the grace period |
| Stop grace period | `stop_grace_period: 15s` on `api` (Docker's default is 10 s = our `SHUTDOWN_TIMEOUT`, which leaves no margin) | Leaving the default: in-flight Wallet calls could be cut, leaving more `PENDING` rows than necessary |
| Healthchecks | `postgres`: `pg_isready -U $POSTGRES_USER -d $POSTGRES_DB`. `wallet`: `wget -qO- http://localhost:8000/health` (the image is Alpine and has `wget`, checked; `/health` needs no API key — findings Q1). `api`: `wget -qO- http://localhost:8080/health` | API healthcheck in the `Dockerfile` (`HEALTHCHECK`): keeping the three in one file is easier to read; the image is only run through Compose |
| Start order | `api.depends_on: postgres: service_healthy, wallet: service_healthy`. The API gives up after 10 s without a database (Phase 04), so it must not start before PostgreSQL accepts connections | `restart: on-failure` to retry until the DB is up: hides real startup errors behind a restart loop |
| Wallet image | `kentechsp/wallet-client:latest@sha256:682a98803dd1…` (the digest tested in Phase 01) with `platform: linux/arm64` (single-arch image, emulated on amd64 — findings) | Plain `:latest`: a new upstream image could change the failure behaviour every finding relies on |
| Configuration | `api` reads `env_file: .env`. Compose **overrides** two values in `environment:` so they always point at the other containers: `DATABASE_URL` built from `POSTGRES_USER/PASSWORD/DB` (one source for the password) and `WALLET_BASE_URL=http://wallet:8000`. `HTTP_PORT` is fixed to `8080` inside the container | Requiring `DATABASE_URL` in `.env` as today: the password is written twice and can drift. Making the container port configurable: the port mapping and the healthcheck would both need interpolation, for no benefit |
| Published ports | `127.0.0.1:8080:8080` (api), `127.0.0.1:8000:8000` (wallet, Swagger), `127.0.0.1:5432:5432` (postgres, for `go run` and integration tests). Bound to localhost only | `0.0.0.0` (Docker's default): exposes the database and a Wallet with a published API key to the local network |
| Persistence | Named volume `pgdata` for PostgreSQL; `down -v` deletes it. The Wallet has no volume (in-memory) | Bind mount to a host folder: permission issues on Windows, and `down -v` would not reset it |
| Test database | `docker/postgres/init/01-create-test-database.sql` (`CREATE DATABASE gameapi_test;`) mounted into `/docker-entrypoint-initdb.d/`. It runs only when the volume is empty (first start or after `down -v`) | A separate test container: two PostgreSQL instances to explain and two ports |
| `.dockerignore` | Excludes `.git`, `.env*` (keeps `.env.example`), `bin/`, `*.exe`, `docs/`, `.claude/`, `.vscode/`, `.idea/` | No `.dockerignore`: `.env` (secrets) and binaries would be sent to the build context and could end up in a layer |

> **Go/.NET note for the chat:** a Go binary built with `CGO_ENABLED=0` is
> statically linked — like a .NET *self-contained single-file* publish, but
> without a runtime to ship — which is why the runtime image needs nothing
> except the binary.

## Files to create / modify

| File | Change |
|---|---|
| `Dockerfile` | New: build and runtime stages |
| `.dockerignore` | New |
| `docker-compose.yml` | New: `api`, `postgres`, `wallet`, volume `pgdata` |
| `docker/postgres/init/01-create-test-database.sql` | New: creates `gameapi_test` |
| `.env.example` | `DATABASE_URL` comment: under Compose it is built from `POSTGRES_*`; the line stays for `go run` with `localhost`. `TEST_DATABASE_URL` uses the same user/password as `POSTGRES_*`. `POSTGRES_PASSWORD` password-safe note (no characters that need URL-encoding) |
| `README.md` | Quick start (real), services and ports, "Running locally" uses `docker compose up -d postgres wallet`, integration tests use the compose database, "Resetting the state" confirmed, Docker troubleshooting (port 5432 already used, arm64 emulation warning) |
| `AGENTS.md` | Commands: remove the "Not available yet" note, add `docker compose up --build` / `docker compose down -v` |
| `DESIGN.md` | Graceful shutdown paragraph: exec-form entrypoint + `stop_grace_period` |
| `docs/plans/00-general-plan.md` | Phase 12 row: as-built summary |

## Commit sequence

**Working mode** — same as Phases 09–11: **Claude writes every step**, one at
a time, and explains each Docker instruction in the chat; the developer reviews
each step before it is committed.

For every step: `gofmt -l .` empty, `go vet ./...`, `go build ./...`,
`go test ./...` green (no Go changes expected, but they must stay green), plus
the Docker check of the step. Claude proposes the commit message and commits
only when the developer asks. Branch: `11_dockerization` (following the existing numbering).

---

1. **`docs(plan): add phase 12 dockerization plan`** — this file.

2. **`build(docker): add multi-stage Dockerfile`**
   - `Dockerfile` and `.dockerignore` as in the decisions table.
   - Verify: `docker build -t gameapi .` succeeds; `docker image ls gameapi`
     is a few tens of MB; `docker run --rm gameapi` exits with code 1 and logs
     `startup failed … DATABASE_URL is required` (proves the binary runs, as
     non-root: `docker run --rm --entrypoint id gameapi` → `uid=10001`);
     `docker history gameapi` shows no `.env`.

3. **`build(docker): add docker compose with postgres and wallet`**
   - `docker-compose.yml` with the three services, healthchecks,
     `depends_on` conditions, localhost-only ports, `stop_grace_period`,
     volume `pgdata`, the two `environment:` overrides.
   - `docker/postgres/init/01-create-test-database.sql`.
   - `.env.example` adjusted.
   - Verify: `docker compose config` renders without warnings (the rendered
     output contains secrets — only read it locally);
     `docker compose up --build` → all three `healthy` in `docker compose ps`;
     `curl http://localhost:8080/health` → `{"status":"ok"}`; API log shows
     `database migrations applied`; `docker compose exec postgres psql -U gameapi -l`
     lists `gameapi_test`.

4. **`docs(docker): document running with docker compose`**
   - `README.md`, `AGENTS.md`, `DESIGN.md`, general plan row (see table above).
   - Verify: the manual check below, following the README literally from a
     fresh clone of the branch (a new folder), so missing steps show up.

5. *(Only if needed)* **`fix(docker): …`** — anything found by the manual check.

## Test plan

No new automated tests (no Go code changes). Manual check, recorded in
"As built":

1. **Clean start:** `docker compose down -v`, `cp .env.example .env` with a
   real `JWT_SECRET`, `docker compose up --build` → three services healthy.
2. **Happy path through Docker:** login `player_usd` → `GET /player`
   (5000.00) → withdraw 1.00 (`201` or `202`) → deposit / cancel →
   `GET /player` consistent.
3. **Persistence across restart:** `docker compose restart api` → the same
   `providerTransactionId` replays (`200`), so the database survived.
4. **Reset:** `docker compose down -v && docker compose up -d` → the earlier
   `providerTransactionId` is new again (`201`/`202`) and the balance is back
   to 5000 → the README's reset is confirmed.
5. **Graceful stop:** a slow request in flight (`GET /player`) during
   `docker compose stop api` → the request completes; log shows
   `shutdown signal received` and `http server stopped`; exit code 0
   (`docker compose ps -a`). This is the check Phase 11 could not do on Windows.
6. **Pending resolution in Docker:** after a `202`, the resolver completes
   the transaction within ~20 s (`docker compose logs api`).
7. **Integration tests against the compose database:**
   `TEST_DATABASE_URL=postgres://gameapi:<password>@localhost:5432/gameapi_test?sslmode=disable go test -count=1 -tags=integration ./Persistence/...` green.
8. **No secrets in logs:** `docker compose logs` contains no API key,
   JWT secret, password or token.

## Open questions

1. ~~**Who writes the code?**~~ **Resolved:** Claude writes every step, one
   at a time; the developer reviews each commit.
2. ~~**Bring CI forward?**~~ **Resolved:** no, CI stays in Phase 13, as a
   **GitHub Actions** workflow (noted in the general plan).
3. ~~**Should the API container restart on crash**~~ (`restart: unless-stopped`)?
   **Resolved:** no — for an assessment run, a crash should be visible, not
   hidden by a restart loop.

## Definition of done

- `cp .env.example .env` (with a real `JWT_SECRET`) + `docker compose up --build`
  starts the API, PostgreSQL and the Wallet, all healthy, on a clean machine.
- The image builds from source, runs as non-root, contains no `.env`, and
  stops gracefully within `stop_grace_period`.
- `docker compose down -v` resets our database and the Wallet; confirmed by
  hand.
- Integration tests run against the compose `postgres` (`gameapi_test`).
- `gofmt -l .` empty, `go vet ./...` clean, `go build ./...`, `go test ./...`
  green.
- Manual check done (Test plan 1–8) and recorded under "As built".
- `README.md`, `AGENTS.md`, `DESIGN.md`, `.env.example`, `00-general-plan.md`
  updated.
