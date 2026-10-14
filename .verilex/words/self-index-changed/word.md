---
word: self-index-changed
promise: The changed-word lookup selects its claim and agrees with the chain plan.
claim: self-index-changed@14e45886e4ef
entry: cli
inputs: [cmd, internal, agent, go.mod, go.sum, go.work, go.work.sum, tests/fixtures/tally]
env: [CANARY_DEFECT, GOFLAGS, GOTOOLCHAIN]
timeout: 900
---

Drive index-changed through the public CLI or shipped scripts, with separate action and observation evidence.
