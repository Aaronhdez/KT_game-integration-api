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
├── Domain/         Entities and value objects (Money, Currency, IDs, Player,
│                   Bet, Transaction, statuses + transitions), the Cancel
│                   decision rules (§8.5), domain errors, and ports
│                   (interfaces): PlayerRepository, BetRepository,
│                   TransactionRepository, UnitOfWork, PlayerLocker,
│                   WalletClient, Clock, IDGenerator, PasswordHasher, TokenIssuer.
│   └── domaintest/ Test doubles shared by tests (fake Wallet, in-memory repos).
├── Persistence/    PostgreSQL (pgx) implementation of the repository ports,
│                   per-player lock + unit of work, embedded SQL migrations
│                   (goose) and the seed of the four Wallet players.
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

## Domain model (Phase 03)

| Type | Purpose | Rules enforced in `Domain` |
|---|---|---|
| `Money` | Exact amount (`shopspring/decimal`, unexported field) | ≥ 0, ≤ 2 decimals, ≤ 18 integer digits; JSON number in and out, never `float64` |
| `Currency` | `USD`, `EUR`, `KES` | exact uppercase codes only |
| `UserID`, `BetID`, `TransactionID`, `ProviderTransactionID` | Named ID types | the compiler refuses to mix them; provider ID non-empty, ≤ 128 characters |
| `Bet` | A wager, created when its withdraw completes | `PLACED → WON/LOST` (`Settle`), `PLACED → CANCELLED` (`Cancel`), `WON/LOST → PLACED` (`RevertSettlement`) |
| `Transaction` | One money movement (`WITHDRAW`, `DEPOSIT`, `CANCEL`) | born `PENDING`; `→ COMPLETED` / `→ FAILED`; `COMPLETED → CANCELLED` (not for `CANCEL`); withdraw amount > 0 |
| `DecideCancel` | The §8.5 table as a pure function | returns credit back / debit back / no Wallet call, or the domain error |

Every refused transition returns an error and changes nothing. Domain errors
wrap one of seven categories (one per API error code), which `Application`
maps to HTTP with `errors.Is`.

## Data model (Phase 04)

Migrations live in `Persistence/migrations/` (embedded in the binary, applied by
goose at startup): `00001_create_players`, `00002_create_bets`,
`00003_create_transactions`, `00004_seed_players`. A migration that has been
applied is never edited; changes go in a new file.

