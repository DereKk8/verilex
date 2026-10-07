package e2e_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runDoc is the part of verilex's claim-run JSON these tests read.
type runDoc struct {
	Format    string `json:"format"`
	Verdict   string `json:"verdict"`
	Run       string `json:"run"`
	Skipped   bool   `json:"skipped"`
	Warning   string `json:"warning"`
	Uncovered []struct {
		Claim string `json:"claim"`
		Next  string `json:"next"`
	} `json:"uncovered"`
	Requested struct {
		Claims  []string `json:"claims"`
		Named   []string `json:"named"`
		Changed []string `json:"changed"`
	} `json:"requested"`
}

// changedProduct is an admitted tally copy in a git repository whose working tree changes
// bin/tally, which every tally claim depends on.
func changedProduct(t *testing.T) (dir, product, ledger string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TALLY_STORES", t.TempDir())
	dir = t.TempDir()
	product = copyProduct(t, dir)
	ledger = filepath.Join(dir, "ledger")
	admit(t, product, ledger)
	gitRepo(t, product, "bin/tally")
	return dir, product, ledger
}

// A brain that derives one claim from its intent misses the other claims the diff touches. The
// launcher returns verilex's own green with verilex's missed-claim warning, byte for byte.
func TestMissedClaimWarningIsVerilexsOwn(t *testing.T) {
	dir, product, ledger := changedProduct(t)
	printed := filepath.Join(dir, "printed.json")
	t.Setenv("PRINTED", printed)
	brain := writeBrain(t, dir, `#!/bin/sh
printf '%s\n' 'BRAIN SAYS EVERYTHING IS COVERED'
set --
for path in $(sed -n 's/^changed: //p' "$VERILEX_AGENT_PROMPT"); do set -- "$@" --changed "$path"; done
verilex plan --claim store-opened "$@" > /dev/null
verilex run --claim store-opened "$@" > "$PRINTED"
`)
	home := filepath.Join(dir, "home")
	stdout, stderr, code := launch(t, dir, coreBin, brain,
		"--project", product, "--home", home, "--ledger", ledger,
		"--intent", "prove the store opens", "--diff", "HEAD", "--harness", "stub", "--model", "stub")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	want, err := os.ReadFile(printed)
	if err != nil {
		t.Fatal(err)
	}
	if stdout != string(want) {
		t.Fatalf("stdout is not what verilex printed\nstdout: %s\nverilex: %s", stdout, want)
	}
	var doc runDoc
	if err = json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Format != "verilex-claim-run-1" || doc.Verdict != "green" || doc.Warning != "2 touched claims not covered" {
		t.Fatalf("doc %+v", doc)
	}
	var missed []string
	for _, u := range doc.Uncovered {
		missed = append(missed, u.Claim)
	}
	if strings.Join(missed, ",") != "item-added,item-listed" {
		t.Fatalf("uncovered %v", missed)
	}
	record := readRecord(t, home, doc.Run)
	if record["warning"] != doc.Warning {
		t.Fatalf("run record warning %v, stdout warning %q", record["warning"], doc.Warning)
	}
	ticket, _ := record["ticket"].(map[string]any)
	if ticket["harness"] != "stub" || ticket["model"] != "stub" {
		t.Fatalf("run record does not carry the run spec's brain: %v", record["ticket"])
	}

	t.Run("text", func(t *testing.T) {
		stdout, stderr, code := launch(t, dir, coreBin, brain,
			"--project", product, "--ledger", ledger, "--text",
			"--intent", "prove the store opens", "--diff", "HEAD", "--harness", "stub", "--model", "stub")
		lines := strings.Split(strings.TrimSpace(stdout), "\n")
		if code != 0 || len(lines) != 3 || !strings.HasPrefix(lines[0], "green; run ") || !strings.HasSuffix(lines[0], "; 2 touched claims not covered") {
			t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
		}
		if !strings.HasPrefix(lines[1], "  uncovered  item-added  verilex run --claim 'item-added' --changed 'bin/tally'") || !strings.HasPrefix(lines[2], "  uncovered  item-listed  ") {
			t.Fatalf("uncovered lines\n%s", stdout)
		}
	})
}

