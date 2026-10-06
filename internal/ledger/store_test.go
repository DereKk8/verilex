package ledger_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/DereKk8/verilex/internal/ledger"
	"github.com/DereKk8/verilex/internal/stamp"
)

const (
	slot        = "5b0d1a7b6e2f4c3d8a9e0f1b2c3d4e5f60718293a4b5c6d7e8f9a0b1c2d3e4f5"
	digest      = "0f9e8d7c6b5a49382716051f2e3d4c5b6a79881726354a5b6c7d8e9f0a1b2c3d"
	fingerprint = "18e2db0cee8f6c1d2e3f405162738495a6b7c8d9e0f1a2b3c4d5e6f708192a3b"
)

var step = stamp.Stamp{Slot: slot, Digest: digest, Components: map[string]string{"claim": fingerprint, "word": "1"}}

// evidence writes a step's evidence directory: the word's result object and its exit code.
func evidence(t *testing.T, stdout, exit string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range map[string]string{"stdout": stdout, "exit": exit} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func green(t *testing.T, run string) ledger.Entry {
	return ledger.Entry{
		Label: "item-stored apple", Verdict: "green", Stamp: digest, Components: step.Components, Claim: "item-added@18e2db0cee8f",
		Run: run, Owner: run, Observation: "store.json lists apple",
		Evidence: evidence(t, `{"verdict": "pass", "observation": "store.json lists apple"}`+"\n", "0\n"),
	}
}

func reuse(store ledger.Store, claim string) ([]ledger.Entry, string) {
	return store.Reuse([]string{"item-stored apple"}, []string{claim}, []stamp.Stamp{step})
}

// Rule: only a green result whose run launched its own instance, whose claim version its stamp
// holds, and whose evidence meets the evidence contract becomes a pass.
func TestRecordRefusesWhatMayNotBecomeAPass(t *testing.T) {
	cases := []struct {
		name    string
		change  func(*ledger.Entry)
		refusal string
	}{
		{"red result", func(e *ledger.Entry) { e.Verdict = "red" }, "item-stored apple: only a green result is a pass"},
		{"unowned instance", func(e *ledger.Entry) { e.Owner = "1-launcher" }, "item-stored apple: run 2-recorder drove an instance run 1-launcher launched; only a run that launched its own instance records a pass"},
		{"claim version its stamp does not hold", func(e *ledger.Entry) { e.Claim = "item-added@27d4aafc09fd" }, "item-stored apple: claim item-added@27d4aafc09fd is not the claim version its stamp holds"},
		{"pass without a second observation", func(e *ledger.Entry) {
			e.Evidence = evidence(t, `{"verdict": "pass"}`+"\n", "0\n")
		}, "item-stored apple: its evidence misses the evidence contract: pass without a second observation"},
		{"exit code that disagrees", func(e *ledger.Entry) {
			e.Evidence = evidence(t, `{"verdict": "pass", "observation": "fine"}`+"\n", "1\n")
		}, "item-stored apple: its evidence misses the evidence contract: exit 1 disagrees with verdict pass"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := ledger.At(t.TempDir())
			entry := green(t, "2-recorder")
			c.change(&entry)
			err := store.Record(map[string]ledger.Entry{slot: entry})
			if err == nil || err.Error() != c.refusal {
				t.Fatalf("got %v, want %q", err, c.refusal)
			}
			if _, why := reuse(store, "item-added@18e2db0cee8f"); why != "item-stored apple: no green result on record" {
				t.Fatalf("a refused pass stands: %q", why)
			}
		})
	}
}

// Rule: concurrent writers to one ledger each keep their pass; none is lost or mixed into another.
func TestConcurrentRecordsKeepEveryPass(t *testing.T) {
	dir := t.TempDir()
	store := ledger.At(dir)
	const n = 48
	entries := make([]ledger.Entry, n)
	for i := range entries {
		entries[i] = green(t, fmt.Sprintf("%d-run", i))
	}
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range entries {
		wg.Go(func() { errs[i] = ledger.At(dir).Record(map[string]ledger.Entry{slot: entries[i]}) })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	files, err := filepath.Glob(filepath.Join(dir, "passes", slot, "*.json"))
	if err != nil || len(files) != n {
		t.Fatalf("%d passes on record, want %d: %v", len(files), n, err)
	}
	runs := map[string]bool{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		run := strings.SplitN(strings.SplitN(string(data), `"run": "`, 2)[1], `"`, 2)[0]
		runs[run] = true
	}
	if len(runs) != n {
		t.Fatalf("passes name %d runs, want %d", len(runs), n)
	}
	got, why := reuse(store, "item-added@18e2db0cee8f")
	if why != "" || len(got) != 1 || !runs[got[0].Run] || got[0].Observation != "store.json lists apple" {
		t.Fatalf("got %+v, %q", got, why)
	}
	if _, why = reuse(store, "item-added@27d4aafc09fd"); !strings.Contains(why, "proved item-added@18e2db0cee8f, not item-added@27d4aafc09fd") {
		t.Fatalf("a pass counted for another claim version: %q", why)
	}
}
