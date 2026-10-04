# verilex rules

1. Correctness is a hard limit. verilex never trades a correct verdict for speed. A false green is unacceptable; a false red is also a defect because it costs agents time.
2. Three verdicts: green, red, inconclusive. An environment failure is never reported as a product failure.
3. Obsessed with maximum speed and cost reduction on both sides: the tool's own runtime and the tokens agents spend using it.
4. The biggest saving is work that never runs. A step is skipped only when every proof stamp matches; when in doubt, re-run.
5. Quiet output: verdict first, failures only, evidence by reference.
6. Go core; words in any language.
7. No harness machinery inside verilex; resuming a run comes from stamps and the ledger.

Entrypoint: `cmd/verilex/main.go`. Word contract: README.md, "Adding verilex to a project".
Behavior tests: `go test ./...`.
