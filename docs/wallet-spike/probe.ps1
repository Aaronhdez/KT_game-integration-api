<#
.SYNOPSIS
  Phase 01 spike: repeat one Wallet operation N times and record, per call,
  the outcome, the latency and the balance before/after (CSV).

.DESCRIPTION
  Calls are strictly sequential, so a balance change between "before" and
  "after" can only come from the call in between. That is how we tell
  "error before applying" from "error after applying".

  Each iteration uses a fresh betId and reference (no duplicates).
  Balance reads are retried because GET /balance also fails randomly.

.EXAMPLE
  .\probe.ps1 -Operation withdraw -Iterations 200 -OutFile results\withdraw.csv
  .\probe.ps1 -Operation balance  -Iterations 100 -OutFile results\balance.csv
#>
param(
    [ValidateSet('withdraw', 'deposit', 'balance')]
    [string]$Operation = 'withdraw',
    [int]$Iterations = 5,
    [long]$UserId = 34633089486,
    [string]$Currency = 'USD',
    [string]$Amount = '1.00',
    [int]$TimeoutSeconds = 30,
    [string]$BaseUrl = 'http://localhost:8000',
    [string]$ApiKey = 'Wj9QhLqMUPAHSNMxeT2o',  # public key of the mock, from the assessment
    [string]$OutFile = 'results\probe.csv'
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Net.Http
$inv = [Globalization.CultureInfo]::InvariantCulture

$client = New-Object System.Net.Http.HttpClient
$client.Timeout = [TimeSpan]::FromSeconds($TimeoutSeconds)
$client.DefaultRequestHeaders.Add('x-api-key', $ApiKey)

# Sends one request. Never throws: returns status ('200', '500', 'timeout',
# 'no-response'), body and latency in ms.
function Send-Request([string]$method, [string]$path, [string]$json) {
    $sw = [Diagnostics.Stopwatch]::StartNew()
    try {
        if ($method -eq 'GET') {
            $resp = $client.GetAsync("$BaseUrl$path").Result
        } else {
            $content = New-Object System.Net.Http.StringContent($json, [Text.Encoding]::UTF8, 'application/json')
            $resp = $client.PostAsync("$BaseUrl$path", $content).Result
        }
        $body = $resp.Content.ReadAsStringAsync().Result
        $status = [string][int]$resp.StatusCode
    } catch {
        $inner = $_.Exception
        while ($inner.InnerException) { $inner = $inner.InnerException }
        if ($inner -is [System.Threading.Tasks.TaskCanceledException]) { $status = 'timeout' } else { $status = 'no-response' }
        $body = $inner.Message
    }
    $sw.Stop()
    return [pscustomobject]@{ Status = $status; Body = $body; LatencyMs = $sw.ElapsedMilliseconds }
}

# Reads the balance, retrying random failures. Returns $null if it never succeeds.
function Get-Balance {
    for ($i = 0; $i -lt 6; $i++) {
        $r = Send-Request 'GET' "/api/v1/balance/$UserId" $null
        if ($r.Status -eq '200') { return [decimal]::Parse(($r.Body | ConvertFrom-Json).balance, $inv) }
    }
    return $null
}

function Get-Field([string]$body, [string]$name) {
    try { return [string](($body | ConvertFrom-Json).$name) } catch { return '' }
}

$runId = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
$amountDec = [decimal]::Parse($Amount, $inv)
$expectedDelta = switch ($Operation) { 'withdraw' { -$amountDec } 'deposit' { $amountDec } default { [decimal]0 } }

New-Item -ItemType Directory -Force (Split-Path -Parent $OutFile) | Out-Null
$rows = New-Object System.Collections.Generic.List[object]

for ($n = 1; $n -le $Iterations; $n++) {
    $betId = $runId * 1000 + $n
    $reference = "probe-$runId-$n"

    if ($Operation -eq 'balance') {
        $before = $null; $after = $null
        $r = Send-Request 'GET' "/api/v1/balance/$UserId" $null
    } else {
        $before = Get-Balance
        $json = '{"userId":' + $UserId + ',"currency":"' + $Currency + '","transactions":[{"amount":' + $Amount +
                ',"betId":' + $betId + ',"reference":"' + $reference + '"}]}'
        $r = Send-Request 'POST' "/api/v1/$Operation" $json
        $after = Get-Balance
    }

    # applied: did the balance move by exactly the expected amount?
    $delta = ''; $applied = ''
    if ($null -ne $before -and $null -ne $after) {
        $d = $after - $before
        $delta = $d.ToString($inv)
        if ($d -eq $expectedDelta) { $applied = 'yes' } elseif ($d -eq 0) { $applied = 'no' } else { $applied = 'other' }
    } elseif ($Operation -ne 'balance') {
        $applied = 'unreadable'
    }

    $snippet = ($r.Body -replace '\s+', ' ')
    if ($snippet.Length -gt 120) { $snippet = $snippet.Substring(0, 120) }

    $row = [pscustomobject]@{
        iteration     = $n
        operation     = $Operation
        betId         = $betId
        reference     = $reference
        status        = $r.Status
        walletCode    = Get-Field $r.Body 'code'
        latencyMs     = $r.LatencyMs
        balanceBefore = if ($null -ne $before) { $before.ToString($inv) } else { '' }
        balanceAfter  = if ($null -ne $after) { $after.ToString($inv) } else { '' }
        delta         = $delta
        applied       = $applied
        body          = $snippet
    }
    $rows.Add($row)
    Write-Host ("{0,4}/{1}  {2,-11} {3,6} ms  applied={4}" -f $n, $Iterations, $row.status, $row.latencyMs, $row.applied)
}

$rows | Export-Csv -Path $OutFile -NoTypeInformation -Encoding UTF8
Write-Host "Saved $($rows.Count) rows to $OutFile"
$rows | Group-Object status, applied | Sort-Object Count -Descending |
    Format-Table @{ n = 'status, applied'; e = { $_.Name } }, Count -AutoSize
