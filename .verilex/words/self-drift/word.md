---
word: self-drift
promise: Changed word files and added or removed planted defects force drift and recover when restored.
claim: self-drift@98cd5b7a1cad
entry: cli
inputs: [cmd, internal, agent, go.mod, go.sum, go.work, go.work.sum, tests/fixtures/tally]
env: [CANARY_DEFECT, GOFLAGS, GOTOOLCHAIN]
timeout: 900
---

Drive drift through the public CLI or shipped scripts, with separate action and observation evidence.
