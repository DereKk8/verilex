---
word: self-ci-main-ref
promise: The canary scripts refuse a tag in place of the origin/main branch ref.
claim: self-ci-main-ref@3daf1ede9f88
entry: cli
inputs: ["cmd","go.mod","go.sum","go.work","go.work.sum","tests/fixtures/tally","scripts/ci","scripts/canary-guards"]
env: [CANARY_DEFECT, GOFLAGS, GOTOOLCHAIN]
timeout: 900
---

Drive ci-main-ref through the public CLI or shipped scripts, with separate action and observation evidence.
