---
word: self-ledger-integrity
promise: Changed shared-ledger evidence forces a live chain until a fresh pass stands.
claim: self-ledger-integrity@6fbccebde823
entry: cli
inputs: ["cmd","go.mod","go.sum","go.work","go.work.sum","tests/fixtures/tally","internal/ledger/store.go","internal/runner"]
env: [CANARY_DEFECT, GOFLAGS, GOTOOLCHAIN]
timeout: 900
---

Drive ledger-integrity through the public CLI or shipped scripts, with separate action and observation evidence.
