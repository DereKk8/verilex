package e2e_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// admittedProduct is an admitted tally copy in a git repository with a clean working tree.
func admittedProduct(t *testing.T) (dir, product, ledger string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TALLY_STORES", t.TempDir())
	dir = t.TempDir()
	product = copyProduct(t, dir)
	ledger = filepath.Join(dir, "ledger")
	admit(t, product, ledger)
	if err := commitAll(product); err != nil {
		t.Fatal(err)
	}
	return dir, product, ledger
}

// A brain that edits the code under test, even one that puts the bytes back, gets no verdict:
// verilex checked code the brain wrote. The honest brain on the same defect gets verilex's red.
func TestBrainThatEditsTheProjectGetsNoVerdict(t *testing.T) {
	dir, product, ledger := admittedProduct(t)
	t.Setenv("TALLY_DEFECT", "hide-lists")
	fix := `python3 -c "p='bin/tally'; s=open(p).read(); open(p,'w').write(s.replace('hide-lists', 'hide-lists-off'))"`
	for _, tc := range []struct {
		name, body, want string
		exit             int
	}{
		{"honest", "verilex run --claim item-listed", "", 1},
		{"fixes the product", "verilex run --claim item-listed\n" + fix + "\nverilex run --claim item-listed", "bin/tally", 2},
		{"edits and restores", "cp -p bin/tally \"$KEEP\"\nprintf '# x\\n' >> bin/tally\nverilex run --claim item-listed\ncp -p \"$KEEP\" bin/tally", "bin/tally", 2},
		{"adds a file to a word", "printf 'x\\n' > .verilex/words/item-listed/extra.txt\nverilex run --claim item-listed", ".verilex/words/item-listed/extra.txt", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("KEEP", filepath.Join(t.TempDir(), "tally"))
			brain := writeBrain(t, filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")), "#!/bin/sh\n"+tc.body+" > /dev/null\n")
			stdout, stderr, code := launch(t, dir, coreBin, brain,
				"--project", product, "--ledger", ledger, "--intent", "prove a stored apple is listed", "--harness", "stub", "--model", "stub")
			if code != tc.exit {
				t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", code, tc.exit, stdout, stderr)
			}
			if tc.want == "" {
				var doc runDoc
				if json.Unmarshal([]byte(stdout), &doc) != nil || doc.Verdict != "red" {
					t.Fatalf("stdout %s", stdout)
				}
				return
			}
			if stdout != "" || !strings.Contains(stderr, "inconclusive: the brain changed the project during the run") || !strings.Contains(stderr, tc.want) {
				t.Fatalf("stdout: %s\nstderr: %s", stdout, stderr)
			}
		})
		restore := exec.Command("git", "checkout", "-q", "--", ".")
		restore.Dir = product
		clean := exec.Command("git", "clean", "-qfd")
		clean.Dir = product
		for _, cmd := range []*exec.Cmd{restore, clean} {
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %v\n%s", cmd.Args, err, out)
			}
		}
	}
}

