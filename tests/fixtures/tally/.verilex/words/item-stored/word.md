---
word: item-stored
promise: A named item is in the store.
args: [name]
requires: [store]
provides: ["item:{name}"]
inputs: [bin/tally]
env: [TALLY_DEFECT, TALLY_SIMULATE_LOCK]
implements:
  - verify-tally/features/items.md#item-add
---

A user adds an item, and the store keeps it.
