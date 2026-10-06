package tests

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	// Track the compiled CLI in Go's test cache even though tests drive its binary.
	_ "github.com/DereKk8/verilex/internal/cli"
	"github.com/DereKk8/verilex/internal/runner"
)

const chain = "store-open | item-stored apple | item-listed apple"

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "verilex-test-")
	if err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "verilex")
	cmd := exec.Command("go", "build", "-o", binary, "../cmd/verilex")
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		fmt.Fprintln(os.Stderr, string(out), err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func product(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "tally")
	err := filepath.WalkDir("fixtures/tally", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("fixtures/tally", path)
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
	if err = os.Mkdir(filepath.Join(filepath.Dir(root), "stores"), 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

type output struct {
	code           int
	stdout, stderr string
}

func verilex(t *testing.T, root string, env map[string]string, args ...string) output {
	t.Helper()
	cmd, stdout, stderr := command(root, env, args...)
	err := cmd.Run()
	code := 0
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	return output{code, stdout.String(), stderr.String()}
}

// command prepares the CLI in a product checkout with the test's isolated state, stores and config home.
func command(root string, env map[string]string, args ...string) (*exec.Cmd, *strings.Builder, *strings.Builder) {
	cmd := exec.Command(binary, args...)
	cmd.Dir = root
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "TALLY_") && !strings.HasPrefix(value, "VERILEX_") && !strings.HasPrefix(value, "XDG_CONFIG_HOME=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	// The user's own profiles never reach a test: each product gets its own config home.
	cmd.Env = append(cmd.Env, "VERILEX_HOME="+filepath.Join(filepath.Dir(root), "state"), "TALLY_STORES="+filepath.Join(filepath.Dir(root), "stores"), "XDG_CONFIG_HOME="+filepath.Join(filepath.Dir(root), "config"))
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return cmd, stdout, stderr
}

func lastRun(t *testing.T, root string) runner.Record {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(filepath.Dir(root), "state", "tally", "runs", "*", "run.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("expected one run: %v %v", paths, err)
	}
	record, err := runner.ReadRecord(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func stores(t *testing.T, root string) []string {
	t.Helper()
	files, err := os.ReadDir(filepath.Join(filepath.Dir(root), "stores"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, file := range files {
		names = append(names, file.Name())
	}
	return names
}

func write(t *testing.T, path, text string, mode fs.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}
func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func equal[T any](t *testing.T, actual, expected T) {
	t.Helper()
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("got %#v, want %#v", actual, expected)
	}
}
func contains(t *testing.T, actual, expected string) {
	t.Helper()
	if !strings.Contains(actual, expected) {
		t.Fatalf("%q does not contain %q", actual, expected)
	}
}
func verdicts(r runner.Record) []string {
	result := []string{}
	for _, w := range r.Words {
		result = append(result, string(w.Verdict))
	}
	return result
}
func frames(r runner.Record) []string {
	result := []string{}
	for _, f := range r.Frame {
		result = append(result, f.Step)
	}
	return result
}

func addWord(t *testing.T, root, name, script string) {
	t.Helper()
	dir := filepath.Join(root, ".verilex", "words", name)
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "word.md"), fmt.Sprintf("---\nword: %s\npromise: A test word.\nrequires: [store]\nimplements: [verify-tally/features/store.md#store-open]\n---\n", name), 0644)
	write(t, filepath.Join(dir, "run"), "#!/bin/sh\ncat >/dev/null\n"+script, 0755)
}

func TestPassingChainCleansUpInstanceAndKeepsEvidence(t *testing.T) {
	root := product(t)
	done := verilex(t, root, nil, "run", chain)
	if done.code != 0 {
		t.Fatalf("%+v", done)
	}
	record := lastRun(t, root)
	names := []string{}
	for _, w := range record.Words {
		names = append(names, w.Word)
	}
	equal(t, names, []string{"store-open", "item-stored", "item-listed"})
	equal(t, verdicts(record), []string{"green", "green", "green"})
	equal(t, record.Cleanup, "done")
	equal(t, stores(t, root), []string{})
	equal(t, read(t, filepath.Join(record.Words[1].Evidence, "actions.log")), "$ tally add apple\nexit 0\nadded apple\n\n")
	equal(t, done.stdout, "green: 3 green; run "+record.Run+"\n")
}

func TestPlantedDefectIsRedAtWordThatProvesIt(t *testing.T) {
	root := product(t)
	done := verilex(t, root, map[string]string{"TALLY_DEFECT": "drop-adds"}, "run", chain)
	equal(t, done.code, 1)
	record := lastRun(t, root)
	equal(t, verdicts(record), []string{"green", "red"})
	equal(t, *record.Reason, "item-stored apple: tally said 'added apple' but store.json lacks apple")
	contains(t, done.stdout, "    verify skill: verify-tally/features/items.md#item-add\n")
	equal(t, frames(record), []string{"launch", "doctor", "doctor-after-failure", "cleanup"})
	equal(t, stores(t, root), []string{})
}

func TestEnvironmentTroubleIsInconclusiveNeverRed(t *testing.T) {
	root := product(t)
	done := verilex(t, root, map[string]string{"TALLY_SIMULATE_LOCK": "1"}, "run", chain)
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, *record.Verdict, "inconclusive")
	equal(t, record.Words[0].Reported, "blocked")
	reason, ok := record.Words[0].Reason.(string)
	if !ok || !strings.HasSuffix(reason, "is locked by another process") {
		t.Fatalf("reason: %v", record.Words[0].Reason)
	}
	equal(t, stores(t, root), []string{})
}

func TestChainAgainstOrderOfRealityIsRefusedBeforeLaunch(t *testing.T) {
	root := product(t)
	done := verilex(t, root, nil, "run", "item-stored apple | store-open")
	equal(t, done.code, 2)
	equal(t, done.stderr, "verilex: refused: item-stored apple requires store, pinned by claim item-added; nothing earlier provides it\n")
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "state")); !os.IsNotExist(err) {
		t.Fatalf("state exists: %v", err)
	}
	equal(t, stores(t, root), []string{})
}

func TestUnknownWordAndWrongArityAreRefused(t *testing.T) {
	root := product(t)
	equal(t, verilex(t, root, nil, "run", "store-open | item-sold apple").stderr, "verilex: refused: unknown word 'item-sold'; `verilex words` lists the dictionary\n")
	equal(t, verilex(t, root, nil, "run", "store-open | item-stored").stderr, "verilex: refused: item-stored takes 1 argument(s) ['name'], got []\n")
}

func TestInstanceRunDidNotLaunchIsRefusedAndLeftIntact(t *testing.T) {
	root := product(t)
	other := filepath.Join(t.TempDir(), "dev-store")
	if err := os.Mkdir(other, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(other, "owner"), "a-developer\n", 0644)
	write(t, filepath.Join(other, "store.json"), `{"items": ["keep-me"]}`, 0644)
	done := verilex(t, root, map[string]string{"TALLY_ADOPT_STORE": other}, "run", chain)
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, *record.Verdict, "inconclusive")
	equal(t, *record.Reason, "doctor refused the instance (exit 1)")
	equal(t, len(record.Words), 0)
	equal(t, read(t, filepath.Join(other, "store.json")), `{"items": ["keep-me"]}`)
}

