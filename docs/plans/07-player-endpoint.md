# 07 — Player endpoint

## Goal

Give an authenticated player their account data and their **live** balance. At
the end of the phase:

- `GET /api/v1/player` is the first protected route: it runs behind
  `RequireAuth` (Phase 06) and takes the player only from the token;
- the balance is read from the Wallet **on every request** (the Wallet is the
  source of truth) and never cached, neither by us nor by an HTTP cache
  (`Cache-Control: no-store`);
- a Wallet failure is a `502 WALLET_ERROR` and a Wallet that does not answer in
  time is a `504 WALLET_TIMEOUT` — never a zero or stale balance;
- the Wallet client of Phase 05 is wired into `Startup` for the first time;
- `Queries/` gets its first use case, `GetPlayerQueryHandler`.

No new port, no new error, no new configuration, no new dependency.

## Requirements covered (assessment sections)

- **8.2** — player info with the current balance, no stale balance.
- **9.5** — bounded waiting: the balance read is bounded by
  `WALLET_BALANCE_TIMEOUT` with at most one retry (Phase 05), well under the
  server's `WriteTimeout` (30 s).
- **7.3** — `502 WALLET_ERROR`, `504 WALLET_TIMEOUT`, `401 UNAUTHORIZED` with the
  `{code, message}` body (mapping from Phase 06).
- **9.7** — one log line per Wallet attempt (already in the client), nothing
  secret logged.
- AGENTS.md *Security* — the player comes from the token, never from the
  request; there is no way to ask for another player's data.

## Design decisions (and alternatives rejected)

