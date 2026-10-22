package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func usageJSON(t *testing.T, root string, env map[string]string) map[string]any {
	t.Helper()
	got := verilex(t, root, env, "usage", "--json")
	equal(t, got.code, 0)
	var result map[string]any
	if err := json.Unmarshal([]byte(got.stdout), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestUsageRecordsVerdictsAndSurvivesCleanup(t *testing.T) {
	root := product(t)
	green := verilex(t, root, nil, "run", chain, "--tokens", "120", "--estimated", "--change", "local-change")
	equal(t, green.code, 0)
	id := lastRun(t, root).Run
	equal(t, verilex(t, root, map[string]string{"TALLY_DEFECT": "drop-adds"}, "run", chain).code, 1)
	equal(t, verilex(t, root, nil, "run", "--changed", "missing.go").code, 2)
	equal(t, verilex(t, root, nil, "usage", id, "--miss", "review found a broken requirement").code, 0)
	equal(t, verilex(t, root, nil, "usage", id, "--tokens", "150", "--estimated").code, 0)
	if err := os.RemoveAll(filepath.Join(filepath.Dir(root), "state", "tally", "runs")); err != nil {
		t.Fatal(err)
	}
	result := usageJSON(t, root, nil)
	totals := result["totals"].(map[string]any)
	equal[any](t, totals["runs"], float64(3))
	equal[any](t, totals["green"], float64(1))
	equal[any](t, totals["red"], float64(1))
	equal[any](t, totals["inconclusive"], float64(1))
	equal[any](t, totals["misses"], float64(1))
	equal[any](t, totals["tokens"], float64(150))
	equal[any](t, totals["tokens_unknown"], float64(2))
	equal[any](t, totals["tokens_estimated"], float64(1))
	entries := result["entries"].([]any)
	found := false
	for _, raw := range entries {
		entry := raw.(map[string]any)
		record := entry["record"].(map[string]any)
		if record["run"] == id {
			found = true
			equal(t, record["project"], "tally")
			equal(t, entry["change"], "local-change")
			equal(t, entry["miss"].(map[string]any)["review"], "review found a broken requirement")
			if entry["seconds"].(float64) <= 0 {
				t.Fatal("missing measured runtime")
			}
			equal(t, len(record["words"].([]any)), 3)
		}
	}
	equal(t, found, true)
}

func TestUsageConcurrentRunsAndInvalidInput(t *testing.T) {
	root := product(t)
	shared := t.TempDir()
	env := map[string]string{"VERILEX_USAGE_LEDGER": shared}
	var pending []*process
	for i := 0; i < 4; i++ {
		pending = append(pending, start(t, root, env, "run", chain))
	}
	for _, p := range pending {
		equal(t, p.wait(t).code, 0)
	}
	totals := usageJSON(t, root, env)["totals"].(map[string]any)
	equal[any](t, totals["runs"], float64(4))
	equal[any](t, totals["green"], float64(4))
	equal(t, verilex(t, root, env, "run", chain, "--tokens", "-1").code, 2)
	equal(t, verilex(t, root, env, "usage", "../outside", "--miss", "review").code, 2)
	equal(t, verilex(t, root, env, "usage", "absent", "--tokens", "10").code, 2)
	equal[any](t, usageJSON(t, root, env)["totals"].(map[string]any)["runs"], float64(4))
}

func TestUsageNeverMarksRedOrInconclusiveAsMiss(t *testing.T) {
	root := product(t)
	equal(t, verilex(t, root, map[string]string{"TALLY_DEFECT": "drop-adds"}, "run", chain).code, 1)
	id := lastRun(t, root).Run
	got := verilex(t, root, nil, "usage", id, "--miss", "review")
	equal(t, got.code, 2)
	contains(t, got.stderr, "only a green run")
	equal[any](t, usageJSON(t, root, nil)["totals"].(map[string]any)["misses"], float64(0))
}

func TestUsageAnnotationHonorsJSON(t *testing.T) {
	root := product(t)
	equal(t, verilex(t, root, nil, "run", chain).code, 0)
	id := lastRun(t, root).Run
	got := verilex(t, root, nil, "usage", id, "--tokens", "3", "--json")
	equal(t, got.code, 0)
	var result map[string]any
	if err := json.Unmarshal([]byte(got.stdout), &result); err != nil {
		t.Fatalf("not JSON: %q: %v", got.stdout, err)
	}
	equal[any](t, result, map[string]any{"run": id})
	equal[any](t, usageJSON(t, root, nil)["totals"].(map[string]any)["tokens"], float64(3))
}

func TestUsageJSONListsEveryMissOldestFirst(t *testing.T) {
	root := product(t)
	equal(t, verilex(t, root, nil, "run", chain).code, 0)
	id := lastRun(t, root).Run
	notes := []string{"first finding", "second finding", "third finding", "fourth finding"}
	for _, note := range notes {
		equal(t, verilex(t, root, nil, "usage", id, "--miss", note).code, 0)
	}
	result := usageJSON(t, root, nil)
	equal[any](t, result["totals"].(map[string]any)["misses"], float64(1))
	entry := result["entries"].([]any)[0].(map[string]any)
	misses, _ := entry["misses"].([]any)
	var got []string
	for _, raw := range misses {
		got = append(got, raw.(map[string]any)["review"].(string))
	}
	equal(t, len(got), len(notes))
	for i, note := range notes {
		equal(t, got[i], note)
	}
	equal[any](t, entry["miss"].(map[string]any)["review"], notes[len(notes)-1])
}

func TestUsageConcurrentMissesKeepRecordedOrder(t *testing.T) {
	root := product(t)
	equal(t, verilex(t, root, nil, "run", chain).code, 0)
	id := lastRun(t, root).Run
	for round := 0; round < 4; round++ {
		var pending []*process
		for i := 0; i < 8; i++ {
			pending = append(pending, start(t, root, nil, "usage", id, "--miss", fmt.Sprintf("finding %d-%d", round, i)))
		}
		for _, p := range pending {
			equal(t, p.wait(t).code, 0)
		}
	}
	entry := usageJSON(t, root, nil)["entries"].([]any)[0].(map[string]any)
	misses := entry["misses"].([]any)
	equal(t, len(misses), 32)
	var last time.Time
	for _, raw := range misses {
		recorded, err := time.Parse(time.RFC3339Nano, raw.(map[string]any)["recorded"].(string))
		if err != nil {
			t.Fatal(err)
		}
		if recorded.Before(last) {
			t.Fatalf("miss recorded %s listed after %s", recorded, last)
		}
		last = recorded
	}
	equal[any](t, entry["miss"], misses[len(misses)-1])
}
