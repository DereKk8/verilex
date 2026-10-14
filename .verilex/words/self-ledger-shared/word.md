---
word: self-ledger-shared
promise: A fresh stateless instance reuses intact shared passes without launching a store.
claim: self-ledger-shared@b0a08b1373c5
entry: cli
inputs: [cmd, internal, agent, go.mod, go.sum, go.work, go.work.sum, tests/fixtures/tally, .cursor/skills/verify-verilex/scripts]
env: [CANARY_DEFECT, GOFLAGS, GOTOOLCHAIN]
timeout: 900
---

Drive ledger-shared through the public CLI or shipped scripts, with separate action and observation evidence.
