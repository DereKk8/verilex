package stamp_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DereKk8/verilex/internal/cli"
	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/lifecycle"
	"github.com/DereKk8/verilex/internal/runner"
	"github.com/DereKk8/verilex/internal/stamp"
)

// A skip decision relies on both a step's stamp and its admission hold. Both must come from one
// read of the word's files, the binding, the grouping file and the claim: otherwise an unadmitted
// state that has a green pass in the ledger can be stamped on disk, then appear admitted when Hold
// reads a restored state afterwards, turning an unadmitted state into a skipped green.
func TestStampChainJudgesHoldFromTheSameReadAsTheStamp(t *testing.T) {
	t.Run("word files", func(t *testing.T) {
		root := onboardedStoreOpen(t)
		run := filepath.Join(root, ".verilex", "words", "store-open", "run")
		admitted := read(t, run)
		unadmitted := admitted + "\n# unadmitted edit B\n"
		write(t, run, unadmitted)
		// Run live on the unadmitted word so the ledger holds a green pass for B.
		mustRun(t, root, "run", "store-open")
		if got := mustRun(t, root, "plan", "store-open"); !strings.HasPrefix(got, "plan: skip 0, run 1; store-open: drift-suspect:") {
			t.Fatalf("baseline unadmitted plan: %q", got)
		}

		// Restore admitted A right after the stamp reads B, before Hold runs.
		stamp.SetAfterStamp(t, func(word string) {
			if word == "store-open" {
				write(t, run, admitted)
			}
		})
		got := mustRun(t, root, "plan", "store-open")
		if got != "plan: skip 0, run 1; store-open: drift-suspect: the word's files changed since onboarding\n" {
			t.Fatalf("plan skipped or misjudged unadmitted word B: %q", got)
		}
	})

	t.Run("frame", func(t *testing.T) {
		root := onboardedStoreOpen(t)
		launch := filepath.Join(root, ".verilex", "frame", "launch")
		admitted := read(t, launch)
		unadmitted := admitted + "\n# unadmitted frame B\n"
		write(t, launch, unadmitted)
		mustRun(t, root, "run", "store-open")
		if got := mustRun(t, root, "plan", "store-open"); !strings.HasPrefix(got, "plan: skip 0, run 1; store-open: drift-suspect:") {
			t.Fatalf("baseline unadmitted frame plan: %q", got)
		}

		stamp.SetAfterStamp(t, func(word string) {
			if word == "store-open" {
				write(t, launch, admitted)
			}
		})
		got := mustRun(t, root, "plan", "store-open")
		if got != "plan: skip 0, run 1; store-open: drift-suspect: the frame changed since onboarding (.verilex/frame)\n" {
			t.Fatalf("plan skipped or misjudged unadmitted frame B: %q", got)
		}
	})

	t.Run("grouping file", func(t *testing.T) {
		root := product(t)
		// Record a green pass while store-open is provisional ("word admission" = "none").
		mustRun(t, root, "run", "store-open")
		mustRun(t, root, "run", "store-open")
		mustRun(t, root, "onboard", "store-open")
		groupingPath := filepath.Join(root, ".verilex", "grouping.yaml")
		admittedGrouping := read(t, groupingPath)
		if err := os.Remove(groupingPath); err != nil {
			t.Fatal(err)
		}
		if got := mustRun(t, root, "plan", "store-open"); got != "plan: skip 0, run 1; store-open: provisional; only admitted words are skipped\n" {
			t.Fatalf("baseline provisional plan: %q", got)
		}

		// Write admitted grouping right after Chain read the missing grouping file.
		stamp.SetAfterStamp(t, func(word string) {
			if word == "store-open" {
				writeMode(t, groupingPath, admittedGrouping, 0o644)
			}
		})
		got := mustRun(t, root, "plan", "store-open")
		if got != "plan: skip 0, run 1; store-open: provisional; only admitted words are skipped\n" {
			t.Fatalf("plan skipped on a provisional pass: %q", got)
		}
	})

	t.Run("claim sources", func(t *testing.T) {
		root := onboardedStoreOpen(t)
		claimPath := filepath.Join(root, ".verilex", "claims", "store-opened.yaml")
		admittedClaim := read(t, claimPath)
		re := regexp.MustCompile(`prose: [0-9a-f]{12}`)
		if !re.MatchString(admittedClaim) {
			t.Fatalf("claim has no prose pin:\n%s", admittedClaim)
		}
		editedClaim := re.ReplaceAllString(admittedClaim, "prose: 000000000000")
		if editedClaim == admittedClaim {
			t.Fatal("failed to alter prose pin")
		}
		// Run live with edited claim sources so the ledger holds a green pass for them.
		write(t, claimPath, editedClaim)
		mustRun(t, root, "run", "store-open")

		// Load words while admittedClaim is on disk, then switch the claim file to editedClaim
		// before Decide stamps and checks Hold.
		write(t, claimPath, admittedClaim)
		t.Setenv("VERILEX_HOME", filepath.Join(filepath.Dir(root), "state"))
		t.Setenv("TALLY_STORES", filepath.Join(filepath.Dir(root), "stores"))
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(filepath.Dir(root), "config"))
		t.Setenv("VERILEX_LEDGER", "")
		p, err := dictionary.FindProject(root)
		if err != nil {
			t.Fatal(err)
		}
		words, err := lifecycle.LoadWords(p)
		if err != nil {
			t.Fatal(err)
		}
		steps, err := dictionary.ParseChain("store-open", words)
		if err != nil {
			t.Fatal(err)
		}
		write(t, claimPath, editedClaim)
		decided, err := runner.Decide(p, steps, runner.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if decided.Skipped() {
			t.Fatalf("Decide skipped on unadmitted claim sources: %+v", decided)
		}
		if !strings.Contains(decided.Rerun, "drift-suspect") {
			t.Fatalf("Decide rerun = %q, want drift-suspect", decided.Rerun)
		}
	})
}

func onboardedStoreOpen(t *testing.T) string {
	t.Helper()
	root := product(t)
	mustRun(t, root, "run", "store-open")
	mustRun(t, root, "run", "store-open")
	mustRun(t, root, "onboard", "store-open")
	return root
}

func mustRun(t *testing.T, root string, args ...string) string {
	t.Helper()
	t.Setenv("VERILEX_HOME", filepath.Join(filepath.Dir(root), "state"))
	t.Setenv("TALLY_STORES", filepath.Join(filepath.Dir(root), "stores"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(filepath.Dir(root), "config"))
	t.Setenv("VERILEX_LEDGER", "")
	t.Setenv("VERILEX_USAGE_LEDGER", "")
	t.Setenv("TALLY_DEFECT", "")
	t.Setenv("TALLY_SIMULATE_LOCK", "")
	t.Setenv("TALLY_ADOPT_STORE", "")
	var stdout, stderr strings.Builder
	code := cli.Main(append([]string{"--project", root}, args...), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("verilex %v exited %d\nstdout: %s\nstderr: %s", args, code, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func product(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join("..", "..", "tests", "fixtures", "tally")
	root := filepath.Join(base, "tally")
	err = filepath.WalkDir(fixture, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(fixture, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(root, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(dest, info.Mode().Perm())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(filepath.Dir(root), "stores"), 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func write(t *testing.T, path, text string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	writeMode(t, path, text, info.Mode().Perm())
}

func writeMode(t *testing.T, path, text string, mode fs.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}
