---
word: chain-skipped
promise: A proven, admitted chain is skipped on its stamps, launching nothing.
claim: chain-skipped@629142197b43
entry: cli
inputs: [cmd, internal, go.mod, go.sum, tests/fixtures/tally]
env: [CANARY_DEFECT, GOFLAGS, GOTOOLCHAIN]
timeout: 900
---

The verilex under test onboards the tally words, then skips the proven chain.
