package tests

import (
	"path/filepath"
	"testing"
)

func TestUsageKeepsStartAndCallerTokensUntilCompletion(t *testing.T) {
	root := product(t)
	addHeldWord(t, root)
	holding := t.TempDir()
	p := start(t, root, map[string]string{"HOLDING": holding}, "run", "store-open | store-held")
	id := held(t, holding, 1)[0]
	t.Cleanup(func() { release(t, holding, id) })
	equal[any](t, usageJSON(t, root, nil)["totals"].(map[string]any)["unfinished"], float64(1))
	equal(t, verilex(t, root, nil, "usage", id, "--tokens", "0").code, 0)
	equal(t, verilex(t, root, nil, "usage", id, "--miss", "review").code, 2)
	release(t, holding, id)
	equal(t, p.wait(t).code, 0)
	totals := usageJSON(t, root, nil)["totals"].(map[string]any)
	equal[any](t, totals["unfinished"], float64(0))
	equal[any](t, totals["green"], float64(1))
	equal[any](t, totals["tokens"], float64(0))
	equal[any](t, totals["tokens_unknown"], float64(0))
	equal(t, verilex(t, root, nil, "plan", "store-open").code, 0)
	equal[any](t, usageJSON(t, root, nil)["totals"].(map[string]any)["runs"], float64(1))
}

func TestUsageWriteFailureDoesNotChangeProductVerdict(t *testing.T) {
	root := product(t)
	path := filepath.Join(t.TempDir(), "not-a-directory")
	write(t, path, "occupied", 0600)
	env := map[string]string{"VERILEX_USAGE_LEDGER": path}
	got := verilex(t, root, env, "run", chain)
	equal(t, got.code, 0)
	contains(t, got.stdout, "green:")
	contains(t, got.stderr, "usage ledger write failed:")
	env["TALLY_DEFECT"] = "drop-adds"
	got = verilex(t, root, env, "run", chain)
	equal(t, got.code, 1)
	contains(t, got.stdout, "red:")
	contains(t, got.stderr, "usage ledger write failed:")
}

func TestUsageStoresClaimCoverageAndSkip(t *testing.T) {
	root := product(t)
	admitted(t, root, "store-open", "item-stored", "item-listed")
	equal(t, verilex(t, root, nil, "run", chain).code, 0)
	equal(t, verilex(t, root, nil, "run", "--claim", "store-opened", "--changed", ".verilex/words/item-listed/run").code, 0)
	result := usageJSON(t, root, nil)
	entries := result["entries"].([]any)
	found := false
	for _, raw := range entries {
		record := raw.(map[string]any)["record"].(map[string]any)
		if record["warning"] == "1 touched claim not covered" {
			found = true
			equal[any](t, record["skipped"], true)
			equal[any](t, record["requested"].(map[string]any)["claims"].([]any)[0], "store-opened")
			equal[any](t, record["uncovered"].([]any)[0].(map[string]any)["claim"], "item-listed")
			equal[any](t, record["claims"].([]any)[0].(map[string]any)["verdict"], "green")
		}
	}
	equal(t, found, true)
}
