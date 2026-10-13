package tests

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
