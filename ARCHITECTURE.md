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
          0. validate input (400) and find the player (404)
          1. lock player (per-player serialisation)
          2. idempotency lookup by providerTransactionId
               same payload → replay (200 / 202) | different → CONFLICT (409)
          3. read the balance from the Wallet (oldBalance)
          4. persist WITHDRAW as PENDING (+ reserved betId)   ← committed
          5. WalletClient.Withdraw (with timeout)
          6. classify outcome:
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
`00003_create_transactions`, `00004_seed_players`,
`00005_global_provider_transaction_id` (Phase 08). A migration that has been
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
| `transactions_idempotency_key` `(provider_transaction_id)` where `WITHDRAW`/`DEPOSIT` | One transaction per provider ID across the whole system (§6.1, §7.1; per player until Phase 08) | `ErrTransactionExists` |
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
| `client.go` | `Config{BaseURL, APIKey, OperationTimeout, BalanceTimeout}`, then `Withdraw`, `Deposit`, `Balance` and their helpers in call order; one log line per call |
| `dto.go` | Wallet JSON shapes (unexported); amounts and balances as `json.Number`, never `float64` |
| `classify.go` | `classifyOperation(status, code)`: the applied / not applied / unknown table; `isSuccess` |

Withdraw / Deposit result → domain outcome:

| Wallet answer | Returned | Use case will |
|---|---|---|
| `2xx` with a valid balance | new balance, `nil` | `COMPLETED` |
| `2xx` with an unusable body, `409 DuplicatedTransaction` | `ErrWalletAppliedBalanceUnknown` | `COMPLETED` |
| `400 InsufficientBalance` | `ErrInsufficientFunds` | `FAILED`, 422 |
| other `400`, `401` | `ErrWalletRejected` | `FAILED`, 502 |
| `5xx`, other `4xx`, timeout, dropped connection | `ErrWalletOutcomeUnknown` | `PENDING`, 202 |

`Balance` returns `ErrWalletFailure` (502) or `ErrWalletTimeout` (504), after at
most one retry. `Startup` builds one `wallet.Client` (shared by every request,
safe for concurrent use); its first consumer is `GET /player` (Phase 07).

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

## Player information (Phase 07)

```
GET /api/v1/player
  application.RequireAuth          no / invalid token → 401, the Wallet is never called
    application.PlayerController   user ID from the context, Cache-Control: no-store
      queries.GetPlayerQueryHandler
        domain.PlayerRepository    ← persistence.PlayerRepository   unknown → 404
        domain.WalletClient        ← wallet.Client                  502 / 504; currency mismatch → 502
```

| Package / file | Content |
|---|---|
| `Queries/get_player.go` | `GetPlayerQueryHandler`, `GetPlayerQuery{UserID}`, `PlayerResult{UserID, Balance, Currency}` |
| `Application/player.go` | `PlayerController` and its `playerResponse` DTO (`userId`, `balance`, `currency`, in that order) |
| `Startup/main.go`, `router.go` | `wallet.NewClient` from the config; `Handlers.Player` = `RequireAuth(...)(PlayerController)`; route `GET /api/v1/player` |

No per-player lock: the read changes nothing (see `DESIGN.md`).

## Withdraw (Phase 08)

```
POST /api/v1/withdraw
  application.RequireAuth           no / invalid token → 401
    application.WithdrawController  decode (missing amount → 400), user ID from the context
      actions.WithdrawCommandHandler
        domain.PlayerRepository     ← persistence.PlayerRepository
        domain.PlayerLocker         ← persistence.PlayerLocker       advisory lock + UnitOfWork
        domain.WalletClient         ← wallet.Client                  balance read, then withdraw
        domain.IDGenerator          ← id.UUID                        (Shared/id)
        domain.Clock                ← clock.System
    application.writeTransactionResult   §7.4 body; 202 PENDING / 200 replay / 201
```

| Package / file | Content |
|---|---|
| `Actions/transaction_result.go` | `TransactionResult{Transaction, BetStatus *BetStatus, Replayed}`, shared by Withdraw, Deposit and Cancel |
| `Actions/withdraw.go` | `WithdrawCommandHandler`: validation, `withdrawLocked` (replay → old balance → `PENDING` → Wallet → outcome), `replayIfProcessed`, `complete` (the shared steps are in `operation`, see "Shared operation helpers") |
| `Application/transaction_response.go` | `transactionResponse` (§7.4) and `writeTransactionResult` (§7.2 status) |
| `Application/withdraw.go` | `WithdrawController` and its `withdrawRequest` DTO (`Amount *domain.Money`) |
| `Shared/id` | `UUID`: `domain.IDGenerator` with the standard library's `uuid.NewV4` |
| `Domain/domaintest/store.go` | `FakeStore`: in-memory transactions and bets, `PlayerLocker` (one lock per player), `UnitOfWork` (copy, commit on `nil`), same unique rules as PostgreSQL |
| `Domain/domaintest/ids.go` | `FakeIDGenerator` and `FakeTransactionID(n)` |
| `Startup/main.go`, `router.go` | `Handlers.Withdraw` = `RequireAuth(...)(WithdrawController)`; route `POST /api/v1/withdraw` |

