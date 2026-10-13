---
word: self-skill-session
promise: The verification session freezes its binary, detects tampering and refuses another worker's cleanup.
claim: self-skill-session@69aeb6897d44
entry: cli
inputs: ["cmd","go.mod","go.sum","go.work","go.work.sum","tests/fixtures/tally",".cursor/skills/verify-verilex/scripts/launch",".cursor/skills/verify-verilex/scripts/doctor",".cursor/skills/verify-verilex/scripts/cleanup",".cursor/skills/verify-verilex/scripts/capture",".cursor/skills/verify-verilex/scripts/worker-id"]
env: [CANARY_DEFECT, GOFLAGS, GOTOOLCHAIN]
timeout: 900
---

Drive skill-session through the public CLI or shipped scripts, with separate action and observation evidence.
