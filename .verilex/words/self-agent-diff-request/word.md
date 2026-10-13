---
word: self-agent-diff-request
promise: The launcher refuses a green run whose request omits a changed path.
claim: self-agent-diff-request@dd9743eddff1
entry: cli
inputs: ["cmd","go.mod","go.sum","go.work","go.work.sum","tests/fixtures/tally","agent","internal/index","internal/cli"]
env: [CANARY_DEFECT, GOFLAGS, GOTOOLCHAIN]
timeout: 900
---

Drive agent-diff-request through the public CLI or shipped scripts, with separate action and observation evidence.
