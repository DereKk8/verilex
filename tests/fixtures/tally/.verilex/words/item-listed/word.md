---
word: item-listed
promise: A stored item shows up when a user lists the store.
args: [name]
claim: item-listed@b8fa4e44bc73
entry: cli
read_only: true
inputs: [bin/tally]
env: [TALLY_DEFECT, TALLY_SIMULATE_LOCK]
depends:
  config_keys: [tally.list]
  images: [ghcr.io/example/list:1]
---

The listing a user reads back.
