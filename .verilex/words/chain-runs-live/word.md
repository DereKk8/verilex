---
word: chain-runs-live
promise: A chain of the sample product's words runs live, is green, and leaves no instance behind.
claim: chain-runs-live@14f20a7ab1be
entry: cli
inputs: [cmd, internal, go.mod, go.sum, tests/fixtures/tally]
env: [CANARY_DEFECT, GOFLAGS, GOTOOLCHAIN]
timeout: 900
---

The verilex under test runs a tally chain end to end and cleans up after it.
