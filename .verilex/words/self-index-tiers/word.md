---
word: self-index-tiers
promise: Index tiers list this product's admitted words and expose word drift consistently.
claim: self-index-tiers@ac98ed92d7fc
entry: cli
inputs: ["cmd","go.mod","go.sum","go.work","go.work.sum","tests/fixtures/tally","internal/index/index.go","internal/cli/index.go"]
env: [CANARY_DEFECT, GOFLAGS, GOTOOLCHAIN]
timeout: 900
---

Drive index-tiers through the public CLI or shipped scripts, with separate action and observation evidence.
