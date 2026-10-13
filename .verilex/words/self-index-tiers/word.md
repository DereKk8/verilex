---
word: self-index-tiers
promise: Index tiers list admitted words only for the product that used them.
claim: self-index-tiers@a1bbb8e76ae2
entry: cli
inputs: ["cmd","go.mod","go.sum","go.work","go.work.sum","tests/fixtures/tally","internal/index/index.go","internal/cli/index.go"]
env: [CANARY_DEFECT, GOFLAGS, GOTOOLCHAIN]
timeout: 900
---

Drive index-tiers through the public CLI or shipped scripts, with separate action and observation evidence.