// The diff names both sides of a rename and non-ASCII paths as they are, so verilex still sees
// every claim the change touches and warns about the one the brain left out.
func TestDiffKeepsRenamesAndNonASCIIPaths(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TALLY_STORES", t.TempDir())
	dir := t.TempDir()
	product := copyProduct(t, dir)
	words := filepath.Join(product, ".verilex", "words", "item-listed")
	for _, name := range []string{"notes.txt", "café.txt"} {
		if err := os.WriteFile(filepath.Join(words, name), []byte("note\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ledger := filepath.Join(dir, "ledger")
	admit(t, product, ledger)
	if err := commitAll(product); err != nil {
		t.Fatal(err)
	}
	seen := filepath.Join(dir, "seen")
	t.Setenv("PROMPT_COPY", seen)
	brain := writeBrain(t, dir, `#!/bin/sh
cp "$VERILEX_AGENT_PROMPT" "$PROMPT_COPY"
set --
for path in $(sed -n 's/^changed: //p' "$VERILEX_AGENT_PROMPT"); do set -- "$@" --changed "$path"; done
verilex run --claim store-opened "$@" > /dev/null
`)
	for _, tc := range []struct {
		name    string
		change  func() error
		changed []string
	}{
		{"rename", func() error {
			cmd := exec.Command("git", "mv", ".verilex/words/item-listed/notes.txt", "notes-moved.txt")
			cmd.Dir = product
			return cmd.Run()
		}, []string{".verilex/words/item-listed/notes.txt", "notes-moved.txt"}},
		{"non-ASCII name", func() error {
			return os.WriteFile(filepath.Join(words, "café.txt"), []byte("changed\n"), 0o600)
		}, []string{".verilex/words/item-listed/café.txt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.change(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				cmd := exec.Command("git", "reset", "-q", "--hard")
				cmd.Dir = product
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%v\n%s", err, out)
				}
			}()
			stdout, stderr, code := launch(t, dir, coreBin, brain,
				"--project", product, "--ledger", ledger, "--intent", "prove the store opens", "--diff", "HEAD", "--harness", "stub", "--model", "stub")
			var doc runDoc
			if code != 0 || json.Unmarshal([]byte(stdout), &doc) != nil {
				t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
			}
			if strings.Join(doc.Requested.Changed, ",") != strings.Join(tc.changed, ",") {
				t.Fatalf("requested.changed %q, want %q", doc.Requested.Changed, tc.changed)
			}
			var missed []string
			for _, u := range doc.Uncovered {
				missed = append(missed, u.Claim)
			}
			if strings.Join(missed, ",") != "item-listed" {
				t.Fatalf("uncovered %v, warning %q", missed, doc.Warning)
			}
			prompt, err := os.ReadFile(seen)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range tc.changed {
				if !strings.Contains(string(prompt), "changed: "+path+"\n") {
					t.Fatalf("prompt lacks %s\n%s", path, prompt)
				}
			}
		})
	}
}

// The time budget bounds the whole run, both brain phases, and no child of the brain can hold
// the launcher past it. Without a budget, a background child cannot hold the launcher either.
func TestBudgetBoundsTheRunWhateverTheBrainStarts(t *testing.T) {
	escape := "python3 -c 'import os, time; os.setsid(); time.sleep(8)' &\n"
	for _, tc := range []struct {
		name, body, budget string
		suggest            bool
		exit               int
	}{
		{"setsid child", escape + "sleep 8\n", "300ms", false, 2},
		{"suggest phase", "if [ \"$VERILEX_AGENT_PHASE\" = suggest ]; then sleep 8; fi\nverilex run --named store-opened\n", "300ms", true, 2},
		{"background child, no budget", "sleep 8 &\n" + escape + "verilex run --named store-opened\n", "", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ticket := `{"intent":"prove the store opens","harness":"stub","model":"stub","time_budget":"` + tc.budget + `"}`
			fake := writeFake(t, dir, fakeFiles{ticket: []byte(ticket + "\n"), run: claimRun("green", "r1", []string{"store-opened"}, nil)})
			brain := writeBrain(t, dir, "#!/bin/sh\n"+tc.body)
			args := []string{"--intent", "prove the store opens", "--claim", "store-opened", "--harness", "stub", "--model", "stub"}
			if tc.suggest {
				args = append(args, "--suggest")
			}
			start := time.Now()
			stdout, stderr, code := launch(t, dir, fake, brain, args...)
			if took := time.Since(start); took > 3*time.Second {
				t.Fatalf("launcher took %s\nstderr: %s", took, stderr)
			}
			if code != tc.exit {
				t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", code, tc.exit, stdout, stderr)
			}
			if tc.exit == 2 && (stdout != "" || !strings.Contains(stderr, "inconclusive: the time budget ended")) {
				t.Fatalf("stdout: %s\nstderr: %s", stdout, stderr)
			}
		})
	}
}

// A brain that calls the real verilex around the launcher gets no verdict, and a kept instance
// it left behind is torn down.
func TestBrainThatBypassesTheLauncherGetsNoVerdict(t *testing.T) {
	dir, product, ledger := admittedProduct(t)
	t.Setenv("REAL_VERILEX", coreBin)
	for _, tc := range []struct {
		name, flags, want string
	}{
		{"keeps an instance", "--keep", "kept its instance; the launcher tore it down"},
		{"runs in the home", "", "did not come through the launcher"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home")
			t.Setenv("AGENT_HOME", home)
			brain := writeBrain(t, filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")), "#!/bin/sh\nVERILEX_HOME=\"$AGENT_HOME\" \"$REAL_VERILEX\" run "+tc.flags+" --claim store-opened > /dev/null\nverilex run --claim store-opened > /dev/null\n")
			stdout, stderr, code := launch(t, dir, coreBin, brain,
				"--project", product, "--home", home, "--ledger", ledger, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub")
			if code != 2 || stdout != "" || !strings.Contains(stderr, tc.want) || !strings.Contains(stderr, "did not come through the launcher") {
				t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			cmd := exec.Command(coreBin, "--project", product, "runs", "--json")
			cmd.Env = append(os.Environ(), "VERILEX_HOME="+home)
			out, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			var rows []struct{ Run, Cleanup string }
			if err = json.Unmarshal(out, &rows); err != nil || len(rows) != 2 {
				t.Fatalf("runs %s", out)
			}
			for _, row := range rows {
				if row.Cleanup == "kept" || row.Cleanup == "pending" {
					t.Fatalf("run %s cleanup=%s: an instance is still up", row.Run, row.Cleanup)
				}
				if _, err = os.Stat(filepath.Join(os.Getenv("TALLY_STORES"), "tally-"+row.Run)); !os.IsNotExist(err) {
					t.Fatalf("store of run %s is still there", row.Run)
				}
			}
		})
	}
}
