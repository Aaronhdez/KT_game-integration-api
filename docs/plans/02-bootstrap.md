# 02 — Bootstrap

## Goal

Get a **running, empty Go service**: a module that builds, reads its config from
environment variables, logs as structured JSON, answers `GET /health` and shuts
down cleanly on Ctrl+C / `SIGTERM`. It is also the shared JSON error format that
later phases will use.

This phase also serves as the **Go learning ramp**. Every commit introduces a
small set of language concepts (listed per step under *Go concepts*), so the
developer can understand, explain and change every line before moving on. No
business logic yet: no database, no Wallet, no auth.

## Requirements covered (assessment sections)

- 10.1 — Clean architecture skeleton, env-based config, Git history.
- 7.3 — Error body `{ "code": "...", "message": "..." }` and the error codes
  (writer only; the codes are used from Phase 06 onwards).
- 9.7 — Observability: structured logs, never secrets (logger + request log).
- 11 — `README.md` "Running the tests" and "Configuration" sections (partial).

## Prerequisite (no commit): install 64-bit Go

> ✅ **Done (2026-11-05):** `go1.27.3 windows/amd64` at `C:\Program Files\Go`;
> the 386 install was removed.

The machine had `go1.27.3 windows/386` (32-bit, under `C:\Program Files (x86)\Go`).
It works, but:
- on 386, Go's `int` is 32 bits, so the toolchain does not match the 64-bit
  Linux binary we will ship in Docker (Phase 12);
- the race detector (`go test -race`), which we want for the concurrency tests in
  Phases 04 and 08–10, is **not available** on 386.

Action: uninstall it and install the **windows-amd64** MSI of the latest stable Go
from <https://go.dev/dl/>. Verify with `go version` → `… windows/amd64`.
(`-race` on Windows also needs a C compiler; if we don't want to install one, the
race tests can run in CI on Linux in Phase 13. Decided then, not now.)

## Design decisions (and alternatives rejected)

| Decision | Why | Rejected alternative |
|---|---|---|
| Module path `github.com/aaronhdez/game-integration-api` *(later `…/kt_game-integration-api`, see "As built")* | Conventional `host/user/repo`, lowercase (module paths are case-sensitive and the community uses lowercase); works even if the repo is never pushed. | `github.com/Aaronhdez/…` (mixed case); a short name like `gameapi` (no domain, not fetchable, unusual); another host (repo goes to GitHub). |
| `package main` lives directly in `Startup/` → run with `go run ./Startup` | Matches `ARCHITECTURE.md` (Startup = composition root ≈ `Program.cs`). | `cmd/api/main.go` (the common Go layout, but contradicts our agreed folders). |
| Config, logger, router and server are **files of `package main`** in `Startup/` (`config.go`, `logger.go`, `router.go`, `main.go`), not subpackages | Fewer packages to learn on day one; files in the same package see each other with no imports. Can be split later if it grows. | `Startup/config`, `Startup/server` subpackages (more imports and exported names for no gain yet). |
| Config only reads the variables **used in this phase**: `HTTP_PORT`, `LOG_LEVEL`, `SHUTDOWN_TIMEOUT` | Each phase adds its own (`DATABASE_URL` in 04, `WALLET_*` in 05, `JWT_*` in 06). Reading required secrets now would make the service fail to start for values nobody uses yet. | Loading the whole `.env.example` now. |
| Config is read with `os.Getenv` + defaults; invalid values → startup error | Stdlib only, easy to explain. Defaults mean `go run ./Startup` works with no setup. | `godotenv`, `viper`, `envconfig` (dependencies we don't need). |
| `.env` is **not** loaded by the program | Docker Compose reads it (`env_file`, Phase 12). Locally, defaults are enough; variables can be set in the shell. | Auto-loading `.env` from code (hidden magic, extra dependency). |
| Logger: `log/slog` JSON handler to stdout, level from `LOG_LEVEL`; the `*slog.Logger` is **passed explicitly** to whoever needs it | Stdlib, structured, matches AGENTS.md "no globals". | `zap`/`zerolog` (deps); using the global `slog.Default()` everywhere. |
| Router: stdlib `http.ServeMux` with Go 1.22+ patterns (`"GET /health"`) | No framework. Method + path routing is built in. | `chi`, `gin`, `echo`. |
| Unmatched routes return `404 {"code":"NOT_FOUND",…}` via a catch-all `"/"` | Every error body follows the 7.3 format. | Leaving the mux's plain-text 404. (Side effect: wrong methods also get this JSON `404` instead of `405` — accepted, see open questions.) |
| JSON error writer + error-code constants in `Application/` | It's HTTP-layer code; Phase 06+ handlers reuse it. | Putting it in `Startup` (handlers in `Application` could not use it). |
| `/health` handler in `Application/`, returns `200 {"status":"ok"}` | It's an HTTP handler; liveness only. A DB check (readiness) can be added in Phase 04. | Checking DB/Wallet now (they don't exist yet). |
| `http.Server` with explicit timeouts (`ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`) | The zero-value server has no timeouts (slow-client attacks, `go vet`-style linters flag it). | `http.ListenAndServe(addr, mux)` (no timeouts). |
| Graceful shutdown with `signal.NotifyContext` + `server.Shutdown(ctx)` bounded by `SHUTDOWN_TIMEOUT` (default `10s`) | In-flight requests finish (important later: a Withdraw mid-Wallet-call). | Killing the process immediately. |
| `main()` only calls `run()` and exits non-zero on error | `run` returns `error`, so `defer`s run and the flow is testable. | Logic and `os.Exit`/`log.Fatal` scattered inside `main`. |
| Request-logging middleware in `Application/` (method, path, status, duration) | Observability (9.7) and introduces the `func(http.Handler) http.Handler` pattern reused by auth in Phase 06. Never logs headers or bodies (tokens!). | No request log; or logging full requests. |
| Tests: stdlib `testing` + `net/http/httptest` only | Zero dependencies in this phase. Revisit `testify` in Phase 03 if assertions get verbose. | Adding `testify` now. |
| **No Makefile/Taskfile** | Neither `make` nor `task` is installed on this Windows machine, and the commands are short. They are documented in `README.md` / `AGENTS.md`. | Makefile (needs extra install on Windows). *This changes the Phase 02 row of the general plan — updated in the last commit.* |

