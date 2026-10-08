---
word: chain-red
promise: A broken product gives a red verdict and exit 1.
claim: chain-red@a5fa50654384
entry: cli
inputs: [cmd, internal, go.mod, go.sum, tests/fixtures/tally]
env: [CANARY_DEFECT, GOFLAGS, GOTOOLCHAIN]
timeout: 900
---

The verilex under test reports red on a tally with a planted defect.
