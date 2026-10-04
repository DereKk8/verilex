# verilex rules

1. Correctness is a hard limit. Never trade a correct verdict for speed. A false green is unacceptable; a false red is also a defect because it costs agents time.
2. Three verdicts: green, red, inconclusive. Never report an environment failure as a product failure.
3. Maximize speed and reduce cost in both the tool's runtime and the tokens agents spend using it.
4. The biggest saving is work that never runs. Skip a step only when every proof stamp matches. When in doubt, re-run.
5. Quiet output: verdict first, failures only, evidence by reference.
6. Go core; words in any language.
7. No harness machinery inside verilex. Resume runs from stamps and the ledger.

Entrypoint: `cmd/verilex/main.go`. Word contract: README.md, "Adding verilex to a project".
Behavior tests: `go test ./...`. Live verification: `.cursor/skills/verify-verilex/SKILL.md`.