## Files to create / modify

| File | Content |
|---|---|
| `go.mod` | Module path and `go` version. (No `go.sum` yet: no dependencies.) |
| `Startup/main.go` | `main()` → `run()`: load config, build logger, build router, start server, wait for signal, shut down. |
| `Startup/config.go` | `Config` struct + `LoadConfig()` reading env vars with defaults and validation. |
| `Startup/config_test.go` | Table-driven tests: defaults, valid values, invalid port/level/duration. |
| `Startup/logger.go` | `NewLogger(w, level)` → `*slog.Logger` (JSON lines to `w`; stdout in `run`). |
| `Startup/logger_test.go` | Output is valid JSON with msg/level/attrs; messages below the level are dropped. |
| `Startup/router.go` | `NewRouter() http.Handler` (step 7), `NewRouter(logger)` from step 9: registers routes and wraps them with middleware. |
| `Startup/router_test.go` | `/health` → 200; unknown path → 404 JSON. |
| `Application/errors.go` | Error-code constants (`INVALID_REQUEST`, `UNAUTHORIZED`, `NOT_FOUND`, `CONFLICT`, `INSUFFICIENT_FUNDS`, `WALLET_ERROR`, `WALLET_TIMEOUT`), `WriteError`, `writeJSON`. |
| `Application/errors_test.go` | Status, `Content-Type` and body of `WriteError`. |
| `Application/health.go` | `Health` handler. |
| `Application/health_test.go` | 200 + body. |
| `Application/middleware.go` | `RequestLogger(logger)` middleware + small `ResponseWriter` wrapper that remembers the status. |
| `Application/middleware_test.go` | Logs one line with the right status; response unchanged. |
| `.env.example` | Add `SHUTDOWN_TIMEOUT=10s`. |
| `README.md` | Run locally, run tests, configuration table (mark which vars exist already). |
| `AGENTS.md` | Fill the "Commands" section. |
| `docs/plans/00-general-plan.md` | Phase 02 row: drop "Makefile/Taskfile"; Prerequisites: Go installed (amd64). |

## Commit sequence

**Working mode** (the learning loop). Who writes the code depends on the step:

