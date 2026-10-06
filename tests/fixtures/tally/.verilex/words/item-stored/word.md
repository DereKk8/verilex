---
word: item-stored
promise: A named item is in the store.
args: [name]
claim: item-added@18e2db0cee8f
entry: cli
inputs: [bin/tally]
env: [TALLY_DEFECT, TALLY_SIMULATE_LOCK]
depends:
  config_keys: [tally.store]
  images: [ghcr.io/example/tally:1]
---

A user adds an item, and the store keeps it.
