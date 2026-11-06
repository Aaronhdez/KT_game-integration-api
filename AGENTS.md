# AGENTS.md

Instructions for AI coding agents (Claude Code and others) working in this repository.

## Context

- Project: **Game Integration API** for the KenTech Backend Go technical assessment.
  Casino games place (Withdraw), settle (Deposit) and revert (Cancel) bets; we call
  an intentionally unreliable Wallet mock (`kentechsp/wallet-client`).
- The human developer is an experienced **.NET** engineer who is new to Go.
  Prefer idiomatic but **simple** Go, and when introducing a Go idiom that has no
  obvious .NET equivalent (errors as values, `context.Context`, implicit
  interfaces, goroutines, `defer`), add a short explanation in the chat — not in
  code comments.
- The developer must be able to **explain and modify every line live** in the
  interview. Avoid cleverness, magic and unnecessary dependencies.

## Source of truth

1. `docs/plans/00-general-plan.md` — master plan and phases.
2. `docs/plans/NN-*.md` — the plan of the phase being implemented. **Follow it.**
   If you need to deviate, stop and propose the change to the plan first.
3. `ARCHITECTURE.md` — layout and dependency rules.
4. `DESIGN.md` — behavioural decisions (failure handling, idempotency, …).

## Workflow

1. Never start coding a phase without an approved plan file for it.
2. Implement the plan **one commit at a time**, in the order listed.
3. After each step: `gofmt`/`go vet`, `go build ./...`, `go test ./...` must pass.
4. Propose the commit message; commit only when the developer asks.
5. If the change affects behaviour, update `README.md` / `DESIGN.md` in the same phase.

## Commits

- Small and focused: one logical change, compiling, tests green.
- Conventional Commits: `feat(scope)`, `fix(scope)`, `test(scope)`, `refactor(scope)`,
  `docs(scope)`, `chore(scope)`, `build(scope)`, `ci(scope)`.
  Scopes: `domain`, `actions`, `queries`, `application`, `persistence`, `wallet`,
  `auth`, `startup`, `docker`, `docs`, `plan`.
- Message body explains *why* when it is not obvious.

## Folder layout and dependency rules

| Folder | Package | Contains | May import |
|---|---|---|---|
| `Startup/` | `main` (+ subpackages) | main, config, wiring, router, server | everything |
| `Application/` | `application` | HTTP handlers, DTOs, middleware, error mapping | `Actions`, `Queries`, `Domain` |
| `Actions/` | `actions` | state-changing use cases | `Domain` |
| `Queries/` | `queries` | read-only use cases | `Domain` |
| `Domain/` | `domain` | entities, value objects, errors, **ports (interfaces)** | stdlib, `shopspring/decimal` |
| `Domain/domaintest/` | `domaintest` | fakes for tests (fake Wallet, in-memory repos) | `Domain` |
| `Persistence/` | `persistence` | PostgreSQL repos, migrations, seed | `Domain` |
| `Shared/` | subpackages | Wallet HTTP client, JWT, bcrypt, clock, IDs | `Domain` |

Folders are PascalCase by project decision; **package names are lowercase**.
Interfaces are declared where they are consumed (in `Domain`), never next to the
implementation. Never let `Domain`, `Actions` or `Queries` import `net/http`,
`pgx` or the Wallet client.

## Non-negotiable rules

### Money
- **Never** use `float32`/`float64` for amounts or balances — not even in DTOs.
- Use `domain.Money` (backed by `shopspring/decimal`); DB type `NUMERIC(20,2)`.
- Decode JSON amounts via `json.Number`/custom `UnmarshalJSON`; reject more than
  2 decimal places, negatives where not allowed, and NaN/Inf-like input.
- Currencies: only `USD`, `EUR`, `KES`.

### Identifiers
- Wallet user IDs and `betId`: `int64`. `transactionId`: UUID string.

### Reliability (see `DESIGN.md`)
- Persist the transaction as `PENDING` **before** calling the Wallet.
- Every Wallet call has a timeout (`context.WithTimeout`); retries are bounded.
- Classify each Wallet result as *applied*, *not applied* or *unknown*. Only
  *not applied* may become `FAILED`; *unknown* stays `PENDING` (HTTP 202).
- Idempotency is enforced by DB unique constraints, not only by in-memory checks.
- Operations of the same player are serialised.

### Security
- Parameterised SQL only (`$1, $2…`); never build SQL with string concatenation.
- Never log or return tokens, passwords, password hashes, the JWT secret or the
  Wallet API key. Never commit secrets: config comes from env vars (`.env` is git-ignored).
- A player can only see/act on their own bets and transactions → otherwise 404.
- Login returns the same error for unknown user and wrong password.

### Errors and HTTP
- Error body: `{ "code": "...", "message": "..." }` with the codes of the
  assessment (`INVALID_REQUEST`, `UNAUTHORIZED`, `NOT_FOUND`, `CONFLICT`,
  `INSUFFICIENT_FUNDS`, `WALLET_ERROR`, `WALLET_TIMEOUT`).
- Use cases return typed domain errors; only `Application` maps them to HTTP.
- Wrap errors with context: `fmt.Errorf("withdraw: %w", err)`; check with `errors.Is/As`.

## Go style

- `gofmt` formatting, `go vet` clean. Short, descriptive names; no stutter
  (`domain.Bet`, not `domain.DomainBet`).
- `context.Context` is the first parameter of anything doing I/O.
- Constructors `NewX(deps…) *X`; dependencies injected explicitly, no globals.
- Prefer the standard library; adding a dependency requires a reason in the plan.
- Logging with `log/slog`, structured key/values.

## Testing

- Business rules in `Actions`/`Domain` are unit-tested with fakes from
  `Domain/domaintest` — no Docker needed for `go test ./...`.
- The fake Wallet must deterministically simulate: success, insufficient funds,
  error-before-apply, error-after-apply, timeout, slow response.
- Mandatory coverage: idempotency (replay, conflict, concurrent duplicates) and
  every Cancel case of the assessment's section 8.5.
- Table-driven tests; test names describe behaviour (`TestWithdraw_WalletTimeout_ReturnsPending`).
- Integration tests needing PostgreSQL use a build tag (`//go:build integration`).

## Commands

```bash
gofmt -l .                          # must print nothing (files must use LF; see .gitattributes)
go vet ./...
go build ./...
go test ./...
go run ./Startup                    # serves on :8080; GET /health
go list -f '{{.GoFiles}}' ./...     # check no test helpers leaked into production files
```

> Not available yet: `go test -tags=integration ./...` (Phase 04),
> `docker compose up --build` / `docker compose down -v` (Phase 12).
> There is no Makefile: `make` is not installed on the developer's Windows machine.
