---
word: chain-order-refused
promise: A chain whose order breaks a pinned requirement is refused before anything runs.
claim: chain-order-refused@35ebdffb4db9
entry: cli
inputs: [cmd, internal, go.mod, go.sum, tests/fixtures/tally]
env: [CANARY_DEFECT, GOFLAGS, GOTOOLCHAIN]
timeout: 900
---

The verilex under test refuses a tally chain out of order and records no run.
