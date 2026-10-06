# verilex rules

1. Correctness is a hard limit. verilex never trades a correct verdict for speed. A false green is unacceptable; a false red is also a defect because it costs agents time.
2. Three verdicts: green, red, inconclusive. An environment failure is never reported as a product failure.
3. Obsessed with maximum speed and cost reduction on both sides: the tool's own runtime and the tokens agents spend using it.
4. The biggest saving is work that never runs. A chain is skipped only when every proof stamp matches a green result younger than 7 days and every word is admitted (never provisional or drift-suspect); when in doubt, re-run. On a kept instance (`--continue`) only the prefix its history proves is skipped, and a word that changes state never runs twice on one instance. `plan` and `run` share one skip decision.
5. Quiet output: verdict first, failures only, evidence by reference.
6. Go core; words in any language.
7. No harness machinery inside the verilex core; harness code lives only in agent/. Resuming a run comes from stamps, the ledger and a kept instance's history.

Entrypoint: `cmd/verilex/main.go`. Word contract: README.md, "Adding verilex to a project".
Behavior tests: `go test ./...`.