| Decision | Why | Rejected alternative |
|---|---|---|
| **Response** exactly as §8.2: `{"userId", "balance", "currency"}`, in that order; `userId` a JSON integer, `balance` a JSON number with two decimals (`domain.Money`'s `MarshalJSON`) | The assessment's example is `{ "userId": 34633089486, "balance": 5000.00, "currency": "USD" }`. `int64` IDs fit in a JSON number without loss (they are < 2⁵³). | Adding `username`: not asked for. `balance` as a string (the Wallet's own format): §7 says amounts are JSON numbers. |
| The query reads the **player from the database first**, then the balance from the Wallet | It confirms the token's player exists (unknown → `404`, before any Wallet call) and gives the account currency to check the Wallet's answer against. | Only the Wallet: for an unknown user it answers a `200` with an empty currency, and there would be nothing to cross-check. |
| **Never cached**: one Wallet read per request, `Cache-Control: no-store` on the response | §8.2 "no stale balance". The header stops proxies and browsers from reusing an old balance. | An in-memory cache with a short TTL: the balance could be wrong right after a withdraw. |
| **No per-player lock** for this read | It changes nothing, so it cannot break consistency; locking would make `GET /player` wait behind a slow withdraw (up to `WALLET_OPERATION_TIMEOUT`). A balance read while an operation is in flight shows the Wallet's value at that moment, which is still the truth. | Taking `PlayerLocker` to show a "settled" balance: slower and no requirement asks for it. |
| **Currency check**: if the Wallet's currency is not the player's currency → `ErrWalletFailure` (`502`) | Our data and the Wallet disagree; returning a balance labelled with the wrong currency is worse than an error. Wrapped with `%v` for the mismatch detail, `%w` for `ErrWalletFailure` (same rule as Phase 05). | Returning the Wallet's currency silently, or `ErrCurrencyMismatch` (a `400`: the client did nothing wrong). |
| **Player in the token but not in the database** → `ErrPlayerNotFound` → `404 NOT_FOUND` | Only possible with a token signed by our secret for a player that no longer exists; the existing mapping covers it with no new code. | `401`: would need a special case in the mapper or the query. |
| **No retry in the query**: the client already retries a balance read once | Retries live in one place; worst case ≈ 2 × 10 s + 200 ms. | A second retry loop in the use case (up to 4 Wallet calls per request). |
| The controller depends on the concrete `*queries.GetPlayerQueryHandler` | Same pattern as `LoginController` → `*actions.LoginCommandHandler`; tests build the real query with fakes from `domaintest`. | An interface in `Application` for one implementation. |
| **No user in the request context** inside the controller → `500 INTERNAL_ERROR`, logged | Only happens if the route was registered without `RequireAuth` — a wiring bug, not a client error. | `401`: would hide the bug behind a normal-looking answer. |

## Files to create / modify

| File | Change |
|---|---|
| `Queries/get_player.go` | `GetPlayerQueryHandler`, `NewGetPlayerQueryHandler(players, wallet)`, `GetPlayerQuery{UserID}`, `PlayerResult{UserID, Balance, Currency}` |
| `Queries/get_player_test.go` | Use case tests with `domaintest.FakePlayerRepository` and `domaintest.FakeWallet` |
| `Application/player.go` | `PlayerController`, `NewPlayerController(query, logger)`, `playerResponse` DTO |
| `Application/player_test.go` | Controller tests with `httptest`, including through `RequireAuth` |
| `Startup/main.go` | `newHandlers`: build `wallet.NewClient`, the query, the controller wrapped in `RequireAuth` |
| `Startup/router.go`, `router_test.go` | `Handlers.Player`, route `GET /api/v1/player`, router tests |
| `README.md`, `DESIGN.md`, `ARCHITECTURE.md`, `docs/plans/00-general-plan.md` | Endpoint, decisions, layout, phase row |

## Commit sequence

**Working mode** — same as Phase 06 as built: test-first in small increments;
Claude writes the tests first and checks them against a throw-away reference
implementation outside the repo, the developer writes the production code from
step-by-step instructions and Claude reviews until green. Proposed split (open
question 1):

| Steps | Who writes |
|---|---|
| 1, 4, 5 and the tests of 2, 3 | Claude writes and explains |
| **2, 3 (production code)** | **Developer**, from step-by-step instructions; Claude reviews |

For every step: `gofmt -l .` empty, `go vet ./...`, `go build ./...`,
`go test ./...` green; Claude proposes the commit message.

---

1. **`docs(plan): add phase 07 player endpoint plan`** — this file.

2. **`feat(queries): add get player query`**
   - `Queries/get_player.go`: `Execute(ctx, GetPlayerQuery) (PlayerResult, error)`
     → `players.FindByID`, then `wallet.Balance`, then the currency check.
     Errors wrapped as `fmt.Errorf("get player: %w", err)`.
   - Tests (`TestGetPlayer_…`):
     - known player → `PlayerResult` with the user ID, the Wallet balance and
       the currency (e.g. `750.50 KES`);
     - the Wallet is asked for the user ID of the query (check `Calls()`);
     - two executions → two `Balance` calls (nothing cached);
     - unknown player → `ErrPlayerNotFound` and **no** Wallet call;
     - repository failure → error that is not `ErrNotFound`, no Wallet call;
     - Wallet failure (`Next(Reject)`) → `ErrWalletFailure`;
     - Wallet timeout (`Next(TimeOut)`) → `ErrWalletTimeout`;
     - Wallet currency ≠ player currency → `ErrWalletFailure`.
   - *Go concepts:* first file of a new package (`package queries` inside the
     PascalCase folder, imported with an alias like the other layers); returning
     a struct by value plus an error (≈ a result record, no `null`); `%v` vs `%w`
     when only part of an error should be matchable with `errors.Is`.

3. **`feat(application): add player endpoint`**
   - `Application/player.go`: `UserIDFromContext` → query → `200` with
     `Cache-Control: no-store`; errors through `writeDomainError`.
   - Tests (`TestPlayerController_…`):
     - `200` with the exact JSON body
       `{"userId":34616761765,"balance":750.50,"currency":"KES"}` (§8.2 shape);
     - `Cache-Control: no-store` and `Content-Type: application/json`;
     - Wallet failure → `502 WALLET_ERROR`; Wallet timeout → `504 WALLET_TIMEOUT`;
     - player not in the repository → `404 NOT_FOUND`;
     - no user in the context → `500 INTERNAL_ERROR`, logged;
     - through `RequireAuth(domaintest.FakeTokens, FixedClock)`: no token →
       `401` and **no** Wallet call; valid token → `200` for that token's player.
   - *Go concepts:* `encoding/json` calls `Money.MarshalJSON` by itself because
     `Money` implements `json.Marshaler` (≈ a `JsonConverter` attached to the
     type); wrapping a handler with a middleware at construction time.

4. **`feat(startup): wire player endpoint and wallet client`**
   - `newHandlers`: `wallet.NewClient(wallet.Config{BaseURL, APIKey,
     OperationTimeout, BalanceTimeout}, logger)`, `queries.NewGetPlayerQueryHandler`,
     `application.RequireAuth(tokens, clock.System{})(application.NewPlayerController(...))`.
     The same `token.JWT` value issues (login) and verifies (middleware).
   - `Handlers.Player`, route `GET /api/v1/player`; router tests: `GET` reaches
     the player handler, `POST /api/v1/player` → `404`.
   - Manual check (see Test plan).
   - *Go concepts:* one `*wallet.Client` (and its `http.Client` connection pool)
     shared by every request — safe for concurrent use, ≈ a singleton
     `HttpClient` via `IHttpClientFactory`.

5. **`docs(player): document player endpoint`**
   - `README.md` (endpoint, response example, curl, status codes),
     `DESIGN.md` (never cached, no lock for reads, currency mismatch → 502,
     unknown player → 404), `ARCHITECTURE.md` (`Queries` section, Wallet wired),
     `00-general-plan.md` (phase 07 row), and an "As built" section here.

## Test plan

- **Unit (`go test ./...`, no Docker):** query with fakes; controller with
  `httptest.NewRecorder`, alone and behind `RequireAuth`; router. The Wallet
  client's balance classification and retry are already covered (Phase 05).
- **Integration:** none new (`PlayerRepository.FindByID` is covered by Phase 04).
- **Manual (`curl`)**, with the local PostgreSQL, the Wallet mock on
  `localhost:8000` and `WALLET_BASE_URL=http://localhost:8000`:
  ```bash
  TOKEN=$(curl -s -X POST localhost:8080/api/v1/auth/login \
       -H 'Content-Type: application/json' \
       -d '{"username":"player_kes","password":"kes-pass-2026"}' | jq -r .token)
  curl -i localhost:8080/api/v1/player -H "Authorization: Bearer $TOKEN"   # 200, live balance
  curl -i localhost:8080/api/v1/player                                     # 401
  # repeat a few times: some answers are slow, some 502/504 (unreliable Wallet)
  # stop the Wallet container → 502 WALLET_ERROR
  ```
  Check in the log: one `wallet call` line per attempt, no API key or token.
- **Not covered:** `go test -race` (no C compiler on Windows; CI in Phase 13).

## Open questions

1. ~~**Who writes which step?**~~ **Resolved:** as proposed — the developer
   writes the production code of 2 and 3; Claude writes the tests, the wiring
   (4) and the docs (5).
2. ~~**Exact response fields of §8.2.**~~ **Resolved:** `{"userId", "balance",
   "currency"}`, as in the assessment's example (no `username`).
3. ~~**Unknown player with a valid token.**~~ **Resolved:** `404 NOT_FOUND`
   through the existing mapping (recorded in `DESIGN.md` in step 5).

## As built — deviations from this plan

| Planned | Built | Why |
|---|---|---|
| — | Before the phase: `fix(auth): add tokenType to login response` | Reading §8.1 for this phase showed the login response lacked `"tokenType": "Bearer"`; the Phase 06 plan had recorded only `token` and `expiresAt`. |
| Developer writes the production code of 2 and 3 | The developer wrote first drafts; Claude reviewed them and, at the developer's request, applied the corrections (package name, domain types instead of `string`/`int64` conversions, wallet call, currency check, error wrapping) | The review focused on value conversions: `string(int64)` yields a rune, not digits, and `Money` cannot become an `int64` without losing the decimals. |
| Commit scopes `queries` / `application` | `feat(player): …` for steps 2 and 3 | The developer's choice. |
| Router tests with one stub | A `statusStub(status)` helper; each route's stub answers its own unusual status | Two stubs with the same shape. |
| Manual check on `:8080` | Run on `HTTP_PORT=8081` (an older build occupied `:8080`) | — |
| Manual `curl` results | `player_kes`: 5 × `200 {"userId":34616761765,"balance":750.50,"currency":"KES"}`, 1 × `502` (first attempt timed out at 10 s, retry dropped; 15.5 s total); `player_eur`: `9000000000.00` exact; no token → `401`; `Cache-Control: no-store`; `POST` → `404`; log: one `wallet call` line per attempt, no API key, secret, token or password | The Wallet dropped 3 of 11 balance attempts (more than the Phase 01 sample suggested), so the client's single retry matters in practice. |

## Definition of done

- `Queries` imports only stdlib and `Domain`; `Application` does not import
  `Shared` or `Persistence`; `domaintest` is not imported by production code
  (`go list -f '{{.Imports}}' ./...`).
- `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` green without Docker.
- Every case listed in steps 2 and 3 has a test, including "two requests → two
  Wallet reads" and "no token → no Wallet call".
- A Wallet failure or timeout never produces a `200`.
- Manual `curl` check of step 4 done against the real Wallet mock.
- `README.md`, `DESIGN.md`, `ARCHITECTURE.md`, `00-general-plan.md` updated.
- The developer can explain each Go concept listed in steps 2–4.
