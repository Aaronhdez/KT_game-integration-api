# 06 — Authentication

## Goal

Let a seeded player log in and prove who they are on every later request. At the
end of the phase:

- `POST /api/v1/auth/login` takes `{username, password}`, checks the password
  against the bcrypt hash stored by the seed (Phase 04) and returns a signed
  JWT plus its expiry;
- an unknown username and a wrong password produce **exactly the same** `401`
  response (body and, as far as reasonable, timing);
- a reusable middleware verifies `Authorization: Bearer <JWT>` and puts the
  player's wallet user ID in the request context, so the handlers of Phases
  07–10 never read the user from the body;
- `JWT_SECRET` and `JWT_TTL` are read from the environment and validated at
  startup; the secret, the token and passwords are never logged or returned;
- domain errors get their single mapping to HTTP status + `{code, message}`,
  which every later endpoint reuses.

No protected endpoint exists yet: the middleware is unit-tested here and first
attached to a route in Phase 07 (`GET /api/v1/player`).

## Requirements covered (assessment sections)

- **8.1** — login against the seeded players, bcrypt hashes, JWT signed with a
  secret from the environment, token lifetime, `expiresAt` in the response.
- **7.3** — error body `{code, message}` with `INVALID_REQUEST` / `UNAUTHORIZED`;
  the domain error → HTTP mapping used by every endpoint.
