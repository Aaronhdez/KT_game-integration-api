# 01 — Wallet spike

## Goal

Learn **empirically** how the mock Wallet (`kentechsp/wallet-client`) behaves —
especially how and when it fails — before writing any code that depends on it.
The output is knowledge, not code: a findings document (`docs/wallet-findings.md`)
that later phases (05, 08–11) use as their source of truth about the Wallet.

**No production code** in this phase: nothing under `Startup/`, `Domain/`,
`Shared/`, etc. Go is not needed (and is not installed yet).

## Requirements covered (assessment sections)

- 5.1 — Run the Wallet, `x-api-key` header, how to reset its state.
- 5.2 — Big user IDs, big and fractional balances (how they travel in JSON).
- 5.3 — No rollback endpoint, list payload, integer `betId`, unreliability.
- 9.3 / 9.5 — Input for classifying outcomes (*applied / not applied / unknown*)
  and for choosing a sensible `WALLET_TIMEOUT`.
- Open question 1 of `00-general-plan.md`.

## Design decisions (and alternatives rejected)

| Decision | Why | Rejected alternative |
|---|---|---|
| Run the Wallet alone with `docker run`, not with `docker-compose.yml` | Compose is Phase 12; a single command is enough for a spike. | Writing the compose file now (scope creep). |
| Manual requests in a `.http` file (VS Code *REST Client* extension) | Same format as Visual Studio `.http` files, familiar to a .NET developer; versionable. | Postman (not versioned in the repo). |
| Repeated/automated probing in a **PowerShell** script | Already on the machine; `Invoke-RestMethod` parses JSON natively (no `jq` needed). | Bash + curl + jq (`jq` not installed); a Go program (Go not installed, and it would be code). |
| Detect "error after applying" by **reading the balance before and after** each call, one call at a time | It is the only observable effect we have if the Wallet offers no transaction lookup. | Trusting the response body alone (that is exactly what we cannot trust). |
| Spike artefacts live in `docs/wallet-spike/` | They are documentation/evidence, not part of the build. | `scripts/` at the root (looks like production tooling). |
| Pin the image by tag/digest observed on the day | Findings are only valid for the version we tested. | `latest` without recording what it was. |

## Files to create / modify

| File | Content |
|---|---|
| `docs/wallet-findings.md` | **Main deliverable.** One section per question below, each with: what we did, what we saw (real request/response), conclusion. |
| `docs/wallet-spike/README.md` | How to start/stop/reset the Wallet and run the probes. |
| `docs/wallet-spike/swagger.json` | Copy of the Wallet's OpenAPI document (reference for Phase 05). |
| `docs/wallet-spike/requests.http` | Manual requests, grouped by experiment. |
| `docs/wallet-spike/probe.ps1` | Script that repeats a call N times and records outcome, latency and balance delta to a CSV. |
| `docs/wallet-spike/results/*.csv` | Raw probe output used as evidence in the findings. |
| `DESIGN.md` | Fill the rows owned by Phase 01 ("error after applying", "Wallet restarts"). |
| `docs/plans/00-general-plan.md` | Mark open question 1 as resolved, linking to the findings. |

## Commit sequence

Each step: do the experiment, write down what was observed, then commit.
If a step reveals something that changes later steps, **stop and update this plan
first**.

1. **`docs(plan): add phase 01 wallet spike plan`**
   This file. Verified by review.

2. **`docs(wallet): add findings skeleton and how to run the wallet`**
   - `docs/wallet-findings.md` with the list of questions (sections Q1–Q10 below), all *TBD*.
   - `docs/wallet-spike/README.md` with the `docker run` command (image, tag/digest
     actually pulled, port mapping) and how to stop it.
   - Verified: the container starts and Swagger UI opens in the browser.
     Record the image digest (`docker image inspect`).

3. **`docs(wallet): record API surface from swagger`** — *Q1*
   - Save `swagger.json`. In the findings, list every endpoint: method, path,
     request/response schemas, documented status codes, required headers.
   - Answer from the docs only (not yet tested): is there a way to query a past
     transaction? Is there a rollback? What does the "list payload" look like?

4. **`docs(wallet): add happy-path requests`** — *Q2*
   - `requests.http`: get balance, withdraw, deposit for one seeded player.
   - Record: real response bodies, how amounts are encoded (JSON number or
     string? how many decimals? `100` vs `100.00`?), how big user IDs are
     encoded (number or string?), which fields come back (new balance? an id?).

5. **`docs(wallet): record api-key behaviour`** — *Q3*
   - Requests with no `x-api-key`, a wrong one, the right one. Record status
     codes and bodies.

6. **`docs(wallet): record validation errors`** — *Q4*
   - Negative amount, zero amount, 3 decimals, unknown user, wrong currency for
     the user, malformed JSON, missing fields, non-integer `betId`.
   - Record status + body for each: these are the *not applied* cases.

