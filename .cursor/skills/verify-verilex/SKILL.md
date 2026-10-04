---
name: verify-verilex
description: Drive the compiled verilex CLI against the disposable tally fixture to prove chains, refusals, evidence, and kept-run management.
---

# Verify verilex

Read [the feature map](features/README.md). Use Go 1.27.1+, Python 3, and a POSIX shell.
Run the commands from the repository root. Use a fresh `.verification` directory for each proof.
Never drive the fixture in place or use a shared product instance.

## Launch

```sh
mkdir -p .verification
go build -o .verification/verilex ./cmd/verilex
cp -a tests/fixtures/tally .verification/product
mkdir .verification/stores
export VERILEX_HOME="$PWD/.verification/state"
export TALLY_STORES="$PWD/.verification/stores"
unset TALLY_DEFECT TALLY_SIMULATE_LOCK TALLY_ADOPT_STORE
```

The CLI is short-lived. Each `run` launches and doctors its own product instance.

## Doctor

```sh
.verification/verilex --project .verification/product words
```

Expect `item-listed name`, `item-stored name`, and `store-open`, with their promises and states.
If the dictionary fails to load, stop before driving a chain.

## Drive

```sh
.verification/verilex --project .verification/product run 'store-open | item-stored apple | item-listed apple'
.verification/verilex --project .verification/product run 'item-stored apple | store-open'
TALLY_DEFECT=drop-adds .verification/verilex --project .verification/product run 'store-open | item-stored apple | item-listed apple'
TALLY_SIMULATE_LOCK=1 .verification/verilex --project .verification/product run 'store-open'
.verification/verilex --project .verification/product run 'store-open' --keep --json
.verification/verilex --project .verification/product runs
```

Expected exits in order: 0, 2, 1, 2, 0, 0. Read the kept run ID from the JSON or `runs` output.
Then run `.verification/verilex --project .verification/product cleanup <run-id>`.
The management command must print `cleanup: done` and the store directory must become empty.

## Evidence

Capture each command, stdout, stderr, and exit code under `.verification/`.
Read the actual `run.json` files under `.verification/state/tally/runs/`.
Confirm the passing chain has three passing words and `cleanup: done`.
Read the stored word's `actions.log` and observed `store.json`.
The defect must stop after `item-stored` with `fail` and a doctor-after-failure frame.
The lock must produce `blocked`, never `fail`.
The invalid chain must leave the ledger unchanged because it never launches.
Capture the kept store's owner before cleanup, then confirm cleanup removes the store while retaining evidence.

## Cleanup

Use `verilex cleanup <run-id>` for every kept instance. Never remove stores directly.
After every failed attempt, inspect `runs` and clean any kept instance owned by the proof.
Remove `.verification/product`, `.verification/stores`, and `.verification/verilex` after all instances are cleaned.
Keep `.verification/state` and the transcripts. Confirm each word's `stdout` still exists after teardown.

## Helpers

None. `go test ./...` separately runs the full isolated CLI behavior suite.