- **9.7** — no token, password, hash or secret in logs (tested).
- **10.1** — env configuration, unit tests without Docker.
- AGENTS.md *Security* — same error for unknown user and wrong password;
  the authenticated user comes only from the token (prepares "a player can only
  act on their own bets" for Phases 07–10).

## Design decisions (and alternatives rejected)

| Decision | Why | Rejected alternative |
|---|---|---|
| **New dependencies:** `github.com/golang-jwt/jwt/v5` and `golang.org/x/crypto/bcrypt` | Already chosen in the master plan. Signing/verifying JWTs and bcrypt are security code we should not write ourselves; both are the de-facto standard Go libraries. | Hand-rolled HMAC JWT (easy to get subtly wrong: `alg` confusion, constant-time compare); `lestrrat-go/jwx` (bigger API to explain). |
| Three small packages in `Shared/`: **`Shared/password`** (`password.Bcrypt`, implements `domain.PasswordHasher`), **`Shared/token`** (`token.JWT`, implements `domain.TokenIssuer` and `domain.TokenVerifier`), **`Shared/clock`** (`clock.System`, implements `domain.Clock`) | One concern per package, named by what it provides, as `ARCHITECTURE.md` lists ("JWT, bcrypt, clock"). | One `Shared/auth` package mixing all three; a package named `jwt` (collides with the library's own package name in imports). |
| **New port `domain.TokenVerifier`**: `Verify(token string, now time.Time) (UserID, error)`; every failure returns **`ErrInvalidToken`** (wraps `ErrUnauthorized`) | Interfaces live in `Domain`, where consumers find them; Phase 03 deferred verification to this phase. `now` as a parameter mirrors `TokenIssuer.Issue`, so expiry is testable with a fixed time. | The middleware importing `Shared/token` directly (`Application` must not import `Shared`). |
| JWT: **HS256**, claims **`sub`** (wallet user ID as a decimal string), **`iat`**, **`exp`**; built with `jwt.RegisteredClaims` | §8.1 asks for a secret-signed token whose subject is the player. `RegisteredClaims.Subject` is a `string`, so the 11-digit user ID never goes through `float64` (which `jwt.MapClaims` would do when decoding JSON numbers). | `MapClaims` with a numeric `userId` claim; extra claims (`iss`, `aud`, username) nobody checks. |
| Verification is strict: `jwt.WithValidMethods([]string{"HS256"})`, `jwt.WithExpirationRequired()`, `jwt.WithTimeFunc(now)`, **no leeway**; `sub` must parse as a positive `int64` | Rejects `alg: none` and algorithm-confusion tokens, tokens without `exp`, and garbage subjects. A single API instance signs and verifies with the same clock, so no clock-skew leeway is needed. | Trusting the `alg` header; accepting tokens without `exp`. |
| **`JWT_SECRET` required, at least 32 bytes**; **`JWT_TTL`** positive duration, default **`1h`** | RFC 7518 §3.2: an HS256 key should be at least 256 bits. Failing at startup is better than signing with a weak or empty key. The `.env.example` value (33 characters) already passes. | No minimum (an empty or `"secret"` key would be accepted silently). |
| **No refresh tokens, no revocation, no logout** | Not asked by §8.1; a short TTL limits the damage of a leaked token. Recorded as a future improvement in `DESIGN.md`. | A token blacklist table. |
| Login use case **`actions.Login`** (`NewLogin(players, hasher, tokens, clock)`, `Execute(ctx, LoginCommand) (LoginResult, error)`) in `Actions/` | `ARCHITECTURE.md` places Login among the Actions; it orchestrates four ports and owns the "same error" rule, so the rule is unit-testable without HTTP. | Doing it all in the HTTP handler (rule hidden in `Application`, harder to test). |
| `ErrPlayerNotFound` **and** a password mismatch both become **`ErrInvalidCredentials`**; any other error (DB down, unusable hash, signing failure) is returned wrapped and becomes `500` | §8.1 / AGENTS.md: the client must not learn which usernames exist. A DB failure is not "wrong password". | Returning `404` for an unknown user. |
| **Equal timing:** when the username is unknown, the use case still calls `hasher.Verify` against a fixed **dummy bcrypt hash** (same cost as the seed) and discards the result | Without it an unknown username answers in ~1 ms and a known one in ~60 ms: the usernames leak through timing even if the body is identical. One extra line, easy to explain. See open question 2. | Skipping it (identical bodies but measurable enumeration). |
| Empty `username` or `password` → **`ErrMissingCredentials`** (wraps `ErrInvalidInput`, `400`) | A request without credentials is malformed, not a failed login. No other format rule on username (the DB match is exact and case-sensitive). | `401` for empty fields; trimming or lower-casing the username. |
| `password.Bcrypt.Verify`: `bcrypt.ErrMismatchedHashAndPassword` → `ErrInvalidCredentials`; a password longer than **72 bytes** → `ErrInvalidCredentials` without calling bcrypt; any other bcrypt error (malformed hash) → wrapped error (`500`) | Matches the existing port contract. bcrypt only uses the first 72 bytes (and newer `x/crypto` versions reject longer input); no seeded password is that long, so it can never match. | Truncating silently. |
| **One error mapper** in `Application`: `writeDomainError(w, logger, err)` uses `errors.Is` on the seven categories → status + code; the response `message` is a fixed text per category, never `err.Error()`; anything unrecognised → **`500 {"code":"INTERNAL_ERROR","message":"internal error"}`**, logged at `error` with the wrapped error | Phase 03 designed the categories for exactly this. Fixed messages never leak internals (SQL, hashes). The assessment has no code for unexpected failures, so we add one and document it. See open question 3. | A per-handler `switch`; echoing `err.Error()` to the client. |
| JSON body decoding helper `decodeJSON(w, r, dst)` in `Application`: `http.MaxBytesReader` (1 MiB), rejects malformed JSON, an empty body and trailing data after the object; **unknown fields are ignored** | Reused by withdraw/deposit/cancel. A size limit stops a client from making us read an unbounded body. Ignoring unknown fields matches ASP.NET's default and the providers' habit of sending extra fields. | `DisallowUnknownFields` (rejects harmless extra fields); no size limit. |
| Login response **`200 {"token": "<jwt>", "expiresAt": "2026-11-07T18:00:00Z"}`** (RFC 3339, UTC) with **`Cache-Control: no-store`** | `expiresAt` is required by §8.1 (Phase 03 notes). `no-store` stops proxies/browsers from caching a credential (RFC 6749 §5.1 practice). See open question 4 for the exact field names. | `expiresIn` seconds only; letting the response be cached. |
| Auth middleware **`RequireAuth(verifier, clock)`** returns `func(http.Handler) http.Handler`; accepts only `Authorization: Bearer <token>` (scheme case-insensitive, one space); missing header, wrong scheme, empty or invalid token → the **same** `401 UNAUTHORIZED` body plus `WWW-Authenticate: Bearer`; on success stores the `domain.UserID` in the context under an unexported key; **`UserIDFromContext(ctx) (domain.UserID, bool)`** reads it | Handlers get the user only from the verified token. One response for every failure gives an attacker nothing to probe. An unexported key type means no other package can overwrite the value. `WWW-Authenticate` is what RFC 6750 asks of a `401`. | Reading `userId` from the request body; different messages per failure ("expired", "bad signature"). |
| The middleware does **not** check that the player still exists in the DB | Only we sign tokens and the four players are fixed. Use cases that need the player (Phase 07+) call `PlayerRepository.FindByID` anyway. | A DB lookup on every request. |
| Logging: no new log lines for login. The request logger already records `401`/`200` with the path only; `500`s are logged by the error mapper. The token, password, hash and secret never appear (tests assert it) | Keeps the log useful without ever touching credentials. Logging the submitted username was rejected because users sometimes type their password into the username field. | Logging failed usernames. |
| **Fakes in `Domain/domaintest`** for the use-case and handler tests: `FixedClock`, `FakePlayerRepository` (in memory), `FakePasswordHasher` (plain comparison, records which hashes it verified), `FakeTokenIssuer` (predictable token, optional error) | Phase 04 deferred in-memory fakes to their first consumer — this phase. Real bcrypt and JWT are already covered by the tests of `Shared/password` and `Shared/token`. | Real bcrypt in use-case tests (slow, and cannot assert that the dummy hash was used). |
| Handler tests use the **real `actions.Login`** with the fakes | `Application` may import `Actions`; no extra interface just for tests, and the tests exercise the real rule end to end at HTTP level. | An `Application`-side interface for the use case. |
| `NewRouter(logger, handlers Handlers)` where `Handlers` (in `Startup`) holds the HTTP handlers, starting with `Login http.Handler` | The router signature stays stable as Phases 07–10 add fields; router tests pass simple stub handlers. | A growing positional parameter list. |

### Login flow

```
POST /api/v1/auth/login {username, password}
  Application.LoginHandler
    decodeJSON (400 on malformed body)
    actions.Login.Execute
      empty field?                   → ErrMissingCredentials   (400)
      players.FindByUsername
        ErrPlayerNotFound            → hasher.Verify(dummyHash) → ErrInvalidCredentials (401)
        other error                  → wrapped                  (500)
      hasher.Verify(player.PasswordHash, password)
        ErrInvalidCredentials        → ErrInvalidCredentials    (401)
        other error                  → wrapped                  (500)
      tokens.Issue(player.UserID, clock.Now())
    200 {token, expiresAt} + Cache-Control: no-store
```

### Ports sketch (`Domain/ports.go`, additions)

```go
// TokenVerifier checks a token produced by TokenIssuer and returns its subject.
// Every failure (bad signature, expired, malformed, wrong algorithm, bad
// subject) returns ErrInvalidToken.
type TokenVerifier interface {
	Verify(token string, now time.Time) (UserID, error)
}
```

```go
// Domain/errors.go
ErrMissingCredentials = fmt.Errorf("%w: username and password are required", ErrInvalidInput)
ErrInvalidToken       = fmt.Errorf("%w: invalid token", ErrUnauthorized)
```

### Error mapping (`Application/errors.go`)

| Category (`errors.Is`) | Status | Code | Message (fixed) |
|---|---|---|---|
| `ErrInvalidInput` | 400 | `INVALID_REQUEST` | `invalid request` (or sentinel text, see below) |
| `ErrUnauthorized` | 401 | `UNAUTHORIZED` | `invalid credentials or token` |
| `ErrNotFound` | 404 | `NOT_FOUND` | `resource not found` |
| `ErrConflict` | 409 | `CONFLICT` | `conflict` (or sentinel text, see below) |
| `ErrInsufficientFunds` | 422 | `INSUFFICIENT_FUNDS` | `insufficient funds` |
| `ErrWalletFailure` | 502 | `WALLET_ERROR` | `wallet error` |
| `ErrWalletTimeout` | 504 | `WALLET_TIMEOUT` | `wallet did not answer in time` |
| anything else | 500 | `INTERNAL_ERROR` | `internal error` |

For `400`/`409` the domain error text (e.g. `invalid input: username and
password are required`) is useful to the game provider and contains no secrets,
because domain sentinels never carry user data. Proposal: for these two
categories return the **outermost domain sentinel's text**, for all others the
fixed message. Decided in open question 3.

## Files to create / modify

| File | Content |
|---|---|
| `go.mod`, `go.sum` | `golang-jwt/jwt/v5`, `golang.org/x/crypto`. |
| `Domain/ports.go` | `TokenVerifier`. |
| `Domain/errors.go`, `errors_test.go` | `ErrMissingCredentials`, `ErrInvalidToken` (+ category tests). |
| `Startup/config.go`, `config_test.go` | `JWTSecret []byte`, `JWTTTL time.Duration`. |
| `Startup/main.go` | Startup log adds `jwt_ttl` (never the secret); wiring of pool → repo, hasher, JWT, clock, login action, handler. |
| `Startup/router.go`, `router_test.go` | `Handlers`, route `POST /api/v1/auth/login`. |
| `Shared/password/bcrypt.go`, `bcrypt_test.go` | `Bcrypt`, `NewBcrypt()`, `Verify`. |
| `Shared/token/jwt.go`, `jwt_test.go` | `JWT`, `NewJWT(secret []byte, ttl time.Duration)`, `Issue`, `Verify`. |
| `Shared/clock/clock.go` | `System`, `Now()` (UTC). |
| `Domain/domaintest/clock.go`, `players.go`, `auth.go` (+ tests) | `FixedClock`, `FakePlayerRepository`, `FakePasswordHasher`, `FakeTokenIssuer`. |
| `Actions/login.go`, `login_test.go` | `Login`, `LoginCommand`, `LoginResult`, dummy hash. |
| `Application/errors.go`, `errors_test.go` | `CodeInternal`, `writeDomainError`. |
| `Application/request.go`, `request_test.go` | `decodeJSON`. |
| `Application/login.go`, `login_test.go` | `LoginHandler`, request/response DTOs. |
| `Application/auth.go`, `auth_test.go` | `RequireAuth`, `UserIDFromContext`. |
| `README.md` | JWT variables marked as read; login `curl` example; error codes incl. `INTERNAL_ERROR`. |
| `DESIGN.md` §5 | Authentication decisions; future improvements (refresh, revocation, rate limiting). |
| `ARCHITECTURE.md` | "Authentication (Phase 06)" section. |
| `docs/plans/00-general-plan.md` | Phase 06 row note. |

## Commit sequence

**Working mode** — same loop as Phase 05 as built: **test-first in small
increments** where it helps (each increment = a failing test, then the code),
one commit per step below or per increment if the developer prefers. Proposed
split (open question 1):

| Steps | Who writes |
|---|---|
| 2, 3, 6, 11, 12 and the tests of 4, 5, 7–10 | Claude writes and explains |
| **4, 5, 7, 8, 9, 10 (production code)** | **Developer**, from step-by-step instructions; Claude reviews |

For every step: `gofmt -l .` empty, `go vet ./...`, `go build ./...`,
`go test ./...` green; Claude proposes the commit message.

---

1. **`docs(plan): add phase 06 authentication plan`** — this file.

2. **`feat(domain): add token verifier port and auth errors`**
   - `TokenVerifier`; `ErrMissingCredentials`, `ErrInvalidToken` + tests that
     each wraps the right category.

3. **`feat(startup): read and validate JWT configuration`**
   - `JWT_SECRET` (required, ≥ 32 bytes), `JWT_TTL` (default `1h`, positive);
     tests (default TTL, missing secret, short secret, invalid TTL, zero TTL);
     startup log shows the TTL only; README table and `.env.example` comments.
   - *Go concepts:* `[]byte` vs `string` (why the secret is kept as bytes);
     `len()` counts bytes, not characters.

4. **`feat(auth): add bcrypt password hasher`**
   - `go get golang.org/x/crypto`; `Shared/password`.
   - Tests: a seeded hash + its README password → `nil`; wrong password →
     `ErrInvalidCredentials`; 73-byte password → `ErrInvalidCredentials`;
     malformed hash → error that is **not** `ErrInvalidCredentials`.
   - *Go concepts:* compile-time interface check
     `var _ domain.PasswordHasher = Bcrypt{}`; implicit interfaces (no
     `implements` keyword); a struct with no fields.

5. **`feat(auth): issue and verify JWTs`**
   - `go get github.com/golang-jwt/jwt/v5`; `Shared/token`.
   - Tests: issue → verify round trip returns the user ID; `ExpiresAt` = now +
     TTL; token verified at its exact expiry → invalid; one second before → valid;
     wrong secret; `alg: none`; `HS512` token; malformed string; missing `exp`;
     missing / non-numeric / zero `sub`; every failure `errors.Is(err,
     domain.ErrInvalidToken)`. Foreign tokens are built in the test with the
     library directly.
   - *Go concepts:* the key function `func(*jwt.Token) (any, error)`
     (≈ `IssuerSigningKeyResolver`); functional options (`jwt.WithXxx(...)`);
     `strconv.FormatInt` / `ParseInt`.

6. **`test(domain): add auth fakes and fixed clock`**
   - `domaintest.FixedClock`, `FakePlayerRepository`, `FakePasswordHasher`,
     `FakeTokenIssuer` + small tests; `Shared/clock.System`.
   - *Go concepts:* zero-value-ready structs; maps as in-memory stores.

7. **`feat(actions): add login use case`**
   - `Actions/login.go`.
   - Tests (`TestLogin_…`): valid credentials → token and `expiresAt` from the
     issuer with the clock's `now`; unknown user → `ErrInvalidCredentials`;
     wrong password → `ErrInvalidCredentials`; unknown user **still verifies the
     dummy hash**; empty username / empty password → `ErrMissingCredentials`
     (table); repository failure → error, not `ErrInvalidCredentials`; hasher
     failure → error; issuer failure → error.
   - *Go concepts:* errors as values and `errors.Is` through `%w` wrapping
     (≈ exception filters, but explicit); `context.Context` as first parameter.

8. **`feat(application): map domain errors to HTTP responses`**
   - `writeDomainError` + `CodeInternal`.
   - Tests: one row per category (status + code); a wrapped error
     (`fmt.Errorf("login: %w", domain.ErrInvalidCredentials)`) still maps to 401;
     unknown error → 500 `INTERNAL_ERROR`, message does not contain the
     original text, and the error is logged.
   - *Go concepts:* `switch { case errors.Is(...): }`; why order matters only if
     categories overlapped (they do not).

9. **`feat(application): add login endpoint`**
   - `decodeJSON`, `LoginHandler`, DTOs.
   - Tests: `200` with `token` and `expiresAt` (RFC 3339); `Cache-Control:
     no-store`; malformed JSON / empty body / trailing data / body over 1 MiB →
     `400 INVALID_REQUEST`; missing field → `400`; **unknown user and wrong
     password return byte-identical responses**; the password never appears in
     the log output.
   - *Go concepts:* struct tags (`json:"username"`); `http.MaxBytesReader`;
     `json.Decoder.More()` to detect trailing data; `time.Time.Format(time.RFC3339)`.

10. **`feat(application): add bearer token middleware`**
    - `RequireAuth`, `UserIDFromContext`.
    - Tests: no header / `Basic …` / `Bearer` with no token / invalid token →
      `401 UNAUTHORIZED`, identical bodies, `WWW-Authenticate: Bearer`, next
      handler **not** called; valid token → next handler called and sees the
      user ID; `bearer` lower-case accepted; the token never appears in the log.
    - *Go concepts:* middleware as `func(http.Handler) http.Handler`
      (≈ ASP.NET middleware delegate); `context.WithValue` with an unexported
      key type (≈ `HttpContext.Items`, but immutable: you get a new context).

11. **`feat(startup): wire login endpoint`**
    - `Handlers`, route, wiring in `run()`; router tests updated
      (`POST /api/v1/auth/login` reaches the login handler, `GET` on it → 404).
    - Manual check: `go run ./Startup` against the local PostgreSQL, then
      `curl` login OK / wrong password / unknown user (same body) / empty body.

12. **`docs(auth): document authentication`**
    - `README.md`, `DESIGN.md`, `ARCHITECTURE.md`, `00-general-plan.md`, and an
      "As built" section here.

## Test plan

- **Unit (`go test ./...`, no Docker):** domain errors; config; bcrypt hasher
  (real bcrypt, cost 10, a handful of calls); JWT issuer/verifier (every
  rejection case); login use case with fakes; error mapper; login handler and
  middleware with `httptest.NewRecorder`; router.
- **Integration:** none new — `PlayerRepository` is already covered by Phase 04
  (`-tags=integration`).
- **Manual (`curl`):**
  ```bash
  curl -i -X POST localhost:8080/api/v1/auth/login \
       -H 'Content-Type: application/json' \
       -d '{"username":"player_usd","password":"usd-pass-2026"}'   # 200 + token
  # wrong password and unknown user → identical 401 bodies
  # '{}' or 'not json' → 400 INVALID_REQUEST
  ```
  Paste the token into <https://jwt.io>-like decoding *locally* (or
  `base64 -d` the payload) to check `sub`, `iat`, `exp` — never a real secret.
- **Not covered:** `go test -race` (no C compiler on Windows; CI in Phase 13);
  timing equality is asserted indirectly (the dummy hash is verified), not by
  measuring durations.

## Open questions

1. **Who writes which step?** Proposal: developer writes the production code of
   4, 5, 7, 8, 9, 10; Claude writes the domain additions, config, fakes, wiring,
   all tests and docs.
2. **Dummy-hash timing protection: keep it?** Recommended (one call, defensible
   in the interview). The cost: the dummy hash is a bcrypt-specific constant
   living in `Actions` as an opaque string.
3. **Error messages:** fixed text per category for everything (simplest, zero
   leak risk), or the domain sentinel's text for `400`/`409` (more helpful to
   providers: "currency does not match")? Recommended: sentinel text for
   `400`/`409`, fixed text otherwise. Also confirm the extra
   `500 INTERNAL_ERROR` code.
4. **Exact login response fields:** this plan assumes `{"token", "expiresAt"}`
   from §8.1 as recorded in Phase 03. Please check the assessment PDF for the
   exact names (e.g. `accessToken`, `tokenType`, `expiresIn`) before step 9.

## Definition of done

- `Domain` still imports only stdlib + `decimal`; `Application` does not import
  `Shared` or `Persistence`; `Shared/*` import only stdlib, their library and
  `Domain`; `domaintest` is not imported by production code
  (`go list -f '{{.Imports}}' ./...`).
- `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` green without Docker.
- Unknown user and wrong password produce byte-identical `401` responses (tested).
- Every JWT rejection case of step 5 has a test; `alg: none` is rejected.
- No log line or response contains the password, hash, token or `JWT_SECRET`
  (tested where the value passes through our code).
- The service refuses to start without `JWT_SECRET` or with one shorter than
  32 bytes.
- Manual `curl` check of step 11 done.
- `README.md`, `DESIGN.md`, `ARCHITECTURE.md`, `00-general-plan.md` updated.
- The developer can explain each Go concept listed in steps 3–10.