7. **`docs(wallet): record insufficient funds behaviour`** — *Q5*
   - Withdraw more than the balance. Record status + body, and confirm the
     balance did not change.

8. **`docs(wallet): add probe script`**
   - `probe.ps1`: for N iterations → read balance, call the operation with a
     fresh `reference`, read balance again; write one CSV row per iteration
     (`iteration, status, latencyMs, balanceBefore, balanceAfter, delta, bodySnippet`).
     Client timeout configurable (start high, e.g. 30 s, to observe real latency).
   - Verified by a small run (N = 5) against the happy path. No findings yet.

9. **`docs(wallet): record failure modes of withdraw`** — *Q6*
   - Run the probe on withdraw with N ≈ 200. Classify each row:
     - error + balance unchanged → **error before applying**;
     - error + balance changed → **error after applying**;
     - success + balance unchanged (if it happens!) → note it;
     - slow / no response → latency distribution.
   - Save the CSV in `results/`. In the findings: frequencies, status codes and
     bodies of each failure kind, and whether the body lets us tell them apart.

10. **`docs(wallet): record failure modes of deposit`** — *Q6*
    Same as step 9 for deposit. Note any difference with withdraw.

11. **`docs(wallet): record latency and timeout recommendation`** — *Q7*
    - From the CSVs: p50 / p95 / p99 / max latency, and how many calls never
      answered within 30 s.
    - Propose the value of `WALLET_TIMEOUT` and whether a retry is ever safe
      (it is only safe if Q8 shows the Wallet de-duplicates).

12. **`docs(wallet): record duplicate request behaviour`** — *Q8*
    - Same request twice with the same `reference`; same `betId` with a different
      `reference`; same `reference` with a different amount.
    - Does the Wallet de-duplicate? Does it apply twice? Does it return an error
      or the original result? **This decides whether retries and PENDING
      resolution by re-sending are safe** (Phases 05 and 11).

13. **`docs(wallet): record how to revert an operation`** — *Q9*
    - With no rollback endpoint: confirm that a withdraw can be reverted with a
      compensating deposit (and vice versa) for the same `betId`, and whether the
      Wallet imposes any rule about it (e.g. deposit only after withdraw of the
      same bet). Input for Phase 10 (Cancel).

14. **`docs(wallet): record restart behaviour`** — *Q10*
    - Change a balance, restart the container, read the balance again. Confirm
      the state is lost and what the initial balances are (incl. big/fractional ones).

15. **`docs(design): apply wallet findings`**
    - Fill the Phase 01 rows of `DESIGN.md` §1.
    - Mark open question 1 in `00-general-plan.md` as resolved (link to findings).
    - Add a short "Implications for later phases" section at the end of the
      findings (classification table *status/body → applied / not applied /
      unknown*, recommended timeout, retry policy).
    - If the findings contradict §3 of the general plan, list the contradictions
      here and propose the plan change — do not edit §3 silently.

## Questions to answer (sections of `docs/wallet-findings.md`)

| # | Question |
|---|---|
| Q1 | API surface: endpoints, schemas, headers, documented errors, any transaction lookup? |
| Q2 | Happy path: JSON encoding of amounts and IDs, response fields. |
| Q3 | `x-api-key`: what happens without it or with a wrong one? |
| Q4 | Validation errors: status/body per invalid input. |
| Q5 | Insufficient funds: status/body; balance untouched? |
| Q6 | Random failures: kinds, frequencies, error before vs after applying, can we tell them apart? |
| Q7 | Latency: distribution, hangs, recommended timeout. |
| Q8 | Duplicates: de-duplication by `reference` and/or `betId`? |
| Q9 | Reverting: compensating operations and their rules. |
| Q10 | Restart: state lost? initial balances? |

## Test plan

No automated tests (no code). Evidence is:
- real request/response pairs pasted in the findings;
- probe CSVs committed in `docs/wallet-spike/results/`;
- every conclusion in the findings points to the request or CSV that supports it.

Before each probe run, restart the Wallet to start from known balances.

## Open questions

1. Exact image tag to pin — decided in step 2 when we pull it.
2. Internal port of the container (README assumes `8000`) — confirmed in step 2.
3. If the failures are rare, N ≈ 200 may not be enough to see "error after
   applying"; we increase N in steps 9–10 if needed.
4. If the Wallet does **not** let us tell "error before" from "error after"
   applying, every 5xx must be treated as *unknown* → this is a finding, not a
   blocker.

## Definition of done

- Q1–Q10 answered in `docs/wallet-findings.md`, each with evidence, or explicitly
  marked "cannot be determined" with the reason.
- Classification table (*applied / not applied / unknown*) and timeout
  recommendation written.
- `DESIGN.md` Phase 01 rows filled; open question 1 of the general plan resolved.
- No files touched outside `docs/`, `DESIGN.md` and the general plan.
