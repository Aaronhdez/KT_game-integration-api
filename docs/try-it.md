# Try it

Needs the stack running (Quick start in [`README.md`](../README.md)), which
also lists the seeded players and the full API reference.

A complete round trip with `curl` (bash): log in, read the balance, place a
bet, repeat it (replay), reuse its ID with another amount (conflict), settle
it, cancel the settlement and read the balance again. `RUN` makes the provider
IDs new on every run.

```bash
BASE=http://localhost:8080/api/v1
RUN=$(date +%s)
TOKEN=$(curl -s -X POST $BASE/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"player_usd","password":"usd-pass-2026"}' \
  | sed -E 's/.*"token":"([^"]+)".*/\1/')
AUTH="Authorization: Bearer $TOKEN"
JSON='Content-Type: application/json'

curl -s $BASE/player -H "$AUTH"
# {"userId":34633089486,"balance":5000.00,"currency":"USD"}

curl -s -w '%{http_code}\n' -X POST $BASE/withdraw -H "$AUTH" -H "$JSON" \
  -d '{"currency":"USD","amount":10.50,"providerTransactionId":"demo-'$RUN'-w"}'
# 201 {...,"oldBalance":5000.00,"newBalance":4989.50,"status":"COMPLETED","betStatus":"PLACED"}

curl -s -w '%{http_code}\n' -X POST $BASE/withdraw -H "$AUTH" -H "$JSON" \
  -d '{"currency":"USD","amount":10.50,"providerTransactionId":"demo-'$RUN'-w"}'
# 200 — the same body and transactionId: a replay, the Wallet is not called

curl -s -w '%{http_code}\n' -X POST $BASE/withdraw -H "$AUTH" -H "$JSON" \
  -d '{"currency":"USD","amount":99.00,"providerTransactionId":"demo-'$RUN'-w"}'
# 409 {"code":"CONFLICT","message":"withdraw: conflict: same provider transaction id with a different payload"}

curl -s -w '%{http_code}\n' -X POST $BASE/deposit -H "$AUTH" -H "$JSON" \
  -d '{"currency":"USD","amount":25.00,"providerTransactionId":"demo-'$RUN'-d","providerWithdrawnTransactionId":"demo-'$RUN'-w"}'
# 201 {...,"oldBalance":4989.50,"newBalance":5014.50,"status":"COMPLETED","betStatus":"WON"}

curl -s -w '%{http_code}\n' -X POST $BASE/cancel -H "$AUTH" -H "$JSON" \
  -d '{"providerTransactionId":"demo-'$RUN'-d"}'
# 201 {...,"oldBalance":5014.50,"newBalance":4989.50,"status":"COMPLETED","betStatus":"PLACED"}

curl -s $BASE/player -H "$AUTH"
# {"userId":34633089486,"balance":4989.50,"currency":"USD"}
```

The Wallet is deliberately unreliable: any operation may answer `202`
(`"status":"PENDING"`) instead of `201`, and `GET /player` may answer `502` or
`504`. That is expected — see [Pending transactions](../README.md#pending-transactions).
If the withdraw answers `202`, the deposit is refused with `409` ("withdraw is
not completed") until it is resolved: wait 20–40 s, repeat the withdraw
(it now answers `200` with `"status":"COMPLETED"`) and continue. The balances
above assume a fresh state (`docker compose down -v`).

In PowerShell (5.1 or later):

```powershell
$base = "http://localhost:8080/api/v1"
$run = [DateTimeOffset]::Now.ToUnixTimeSeconds()
$token = (Invoke-RestMethod -Method Post "$base/auth/login" -ContentType "application/json" `
  -Body '{"username":"player_usd","password":"usd-pass-2026"}').token
$auth = @{ Authorization = "Bearer $token" }
Invoke-RestMethod "$base/player" -Headers $auth
Invoke-RestMethod -Method Post "$base/withdraw" -Headers $auth -ContentType "application/json" `
  -Body "{`"currency`":`"USD`",`"amount`":10.50,`"providerTransactionId`":`"demo-$run-w`"}"
```

`Invoke-RestMethod` throws on a `4xx`/`5xx` answer; use `curl.exe` (not the
`curl` alias) to see those bodies.
