# Trust frame

## Sub-features

Launch ownership, doctor checks, environment refusal, per-step evidence, and automatic cleanup.

## How to get to it (user POV)

Every `verilex run` enters the frame automatically.

## Driving it with the CLI

Run the apple chain and read launch, doctor, and cleanup in its ledger.
Confirm no store remains and every word's evidence stdout survives.
Set `TALLY_SIMULATE_LOCK=1` and run `store-open`. Expect exit 2 and a blocked verdict.
Read doctor-after-failure and the reason naming the lock.
Use `go test ./...` for foreign-instance refusal, secret leaks, result honesty, and timeout cases.

## Gotchas

Evidence lives under `VERILEX_HOME`, separate from product stores. A word failure can become blocked
when the second doctor refuses the instance. Python 3 belongs to the sample product, not the core.