func TestDishonestWordResultsAreInconclusive(t *testing.T) {
	cases := []struct{ name, script, reason string }{
		{"no-observation", `echo '{"verdict": "pass"}'` + "\n", "pass without a second observation"},
		{"no-preconditions", `echo '{"verdict": "fail", "detail": "broken"}'` + "\nexit 1\n", "fail without stating that its preconditions held"},
		{"wrong-exit", `echo '{"verdict": "pass", "observation": "fine"}'` + "\nexit 1\n", "exit 1 disagrees with verdict pass"},
		{"not-json", "echo all good\n", "stdout is not one result JSON object"},
		{"secret", `printf -- "-----BEGIN PRIVATE KEY-----\n" > "$VERILEX_EVIDENCE/key.pem"` + "\n" + `echo '{"verdict": "pass", "observation": "fine"}'` + "\n", "secret pattern in evidence key.pem"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := product(t)
			addWord(t, root, "store-glanced", c.script)
			done := verilex(t, root, nil, "run", "store-open | store-glanced")
			equal(t, done.code, 2)
			record := lastRun(t, root)
			equal(t, record.Words[len(record.Words)-1].Verdict, "inconclusive")
			equal(t, *record.Verdict, "inconclusive")
			equal(t, record.Words[len(record.Words)-1].Reason, any(c.reason))
			equal(t, stores(t, root), []string{})
		})
	}
}

func TestKeptInstanceListedUntilCleanedUp(t *testing.T) {
	root := product(t)
	done := verilex(t, root, nil, "run", "store-open", "--keep")
	equal(t, done.code, 0)
	id := lastRun(t, root).Run
	equal(t, len(stores(t, root)), 1)
	equal(t, verilex(t, root, nil, "runs").stdout, id+"  green  cleanup=kept  store-open\n")
	equal(t, verilex(t, root, nil, "cleanup", id).stdout, "cleanup: done\n")
	equal(t, stores(t, root), []string{})
	equal(t, verilex(t, root, nil, "runs").stdout, id+"  green  cleanup=done  store-open\n")
}

