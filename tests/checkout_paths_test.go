package tests

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DereKk8/verilex/internal/runner"
)

// admittedSkippingCheckout is a tally checkout whose three words are admitted and whose
// default chain already has a green run, so check is clean and plan skips.
func admittedSkippingCheckout(t *testing.T) string {
	t.Helper()
	root := baseline(t)
	used(t, root, chain)
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (3 admitted)\n")
	plan := verilex(t, root, nil, "plan", chain)
	if !strings.HasPrefix(plan.stdout, "plan: skip 3, run 0\n") {
		t.Fatalf("baseline plan: %q", plan.stdout)
	}
	return root
}

// verilexIn runs the CLI with working directory dir. root is still the product: pass
// --project root when dir is not the product.
func verilexIn(t *testing.T, root, dir string, args ...string) output {
	t.Helper()
	cmd, stdout, stderr := command(root, nil, args...)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return (&process{cmd, stdout, stderr}).wait(t)
}

func groupWritable(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.Chmod(path, info.Mode().Perm()|0o020)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func stampsOf(t *testing.T, raw string) []string {
	t.Helper()
	var rec runner.Record
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		t.Fatal(err)
	}
	if len(rec.Words) != 3 {
		t.Fatalf("words: %+v", rec.Words)
	}
	stamps := make([]string, len(rec.Words))
	for i, word := range rec.Words {
		if word.Stamp == "" {
			t.Fatalf("%s has no stamp in %s", word.Word, raw)
		}
		stamps[i] = word.Word + "=" + word.Stamp
	}
	return stamps
}

// Group-write bits are not part of the commit. A checkout that differs only by those
// bits keeps the same proof-stamp digests, and check reports no drift.
func TestGroupWritableCheckoutKeepsDigestsAndSkips(t *testing.T) {
	root := admittedSkippingCheckout(t)
	beforeRun := verilex(t, root, nil, "run", "--json", chain)
	equal(t, beforeRun.code, 0)
	beforeStamps := stampsOf(t, beforeRun.stdout)
	beforePlan := verilex(t, root, nil, "plan", chain).stdout
	if !strings.HasPrefix(beforePlan, "plan: skip 3, run 0\n") {
		t.Fatalf("baseline plan: %q", beforePlan)
	}

	groupWritable(t, filepath.Join(root, ".verilex"))

	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (3 admitted)\n")
	equal(t, verilex(t, root, nil, "plan", chain).stdout, beforePlan)
	afterRun := verilex(t, root, nil, "run", "--json", chain)
	equal(t, afterRun.code, 0)
	equal(t, stampsOf(t, afterRun.stdout), beforeStamps)
}

const changedTally = "changed: 3 claim(s); 0 of 3 known chain(s) must re-run\n" +
	"item-added  A named item is in the store.\n" +
	"item-listed  A stored item shows up when a user lists the store.\n" +
	"store-opened  A new, empty store is open and ready for items.\n" +
	"  3 chain(s) still skip: `verilex plan '<chain>'` cites the run each relies on\n"

// A relative --changed path is resolved against --project, from any working directory.
func TestRelativeChangedPathResolvesAgainstProject(t *testing.T) {
	root := admittedSkippingCheckout(t)
	outside := t.TempDir()

	t.Run("index", func(t *testing.T) {
		got := verilexIn(t, root, outside, "--project", root, "index", "--changed", "bin/tally")
		equal(t, got.code, 0)
		equal(t, got.stderr, "")
		equal(t, got.stdout, changedTally)
	})
	t.Run("files", func(t *testing.T) {
		got := verilexIn(t, root, outside, "--project", root, "index", "--json", "--changed", "bin/tally")
		equal(t, got.code, 0)
		var change struct {
			Files  []string `json:"files"`
			Claims []struct {
				Claim string `json:"claim"`
			} `json:"claims"`
		}
		if err := json.Unmarshal([]byte(got.stdout), &change); err != nil {
			t.Fatal(err)
		}
		equal(t, change.Files, []string{"bin/tally"})
		names := make([]string, len(change.Claims))
		for i, claim := range change.Claims {
			names[i] = claim.Claim
		}
		equal(t, names, []string{"item-added", "item-listed", "store-opened"})
	})
	t.Run("run", func(t *testing.T) {
		got := verilexIn(t, root, outside, "--project", root, "run", "--changed", "bin/tally")
		equal(t, got.code, 0)
		equal(t, got.stderr, "")
		if !strings.HasPrefix(got.stdout, "green: 3 green, skipped: stamps match run ") {
			t.Fatalf("stdout %q", got.stdout)
		}
	})
	t.Run("plan", func(t *testing.T) {
		got := verilexIn(t, root, outside, "--project", root, "plan", "--changed", "bin/tally")
		equal(t, got.code, 0)
		equal(t, got.stderr, "")
		if !strings.HasPrefix(got.stdout, "plan: skip 3, run 0;") {
			t.Fatalf("stdout %q", got.stdout)
		}
	})
	t.Run("absolute", func(t *testing.T) {
		got := verilexIn(t, root, outside, "--project", root, "index", "--changed", filepath.Join(root, "bin", "tally"))
		equal(t, got.code, 0)
		equal(t, got.stderr, "")
		equal(t, got.stdout, changedTally)
	})
}

