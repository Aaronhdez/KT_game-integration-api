# 13 — Docs and delivery

## Goal

Leave the repository ready to hand in: a reviewer who clones it can run it,
understand it and check it without asking anything.

At the end of the phase:

- `docs/openapi.yaml` describes the five endpoints exactly as they behave
  (every status code, field and error code of `README.md`);
- a GitHub Actions workflow runs, on every push and pull request, the same
  checks the developer runs locally (format, vet, build, unit tests with the
  race detector, integration tests against PostgreSQL, Docker image build,
  OpenAPI lint);
- `DESIGN.md` is the short document §11 asks for: the five required sections
  in **one or two pages** (§11 says so explicitly), with the detailed
  per-phase decisions moved to `docs/decisions.md` and linked;
- `README.md` is final: no "work in progress" notes, phase references removed
  from user-facing tables, a complete `curl` walkthrough, links to the spec
  and the CI badge;
- a last review against the assessment checklist (§10–§12) is done and
  recorded under "As built";
- `main` is tagged `v1.0.0` (the delivery version).

No behaviour changes are planned. Any Go change found by the review (or by
the race detector in CI) is a separate `fix(scope)` commit, proposed first.

## Requirements covered (assessment sections)

| Section | How |
|---|---|
| 10.1 Clean architecture, tests, Git, Docker, env config | CI runs every check on each push; final review |
| 10.2 Nice-to-haves | OpenAPI spec, CI pipeline |
| 11 `README.md`, `DESIGN.md` | Final README; DESIGN condensed to the five required sections |
| 12 Evaluation criteria | Final review checklist (failure handling, idempotency, money, cancel, quality, security, operability) |

## Design decisions (and alternatives rejected)

| Topic | Decision | Alternatives rejected |
|---|---|---|
| OpenAPI format | `docs/openapi.yaml`, **OpenAPI 3.1**, written by hand. One reusable schema per body (`LoginRequest`, `TransactionResponse`, `Error`, …), `Money` as `type: number` with `multipleOf: 0.01` and a note that it is decoded exactly, IDs as `integer`/`int64`, `bearerAuth` security scheme, an example per status code | Generated from code annotations (`swaggo/swag`): a new dependency and comments on every handler, for five endpoints. OpenAPI 3.0: 3.1 allows `null` in a type list (`newBalance: [number, "null"]`), which is how a `202` answers |
| Serving the spec | Not served by the API: it lives in the repo, linked from the README | `GET /openapi.yaml` via `embed`: a new route and test for something a reviewer reads on GitHub anyway |
| Spec validation | `npx @redocly/cli@<pinned version> lint docs/openapi.yaml` in its own CI job (Node only there), with a minimal `redocly.yaml` if a default rule needs turning off (justified in the file) | No lint: a typo in a `$ref` would only show up when someone opens the file in Swagger UI |
| CI platform | GitHub Actions, `.github/workflows/ci.yml` (decided in Phase 12); runs on `push` to `main` and on `pull_request`; `permissions: contents: read` | — |
| CI jobs | Four parallel jobs: **checks** (`gofmt -l .` empty, `go vet ./...`, `go vet -tags=integration ./...`, `go build ./...`, `go test -race ./...`), **integration** (a `postgres:16` service container with database `gameapi_test`, `TEST_DATABASE_URL`, `go test -count=1 -tags=integration ./Persistence/...`), **docker** (`docker build .`), **openapi** (lint) | One sequential job: a failure in the spec would hide whether the Go tests pass. Running `docker compose up` in CI: the Wallet image is arm64-only and runs emulated; slow and flaky for no extra confidence over the image build plus the manual check of Phase 12 |
| Go version in CI | `actions/setup-go` with `go-version-file: go.mod` (one source of truth, `go 1.27.3`); module cache on | A hard-coded version: drifts from `go.mod` |
| Race detector | `-race` only in CI (Linux runners have the C toolchain it needs). If it finds a race, stop and fix it in a `fix(scope)` commit before the CI commit | Skipping `-race`: the per-player lock and the resolver are exactly the code it checks |
| Action versions | Pinned to a major version tag of the official actions (`actions/checkout`, `actions/setup-go`, `actions/setup-node`), latest major checked when writing the file | Floating `@main`: changes without notice |
| Secrets in CI | None needed: the integration tests use a throwaway PostgreSQL password written in the workflow; the Wallet is not used | Repository secrets: nothing secret to store |
| DESIGN.md | Keep the five sections §11 asks for, in its order: (1) the six failure situations — error before applying, error after applying, timeout, process stops between Wallet call and DB write, provider retries / concurrent duplicates, Wallet restart — each with behaviour **and why**; (2) PENDING resolution; (3) retry after FAILED; (4) zero-amount deposits; (5) other assumptions/decisions **and future improvements**. Each item keeps the decision and the reason in a few lines; measurements and step-by-step flows move out. Target **≤ 2 printed pages** (roughly 900–1,100 words) | Leaving it at ~315 lines: §11 asks for "one or two pages", and the key reasoning gets lost |
| Where the details go | New `docs/decisions.md`: the per-phase paragraphs of DESIGN §5 (logging, money, lock, Wallet client, auth, each endpoint…), moved without loss and lightly edited, linked from each DESIGN section | Into `ARCHITECTURE.md`: that file describes structure, not behaviour. Deleting them: they are the evidence behind the short version |
| Open question 6 of the general plan (where errors live) | **Keep every business error in `Domain/errors.go`**, recorded in `docs/decisions.md`. With the three operations done, the errors are shared by `Actions` and `Application`, and moving them would change many files for no behaviour gain right before delivery | Moving action-specific errors to `Actions/errors.go`: listed as a possible future refactor |
| README | Must cover what §11 lists: how to run the system and the tests, the environment variables, the seeded usernames and passwords, how to reset the state. Final structure: what it is → quick start (Docker) → walkthrough → API reference (existing tables) → pending transactions → configuration → seeded players → running locally → tests → CI → reset → project layout → links. The "Read from" column and "Variables of later phases…" note are removed | A separate `docs/usage.md` for the walkthrough: the README is where a reviewer looks first |
| `curl` walkthrough | One bash sequence that a reviewer can paste: login (`TOKEN` captured with `sed`, no `jq` needed) → `GET /player` → withdraw → same withdraw again (`200` replay) → same ID with another amount (`409`) → deposit → cancel of the deposit → `GET /player`. PowerShell equivalent for login and one call | A script file in the repo: more to maintain; a pasteable block shows the calls themselves |

