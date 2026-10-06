# Store

A user opens a new, empty store before adding anything.

## Sub-features

- `store-open` opens an empty store.

## How to get to it (user POV)

- Run `tally --store DIR open`.

## Driving it with the tally CLI

Preconditions: an owned, empty store directory.

- `store-open`: Run `bin/tally --store "$STORE" open`. Expect exit 0 and `opened`; `store.json` holds `{"items": []}`.

## Gotchas

- Opening an already-open store exits 1.