func TestDoctorRefusalAfterRedWordMakesRunInconclusive(t *testing.T) {
	root := product(t)
	script := `python3 -c "import json, os, pathlib; p = pathlib.Path(json.loads(os.environ['VERILEX_INSTANCE'])['store']) / 'owner'; p.unlink()"` + "\n" + `echo '{"verdict": "fail", "preconditions_held": true, "detail": "failed and corrupted"}'` + "\nexit 1\n"
	addWord(t, root, "store-corrupted", script)
	done := verilex(t, root, nil, "run", "store-open | store-corrupted")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, verdicts(record), []string{"green", "red"})
	equal(t, record.Words[0].Word, "store-open")
	equal(t, record.Words[1].Word, "store-corrupted")
	equal(t, *record.Verdict, "inconclusive")
	equal(t, *record.Reason, "doctor-after-failure refused the instance (exit 1)")
	equal(t, frames(record), []string{"launch", "doctor", "doctor-after-failure", "cleanup"})
	codes := []int{}
	for _, f := range record.Frame {
		codes = append(codes, *f.Exit)
	}
	equal(t, codes, []int{0, 0, 1, 0})
	contains(t, done.stdout, "inconclusive: 1 green, 1 red; run "+record.Run+"\n")
	contains(t, done.stdout, "  inconclusive  doctor-after-failure: refused the instance (exit 1)\n")
	if strings.Contains(done.stdout, "cause:") {
		t.Fatal(done.stdout)
	}
}

func TestCleanupEvidenceSecretLeakMakesRunInconclusive(t *testing.T) {
	root := product(t)
	path := filepath.Join(root, ".verilex", "frame", "cleanup")
	write(t, path, read(t, path)+"\nfrom pathlib import Path\nimport os\n"+`(Path(os.environ["VERILEX_EVIDENCE"]) / "key.pem").write_text("-----BEGIN PRIVATE KEY-----\n")`+"\n", 0755)
	done := verilex(t, root, nil, "run", "store-open")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, *record.Verdict, "inconclusive")
	equal(t, *record.Reason, "secret pattern in evidence key.pem")
	equal(t, record.Cleanup, "done")
	contains(t, done.stdout, "  inconclusive  cleanup: secret pattern in evidence key.pem\n")
}

func TestWordFailureWithoutDetailDoesNotFormatNone(t *testing.T) {
	root := product(t)
	addWord(t, root, "store-flaked", `echo '{"verdict": "fail", "preconditions_held": true}'`+"\nexit 1\n")
	done := verilex(t, root, nil, "run", "store-open | store-flaked")
	equal(t, done.code, 1)
	record := lastRun(t, root)
	equal(t, *record.Verdict, "red")
	equal(t, *record.Reason, "store-flaked")
	contains(t, done.stdout, "  red  store-flaked\n")
	if strings.Contains(done.stdout, ": None") {
		t.Fatal(done.stdout)
	}
}

func TestManagementCommandsWorkWithMalformedWordDictionary(t *testing.T) {
	root := product(t)
	dir := filepath.Join(root, ".verilex", "words", "broken")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "word.md"), "not yaml frontmatter at all\n", 0644)
	done := verilex(t, root, nil, "runs")
	equal(t, done.code, 0)
	equal(t, done.stdout, "")
	done = verilex(t, root, nil, "cleanup", "no-such-run")
	equal(t, done.code, 2)
	contains(t, done.stderr, "no-such-run is not a run of tally")
}

func TestSyntaxAndMetadataValidationErrorsRefused(t *testing.T) {
	root := product(t)
	done := verilex(t, root, nil, "run", `store-open | "item-stored`)
	equal(t, done.code, 2)
	contains(t, done.stderr, "verilex: refused: invalid word syntax in chain")
	contains(t, done.stderr, "No closing quotation")
	config := filepath.Join(root, ".verilex", "config.yaml")
	write(t, config, "project: tally\nsecret_patterns: ['[']\n", 0644)
	done = verilex(t, root, nil, "words")
	equal(t, done.code, 2)
	contains(t, done.stderr, "verilex: refused:")
	contains(t, done.stderr, "invalid regex in 'secret_patterns'")
	write(t, config, "project: tally\n", 0644)
	write(t, filepath.Join(root, ".verilex", "words", "store-open", "word.md"), "---\nword: store-open\npromise: A store is open.\ntimeout: not-a-number\nimplements: [verify-tally/features/store.md#store-open]\n---\n", 0644)
	done = verilex(t, root, nil, "words")
	equal(t, done.code, 2)
	contains(t, done.stderr, "verilex: refused:")
	contains(t, done.stderr, "'timeout' must be a positive integer")
}

