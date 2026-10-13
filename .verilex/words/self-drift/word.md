---
word: self-drift
promise: Changed word files and newly added planted defects force drift and recover when restored.
claim: self-drift@fa592255b621
entry: cli
inputs: ["cmd","go.mod","go.sum","go.work","go.work.sum","tests/fixtures/tally","internal/lifecycle","internal/cli/surface.go"]
env: [CANARY_DEFECT, GOFLAGS, GOTOOLCHAIN]
timeout: 900
---

Drive drift through the public CLI or shipped scripts, with separate action and observation evidence.
