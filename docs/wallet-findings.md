# Wallet findings

Empirical behaviour of the mock Wallet (`kentechsp/wallet-client`), produced in
Phase 01 ([plan](plans/01-wallet-spike.md)). Later phases (05, 08–11) treat this
document as the source of truth about the Wallet.

- Image tested: `kentechsp/wallet-client@sha256:682a98803dd117469759e5f10be788e4a0418de3fa5dd12819bb4fe2d1890abf`
  (`linux/arm64` only, run under emulation on amd64). How to run it:
  [`wallet-spike/README.md`](wallet-spike/README.md).
- Each answer states **what we did**, **what we saw** (real request/response or
  CSV in `wallet-spike/results/`) and **the conclusion**.

## Q1 — API surface

*TBD (step 3).* Endpoints, schemas, headers, documented errors; is there a
transaction lookup or a rollback?

## Q2 — Happy path

*TBD (step 4).* JSON encoding of amounts and IDs, response fields.

## Q3 — `x-api-key`

*TBD (step 5).* Missing / wrong key.

## Q4 — Validation errors

*TBD (step 6).* Status and body per invalid input.

## Q5 — Insufficient funds

*TBD (step 7).* Status and body; balance untouched?

## Q6 — Random failures

*TBD (steps 9–10).* Kinds, frequencies, error before vs after applying, can we
tell them apart?

## Q7 — Latency and timeout

*TBD (step 11).* Distribution, hangs, recommended `WALLET_TIMEOUT`.

## Q8 — Duplicate requests

*TBD (step 12).* De-duplication by `reference` and/or `betId`?

## Q9 — Reverting an operation

*TBD (step 13).* Compensating operations and their rules.

## Q10 — Restart

*TBD (step 14).* Is the state lost? Initial balances?

## Implications for later phases

*TBD (step 15).* Classification table (*applied / not applied / unknown*),
recommended timeout, retry policy.
