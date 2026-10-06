---
word: store-open
promise: A new, empty store is open and ready for items.
claim: store-opened@828f801851c1
entry: cli
inputs: [bin/tally]
env: [TALLY_DEFECT, TALLY_SIMULATE_LOCK]
depends:
  config_keys: [tally.open]
  images: [ghcr.io/example/store:1]
---

The store a user opens before adding anything.
