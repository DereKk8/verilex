package tests

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DereKk8/verilex/internal/usage"
)

func TestUsageBoundsTrackedDiffAndCapturesSmallChanges(t *testing.T) {
	root := product(t)
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=verilex", "-c", "user.email=verilex@example.com"}, args...)...)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return string(out)
	}
	git("init", "-q")
	write(t, filepath.Join(root, "tracked.txt"), "before\n", 0600)
	git("add", ".")
	git("commit", "-q", "-m", "fixture")
	revision := strings.TrimSpace(git("rev-parse", "HEAD"))
	write(t, filepath.Join(root, "tracked.txt"), "after\n", 0600)
	git("add", "tracked.txt")
	write(t, filepath.Join(root, "tracked.txt"), "after again\n", 0600)
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(git("diff", "HEAD", "--no-ext-diff", "--no-textconv", "--binary", "--", "."))))
	equal(t, verilex(t, root, nil, "run", chain).code, 0)
	write(t, filepath.Join(root, "tracked.txt"), strings.Repeat("large tracked change\n", 110000), 0600)
	equal(t, verilex(t, root, nil, "run", chain).code, 0)
	entries := usageJSON(t, root, nil)["entries"].([]any)
	small, large := 0, 0
	for _, raw := range entries {
		entry := raw.(map[string]any)
		equal[any](t, entry["revision"], revision)
		if entry["diff_sha256"] == digest {
			small++
		} else if _, exists := entry["diff_sha256"]; !exists {
			large++
		}
	}
	equal(t, small, 1)
	equal(t, large, 1)
}

func TestUsageHugeCallerTokensKeepLedgerReadable(t *testing.T) {
	root := product(t)
	for range 2 {
		equal(t, verilex(t, root, nil, "run", chain, "--tokens", "9223372036854775807").code, 0)
	}
	got := verilex(t, root, nil, "usage", "--json")
	equal(t, got.code, 0)
	var report usage.Report
	if err := json.Unmarshal([]byte(got.stdout), &report); err != nil {
		t.Fatal(err)
	}
	equal(t, report.Totals.Runs, 2)
	equal(t, report.Totals.Tokens, int64(math.MaxInt64))
	// Check the JSON flag without depending on the new field at compile time,
	// so this regression can run against the pre-fix implementation too.
	var document map[string]any
	if err := json.Unmarshal([]byte(got.stdout), &document); err != nil {
		t.Fatal(err)
	}
	equal[any](t, document["totals"].(map[string]any)["tokens_saturated"], true)
	contains(t, verilex(t, root, nil, "usage").stdout, "tokens >=9223372036854775807")
	for _, entry := range report.Entries {
		equal(t, entry.Tokens.Count, int64(math.MaxInt64))
		equal(t, verilex(t, root, nil, "usage", entry.Record.Run, "--tokens", "10").code, 0)
	}
	result := usageJSON(t, root, nil)["totals"].(map[string]any)
	equal[any](t, result["tokens"], float64(20))
	equal[any](t, result["tokens_saturated"], false)
}

// commitAll makes root a Git repository holding its files in one commit.
func commitAll(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"commit", "-q", "-m", "fixture"}} {
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=verilex", "-c", "user.email=verilex@example.com"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// Git's porcelain diff refreshes the index under .git/index.lock; a killed refresh left the lock
// in the user's repository. The capture must never write the index, even when files are stat-dirty.
func TestUsageDigestNeverWritesTheProductIndex(t *testing.T) {
	root := product(t)
	write(t, filepath.Join(root, "tracked.txt"), "same\n", 0600)
	commitAll(t, root)
	// Stat-dirty, content unchanged: what a formatter, rsync or a branch switch leaves behind.
	hour := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(root, "tracked.txt"), hour, hour); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(root, ".git", "index")
	before := read(t, index)
	equal(t, verilex(t, root, nil, "run", chain).code, 0)
	equal(t, read(t, index) == before, true)
	if _, err := os.Stat(index + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("index.lock left behind: %v", err)
	}
	entry := usageJSON(t, root, nil)["entries"].([]any)[0].(map[string]any)
	equal[any](t, entry["diff_sha256"], fmt.Sprintf("%x", sha256.Sum256(nil)))
}

// A Git that outlives the budget is asked to stop, so its own handlers remove any lock it holds.
func TestUsageDigestStopsSlowGitGently(t *testing.T) {
	root := product(t)
	commitAll(t, root)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin, mark := t.TempDir(), filepath.Join(t.TempDir(), "signal")
	write(t, filepath.Join(bin, "git"), `#!/bin/sh
case " $* " in
*" diff "*|*" diff-index "*)
	trap 'kill $!; echo "TERM $GIT_OPTIONAL_LOCKS" >"$MARK"; exit 143' TERM
	sleep 10 >/dev/null 2>&1 &
	wait
	exit 0 ;;
esac
exec "$REAL_GIT" "$@"
`, 0700)
	env := map[string]string{"PATH": bin + string(os.PathListSeparator) + os.Getenv("PATH"), "MARK": mark, "REAL_GIT": real}
	equal(t, verilex(t, root, env, "run", chain).code, 0)
	equal(t, read(t, mark), "TERM 0\n")
	entry := usageJSON(t, root, nil)["entries"].([]any)[0].(map[string]any)
	_, hashed := entry["diff_sha256"]
	equal(t, hashed, false)
	equal(t, entry["revision"] != nil, true)
}
