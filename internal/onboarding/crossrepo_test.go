package onboarding_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DereKk8/verilex/internal/cli"
	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/grouping"
	"github.com/DereKk8/verilex/internal/onboarding"
)

// Onboarding checks a cross-repo claim source with lifecycle.Review and records the repository's
// commit in grouping.yaml. If the cross-repo source is edited and committed after Review read it
// and before crossRepo inspects HEAD, onboarding must never record the new commit as what Review
// judged.
func TestCrossRepoCommitMatchesReviewedBytes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := product(t)
	runbooks := filepath.Join(filepath.Dir(root), "runbooks")
	skill := filepath.Join(runbooks, filepath.FromSlash(".cursor/skills/verify-tally/features"))
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	itemsSrc := filepath.Join(root, filepath.FromSlash(".cursor/skills/verify-tally/features/items.md"))
	itemsDst := filepath.Join(skill, "items.md")
	if err := os.WriteFile(itemsDst, []byte(read(t, itemsSrc)), 0o644); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", runbooks, "-c", "user.name=verilex", "-c", "user.email=verilex@example.com"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-q", "-m", "initial runbooks")
	commit1 := git("rev-parse", "HEAD")

	if err := os.Remove(itemsSrc); err != nil {
		t.Fatal(err)
	}
	for _, claim := range []string{"item-added", "item-listed"} {
		p := filepath.Join(root, ".verilex", "claims", claim+".yaml")
		text := read(t, p)
		if claim == "item-added" {
			text = strings.Replace(text, "  - ref: verify-tally/features/items.md#item-add\n", "  - ref: verify-tally/features/items.md#item-add\n    repo: ../runbooks\n", 1)
		} else {
			text = strings.Replace(text, "  - ref: verify-tally/features/items.md#item-list\n", "  - ref: verify-tally/features/items.md#item-list\n    repo: ../runbooks\n", 1)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustCLI(t, root, "run", "store-open | item-stored apple")
	mustCLI(t, root, "run", "store-open | item-stored pear")

	var commit2 string
	onboarding.SetAfterReview(t, func() {
		if err := os.WriteFile(itemsDst, []byte(read(t, itemsDst)+"\nUnreviewed extra line.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git("commit", "-q", "-am", "unreviewed edit")
		commit2 = git("rev-parse", "HEAD")
	})

	var stdout, stderr strings.Builder
	t.Setenv("VERILEX_HOME", filepath.Join(filepath.Dir(root), "state"))
	t.Setenv("TALLY_STORES", filepath.Join(filepath.Dir(root), "stores"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(filepath.Dir(root), "config"))
	t.Setenv("VERILEX_LEDGER", "")
	code := cli.Main([]string{"--project", root, "onboard", "item-stored"}, &stdout, &stderr)
	if code == 0 {
		p, err := dictionary.FindProject(root)
		if err != nil {
			t.Fatal(err)
		}
		g, err := grouping.Load(p)
		if err != nil {
			t.Fatal(err)
		}
		t.Fatalf("onboarding recorded unreviewed commit %s (reviewed %s): decision=%+v stdout=%s", commit2, commit1, g.Words["item-stored"], stdout.String())
	}
	if code != 2 || !strings.Contains(stderr.String(), "verilex: refused:") {
		t.Fatalf("exit %d stdout=%q stderr=%q, want refusal (exit 2)", code, stdout.String(), stderr.String())
	}
}

func mustCLI(t *testing.T, root string, args ...string) string {
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