// A green that verilex was not asked about the whole spec does not prove it. The launcher says
// inconclusive and prints no verdict a caller could read as green.
func TestGreenThatDoesNotCoverTheSpecIsInconclusive(t *testing.T) {
	dir, product, ledger := changedProduct(t)
	for _, tc := range []struct {
		name, run, want string
		floor           []string
	}{
		{"diff left out", "verilex run --claim store-opened", "was not asked about change bin/tally", nil},
		{"floor left out", "verilex run --claim store-opened --changed bin/tally", "was not asked about claim item-listed", []string{"--claim", "item-listed"}},
		{"chain run", "verilex run --changed bin/tally 'store-open'", "", nil},
		{"chain run without a diff", "verilex run 'store-open'", "is green but is not a claim run", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			brain := writeBrain(t, filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")), "#!/bin/sh\n"+tc.run+" > /dev/null\n")
			args := append([]string{"--project", product, "--ledger", ledger,
				"--intent", "prove the store opens", "--diff", "HEAD", "--harness", "stub", "--model", "stub"}, tc.floor...)
			stdout, stderr, code := launch(t, dir, coreBin, brain, args...)
			if tc.want == "" {
				// A chain run given the diff is a claim run: verilex itself warns about what the
				// chain did not prove, so the launcher returns it.
				var doc runDoc
				if code != 0 || json.Unmarshal([]byte(stdout), &doc) != nil || doc.Warning != "2 touched claims not covered" {
					t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
				}
				return
			}
			if code != 2 || stdout != "" || !strings.Contains(stderr, "verilex-agent: inconclusive: ") || !strings.Contains(stderr, tc.want) {
				t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
		})
	}
}

// The brain drives verilex only through the launcher, which refuses commands that change what
// verilex trusts or keep an instance past the run.
func TestBrainCannotChangeTrustOrKeepAnInstance(t *testing.T) {
	dir, product, ledger := changedProduct(t)
	log := filepath.Join(dir, "refusals")
	t.Setenv("REFUSALS", log)
	brain := writeBrain(t, dir, `#!/bin/sh
verilex onboard item-listed 2>> "$REFUSALS"; echo "exit $?" >> "$REFUSALS"
verilex run --keep --claim store-opened --changed bin/tally 2>> "$REFUSALS"; echo "exit $?" >> "$REFUSALS"
verilex run --continue someone-else 'store-open' 2>> "$REFUSALS"; echo "exit $?" >> "$REFUSALS"
verilex run --changed bin/tally > /dev/null
`)
	stdout, stderr, code := launch(t, dir, coreBin, brain,
		"--project", product, "--ledger", ledger, "--diff", "HEAD", "--harness", "stub", "--model", "stub")
	var doc runDoc
	if code != 0 || json.Unmarshal([]byte(stdout), &doc) != nil || doc.Warning != "" {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	body, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"verilex-agent: refused: verilex onboard is not available to the brain",
		"verilex-agent: refused: --keep is not available to the brain",
		"verilex-agent: refused: --continue is not available to the brain",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("refusals missing %q\n%s", want, text)
		}
	}
	if strings.Count(text, "exit 2") != 3 {
		t.Fatalf("refusals did not exit 2\n%s", text)
	}
}

// claimRun is a claim-run document a fake verilex prints.
func claimRun(verdict, run string, named, changed []string) []byte {
	doc := map[string]any{
		"format": "verilex-claim-run-1", "verdict": verdict, "run": run,
		"requested": map[string][]string{"claims": {}, "named": orEmpty(named), "changed": orEmpty(changed)},
	}
	data, _ := json.Marshal(doc)
	return append(data, '\n')
}

func orEmpty(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// gitRepo commits dir as it is, then appends a line to file so the working tree changes it.
func gitRepo(t *testing.T, dir, file string) {
	t.Helper()
	path := filepath.Join(dir, file)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err = os.WriteFile(path, []byte("first\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "base"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.WriteString("# changed\n"); err != nil {
		t.Fatal(err)
	}
}

func runIDs(t *testing.T, home string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(home, "tally", "runs"))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, entry := range entries {
		ids = append(ids, entry.Name())
	}
	return ids
}

func readRecord(t *testing.T, home, run string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(home, "tally", "runs", run, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err = json.Unmarshal(body, &record); err != nil {
		t.Fatal(err)
	}
	return record
}