## Deposit (Phase 09)

Same chain as the withdraw (`RequireAuth` → `DepositController` →
`DepositCommandHandler` with the same ports and adapters, including the same
`PlayerLocker`, so a withdraw and a deposit of one player are serialised too).

| Package / file | Content |
|---|---|
| `Actions/deposit.go` | `DepositCommandHandler`: validation, `depositLocked` (replay → `findBetToSettle` → old balance → zero amount: `settleWithoutWallet` / otherwise `PENDING` → Wallet → outcome), `replayIfProcessed`, `replayAfterDuplicateInsert`, `complete` |
| `Application/deposit.go` | `DepositController` and its `depositRequest` DTO (`Amount *domain.Money`) |
| `Startup/main.go`, `router.go` | `Handlers.Deposit` = `RequireAuth(...)(DepositController)`; route `POST /api/v1/deposit` |

No schema change: "one active settlement per bet" is the
`transactions_active_settlement` index of Phase 04, mapped to
`ErrBetAlreadySettled`.

## Cancel (Phase 10)

Same chain again (`RequireAuth` → `CancelController` → `CancelCommandHandler`,
same ports, adapters and `PlayerLocker`).

| Package / file | Content |
|---|---|
| `Actions/cancel.go` | `CancelCommandHandler`: validation, `cancelLocked` (`replayIfCancelled` → `findWhatToRevert` (lookup + `DecideCancel` + pending-deposit check) → old balance → zero deposit: `cancelWithoutWallet` / otherwise `PENDING` → `sendCompensation` → outcome), `replayAfterDuplicateInsert`, `applyCancel` (cancel `COMPLETED`, original `CANCELLED`, bet reverted, in memory), `complete` |
| `Actions/deposit.go` | `checkNoCancelInProgress`: a deposit is refused while its withdraw has a `PENDING` cancel (a `COMPLETED` one has already made the bet `CANCELLED`) |
| `Domain/ports_persistence.go` | `TransactionRepository.FindActiveDeposit(userID, betID)`: the `PENDING`/`COMPLETED` deposit of a bet (same condition as the active-settlement index) |
| `Application/cancel.go` | `CancelController` and its `cancelRequest` DTO (`providerTransactionId` only) |
| `Startup/main.go`, `router.go` | `Handlers.Cancel` = `RequireAuth(...)(CancelController)`; route `POST /api/v1/cancel` |

No schema change: "one cancel per transaction" is the
`transactions_single_cancel` index of Phase 04, mapped to
`ErrAlreadyCancelled`.

## Shared operation helpers (Phase 10b)

The three money operations embed one `operation` struct instead of
repeating the same fields and steps:

```go
type WithdrawCommandHandler struct { operation }   // same for Deposit and Cancel
```

| Package / file | Content |
|---|---|
| `Actions/operation.go` | `operation`: the six dependencies plus `name` (log message and error prefix), and the steps that are identical in the three: `readOldBalance` (balance read + currency check → `ErrWalletFailure`), `outcomeContext` (writes after the Wallet call: not cancelled with the request, `outcomeWriteTimeout`), `fail` (store `FAILED`, return the Wallet's error), `leavePending` (return the stored `PENDING`), `log` (one line per operation, with `cancelled_transaction_id` for a cancel) |
| `Actions/wallet_outcome.go` | `classifyWalletResult(err)` → `walletApplied` / `walletAppliedBalanceUnknown` / `walletNotApplied` / `walletOutcomeUnknown`: the §9.3 rule, written once; each handler's `switch` decides what to do with each outcome |

What stays in each handler is what differs: `Execute` (its validation),
`complete` (create / settle / revert a bet), the replay rules, the business
checks, and the derived balance when the Wallet applied an operation without
reporting it. Embedding promotes the fields and methods (`handler.wallet`,
`handler.fail(...)`); it is composition, not inheritance: `operation` knows
nothing about the handler that contains it.

### Naming convention

| Layer | Name | Example |
|---|---|---|
| `Application` | `XxxController`, one per endpoint, implements `http.Handler` | `LoginController` |
| `Actions` | `XxxCommandHandler` + `XxxCommand` / `XxxResult` | `LoginCommandHandler` |
| `Queries` | `XxxQueryHandler` + `XxxQuery` / `XxxResult` | `GetPlayerQueryHandler` + `GetPlayerQuery` / `PlayerResult` |

"Handler" is reserved for use cases (the MediatR analogy) and "Controller" for
HTTP, so the two layers never share a name.