| Steps | Who writes | Loop |
|---|---|---|
| 2, 3, 4, 10 | Claude | Claude writes the code and explains the Go concepts in the chat (vs C#/.NET) → developer reads, runs, asks. |
| **5, 6, 7, 8, 9** | **Developer** | Claude gives **step-by-step instructions** in the chat (files, signatures, what each piece must do, the Go concepts and the stdlib functions to look up — not the finished code) → developer writes it → Claude **reviews** (correctness, idiomatic Go, plan compliance) → developer fixes. |

For every step, before committing:
1. `gofmt -l .` (must print nothing), `go vet ./...`, `go build ./...`,
   `go test ./...` → green.
2. Claude proposes the commit message; the developer commits when ready.

---

1. **`docs(plan): add phase 02 bootstrap plan`**
   This file. Verified by review.

2. **`build(startup): initialise go module and minimal main`**
   - `go mod init github.com/aaronhdez/game-integration-api`.
   - `Startup/main.go` with `main()` → `run() error` that just logs "starting"
     with `slog` and returns `nil`.
   - Verify: `go build ./...`, `go run ./Startup` prints one log line (slog's default
     text format; JSON arrives in step 4).
   - *Go concepts:* module vs package (≈ solution/assembly vs namespace),
     `go.mod` (≈ `.csproj`), `package main` + `func main`, import paths built
     from the module path + folder (`…/Application`), `go run`/`go build`,
     `gofmt` (no style debates), errors as return values (`run() error`).
   - *Exercise:* break the code on purpose (unused import/variable) and read the
     compiler error — Go refuses to compile unused things.

3. **`feat(startup): load configuration from environment variables`**
   - `Config{HTTPPort int; LogLevel slog.Level; ShutdownTimeout time.Duration}`.
   - `LoadConfig() (Config, error)`: read env, apply defaults (`8080`, `info`,
     `10s`), validate (port 1–65535, known level, positive duration). Errors
     wrapped with `fmt.Errorf("config: HTTP_PORT: %w", err)`.
   - `run()` calls it and returns the error if any.
   - Tests: table-driven, using `t.Setenv`.
   - Verify: `go test ./...`; `$env:HTTP_PORT="abc"; go run ./Startup` exits
     with a clear error and exit code 1.
   - *Go concepts:* structs (≈ class without inheritance), zero values (no
     `null` for `int`/`string`), multiple return values `(T, error)` instead of
     exceptions, `if err != nil`, `%w` wrapping (≈ `InnerException`),
     `strconv.Atoi`, `time.ParseDuration`, `_test.go` files, table-driven tests
     (≈ xUnit `[Theory]` + `[InlineData]`), `t.Run` subtests.
   - *Exercise:* add a test case for `HTTP_PORT=70000`.

4. **`feat(startup): add structured JSON logger`**
   - `NewLogger(w io.Writer, level slog.Level) *slog.Logger` with
     `slog.NewJSONHandler(w, …)`. `run()` passes `os.Stdout`; tests pass a
     `bytes.Buffer` (`Startup/logger_test.go`: JSON output, level filtering).
   - `run()` builds it from config and logs `"starting …"` with port and
     level (never secrets — there are none yet, but the habit starts here).
     `main()` reports startup errors with a fixed-level logger, because config
     may have failed before `run()` had a logger.
   - Verify: `$env:LOG_LEVEL="debug"; go run ./Startup` shows debug lines;
     `warn` hides info lines.
   - *Go concepts:* pointers (`*slog.Logger`) — when and why, `&T{}`; passing
     dependencies explicitly instead of a DI container; key/value structured
     logging (≈ `ILogger` with message templates).

5. **`feat(application): add JSON error writer and error codes`**
   - `Application/errors.go`: `const ( CodeInvalidRequest = "INVALID_REQUEST" … )`,
     `type ErrorResponse struct { Code string \`json:"code"\`; Message string \`json:"message"\` }`
     (exported — implemented that way so other packages' tests, e.g. the router's, can decode it),
     `WriteError(w http.ResponseWriter, status int, code, message string)`, and
     a private `writeJSON(w, status, v any)` that sets `Content-Type: application/json`.
   - Tests with `httptest.NewRecorder()`.
   - *Go concepts:* exported vs unexported by **capitalisation** (≈ `public` /
     `internal`), `const` blocks, struct tags for JSON (≈ `[JsonPropertyName]`),
     `encoding/json`, interfaces satisfied implicitly (`http.ResponseWriter`;
     the recorder "implements" it without declaring it), `any` (≈ `object`).
   - *Exercise:* make the test fail by changing a JSON tag and read the diff.

6. **`feat(application): add health endpoint`**
   - `Application/health.go`: `func Health(w http.ResponseWriter, r *http.Request)`
     writing `200 {"status":"ok"}` via `writeJSON`.
   - Test with `httptest.NewRequest` + recorder.
   - *Go concepts:* the handler signature, `http.HandlerFunc` (a function type
     that satisfies the `http.Handler` interface — functions as values),
     same-package access to unexported `writeJSON`.

7. **`feat(startup): add router with health and json not-found`**
   - `Startup/router.go`: `NewRouter() http.Handler` with
     `mux.HandleFunc("GET /health", application.Health)` and a catch-all `"/"`
     → `WriteError(404, NOT_FOUND, "resource not found")`. (The `logger`
     parameter is added in step 9, when the middleware needs it.)
   - `Startup/router_test.go`: drive the router with `httptest` (no real network).
   - *Go concepts:* importing our own package (`application.Health`), the Go
     1.22 pattern syntax (`"GET /path/{id}"`, ≈ minimal-API `MapGet`), most-specific
     pattern wins, returning an interface (`http.Handler`).

8. **`feat(startup): run http server with graceful shutdown`**
   - `run()`: `http.Server{Addr, Handler, ReadHeaderTimeout: 5s, ReadTimeout: 10s,
     WriteTimeout: 30s, IdleTimeout: 60s}`; `ctx, stop := signal.NotifyContext(…, os.Interrupt, syscall.SIGTERM)`;
     `ListenAndServe` in a goroutine reporting errors on a channel; `select` on
     `ctx.Done()` vs server error; then `srv.Shutdown(ctx with SHUTDOWN_TIMEOUT)`.
     `ErrServerClosed` is not an error.
   - Verify manually: `go run ./Startup`, `curl http://localhost:8080/health`
     → `{"status":"ok"}`; `curl …/nope` → 404 JSON; Ctrl+C → "shutting down" /
     "stopped" logs and exit code 0. Port already in use → clear error, exit 1.
   - *Go concepts:* `context.Context` (≈ `CancellationToken`, but also carries
     deadlines), `defer` (≈ `finally`/`using`), goroutines (`go f()`, ≈ a very
     cheap `Task.Run`), channels and `select` (≈ `Task.WhenAny`), `errors.Is`
     (≈ `catch (X) when …`).
   - *Exercise:* add a `time.Sleep(5*time.Second)` handler locally, call it and
     press Ctrl+C mid-request: the request still completes (don't commit it).

9. **`feat(application): add request logging middleware`**
   - `Application/middleware.go`: `RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler`
     *(built as `RequestLogger(logger, next) http.Handler` — see "As built")*
     logging `method`, `path`, `status`, `duration_ms` at info level. A small
     `statusRecorder` struct embeds `http.ResponseWriter` and overrides
     `WriteHeader` to remember the status. Never logs headers, query or body.
   - Wired in `NewRouter` around the mux.
   - Test: logger writing to a `bytes.Buffer`, assert one line with status 404
     for an unknown path.
   - *Go concepts:* closures, functions returning functions (≈ ASP.NET Core
     middleware delegates), **struct embedding** (≈ composition that "promotes"
     methods — not inheritance), method receivers (`func (r *statusRecorder) WriteHeader(…)`).

10. **`docs(docs): document local run, tests and configuration`**
    - `README.md`: "Running locally" (`go run ./Startup`, curl `/health`),
      "Running the tests" (`go test ./...`), config table with `SHUTDOWN_TIMEOUT`
      and a column saying from which phase each variable is read.
    - `.env.example`: `SHUTDOWN_TIMEOUT=10s`.
    - `AGENTS.md`: fill "Commands" (build, vet, gofmt, test, run).
    - `00-general-plan.md`: Phase 02 row without Makefile; prerequisite "Go
      installed (amd64)".
    - `DESIGN.md` §5: one line — logs are JSON to stdout, request log never
      includes headers/bodies (tokens).

## Test plan

- **Unit (go test ./...):** config parsing (defaults, valid, invalid); error
  writer (status, header, body); health handler; router (200 health, 404 JSON);
  request-logging middleware (one log line, correct status, response untouched).
- **Manual:** `go run ./Startup` + `curl` for `/health` and an unknown route;
  Ctrl+C shutdown; invalid `HTTP_PORT` → exit 1; port already in use → exit 1.
- **Not tested automatically:** signal handling / `Shutdown` (would need a real
  process; verified manually — acceptable for a thin composition root).

## Open questions

1. ~~**Module path**~~ **Resolved:** `github.com/aaronhdez/game-integration-api`
   (lowercase, by Go convention; the repo is expected to be delivered via GitHub).
   **Changed after the phase** to `github.com/aaronhdez/kt_game-integration-api`
   to match the actual repository, `github.com/Aaronhdez/KT_game-integration-api`
   (still lowercase).
2. ~~**Wrong method on an existing path**~~ **Resolved (step 7):** verified that
   once the catch-all `"/"` is registered it also matches wrong methods, so
   `POST /health` returns `404 {"code":"NOT_FOUND",…}` instead of the mux's
   plain-text `405`. Accepted: every error stays in the 7.3 JSON format and the
   assessment defines no "method not allowed" code. Revisit in Phase 06 if needed.
3. ~~**Who types the code?**~~ **Resolved:** the developer writes steps 5–9
   from Claude's step-by-step instructions, and Claude reviews them. Claude writes
   steps 2–4 and 10. See *Working mode*.

4. **Shared HTTP test helpers** — `assertStatusCode` and `decodeErrorResponse`
   are duplicated in `Application` and `Startup` tests. **Deferred** (rule of
   three): when Phases 06–10 add more handler tests, extract them into a
   non-`_test` package following the `Domain/domaintest` pattern (e.g.
   `Application/apptest` with `AssertStatusCode`, `DecodeJSON[T]`) and record it
   in `ARCHITECTURE.md`.

## As built — deviations from this plan

| Planned | Built | Why |
|---|---|---|
| `NewLogger(level)` | `NewLogger(w io.Writer, level)` | Tests write to a `bytes.Buffer`. |
| `errorBody` (unexported) | `ErrorResponse` (exported), codes prefixed `Code…` | The router tests in `Startup` decode it. |
| `NewRouter(logger)` from step 7 | `NewRouter()` in step 7, `NewRouter(logger)` in step 9 | No unused parameter. |
| Wrong method → plain-text `405` | Wrong method → JSON `404` | The catch-all `"/"` also matches wrong methods (verified). |
| `RequestLogger(logger) func(http.Handler) http.Handler` | `RequestLogger(logger, next) http.Handler`, wrapping the whole mux once | Two levels of functions instead of three; only two middlewares are expected (logging, auth), composed by hand. |
| `duration_ms` field | `duration_ms` (briefly logged as `duration` in nanoseconds; fixed) | Unit in the name. |
| Server code inline in `run()` | `newHTTPServer`, `startServer`, `waitForShutdownSignal`, `shutdownServer` in `main.go` | Readability; `run()` reads as a list of steps. `defer stop()` stays in `run()`. |
| Table-driven tests | One test per behaviour, Arrange/Act/one assert, logic in `t.Helper()` helpers; tables only for cases of identical shape | Developer's preference. Shared helpers in `Application/helpers_test.go`. |
| — | `.gitattributes` (`* text=auto eol=lf`) | VS Code saved `.go` files with CRLF, which `gofmt` rejects. |
| Makefile/Taskfile (general plan) | None | `make`/`task` not installed; commands in README/AGENTS. |
| Module `github.com/aaronhdez/game-integration-api` | `github.com/aaronhdez/kt_game-integration-api` | Match the repository name chosen when pushing. |

Not done (optional in the plan): a unit test for `newHTTPServer` (port → `Addr`).

## Definition of done

- 64-bit Go installed (`go version` shows `windows/amd64`).
- `gofmt -l .` empty, `go vet ./...` clean, `go build ./...` and `go test ./...` green.
- `go run ./Startup` serves `GET /health` → `200 {"status":"ok"}`; unknown routes
  → `404` with the 7.3 error body; Ctrl+C shuts down cleanly with exit code 0.
- No third-party dependencies (`go.mod` has no `require` block).
- `README.md`, `AGENTS.md`, `.env.example`, `00-general-plan.md` and `DESIGN.md`
  updated as listed in step 10.
- The developer can explain every Go concept listed in steps 2–9.