| Table | Columns | Constraints |
|---|---|---|
| `players` | `wallet_user_id BIGINT PK`, `username`, `password_hash` (bcrypt), `currency`, `created_at` | `UNIQUE(username)`, currency check |
| `bets` | `id BIGINT PK` (the `betId` sent to the Wallet, from sequence `bet_id_seq`), `wallet_user_id → players`, `currency`, `stake NUMERIC(20,2)`, `status`, timestamps | status/currency checks, `stake > 0` |
| `transactions` | `id UUID PK`, `type`, `status`, `wallet_user_id → players`, `bet_id` (no FK: a PENDING withdraw's bet does not exist yet), `provider_transaction_id`, `cancelled_transaction_id → transactions` (CANCEL only), `amount`, `currency`, `old_balance`, `new_balance` (NULL until completed), timestamps | type/status/currency checks, `amount >= 0`, withdraw `amount > 0`, `cancelled_transaction_id` present iff `CANCEL` |

The unique indexes carry business rules, so they hold even if Go code is wrong
or two instances race:

| Index (partial, on `transactions`) | Rule | Domain error on violation |
|---|---|---|
| `transactions_idempotency_key` `(wallet_user_id, provider_transaction_id)` where `WITHDRAW`/`DEPOSIT` | One transaction per provider ID and player (§7.1) | `ErrTransactionExists` |
| `transactions_active_settlement` `(bet_id)` where `DEPOSIT` and `PENDING`/`COMPLETED` | One active settlement per bet (§8.4) | `ErrBetAlreadySettled` |
| `transactions_single_cancel` `(cancelled_transaction_id)` where `CANCEL` | One cancel per transaction (§8.5) | `ErrAlreadyCancelled` |

## Persistence ports (Phase 04)

```
PlayerLocker.WithPlayerLock(ctx, userID, func(ctx, uow UnitOfWork) error)
   └─ UnitOfWork.Atomic(ctx, func(ctx, repos Repositories) error)   // one DB transaction
         └─ Repositories{Transactions, Bets}
PlayerRepository (FindByUsername, FindByID)                          // standalone, for login
```

- **Lock:** a PostgreSQL session advisory lock keyed by the wallet user ID, held
  on one pooled connection; every query of the operation runs on that same
  connection. It outlives the commit of the PENDING transaction, so the player
  stays locked across the Wallet call. PostgreSQL releases it if the process dies.
- **Reads are scoped by player:** every `Find…`/`Update` filters by
  `wallet_user_id`, so another player's bet or transaction is "not found".
- **Money** is written as text (`Money.String()`) and read as `amount::text` →
  `domain.ParseMoney`: exact both ways, and re-validated on every read.

## Wallet client (Phase 05)

`Shared/wallet` (`wallet.Client`, built with `wallet.NewClient(config, logger)`)
implements `domain.WalletClient` over HTTP; `Domain/domaintest.FakeWallet`
implements the same port in memory for use-case tests.

```
                 domain.WalletClient (port, in Domain)
                    ▲                         ▲
   Shared/wallet.Client              domaintest.FakeWallet
   (HTTP, production)                (in memory, tests only)
```

| File | Content |
|---|---|
| `config.go` | `Config{BaseURL, APIKey, OperationTimeout, BalanceTimeout}` |
| `dto.go` | Wallet JSON shapes (unexported); amounts and balances as `json.Number`, never `float64` |
| `classify.go` | `classifyOperation(status, code)`: the applied / not applied / unknown table; `isSuccess` |
| `client.go` | `Withdraw`, `Deposit`, `Balance`, then their helpers in call order; one log line per call |

Withdraw / Deposit result → domain outcome:

| Wallet answer | Returned | Use case will |
|---|---|---|
| `2xx` with a valid balance | new balance, `nil` | `COMPLETED` |
| `2xx` with an unusable body, `409 DuplicatedTransaction` | `ErrWalletAppliedBalanceUnknown` | `COMPLETED` |
| `400 InsufficientBalance` | `ErrInsufficientFunds` | `FAILED`, 422 |
| other `400`, `401` | `ErrWalletRejected` | `FAILED`, 502 |
| `5xx`, other `4xx`, timeout, dropped connection | `ErrWalletOutcomeUnknown` | `PENDING`, 202 |

`Balance` returns `ErrWalletFailure` (502) or `ErrWalletTimeout` (504), after at
most one retry. The client is not wired into `Startup` yet: its first consumer is
`GET /player` (Phase 07).

## Authentication (Phase 06)

```
POST /api/v1/auth/login
  application.LoginController      decode JSON (decodeJSON), map errors (writeDomainError)
    actions.LoginCommandHandler    empty fields → 400; unknown user → dummy bcrypt + 401
      domain.PlayerRepository      ← persistence.PlayerRepository
      domain.PasswordHasher        ← password.Bcrypt          (Shared/password)
      domain.TokenIssuer           ← token.JWT                (Shared/token)
      domain.Clock                 ← clock.System             (Shared/clock)

Protected endpoints (from Phase 07)
  application.RequireAuth(verifier, clock)(controller)
      domain.TokenVerifier         ← token.JWT
    controller reads the player with application.UserIDFromContext(r.Context())
```

| Package / file | Content |
|---|---|
| `Domain/ports_auth.go` | `PasswordHasher`, `TokenIssuer` (+ `IssuedToken`), `TokenVerifier` |
| `Shared/password` | `Bcrypt.Verify`: mismatch or > 72 bytes → `ErrInvalidCredentials`; malformed hash → other error |
| `Shared/token` | `JWT.Issue` / `JWT.Verify` (HS256, `sub`/`iat`/`exp`); every failure → `ErrInvalidToken` |
| `Shared/clock` | `System.Now()` in UTC |
| `Actions/login.go` | `LoginCommandHandler`, `LoginCommand`, `LoginResult`, dummy hash |
| `Application/login.go` | `LoginController` and its request/response DTOs |
| `Application/auth.go` | `RequireAuth` middleware, `UserIDFromContext` |
| `Application/errors.go` | `writeDomainError`: category → status + code; unknown → `500 INTERNAL_ERROR` |
| `Application/request.go` | `decodeJSON`: one JSON object, max 1 MiB, unknown fields ignored |
| `Domain/domaintest` | `FixedClock`, `FakePlayerRepository`, `FakePasswordHasher` (+ `FakeHash`), `FakeTokens` |
| `Startup/router.go`, `main.go` | `Handlers` struct and `newHandlers`: the wiring of every concrete type |

`Domain/ports.go` was split by topic into `ports_auth.go`, `ports_persistence.go`,
`ports_wallet.go` and `ports_system.go` (same package, no declaration changed).

### Naming convention

| Layer | Name | Example |
|---|---|---|
| `Application` | `XxxController`, one per endpoint, implements `http.Handler` | `LoginController` |
| `Actions` | `XxxCommandHandler` + `XxxCommand` / `XxxResult` | `LoginCommandHandler` |
| `Queries` | `XxxQueryHandler` | `GetPlayerQueryHandler` (Phase 07) |

"Handler" is reserved for use cases (the MediatR analogy) and "Controller" for
HTTP, so the two layers never share a name.
