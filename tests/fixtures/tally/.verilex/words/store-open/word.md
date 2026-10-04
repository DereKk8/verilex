---
word: store-open
promise: A new, empty store is open and ready for items.
requires: []
provides: [store]
inputs: [bin/tally]
env: [TALLY_DEFECT, TALLY_SIMULATE_LOCK]
implements:
  - verify-tally/features/store.md#store-open
---

The store a user opens before adding anything.
