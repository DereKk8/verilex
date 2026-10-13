---
word: self-skill-session
promise: The verification session freezes its binary and detects later tampering.
claim: self-skill-session@7138f41cd5fa
entry: cli
inputs: ["cmd","go.mod","go.sum","go.work","go.work.sum","tests/fixtures/tally",".cursor/skills/verify-verilex/scripts/launch",".cursor/skills/verify-verilex/scripts/doctor",".cursor/skills/verify-verilex/scripts/cleanup",".cursor/skills/verify-verilex/scripts/capture"]
env: [CANARY_DEFECT, GOFLAGS, GOTOOLCHAIN]
timeout: 900
---

Drive skill-session through the public CLI or shipped scripts, with separate action and observation evidence.
