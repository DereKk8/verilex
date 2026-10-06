# Items

A user adds named items to an open store and lists them.

## Sub-features

- `item-add` stores a named item.
- `item-list` lists every stored item, one per line.

## How to get to it (user POV)

- Run `tally --store DIR add NAME`.
- Run `tally --store DIR list`.

## Driving it with the tally CLI

Preconditions: an open store.

- `item-add`: Run `bin/tally --store "$STORE" add NAME`. Expect exit 0 and `added NAME`; `store.json` lists NAME.
- `item-list`: Run `bin/tally --store "$STORE" list`. Expect NAME on its own line.

## Gotchas

- `added NAME` alone does not prove the item was stored; read `store.json`.