## Files to create / modify

| File | Change |
|---|---|
| `docs/openapi.yaml` | New |
| `redocly.yaml` | New, only if a lint rule must be configured |
| `.github/workflows/ci.yml` | New |
| `DESIGN.md` | Condensed; "Skeleton" header removed |
| `docs/decisions.md` | New: detailed decisions moved from DESIGN §5, plus open question 6 |
| `README.md` | Final version (see decisions) |
| `ARCHITECTURE.md` | Folder tree: `.github/`, `docs/openapi.yaml`, `docs/decisions.md`; anything out of date |
| `AGENTS.md` | Commands: `go test -race ./...` (CI only on Windows), OpenAPI lint; scope list gets `ci` |
| `docs/plans/00-general-plan.md` | Phase 13 row: as-built summary; open question 6 resolved |

## Commit sequence

**Working mode** — same as Phases 09–12: **Claude writes every step**, one
at a time, and explains it in the chat; the developer reviews each step before
it is committed.

For every step: `gofmt -l .` empty, `go vet ./...`, `go build ./...`,
`go test ./...` green. Claude proposes the commit message and commits only
when the developer asks. Branch: `12_docs_and_delivery`.

---

1. **`docs(plan): add phase 13 docs and delivery plan`** — this file.

2. **`docs(docs): add OpenAPI specification`**
   - `docs/openapi.yaml` (+ `redocly.yaml` if needed), checked field by field
     against the handlers in `Application/` and the README tables.
   - Verify: `npx @redocly/cli lint docs/openapi.yaml` clean locally (if Node
     is not installed, the CI job of step 3 is the check); the file opened in
     the Wallet's Swagger UI style viewer or https://editor.swagger.io renders
     every endpoint (developer, optional).

3. **`ci: add GitHub Actions workflow`**
   - `.github/workflows/ci.yml` with the four jobs; `AGENTS.md` commands and
     `ci` scope.
   - Verify: the developer pushes the branch; the four jobs are green on
     GitHub. If `-race` fails, stop: a `fix(scope)` commit comes first.

