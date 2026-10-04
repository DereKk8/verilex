# Run management

## Sub-features

Keeping an owned instance, listing runs, explicit cleanup, and repeat cleanup.

## How to get to it (user POV)

Use `verilex run 'store-open' --keep --json`, `verilex runs`, and `verilex cleanup <run-id>`.

## Driving it with the CLI

Keep a store and read the run ID from the result JSON. Confirm one store exists and its owner matches the ID.
List runs and find `pass  cleanup=kept  store-open` for that ID.
Clean the same ID and expect `cleanup: done`. Confirm the store disappeared.
List again and expect `cleanup=done`. Repeat cleanup and expect the already-cleaned message.
Read the word's evidence stdout after cleanup.

## Gotchas

Run management remains available when the word dictionary is malformed. Cleanup addresses a run ID,
not a store path. Preserve the ledger and transcripts after the proof ends.
