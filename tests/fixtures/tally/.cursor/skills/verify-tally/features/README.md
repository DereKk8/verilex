# tally verification map

## Baseline preconditions

- A store directory created by this run, with an `owner` file naming the run.

## Proof and skips

- Capture the command and its exit code, then read `store.json` as a second observation.
- An exit code of 75 means another process holds the store: report blocked, not failed.

## Features

- [Store](store.md) - open a new store.
- [Items](items.md) - add items and list them.
