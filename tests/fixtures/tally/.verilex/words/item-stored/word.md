---
word: item-stored
promise: A named item is in the store.
args: [name]
requires: [store]
provides: ["item:{name}"]
implements:
  - verify-tally/features/items.md#item-add
---

A user adds an item, and the store keeps it.
