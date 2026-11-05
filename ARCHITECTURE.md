# Architecture

Clean Architecture, kept proportionate: business rules in the centre, with HTTP,
PostgreSQL and the Wallet client at the edges, reached through interfaces that
the business layer owns.

## Folder layout

The repository is a single Go module (monorepo). Folder names are PascalCase by
project convention; **Go package names inside them are lowercase**
(e.g. `Domain/` → `package domain`).

```
.
├── Startup/        Composition root: main.go, config (env), logger, DI wiring,
│                   router, HTTP server, migrations at boot, graceful shutdown.
├── Application/    HTTP layer ("controllers"): handlers, request/response DTOs,
│                   auth middleware, error → HTTP status mapping.
├── Actions/        Use cases that change state (commands): Login, Withdraw,
│                   Deposit, Cancel, ResolvePending.
├── Queries/        Use cases that only read: GetPlayer.
├── Domain/         Entities and value objects (Money, Currency, Bet,
│                   Transaction, statuses + transitions), domain errors, and
│                   ports (interfaces): repositories, WalletClient, Clock,
│                   IDGenerator, PasswordHasher, TokenIssuer.
│   └── domaintest/ Test doubles shared by tests (fake Wallet, in-memory repos).
├── Persistence/    PostgreSQL implementation of the repository ports,
│                   unit-of-work/locking, SQL migrations and seed.
├── Shared/         Adapters to things that are not the API itself:
│                   Wallet HTTP client, JWT, bcrypt, clock, ID generation.
└── docs/plans/     Master plan and per-phase plans.
```

## Dependency rule

```
          Startup  ──────────────► (everything: wires concrete types)
             │
       Application ──► Actions / Queries ──► Domain ◄── Persistence
                                               ▲
                                               └──────── Shared
```

- `Domain` imports **nothing** from this repository (only stdlib + `decimal`).
- `Actions` and `Queries` import only `Domain`.
- `Application` imports `Actions`, `Queries` and `Domain` (for error types/DTO mapping).
  It never imports `Persistence` or `Shared`.
- `Persistence` and `Shared` implement `Domain` ports; they never import
  `Actions`, `Queries` or `Application`.
- `Startup` is the only package that knows every concrete type.

For a .NET developer: `Domain` ≈ the core/domain project, `Actions`/`Queries` ≈
MediatR command/query handlers, `Application` ≈ controllers, `Persistence` ≈
the EF/Dapper infrastructure project, `Startup` ≈ `Program.cs`. Go has no DI
container: wiring is plain constructor calls in `Startup`.

## Request flow (Withdraw, simplified)

```
HTTP ─► Application.WithdrawHandler
          │ decode + validate DTO, read userId from JWT
          ▼
        Actions.Withdraw.Execute(ctx, cmd)
          1. lock player (per-player serialisation)
          2. idempotency lookup by providerTransactionId
               same payload → replay (200) | different → CONFLICT (409)
          3. persist WITHDRAW as PENDING (+ reserved betId)   ← committed
          4. WalletClient.Withdraw (with timeout)
          5. classify outcome:
               applied     → COMPLETED + bet PLACED  (201)
               not applied → FAILED                  (422 / 502)
               unknown     → stays PENDING           (202)
          ▼
        Application maps result/error → status code + JSON body
```

The detailed reliability reasoning lives in `DESIGN.md`.

## Data model (initial)

| Table | Key columns |
|---|---|
| `players` | `wallet_user_id BIGINT PK`, `username UNIQUE`, `password_hash`, `currency` |
| `bets` | `id BIGINT PK` (betId sent to the Wallet), `wallet_user_id`, `currency`, `status` |
| `transactions` | `id UUID PK`, `type`, `status`, `wallet_user_id`, `bet_id`, `provider_transaction_id`, `cancelled_transaction_id`, `amount NUMERIC(20,2)`, `currency`, `old_balance`, `new_balance`, `payload_hash`, `wallet_reference`, timestamps |

Constraints carry part of the correctness: `UNIQUE(provider_transaction_id)` for
WITHDRAW/DEPOSIT, `UNIQUE(cancelled_transaction_id)` for CANCEL, a partial unique
index guaranteeing a single active settlement per bet. Final schema: Phase 04.