4. **`docs(docs): condense DESIGN.md and move details to docs/decisions.md`**
   - Verify: every decision of the old DESIGN.md is in one of the two files
     (checked paragraph by paragraph); the six situations of §11.1 each have
     their own row; word count ≤ ~1,100; all relative links resolve.

5. **`docs(docs): finalize README`**
   - README as in the decisions table, CI badge, link to the spec;
     `ARCHITECTURE.md` tree updated.
   - Verify: the walkthrough is run literally against `docker compose up` and
     its outputs match the README; all links resolve.

6. **`docs(plan): record phase 13 as built`** — final review (test plan
   below), "As built" section, general plan row, open question 6 resolved.
   Fixes found by the review go before it as separate `fix(scope)` commits.

7. **Release (developer only, not a commit):** merge the branch into `main`,
   push, check CI green on `main`, then
   `git tag -a v1.0.0 -m "Game Integration API v1.0.0"` and
   `git push origin v1.0.0`. Claude gives the commands; the developer runs
   them (outward-facing).

## Test plan

No new Go tests. Checks:

1. **CI:** all four jobs green on the pushed branch; a deliberately
   unformatted file on a throwaway branch makes the **checks** job fail
   (optional, proves the gate works).
2. **Spec vs. code:** for each endpoint, every status in the README table
   exists in the spec with the same body schema; field names match the JSON
   tags in `Application/`.
3. **Fresh clone:** clone the branch into a new folder, follow the README
   literally (`.env` → `docker compose up --build` → walkthrough → tests) →
   no missing step.
4. **Final review checklist** — each item checked and recorded with its
   evidence (file, test or command):
   - **§10.1 must-haves:** Go; migrations + seed of §8.1; parameterised SQL
     only (`grep` for `Sprintf`/`+` near SQL in `Persistence/`); business
     rules independent of HTTP/DB/Wallet (`go list -deps` of `Domain`,
     `Actions`, `Queries` contains no `net/http`, `pgx`, `Shared/wallet`);
     security (auth, own-data-only → 404, hashed passwords, no secrets in the
     repo, `.env` not tracked); env config + `.env.example`; one
     `docker compose up`; unit tests with a deterministic fake Wallet,
     idempotency and Cancel tests; meaningful commit history.
   - **§10.2 nice-to-haves:** query builder (not used — explicit SQL, reason
     in `docs/decisions.md`); OpenAPI ✔; structured logs + `/health` ✔;
     background resolver ✔; cancel-before-withdraw (`404`, Phase 14
     candidate); graceful shutdown ✔; CI ✔.
   - **§11 deliverables:** README covers run, tests, env vars, seeded
     credentials, reset; DESIGN has the five sections in 1–2 pages.
   - **§12 criteria:** money (no `float` anywhere: `grep -rn "float"
     --include=*.go .`; currency/amount validation tests), failure handling
     (each §9 requirement mapped to code + test), idempotency and
     concurrency (replay, conflict, concurrent duplicate tests), every §8.5
     row tested, no test helpers in production files
     (`go list -f '{{.GoFiles}}' ./...`), no secrets in the logs, the
     documentation matches the behaviour (test plan 2–3).
5. Optionally `/code-review` on the whole codebase; findings triaged with the
   developer.

## Open questions

1. ~~**Assessment checklist**~~ **Resolved:** the developer shared the
   assessment (v2.0, 27 Oct 2026); the checklist of test plan 4 follows
   §10–§12 literally. The PDF itself stays out of the repository (watermarked
   company document).
2. ~~**Tag a release?**~~ **Resolved:** yes, `v1.0.0` on `main` after the
   merge (step 7), run by the developer. Repository visibility is decided by
   the developer when the recruiter says with whom to share it (§11).

## Definition of done

- `docs/openapi.yaml` lints clean and matches the code.
- The CI workflow is green on GitHub for the delivery branch.
- `DESIGN.md` has the five required sections in one or two pages; nothing
  was lost (it is in `docs/decisions.md`).
- `README.md` has no WIP/TBD notes; the walkthrough works as written from a
  fresh clone.
- Final review done and recorded under "As built"; general plan updated.
- `gofmt -l .` empty, `go vet ./...` clean, `go build ./...`, `go test ./...`
  green.
- `main` tagged `v1.0.0` with CI green.
