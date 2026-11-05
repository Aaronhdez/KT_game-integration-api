# Design decisions

> Skeleton. Each section is filled in by the phase indicated. Required content
> per section 11 of the assessment. Target length: 1–2 pages.

## 1. Behaviour under failure

| Situation | Behaviour | Why |
|---|---|---|
| Wallet returns an error **before** applying the operation | *TBD (05, 08)* | |
| Wallet returns an error **after** applying it | *TBD (01, 05, 08)* | |
| Wallet times out | *TBD (05, 08)* | |
| Process stops between the Wallet call and the DB write | *TBD (08, 11)* | |
| Provider retries / sends the same request concurrently | *TBD (08)* | |
| Wallet restarts and loses its state | *TBD (01, 11)* | |

## 2. How a PENDING transaction gets resolved

*TBD (11).*

## 3. Retry of a request whose previous attempt ended FAILED

*TBD (11).*

## 4. Zero-amount deposits

*TBD (09).*

## 5. Other assumptions, decisions and future improvements

*Filled in progressively.*
