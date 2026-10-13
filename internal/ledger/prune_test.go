package ledger

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	pruneSlot  = "5b0d1a7b6e2f4c3d8a9e0f1b2c3d4e5f60718293a4b5c6d7e8f9a0b1c2d3e4f5"
	pruneStamp = "0f9e8d7c6b5a49382716051f2e3d4c5b6a79881726354a5b6c7d8e9f0a1b2c3d"
	pruneClaim = "18e2db0cee8f6c1d2e3f405162738495a6b7c8d9e0f1a2b3c4d5e6f708192a3b"
)

// seed records one pass in slot as of recorded, then gives its file the file time at.
func seed(t *testing.T, s Store, slot string, recorded, at time.Time) string {
	t.Helper()
	ev := t.TempDir()
	for name, text := range map[string]string{"stdout": `{"verdict": "pass", "observation": "o"}` + "\n", "exit": "0\n"} {
		if err := os.WriteFile(filepath.Join(ev, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	e := Entry{
		Label: "w", Verdict: "green", Stamp: pruneStamp, Components: map[string]string{"claim": pruneClaim},
		Claim: "c@18e2db0cee8f", Run: "1-r", Owner: "1-r", Observation: "o", Evidence: ev,
	}
	glob := filepath.Join(s.dir, "passes", slot, "*.json")
	before, _ := filepath.Glob(glob)
	if err := s.add(slot, e, recorded); err != nil {
		t.Fatal(err)
	}
	after, _ := filepath.Glob(glob)
	for _, f := range after {
		if !slices.Contains(before, f) {
			if err := os.Chtimes(f, at, at); err != nil {
				t.Fatal(err)
			}
			return f
		}
	}
	t.Fatal("no new pass file")
	return ""
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// Rule: prune drops a pass only when it is old by the storage's clock, in any slot, so a young
// pass survives an old file time and a writer whose own clock runs ahead.
func TestPruneJudgesAgeOnTheStorageClock(t *testing.T) {
	s := At(t.TempDir())
	now := time.Now()
	old, young := now.Add(-MaxAge-time.Hour), now.Add(-time.Hour)
	idle := "6c0d1a7b6e2f4c3d8a9e0f1b2c3d4e5f60718293a4b5c6d7e8f9a0b1c2d3e4f5"
	aged := seed(t, s, pruneSlot, old, old)
	idleAged := seed(t, s, idle, old, old)
	recent := seed(t, s, pruneSlot, young.Add(time.Second), old) // young by its record, old file time
	s.prune()
	if exists(aged) || exists(idleAged) {
		t.Errorf("an old pass survived prune")
	}
	if !exists(recent) {
		t.Errorf("a pass young by its own record was pruned")
	}
}

func TestPruneRemovesStrandedTempEntriesOnly(t *testing.T) {
	s := At(t.TempDir())
	seed(t, s, pruneSlot, time.Now(), time.Now())
	stale, fresh := filepath.Join(s.dir, "passes", pruneSlot, ".tmp-1234"), filepath.Join(s.dir, "evidence", ".tmp-5678")
	staleDir := filepath.Join(s.dir, "evidence", ".tmp-9012")
	foreign := filepath.Join(s.dir, "evidence", ".tmp-cache")
	for _, d := range []string{staleDir, fresh, foreign} {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(stale, nil, 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-strandedAge - time.Hour)
	for _, p := range []string{stale, staleDir, foreign} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	s.prune()
	if exists(stale) || exists(staleDir) {
		t.Errorf("a stranded temporary entry survived prune")
	}
	if !exists(fresh) {
		t.Errorf("prune removed a temporary entry a live writer may own")
	}
	if !exists(foreign) {
		t.Errorf("prune removed a .tmp- entry verilex did not name")
	}
}

func TestPruneWithoutReadableStorageClockPrunesNothing(t *testing.T) {
	s := At(filepath.Join(t.TempDir(), "missing"))
	s.prune() // must not panic or create the directory
	if exists(s.dir) {
		t.Errorf("prune created the ledger directory")
	}
}

// Rule: age is judged on the storage's clock. A writer 8 days ahead of storage must not drop a
// pass that is young by the storage clock.
func TestPruneKeepsAPassYoungByTheStorageClockWhenTheWriterRunsAhead(t *testing.T) {
	s := At(t.TempDir())
	behind := time.Now().Add(-8 * 24 * time.Hour)
	s.probed = func(path string) {
		if err := os.Chtimes(path, behind, behind); err != nil {
			t.Fatal(err)
		}
	}
	// recorded 1h ago on the storage clock, so 8 days and 1h ago on the writer's
	pass := seed(t, s, pruneSlot, time.Now().Add(-8*24*time.Hour-time.Hour), time.Now())
	s.prune()
	if !exists(pass) {
		t.Errorf("a writer clock ahead of storage dropped a young pass")
	}
}

// Rule: a pass ages out by its record, not its file time, which a git checkout or a copy resets.
func TestPruneDropsAnOldPassWithAFreshFileTime(t *testing.T) {
	s := At(t.TempDir())
	pass := seed(t, s, pruneSlot, time.Now().Add(-MaxAge-time.Hour), time.Now())
	s.prune()
	if exists(pass) {
		t.Errorf("an expired pass with a fresh file time survived prune")
	}
}

// Rule: prune never follows a symlink out of the ledger.
func TestPruneLeavesTheTargetOfASlotSymlinkAlone(t *testing.T) {
	s := At(t.TempDir())
	seed(t, s, pruneSlot, time.Now(), time.Now())
	outside := t.TempDir()
	victim := filepath.Join(outside, ".tmp-1234")
	if err := os.MkdirAll(filepath.Join(victim, "keep"), 0700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-strandedAge - time.Hour)
	if err := os.Chtimes(victim, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(s.dir, "passes", "linked")); err != nil {
		t.Fatal(err)
	}
	s.prune()
	if !exists(filepath.Join(victim, "keep")) {
		t.Errorf("prune followed a symlink and deleted outside the ledger")
	}
}

// Rule: a clock probe a killed run stranded is swept like any other temporary entry.
func TestPruneRemovesAStrandedClockProbe(t *testing.T) {
	s := At(t.TempDir())
	seed(t, s, pruneSlot, time.Now(), time.Now())
	var probe string
	s.probed = func(path string) { probe = path }
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	s.storageNow(root)
	root.Close()
	if probe == "" || exists(probe) {
		t.Fatal("no probe file was created and removed")
	}
	stranded := filepath.Join(s.dir, filepath.Base(probe)+"0")
	if err := os.WriteFile(stranded, nil, 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-strandedAge - time.Hour)
	if err := os.Chtimes(stranded, old, old); err != nil {
		t.Fatal(err)
	}
	s.prune()
	if exists(stranded) {
		t.Errorf("a stranded clock probe survived prune")
	}
}

// Rule: prune reads no pass file larger than maxPass, so a damaged or planted huge file under a
// pass name costs no recording run. Such a file is left in place, even when it holds an expired pass.
func TestPruneLeavesAnOversizedPassFileUnread(t *testing.T) {
	s := At(t.TempDir())
	ev := t.TempDir()
	for name, text := range map[string]string{"stdout": `{"verdict": "pass", "observation": "o"}` + "\n", "exit": "0\n"} {
		if err := os.WriteFile(filepath.Join(ev, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	e := Entry{
		Label: "w", Verdict: "green", Stamp: pruneStamp, Components: map[string]string{"claim": pruneClaim},
		Claim: "c@18e2db0cee8f", Run: "1-r", Owner: "1-r", Observation: strings.Repeat("o", maxPass), Evidence: ev,
	}
	if err := s.add(pruneSlot, e, time.Now().Add(-MaxAge-time.Hour)); err != nil {
		t.Fatal(err)
	}
	big, _ := filepath.Glob(filepath.Join(s.dir, "passes", pruneSlot, "*.json"))
	if len(big) != 1 {
		t.Fatalf("passes: %v", big)
	}
	s.prune()
	if !exists(big[0]) {
		t.Errorf("prune read a pass file larger than %d bytes", maxPass)
	}
}
