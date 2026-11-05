# Wallet spike — how to run

Tooling and evidence for Phase 01 (`docs/plans/01-wallet-spike.md`).
Conclusions live in [`../wallet-findings.md`](../wallet-findings.md).

## Image tested

| Item | Value |
|---|---|
| Image | `kentechsp/wallet-client:latest` |
| Digest | `sha256:682a98803dd117469759e5f10be788e4a0418de3fa5dd12819bb4fe2d1890abf` |
| Image created | 2025-06-25 |
| Platform | **`linux/arm64` only** (single-arch manifest) |
| Container port | `8000/tcp` |

On an amd64 host Docker runs it under emulation and prints a platform warning.
It works, but latency numbers measured here include the emulation overhead.

## Start

```bash
docker run -d --name wallet-spike -p 8000:8000 kentechsp/wallet-client:latest
```

- Swagger UI: <http://localhost:8000/swagger/index.html>
- OpenAPI document: <http://localhost:8000/swagger/doc.json>

## Reset to the initial balances

The Wallet keeps its state in memory, so a restart resets it
(to be confirmed in Q10 of the findings):

```bash
docker restart wallet-spike
```

## Stop and remove

```bash
docker rm -f wallet-spike
```

## Run the probe

From `docs/wallet-spike/`, with a freshly restarted Wallet:

```powershell
powershell -ExecutionPolicy Bypass -File .\probe.ps1 -Operation withdraw -Iterations 200 -OutFile results\withdraw.csv
```

Parameters: `-Operation withdraw|deposit|balance`, `-Iterations`, `-UserId`,
`-Currency`, `-Amount` (string, e.g. `'1.00'`), `-TimeoutSeconds` (default 30).
CSV columns: `status` (HTTP code, `timeout` or `no-response`), `walletCode`,
`latencyMs`, `balanceBefore`, `balanceAfter`, `delta`, `applied`
(`yes` = balance moved by exactly the amount, `no` = unchanged, `other`,
`unreadable` = balance could not be read), `body` (first 120 chars).

Calls are strictly sequential: do not run two probes (or REST Client requests)
against the same user at the same time, or the balance comparison is meaningless.

## Files

| File | Purpose |
|---|---|
| `swagger.json` | Copy of the Wallet OpenAPI document (step 3). |
| `requests.http` | Manual requests for VS Code REST Client (steps 4–7, 12–13). |
| `probe.ps1` | Repeated calls with balance before/after, CSV output (step 8). |
| `results/` | Raw probe CSVs used as evidence (steps 9–11). |