// Bits git does not record leave the admitted digests and the proof stamps alone.
// 0700 still has the owner execute bit. 0654 has only the group execute bit.
// A directory mode is not recorded at all.
func TestUntrackedPermissionBitsKeepDigestsAndSkip(t *testing.T) {
	cases := []struct {
		name string
		path string
		mode os.FileMode
	}{
		{"file 0700", filepath.Join(".verilex", "words", "item-stored", "run"), 0o700},
		{"directory 0700", filepath.Join(".verilex", "words", "item-stored"), 0o700},
		{"group execute only", filepath.Join(".verilex", "words", "item-stored", "word.md"), 0o654},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := admittedSkippingCheckout(t)
			beforeRun := verilex(t, root, nil, "run", "--json", chain)
			equal(t, beforeRun.code, 0)
			beforeStamps := stampsOf(t, beforeRun.stdout)
			beforePlan := verilex(t, root, nil, "plan", chain).stdout
			if !strings.HasPrefix(beforePlan, "plan: skip 3, run 0\n") {
				t.Fatalf("baseline plan: %q", beforePlan)
			}
			if err := os.Chmod(filepath.Join(root, c.path), c.mode); err != nil {
				t.Fatal(err)
			}
			equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (3 admitted)\n")
			equal(t, verilex(t, root, nil, "plan", chain).stdout, beforePlan)
			afterRun := verilex(t, root, nil, "run", "--json", chain)
			equal(t, afterRun.code, 0)
			equal(t, stampsOf(t, afterRun.stdout), beforeStamps)
		})
	}
}

// Removing the owner execute bit from a file git recorded as executable changes the
// admission digest and the proof stamp. word.md starts at 0644, so the test first
// records it as executable and then returns it to 0644.
func TestRemovingOwnerExecuteInvalidatesAdmissionAndStamp(t *testing.T) {
	root := admittedSkippingCheckout(t)
	contract := filepath.Join(root, ".verilex", "words", "item-stored", "word.md")
	if err := os.Chmod(contract, 0o755); err != nil {
		t.Fatal(err)
	}
	admitted(t, root, "item-stored")
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (3 admitted)\n")

	live := verilex(t, root, nil, "run", "--json", chain)
	equal(t, live.code, 0)
	var rec runner.Record
	if err := json.Unmarshal([]byte(live.stdout), &rec); err != nil {
		t.Fatal(err)
	}
	equal(t, rec.Skipped, false)
	equal(t, rec.Rerun, "item-stored apple: word changed")

	skipped := verilex(t, root, nil, "plan", chain)
	if !strings.HasPrefix(skipped.stdout, "plan: skip 3, run 0\n") {
		t.Fatalf("plan after recording the executable contract: %q", skipped.stdout)
	}
	beforeRun := verilex(t, root, nil, "run", "--json", chain)
	equal(t, beforeRun.code, 0)
	before := stampNamed(t, stampsOf(t, beforeRun.stdout), "item-stored")

	if err := os.Chmod(contract, 0o644); err != nil {
		t.Fatal(err)
	}
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 3 admitted drift-suspect; they always run\n  item-stored: the word's files changed since onboarding\n")
	removed := verilex(t, root, nil, "run", "--json", chain)
	equal(t, removed.code, 0)
	if err := json.Unmarshal([]byte(removed.stdout), &rec); err != nil {
		t.Fatal(err)
	}
	equal(t, rec.Skipped, false)
	after := stampNamed(t, stampsOf(t, removed.stdout), "item-stored")
	if after == before {
		t.Fatalf("item-stored stamp still %s", before)
	}
}

func stampNamed(t *testing.T, stamps []string, name string) string {
	t.Helper()
	prefix := name + "="
	for _, stamp := range stamps {
		if strings.HasPrefix(stamp, prefix) {
			return stamp
		}
	}
	t.Fatalf("no stamp for %s in %v", name, stamps)
	return ""
}
