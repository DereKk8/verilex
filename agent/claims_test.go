package e2e_test

import (
	"encoding/json"
	"fmt"
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
		Chain   *string  `json:"chain"`
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

// A brain that derives one claim from its intent skips the other claims the diff touches. Its
// run is green with verilex's missed-claim warning, but the launcher's floor holds every touched
// claim, so the launcher is inconclusive, prints no green, and names each claim that was missed.
func TestSkippedTouchedClaimIsNeverGreen(t *testing.T) {
	dir, product, ledger := changedProduct(t)
	brain := writeBrain(t, dir, `#!/bin/sh
printf '%s\n' 'BRAIN SAYS EVERYTHING IS COVERED'
set --
for path in $(sed -n 's/^changed: //p' "$VERILEX_AGENT_PROMPT"); do set -- "$@" --changed "$path"; done
verilex plan --claim store-opened "$@" > /dev/null
verilex run --claim store-opened "$@" > "$HOME/printed"
`)
	home := filepath.Join(dir, "home")
	for _, flags := range [][]string{{}, {"--text"}} {
		run := launchKept(t, dir, coreBin, brain, append([]string{
			"--project", product, "--home", home, "--ledger", ledger,
			"--intent", "prove the store opens", "--diff", "HEAD", "--harness", "stub", "--model", "stub"}, flags...)...)
		if run.code != 2 || run.stdout != "" {
			t.Fatalf("exit %d, want 2 with no stdout\n%s\n%s", run.code, run.stdout, run.stderr)
		}
		if want := "inconclusive: run "; !strings.Contains(run.stderr, want) || !strings.Contains(run.stderr, "is green but was not asked about claim item-added, claim item-listed") {
			t.Fatalf("stderr does not name the missed claims\n%s", run.stderr)
		}
		// The brain's own run was green, so the launcher overrode a green, not a failure.
		var doc runDoc
		if err := json.Unmarshal([]byte(run.home(t, "printed")), &doc); err != nil || doc.Verdict != "green" || doc.Warning != "2 touched claims not covered" {
			t.Fatalf("the brain's run %+v: %v", doc, err)
		}
		home = filepath.Join(dir, "home2")
	}
}

// The launcher looks up the touched claims in the project with each path as the brain sends it,
// relative, so a very wide diff needs no more argument space for the lookup than for the brain's run.
func TestTouchedClaimLookupSendsRelativePathsFromTheProject(t *testing.T) {
	dir, product, ledger := changedProduct(t)
	log := filepath.Join(dir, "index-changed.log")
	spy := filepath.Join(dir, "verilex-spy")
	script := fmt.Sprintf(`#!/bin/sh
for arg in "$@"; do
  case "$arg" in --changed=*) { pwd -P; echo "$arg"; } >> %q ;; esac
done
exec %q "$@"
`, log, coreBin)
	if err := os.WriteFile(spy, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	brain := writeBrain(t, dir, "#!/bin/sh\nverilex run --changed bin/tally > /dev/null\n")
	run := launchKept(t, dir, spy, brain,
		"--project", product, "--home", filepath.Join(dir, "home"), "--ledger", ledger,
		"--intent", "prove the store opens", "--diff", "HEAD", "--harness", "stub", "--model", "stub")
	if run.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", run.code, run.stdout, run.stderr)
	}
	text, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	wd, err := filepath.EvalSymlinks(product)
	if err != nil {
		t.Fatal(err)
	}
	if want := wd + "\n--changed=bin/tally\n"; string(text) != want {
		t.Fatalf("index --changed ran as %q, want %q", text, want)
	}
}

// The brain drives verilex only through the launcher, which refuses commands that change what
// verilex trusts or keep an instance past the run.
func TestBrainCannotChangeTrustOrKeepAnInstance(t *testing.T) {
	dir, product, ledger := changedProduct(t)
	brain := writeBrain(t, dir, `#!/bin/sh
log="$HOME/refusals"
verilex onboard item-listed 2>> "$log"; echo "exit $?" >> "$log"
verilex run --keep --claim store-opened --changed bin/tally 2>> "$log"; echo "exit $?" >> "$log"
verilex run --continue someone-else 'store-open' 2>> "$log"; echo "exit $?" >> "$log"
verilex run --changed bin/tally > /dev/null
`)
	run := launchKept(t, dir, coreBin, brain,
		"--project", product, "--ledger", ledger, "--diff", "HEAD", "--harness", "stub", "--model", "stub")
	var doc runDoc
	if run.code != 0 || json.Unmarshal([]byte(run.stdout), &doc) != nil || doc.Warning != "" {
		t.Fatalf("exit %d\n%s\n%s", run.code, run.stdout, run.stderr)
	}
	text := run.home(t, "refusals")
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

// claimRun is a planned claim-run document a fake verilex prints.
func claimRun(verdict, run string, named, changed []string) []byte {
	doc := map[string]any{
		"format": "verilex-claim-run-1", "verdict": verdict, "run": run,
		"requested": map[string]any{"claims": []string{}, "named": orEmpty(named), "changed": orEmpty(changed), "chain": nil},
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
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err = os.WriteFile(path, []byte("first\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := commitAll(dir); err != nil {
		t.Fatal(err)
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

// commitAll makes dir a git repository whose HEAD holds every file in it.
func commitAll(dir string) error {
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "--allow-empty", "-m", "base"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %v\n%s", args, err, out)
		}
	}
	return nil
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