func TestWordsJSONAndProjectDiscovery(t *testing.T) {
	root := product(t)
	done := verilex(t, root, nil, "words")
	equal(t, done.code, 0)
	equal(t, done.stdout, "item-listed name\n  promise:  A stored item shows up when a user lists the store.\n  claim:    "+pinOf(t, root, "item-listed")+" via cli\n  requires: item:{name}  provides: -\n  status:   provisional\n"+
		"item-stored name\n  promise:  A named item is in the store.\n  claim:    "+pinOf(t, root, "item-added")+" via cli\n  requires: store  provides: item:{name}\n  status:   provisional\n"+
		"store-open\n  promise:  A new, empty store is open and ready for items.\n  claim:    "+pinOf(t, root, "store-opened")+" via cli\n  requires: -  provides: store\n  status:   provisional\n")
	done = verilex(t, root, nil, "--project", filepath.Join(root, "bin"), "run", "store-open | item-stored 'red apple'", "--json")
	equal(t, done.code, 0)
	var record runner.Record
	if err := json.Unmarshal([]byte(done.stdout), &record); err != nil {
		t.Fatal(err)
	}
	equal(t, *record.Verdict, "green")
	equal(t, record.Words[1].Args, []string{"red apple"})
	equal(t, record.Words[1].Provides, []string{"item:red apple"})
	equal(t, record.Root, root)
	equal(t, record.Cleanup, "done")
	equal(t, *record.EvidenceKept, true)
}

func TestTimeoutIsInconclusiveAndCleansUp(t *testing.T) {
	root := product(t)
	addWord(t, root, "store-waited", "sleep 10\n")
	path := filepath.Join(root, ".verilex", "words", "store-waited", "word.md")
	write(t, path, strings.Replace(read(t, path), "promise: A test word.", "promise: A test word.\ntimeout: 1", 1), 0644)
	done := verilex(t, root, nil, "run", "store-open | store-waited")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, record.Words[1].Verdict, "inconclusive")
	equal(t, record.Words[1].Reason, any("timed out after 1s"))
	equal(t, record.Words[1].Exit, (*int)(nil))
	equal(t, read(t, filepath.Join(record.Words[1].Evidence, "exit")), "timeout\n")
	equal(t, stores(t, root), []string{})
}

func TestLegacyYAMLAndProjectRegexContracts(t *testing.T) {
	t.Run("duplicate keys retain last value", func(t *testing.T) {
		root := product(t)
		write(t, filepath.Join(root, ".verilex", "config.yaml"), "project: ignored\nproject: tally\n", 0644)
		done := verilex(t, root, nil, "run", "store-open", "--json")
		equal(t, done.code, 0)
		record := lastRun(t, root)
		equal(t, record.Project, "tally")
		equal(t, *record.Verdict, "green")
	})
	t.Run("YAML 1.1 booleans are not list strings", func(t *testing.T) {
		root := product(t)
		path := filepath.Join(root, ".verilex", "words", "store-open", "word.md")
		write(t, path, strings.Replace(read(t, path), "entry: cli", "entry: cli\nrequires: [on]", 1), 0644)
		done := verilex(t, root, nil, "words")
		equal(t, done.code, 2)
		contains(t, done.stderr, "'requires' must be a list of strings")
	})
	t.Run("lookbehind and named backreferences scan evidence", func(t *testing.T) {
		root := product(t)
		write(t, filepath.Join(root, ".verilex", "config.yaml"), "project: tally\nsecret_patterns: ['(?<=value: )(?P<key>[A-Z]+) (?P=key)']\n", 0644)
		addWord(t, root, "store-glanced", `printf 'value: ABC ABC\n' > "$VERILEX_EVIDENCE/custom.txt"`+"\n"+`echo '{"verdict": "pass", "observation": "fine"}'`+"\n")
		done := verilex(t, root, nil, "run", "store-open | store-glanced")
		equal(t, done.code, 2)
		record := lastRun(t, root)
		equal(t, record.Words[1].Verdict, "inconclusive")
		equal(t, record.Words[1].Reason, any("secret pattern in evidence custom.txt"))
		equal(t, stores(t, root), []string{})
	})
}
