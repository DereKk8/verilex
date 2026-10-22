package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

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
	// macOS temp dirs sit behind a symlink (/var -> /private/var) and verilex reports resolved paths.
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "tally")
	err = filepath.WalkDir("fixtures/tally", func(path string, d fs.DirEntry, err error) error {
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
	return start(t, root, env, args...).wait(t)
}

// process is a verilex command started in the background.
type process struct {
	cmd            *exec.Cmd
	stdout, stderr *strings.Builder
}

// start runs verilex in root without waiting for it, so tests can drive several at once.
func start(t *testing.T, root string, env map[string]string, args ...string) *process {
	t.Helper()
	cmd, stdout, stderr := command(root, env, args...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return &process{cmd, stdout, stderr}
}

func (p *process) wait(t *testing.T) output {
	t.Helper()
	err := p.cmd.Wait()
	code := 0
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	return output{code, p.stdout.String(), p.stderr.String()}
}

var children atomic.Int64

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
	// Instrumented children that exit together would race on one shared coverage directory (their metadata temp file name repeats within a clock tick on macOS), so each writes to its own subdirectory.
	if dir := os.Getenv("GOCOVERDIR"); dir != "" {
		own := filepath.Join(dir, fmt.Sprintf("child-%d", children.Add(1)))
		if err := os.MkdirAll(own, 0o755); err == nil {
			cmd.Env = append(cmd.Env, "GOCOVERDIR="+own)
		}
	}
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

// unlock lets the test remove its temporary directory after a word left evidence this user
// cannot write or read.
func unlock(t *testing.T, root string) {
	t.Cleanup(func() { exec.Command("chmod", "-R", "u+rwX", filepath.Dir(root)).Run() })
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
		{"secret-after-underscore", `echo "MY_ghp_$(printf 'A%.0s' $(seq 36))" > "$VERILEX_EVIDENCE/env.txt"` + "\n" + `echo '{"verdict": "pass", "observation": "fine"}'` + "\n", "secret pattern in evidence env.txt"},
		{"secret-after-url-escape", `echo "token%3Dghp_$(printf 'A%.0s' $(seq 36))" > "$VERILEX_EVIDENCE/env.txt"` + "\n" + `echo '{"verdict": "pass", "observation": "fine"}'` + "\n", "secret pattern in evidence env.txt"},
		{"secret-after-json-escape", `printf '%s\n' "line\\nghp_$(printf 'A%.0s' $(seq 36))" > "$VERILEX_EVIDENCE/env.txt"` + "\n" + `echo '{"verdict": "pass", "observation": "fine"}'` + "\n", "secret pattern in evidence env.txt"},
		{"secret-after-letter", `echo "éghp_$(printf 'A%.0s' $(seq 36))" > "$VERILEX_EVIDENCE/env.txt"` + "\n" + `echo '{"verdict": "pass", "observation": "fine"}'` + "\n", "secret pattern in evidence env.txt"},
		{"secret-after-hex-escape", `printf '%s\n' "token\\x3dghp_$(printf 'A%.0s' $(seq 36))" > "$VERILEX_EVIDENCE/env.txt"` + "\n" + `echo '{"verdict": "pass", "observation": "fine"}'` + "\n", "secret pattern in evidence env.txt"},
		{"secret-after-unicode-escape", `printf '%s\n' "token \\u003cghp_$(printf 'A%.0s' $(seq 36))" > "$VERILEX_EVIDENCE/env.txt"` + "\n" + `echo '{"verdict": "pass", "observation": "fine"}'` + "\n", "secret pattern in evidence env.txt"},
		{"secret-after-double-url-escape", `printf '%s\n' "token%253Dghp_$(printf 'A%.0s' $(seq 36))" > "$VERILEX_EVIDENCE/env.txt"` + "\n" + `echo '{"verdict": "pass", "observation": "fine"}'` + "\n", "secret pattern in evidence env.txt"},
		{"secret-after-form-feed-escape", `printf '%s\n' "line\\fghp_$(printf 'A%.0s' $(seq 36))" > "$VERILEX_EVIDENCE/env.txt"` + "\n" + `echo '{"verdict": "pass", "observation": "fine"}'` + "\n", "secret pattern in evidence env.txt"},
		{"secret-after-octal-escape", `printf '%s\n' "token\\075ghp_$(printf 'A%.0s' $(seq 36))" > "$VERILEX_EVIDENCE/env.txt"` + "\n" + `echo '{"verdict": "pass", "observation": "fine"}'` + "\n", "secret pattern in evidence env.txt"},
		{"secret-after-quoted-printable", `printf '%s\n' "token=3Dghp_$(printf 'A%.0s' $(seq 36))" > "$VERILEX_EVIDENCE/env.txt"` + "\n" + `echo '{"verdict": "pass", "observation": "fine"}'` + "\n", "secret pattern in evidence env.txt"},
		{"secret-after-lower-quoted-printable", `printf '%s\n' "token=3dghp_$(printf 'A%.0s' $(seq 36))" > "$VERILEX_EVIDENCE/env.txt"` + "\n" + `echo '{"verdict": "pass", "observation": "fine"}'` + "\n", "secret pattern in evidence env.txt"},
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

// A frame step's evidence verilex cannot read makes the run inconclusive as a leak does, and the
// frame records which entry it could not scan.
func TestUnreadableCleanupEvidenceMakesRunInconclusive(t *testing.T) {
	otherUser(t)
	root := product(t)
	t.Cleanup(func() {
		exec.Command("unshare", "--map-auto", "--map-root-user", "rm", "-rf", filepath.Join(filepath.Dir(root), "state")).Run()
	})
	path := filepath.Join(root, ".verilex", "frame", "cleanup")
	write(t, path, read(t, path)+"\nfrom pathlib import Path\nimport os, subprocess\n"+`k = Path(os.environ["VERILEX_EVIDENCE"]) / "k"
k.write_text("fine\n")
k.chmod(0o600)
subprocess.run(["unshare", "--map-auto", "--map-root-user", "chown", "1:1", str(k)], check=True)`+"\n", 0755)
	done := verilex(t, root, nil, "run", "store-open")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, *record.Verdict, "inconclusive")
	equal(t, *record.Reason, "cannot scan evidence k: permission denied")
	equal(t, record.Frame[len(record.Frame)-1].Unread, "k")
	equal(t, record.Cleanup, "done")
	contains(t, done.stdout, "  inconclusive  cleanup: cannot scan evidence k: permission denied\n")
}

// A product error that echoes a token keeps the run inconclusive, and the token reaches none of
// what verilex writes or prints: the output, the run record, the usage ledger or the evidence.
func TestSecretInWordDetailIsRedactedEverywhere(t *testing.T) {
	root := product(t)
	token := "ghp_" + strings.Repeat("A", 36)
	addWord(t, root, "store-glanced", `tok="ghp_$(printf 'A%.0s' $(seq 36))"`+"\n"+`printf '{"verdict": "fail", "preconditions_held": true, "detail": "auth failed for token %s"}\n' "$tok"`+"\nexit 1\n")
	done := verilex(t, root, nil, "run", "--json", "store-open | store-glanced")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, *record.Verdict, "inconclusive")
	equal(t, record.Words[1].Reason, any("secret pattern in evidence stdout"))
	equal(t, record.Words[1].Detail, any("auth failed for token [redacted]"))
	contains(t, done.stdout, "auth failed for token [redacted]")
	noSecret(t, root, done, token)
}

// noSecret fails when a command's output, or any name or file in the state home of root, the
// usage record of its last run included, holds one of secrets.
func noSecret(t *testing.T, root string, done output, secrets ...string) {
	t.Helper()
	state := filepath.Join(filepath.Dir(root), "state")
	written := map[string]string{"output": done.stdout + done.stderr}
	err := filepath.WalkDir(state, func(path string, d fs.DirEntry, err error) error {
		if err == nil {
			written[path] = ""
			if d.Type().IsRegular() {
				written[path] = read(t, path)
			}
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	contains(t, strings.Join(slices.Collect(maps.Keys(written)), "\n"), filepath.Join(state, "usage", lastRun(t, root).Run, "run.json"))
	for path, text := range written {
		for _, secret := range secrets {
			if strings.Contains(path, secret) || strings.Contains(text, secret) {
				t.Fatalf("%s keeps a secret: %q", path, text)
			}
		}
	}
}

// Redaction replaces only the secret in a file, a whole private key block included, and never
// writes through a link to a file outside the evidence.
func TestEvidenceRedactionKeepsTheRestOfEachFile(t *testing.T) {
	root := product(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	write(t, filepath.Join(root, ".verilex", "config.yaml"), "project: tally\nsecret_patterns: ['(?=PROBE-)']\n", 0644)
	addWord(t, root, "store-glanced", `tok="ghp_$(printf 'A%.0s' $(seq 36))"
printf 'before\377%s\377after\n' "$tok" > "$VERILEX_EVIDENCE/bytes.bin"
printf -- '-----BEGIN PRIVATE KEY-----\nMIIBODY\n-----END PRIVATE KEY-----\nkept\n' > "$VERILEX_EVIDENCE/key.pem"
printf -- '-----BEGIN RSA PRIVATE KEY-----\nMIIBODY\n' > "$VERILEX_EVIDENCE/cut.pem"
printf 'note PROBE-1\n' > "$VERILEX_EVIDENCE/note.txt"
printf '%s\n' "$tok" > "$OUTSIDE"
ln -s "$OUTSIDE" "$VERILEX_EVIDENCE/linked.txt"
echo '{"verdict": "pass", "observation": "fine"}'
`)
	done := verilex(t, root, map[string]string{"OUTSIDE": outside}, "run", "store-open | store-glanced")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, record.Words[1].Reason, any("secret pattern in evidence bytes.bin"))
	evidence := record.Words[1].Evidence
	equal(t, read(t, filepath.Join(evidence, "bytes.bin")), "before\377[redacted]\377after\n")
	equal(t, read(t, filepath.Join(evidence, "key.pem")), "[redacted]\nkept\n")
	equal(t, read(t, filepath.Join(evidence, "cut.pem")), "[redacted]")
	equal(t, read(t, filepath.Join(evidence, "note.txt")), "[redacted]")
	equal(t, read(t, filepath.Join(evidence, "linked.txt")), "[redacted]")
	equal(t, read(t, outside), "ghp_"+strings.Repeat("A", 36)+"\n")
	equal(t, read(t, filepath.Join(evidence, "stdout")), `{"verdict": "pass", "observation": "fine"}`+"\n")
}

// A word may leave a read-only directory in its evidence: verilex still redacts the secret
// there, names the leak, and gives the directory its mode back.
func TestEvidenceRedactionInReadOnlyDirectory(t *testing.T) {
	root := product(t)
	unlock(t, root)
	addWord(t, root, "store-glanced", `mkdir "$VERILEX_EVIDENCE/ro"
printf '%s\n' "ghp_$(printf 'A%.0s' $(seq 36))" > "$VERILEX_EVIDENCE/ro/t.txt"
chmod 555 "$VERILEX_EVIDENCE/ro"
echo '{"verdict": "pass", "observation": "fine"}'
`)
	done := verilex(t, root, nil, "run", "store-open | store-glanced")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, verdicts(record), []string{"green", "inconclusive"})
	equal(t, record.Words[1].Reason, any("secret pattern in evidence t.txt"))
	ro := filepath.Join(record.Words[1].Evidence, "ro")
	equal(t, read(t, filepath.Join(ro, "t.txt")), "[redacted]\n")
	info, err := os.Stat(ro)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, info.Mode().Perm(), fs.FileMode(0o555))
}

// A secret in the name of an evidence file or directory is redacted from the name too, so the
// cause verilex prints and records holds none.
func TestSecretInEvidenceNameIsRedacted(t *testing.T) {
	root := product(t)
	token := "ghp_" + strings.Repeat("A", 36)
	addWord(t, root, "store-glanced", `tok="ghp_$(printf 'A%.0s' $(seq 36))"
echo clean > "$VERILEX_EVIDENCE/$tok"
mkdir "$VERILEX_EVIDENCE/$tok-dir"
echo clean > "$VERILEX_EVIDENCE/$tok-dir/$tok.txt"
echo '{"verdict": "pass", "observation": "fine"}'
`)
	done := verilex(t, root, nil, "run", "store-open | store-glanced")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, record.Words[1].Reason, any("secret pattern in evidence [redacted]"))
	contains(t, done.stdout, "  inconclusive  store-glanced: secret pattern in evidence [redacted]\n")
	evidence := record.Words[1].Evidence
	equal(t, read(t, filepath.Join(evidence, "[redacted]")), "clean\n")
	equal(t, read(t, filepath.Join(evidence, "[redacted]-dir", "[redacted].txt")), "clean\n")
	noSecret(t, root, done, token)
}

// A token the raw evidence hides behind a JSON escape is still a secret once decoded: the run is
// inconclusive, never red, and neither form survives.
func TestEscapedSecretInWordReportIsRedacted(t *testing.T) {
	root := product(t)
	token := "ghp_" + strings.Repeat("A", 36)
	escaped := `ghp\u005f` + strings.Repeat("A", 36)
	addWord(t, root, "store-glanced", `printf '{"verdict": "fail", "preconditions_held": true, "detail": "auth failed for token ghp\\u005f%s", "observation": {"ghp\\u005f%s": ["x"]}}\n' "$(printf 'A%.0s' $(seq 36))" "$(printf 'A%.0s' $(seq 36))"`+"\nexit 1\n")
	done := verilex(t, root, nil, "run", "--json", "store-open | store-glanced")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, record.Words[1].Verdict, "inconclusive")
	equal(t, record.Words[1].Reason, any("secret pattern in evidence stdout"))
	equal(t, record.Words[1].Detail, any("auth failed for token [redacted]"))
	equal(t, record.Words[1].Observation, any(map[string]any{"[redacted]": []any{"x"}}))
	equal(t, read(t, filepath.Join(record.Words[1].Evidence, "stdout")), `{"verdict": "fail", "preconditions_held": true, "detail": "auth failed for token [redacted]", "observation": {"[redacted]": ["x"]}}`+"\n")
	noSecret(t, root, done, token, escaped)
}

// A JSON-escaped token in the launch's instance ends the launch as a raw one does, and the
// instance it launched is still torn down.
func TestEscapedSecretInLaunchInstanceIsNotRecorded(t *testing.T) {
	root := product(t)
	path := filepath.Join(root, ".verilex", "frame", "launch")
	launch := read(t, path)
	printed := `print(json.dumps({"instance": {"store": str(store)}}))`
	contains(t, launch, printed)
	write(t, path, strings.Replace(launch, printed, `print(json.dumps({"instance": {"store": str(store), "dsn": "@@"}}).replace("@@", "ghp\\u005f" + "A" * 36))`, 1), 0755)
	done := verilex(t, root, nil, "run", "--json", "store-open")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, *record.Reason, "launch exited 2")
	equal(t, record.Instance, any(nil))
	equal(t, record.Frame[0].Leak, "stdout")
	equal(t, stores(t, root), []string{})
	noSecret(t, root, done, "ghp_"+strings.Repeat("A", 36), `ghp\u005f`+strings.Repeat("A", 36))
}

// A link to a file outside the evidence that holds a secret becomes a file holding only
// [redacted]: nothing else of the target, such as a secret no pattern knows, is copied in.
func TestSymlinkedSecretFileIsNotCopiedIntoEvidence(t *testing.T) {
	root := product(t)
	outside := filepath.Join(t.TempDir(), "app.env")
	addWord(t, root, "store-glanced", `printf 'GITHUB_TOKEN=ghp_%s\nDB_PASSWORD=hunter2\n' "$(printf 'A%.0s' $(seq 36))" > "$OUTSIDE"
ln -s "$OUTSIDE" "$VERILEX_EVIDENCE/app.env"
echo '{"verdict": "pass", "observation": "fine"}'
`)
	done := verilex(t, root, map[string]string{"OUTSIDE": outside}, "run", "store-open | store-glanced")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, record.Words[1].Reason, any("secret pattern in evidence app.env"))
	equal(t, read(t, filepath.Join(record.Words[1].Evidence, "app.env")), "[redacted]")
	equal(t, read(t, outside), "GITHUB_TOKEN=ghp_"+strings.Repeat("A", 36)+"\nDB_PASSWORD=hunter2\n")
	noSecret(t, root, done, "hunter2")
}

// A link whose target path names a secret is a leak too, and the link is replaced. A dangling
// link and a link to a clean file are kept as they are, and the run stays green.
func TestSymlinkTargetsInEvidence(t *testing.T) {
	root := product(t)
	token := "ghp_" + strings.Repeat("A", 36)
	addWord(t, root, "store-glanced", `ln -s /nonexistent "$VERILEX_EVIDENCE/dangling"
ln -s "$VERILEX_PROJECT_ROOT/bin/tally" "$VERILEX_EVIDENCE/clean"
echo '{"verdict": "pass", "observation": "fine"}'
`)
	done := verilex(t, root, nil, "run", "store-open | store-glanced")
	equal(t, done.code, 0)
	evidence := lastRun(t, root).Words[1].Evidence
	for name, target := range map[string]string{"dangling": "/nonexistent", "clean": filepath.Join(root, "bin", "tally")} {
		link, err := os.Readlink(filepath.Join(evidence, name))
		if err != nil {
			t.Fatal(err)
		}
		equal(t, link, target)
	}
	root = product(t)
	addWord(t, root, "store-glanced", `ln -s "/srv/$(printf 'ghp_%s' "$(printf 'A%.0s' $(seq 36))")/app.env" "$VERILEX_EVIDENCE/config"
echo '{"verdict": "pass", "observation": "fine"}'
`)
	done = verilex(t, root, nil, "run", "store-open | store-glanced")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, record.Words[1].Reason, any("secret pattern in evidence config"))
	equal(t, read(t, filepath.Join(record.Words[1].Evidence, "config")), "[redacted]")
	noSecret(t, root, done, token)
}

// A file or directory the word left without read permission is scanned all the same, and keeps
// its mode: a clean one leaves the run green, and a secret inside one is redacted.
func TestEvidenceWithoutReadPermissionIsScanned(t *testing.T) {
	for _, c := range []struct{ name, content string }{{"clean", "fine"}, {"secret", "ghp_$(printf 'A%.0s' $(seq 36))"}} {
		t.Run(c.name, func(t *testing.T) {
			root := product(t)
			unlock(t, root)
			addWord(t, root, "store-glanced", `mkdir "$VERILEX_EVIDENCE/closed"
echo "`+c.content+`" > "$VERILEX_EVIDENCE/closed/f.txt"
echo "`+c.content+`" > "$VERILEX_EVIDENCE/locked.txt"
chmod 000 "$VERILEX_EVIDENCE/closed/f.txt" "$VERILEX_EVIDENCE/locked.txt" "$VERILEX_EVIDENCE/closed"
echo '{"verdict": "pass", "observation": "fine"}'
`)
			done := verilex(t, root, nil, "run", "store-open | store-glanced")
			record := lastRun(t, root)
			equal(t, len(record.Words), 2)
			evidence := record.Words[1].Evidence
			closed := filepath.Join(evidence, "closed")
			for _, path := range []string{closed, filepath.Join(evidence, "locked.txt")} {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				equal(t, info.Mode().Perm(), fs.FileMode(0))
			}
			if err := os.Chmod(closed, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{filepath.Join(closed, "f.txt"), filepath.Join(evidence, "locked.txt")} {
				if err := os.Chmod(path, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if c.name == "clean" {
				equal(t, done.code, 0)
				equal(t, read(t, filepath.Join(closed, "f.txt")), "fine\n")
				return
			}
			equal(t, done.code, 2)
			equal(t, record.Words[1].Reason, any("secret pattern in evidence f.txt"))
			equal(t, read(t, filepath.Join(closed, "f.txt")), "[redacted]\n")
			equal(t, read(t, filepath.Join(evidence, "locked.txt")), "[redacted]\n")
		})
	}
}

// A JSON-escaped token in any field of the report, not only those verilex records, makes the
// run inconclusive and leaves no form of it in the evidence.
func TestEscapedSecretInAnyReportFieldIsRedacted(t *testing.T) {
	root := product(t)
	addWord(t, root, "store-glanced", `printf '{"verdict": "pass", "observation": "fine", "extra": "ghp\\u005f%s"}\n' "$(printf 'A%.0s' $(seq 36))"`+"\n")
	done := verilex(t, root, nil, "run", "store-open | store-glanced")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, record.Words[1].Reason, any("secret pattern in evidence stdout"))
	equal(t, read(t, filepath.Join(record.Words[1].Evidence, "stdout")), `{"verdict": "pass", "observation": "fine", "extra": "[redacted]"}`+"\n")
	noSecret(t, root, done, `ghp\u005f`+strings.Repeat("A", 36))
}

// A secret only a decoded value shows, as a project pattern anchored to a whole value finds it,
// has no place in the file verilex can redact: the whole stdout of the word or the launch is
// replaced, the run is inconclusive, and the launch's instance is not recorded.
func TestSecretOnlyDecodingShowsReplacesWholeStdout(t *testing.T) {
	root := product(t)
	write(t, filepath.Join(root, ".verilex", "config.yaml"), "project: tally\nsecret_patterns: ['^hunter2$']\n", 0644)
	addWord(t, root, "store-glanced", `echo '{"verdict": "pass", "observation": "fine", "extra": "hunter2"}'`+"\n")
	done := verilex(t, root, nil, "run", "--json", "store-open | store-glanced")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, record.Words[1].Reason, any("secret pattern in evidence stdout"))
	equal(t, read(t, filepath.Join(record.Words[1].Evidence, "stdout")), "[redacted]")
	noSecret(t, root, done, "hunter2")

	root = product(t)
	write(t, filepath.Join(root, ".verilex", "config.yaml"), "project: tally\nsecret_patterns: ['^hunter2$']\n", 0644)
	path := filepath.Join(root, ".verilex", "frame", "launch")
	launch := read(t, path)
	printed := `print(json.dumps({"instance": {"store": str(store)}}))`
	contains(t, launch, printed)
	write(t, path, strings.Replace(launch, printed, `print(json.dumps({"instance": {"store": str(store), "dsn": "hunter2"}}))`, 1), 0755)
	done = verilex(t, root, nil, "run", "--json", "store-open")
	equal(t, done.code, 2)
	record = lastRun(t, root)
	equal(t, *record.Reason, "launch exited 2")
	equal(t, record.Instance, any(nil))
	equal(t, record.Frame[0].Leak, "stdout")
	equal(t, read(t, filepath.Join(record.Frame[0].Evidence, "stdout")), "[redacted]")
	equal(t, stores(t, root), []string{})
	noSecret(t, root, done, "hunter2")
}

// A JSON-escaped token in a report verilex cannot decode whole, as with a duplicate key, a second
// value, a byte order mark or a cut-off key block, still makes the run inconclusive and leaves no
// form of it in the evidence.
func TestEscapedSecretInUndecodableReportIsRedacted(t *testing.T) {
	for name, report := range map[string]string{
		"duplicate-key":   `{"verdict": "pass", "observation": "fine", "extra": "ghp\\u005f%s", "extra": "clean"}`,
		"second-value":    `{"verdict": "pass", "observation": "fine"}{"x": "ghp\\u005f%s"}`,
		"byte-order-mark": `\357\273\277{"verdict": "pass", "observation": "fine", "extra": "ghp\\u005f%s"}`,
		"cut-key-block":   `{"verdict": "pass", "observation": "ghp\\u005f%s", "key": "-----BEGIN RSA PRIVATE KEY-----MIIabc"}`,
	} {
		t.Run(name, func(t *testing.T) {
			root := product(t)
			addWord(t, root, "store-glanced", `printf '`+report+`\n' "$(printf 'A%.0s' $(seq 36))"`+"\n")
			done := verilex(t, root, nil, "run", "--json", "store-open | store-glanced")
			equal(t, done.code, 2)
			record := lastRun(t, root)
			equal(t, record.Words[1].Verdict, "inconclusive")
			equal(t, record.Words[1].Reason, any("secret pattern in evidence stdout"))
			noSecret(t, root, done, "ghp_"+strings.Repeat("A", 36), `ghp\u005f`+strings.Repeat("A", 36))
		})
	}
}

// A JSON-escaped token in any evidence file, not only the report, makes the run inconclusive and
// is redacted where it stands: the rest of the file, every other escape included, is kept.
func TestEscapedSecretInEvidenceFileIsRedacted(t *testing.T) {
	root := product(t)
	addWord(t, root, "store-glanced", `printf '{"a": "token \\u0067hp_%s", "b": "ghp\\u005f%s\\u0041", "c": "\\\\u0067hp_%s", "d": "\\"\\\\\\/\\b\\f\\n\\r\\t\\ud83d\\ude00"}\n' "$(printf 'A%.0s' $(seq 36))" "$(printf 'A%.0s' $(seq 35))" "$(printf 'B%.0s' $(seq 36))" > "$VERILEX_EVIDENCE/log.json"
echo '{"verdict": "pass", "observation": "fine"}'
`)
	done := verilex(t, root, nil, "run", "store-open | store-glanced")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, record.Words[1].Reason, any("secret pattern in evidence log.json"))
	equal(t, read(t, filepath.Join(record.Words[1].Evidence, "log.json")), `{"a": "token [redacted]", "b": "[redacted]", "c": "\\u0067hp_`+strings.Repeat("B", 36)+`", "d": "\"\\\/\b\f\n\r\t\ud83d\ude00"}`+"\n")
	noSecret(t, root, done, `hp_`+strings.Repeat("A", 36), `ghp\u005f`+strings.Repeat("A", 35))
}

// Patterns run on evidence as JSON decoding shows it: each escape stands for its own character,
// an escaped UTF-16 pair for one character, and a lone surrogate for U+FFFD.
func TestPatternsMatchDecodedJSONEscapes(t *testing.T) {
	root := product(t)
	write(t, filepath.Join(root, ".verilex", "config.yaml"), `project: tally
secret_patterns: ['Q"/\\\x08\x0c\n\r\t\u00e9\uFFFD.Q']
`, 0644)
	addWord(t, root, "store-glanced", `cat > "$VERILEX_EVIDENCE/log.json" <<'EOF'
{"p": "Q\"\/\\\b\f\n\r\t\u00e9\udc00\ud83d\ude00Q", "kept": "Q\\nQ"}
EOF
echo '{"verdict": "pass", "observation": "fine"}'
`)
	done := verilex(t, root, nil, "run", "store-open | store-glanced")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, record.Words[1].Reason, any("secret pattern in evidence log.json"))
	equal(t, read(t, filepath.Join(record.Words[1].Evidence, "log.json")), `{"p": "[redacted]", "kept": "Q\\nQ"}`+"\n")
}

// A link to a directory is kept as a link and not followed, because its target lies outside the
// evidence and can be as large as the whole file system: a secret in a file the directory holds
// leaves the run green. A secret in the link's target path is still found.
func TestSymlinkToDirectoryIsNotFollowed(t *testing.T) {
	root := product(t)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "t.txt"), "ghp_"+strings.Repeat("A", 36)+"\n", 0644)
	addWord(t, root, "store-glanced", `ln -s "$OUTSIDE" "$VERILEX_EVIDENCE/cfg"
echo '{"verdict": "pass", "observation": "fine"}'
`)
	done := verilex(t, root, map[string]string{"OUTSIDE": outside}, "run", "store-open | store-glanced")
	equal(t, done.code, 0)
	link, err := os.Readlink(filepath.Join(lastRun(t, root).Words[1].Evidence, "cfg"))
	if err != nil {
		t.Fatal(err)
	}
	equal(t, link, outside)
	equal(t, read(t, filepath.Join(outside, "t.txt")), "ghp_"+strings.Repeat("A", 36)+"\n")
}

// Two report keys that both redact to [redacted] keep both values in the recorded observation.
func TestRedactedReportKeysKeepEveryValue(t *testing.T) {
	root := product(t)
	addWord(t, root, "store-glanced", `printf '{"verdict": "pass", "observation": {"ghp\\u005f%s": "first", "ghp\\u005f%s": "second"}}\n' "$(printf 'A%.0s' $(seq 36))" "$(printf 'B%.0s' $(seq 36))"`+"\n")
	equal(t, verilex(t, root, nil, "run", "store-open | store-glanced").code, 2)
	observation := lastRun(t, root).Words[1].Observation.(map[string]any)
	values := []string{}
	for _, v := range observation {
		values = append(values, v.(string))
	}
	slices.Sort(values)
	equal(t, values, []string{"first", "second"})
	equal(t, slices.Sorted(maps.Keys(observation)), []string{"[redacted]", "[redacted]-2"})
}

// Raw secret keys keep every value too: verilex judges the report as the word wrote it.
func TestRawSecretReportKeysKeepEveryValue(t *testing.T) {
	root := product(t)
	addWord(t, root, "store-glanced", `printf '{"verdict": "pass", "observation": {"ghp_%s": "first", "ghp_%s": "second"}}\n' "$(printf 'A%.0s' $(seq 36))" "$(printf 'B%.0s' $(seq 36))"`+"\n")
	done := verilex(t, root, nil, "run", "--json", "store-open | store-glanced")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, record.Words[1].Reason, any("secret pattern in evidence stdout"))
	equal(t, record.Words[1].Observation, any(map[string]any{"[redacted]": "first", "[redacted]-2": "second"}))
	noSecret(t, root, done, "ghp_"+strings.Repeat("A", 36), "ghp_"+strings.Repeat("B", 36))
}

// A redacted name longer than a file name can be still ends the run: the marker alone names it.
func TestLongRedactedEvidenceNameEnds(t *testing.T) {
	root := product(t)
	write(t, filepath.Join(root, ".verilex", "config.yaml"), "project: tally\nsecret_patterns: ['QZ']\n", 0644)
	addWord(t, root, "store-glanced", `touch "$VERILEX_EVIDENCE/$(printf 'QZ%.0s' $(seq 100))"`+"\n"+`echo '{"verdict": "pass", "observation": "fine"}'`+"\n")
	done := verilex(t, root, nil, "run", "store-open | store-glanced")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, record.Words[1].Verdict, "inconclusive")
	equal(t, record.Words[1].Reason, any("secret pattern in evidence [redacted]"))
	equal(t, read(t, filepath.Join(record.Words[1].Evidence, "[redacted]")), "")
}

// otherUser skips the test unless unshare can give a file to a subordinate uid, as a container's
// root does: creating the namespace can succeed while chown inside it fails, as under Ubuntu's
// AppArmor rule for unprivileged namespaces. The Linux CI runner is set up for it, so there a
// failed probe fails the test instead of skipping it.
func otherUser(t *testing.T) {
	t.Helper()
	probe := filepath.Join(t.TempDir(), "probe")
	write(t, probe, "", 0644)
	err := exec.Command("unshare", "--map-auto", "--map-root-user", "chown", "1:1", probe).Run()
	if err == nil {
		var info os.FileInfo
		if info, err = os.Stat(probe); err == nil && info.Sys().(*syscall.Stat_t).Uid == uint32(os.Getuid()) {
			err = errors.New("the file kept its owner")
		}
	}
	if err == nil {
		return
	}
	if runtime.GOOS == "linux" && os.Getenv("GITHUB_ACTIONS") == "true" {
		t.Fatalf("unshare cannot give a file to another user on the CI runner: %v", err)
	}
	t.Skipf("unshare cannot give a file to another user here (%v)", err)
}

// Evidence another user owns, as a container's root writes it, cannot be rewritten: the run still
// ends inconclusive and names the leak, and no token reaches the output or the records.
func TestSecretInEvidenceAnotherUserOwns(t *testing.T) {
	otherUser(t)
	root := product(t)
	t.Cleanup(func() {
		exec.Command("unshare", "--map-auto", "--map-root-user", "rm", "-rf", filepath.Join(filepath.Dir(root), "state")).Run()
	})
	token := "ghp_" + strings.Repeat("A", 36)
	addWord(t, root, "store-glanced", `tok="ghp_$(printf 'A%.0s' $(seq 36))"
mkdir "$VERILEX_EVIDENCE/$tok-dir"
echo "$tok" > "$VERILEX_EVIDENCE/$tok-dir/t.txt"
unshare --map-auto --map-root-user chown -R 1:1 "$VERILEX_EVIDENCE/$tok-dir"
echo '{"verdict": "pass", "observation": "fine"}'
`)
	done := verilex(t, root, nil, "run", "--json", "store-open | store-glanced")
	record := lastRun(t, root)
	equal(t, done.code, 2)
	equal(t, verdicts(record), []string{"green", "inconclusive"})
	equal(t, record.Words[1].Reason, any("secret pattern in evidence [redacted]-dir"))
	runs := filepath.Join(filepath.Dir(root), "state", "tally", "runs", record.Run, "run.json")
	usage := filepath.Join(filepath.Dir(root), "state", "usage", record.Run, "run.json")
	for _, text := range []string{done.stdout + done.stderr, read(t, runs), read(t, usage)} {
		if strings.Contains(text, token) {
			t.Fatalf("a token reached the output or the records: %q", text)
		}
	}
}

// A secret name in a directory another user owns cannot be renamed: the run still ends
// inconclusive and names the leak.
func TestSecretNameInDirectoryAnotherUserOwns(t *testing.T) {
	otherUser(t)
	root := product(t)
	t.Cleanup(func() {
		exec.Command("unshare", "--map-auto", "--map-root-user", "rm", "-rf", filepath.Join(filepath.Dir(root), "state")).Run()
	})
	addWord(t, root, "store-glanced", `mkdir "$VERILEX_EVIDENCE/d"
touch "$VERILEX_EVIDENCE/d/ghp_$(printf 'A%.0s' $(seq 36))"
unshare --map-auto --map-root-user chown -R 1:1 "$VERILEX_EVIDENCE/d"
echo '{"verdict": "pass", "observation": "fine"}'
`)
	done := verilex(t, root, nil, "run", "--json", "store-open | store-glanced")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, verdicts(record), []string{"green", "inconclusive"})
	equal(t, record.Words[1].Reason, any("secret pattern in evidence [redacted]"))
	if strings.Contains(done.stdout+done.stderr, "ghp_"+strings.Repeat("A", 36)) {
		t.Fatalf("a token reached the output: %q", done.stdout+done.stderr)
	}
}

// Evidence another user owns that verilex cannot read, such as a container's private file or
// directory, cannot be scanned: the run ends inconclusive and names it, and the rest of the
// evidence is still scanned and redacted.
func TestUnreadableEvidenceAnotherUserOwns(t *testing.T) {
	otherUser(t)
	foreign := `unshare --map-auto --map-root-user chown -R 1:1 "$VERILEX_EVIDENCE/k" "$VERILEX_EVIDENCE/d"
echo '{"verdict": "pass", "observation": "fine"}'
`
	for _, c := range []struct{ name, script, reason string }{
		{"file", `echo fine > "$VERILEX_EVIDENCE/k"
chmod 600 "$VERILEX_EVIDENCE/k"
mkdir "$VERILEX_EVIDENCE/d"
`, "cannot scan evidence k: permission denied"},
		{"directory", `touch "$VERILEX_EVIDENCE/k"
mkdir "$VERILEX_EVIDENCE/d"
echo fine > "$VERILEX_EVIDENCE/d/t.txt"
chmod 700 "$VERILEX_EVIDENCE/d"
`, "cannot scan evidence d: permission denied"},
		{"secret-beside", `echo fine > "$VERILEX_EVIDENCE/k"
chmod 600 "$VERILEX_EVIDENCE/k"
mkdir "$VERILEX_EVIDENCE/d"
echo "ghp_$(printf 'A%.0s' $(seq 36))" > "$VERILEX_EVIDENCE/t.txt"
`, "secret pattern in evidence t.txt"},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := product(t)
			t.Cleanup(func() {
				exec.Command("unshare", "--map-auto", "--map-root-user", "rm", "-rf", filepath.Join(filepath.Dir(root), "state")).Run()
			})
			addWord(t, root, "store-glanced", c.script+foreign)
			done := verilex(t, root, nil, "run", "store-open | store-glanced")
			equal(t, done.code, 2)
			record := lastRun(t, root)
			equal(t, verdicts(record), []string{"green", "inconclusive"})
			equal(t, record.Words[1].Reason, any(c.reason))
			equal(t, stores(t, root), []string{})
			if c.name == "secret-beside" {
				equal(t, read(t, filepath.Join(record.Words[1].Evidence, "t.txt")), "[redacted]\n")
				if strings.Contains(done.stdout+done.stderr, "ghp_"+strings.Repeat("A", 36)) {
					t.Fatalf("a token reached the output: %q", done.stdout+done.stderr)
				}
			}
		})
	}
}

// A word that gives its whole evidence directory away leaves verilex unable to write the exit
// file, and the run is inconclusive, not "not run" and never green.
func TestEvidenceDirectoryGivenAway(t *testing.T) {
	otherUser(t)
	cases := []struct{ mode, reason string }{
		{"700", "cannot scan evidence 02-store-glanced: permission denied"},
		{"755", "cannot scan evidence exit: permission denied"},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			root := product(t)
			t.Cleanup(func() {
				exec.Command("unshare", "--map-auto", "--map-root-user", "rm", "-rf", filepath.Join(filepath.Dir(root), "state")).Run()
			})
			addWord(t, root, "store-glanced", `echo fine > "$VERILEX_EVIDENCE/f.txt"
chmod `+c.mode+` "$VERILEX_EVIDENCE"
unshare --map-auto --map-root-user chown -R 1:1 "$VERILEX_EVIDENCE"
echo '{"verdict": "pass", "observation": "fine"}'
`)
			done := verilex(t, root, nil, "run", "store-open | store-glanced")
			equal(t, done.code, 2)
			record := lastRun(t, root)
			equal(t, verdicts(record), []string{"green", "inconclusive"})
			equal(t, record.Words[1].Reason, any(c.reason))
			equal(t, *record.Reason, "store-glanced: "+c.reason)
			equal(t, *record.EvidenceKept, true)
		})
	}
}

// A frame step that gives its evidence directory away but leaves it open keeps verilex from
// writing its exit code, so the instance is not vouched for and the run is inconclusive.
func TestFrameEvidenceGivenAway(t *testing.T) {
	otherUser(t)
	root := product(t)
	t.Cleanup(func() {
		exec.Command("unshare", "--map-auto", "--map-root-user", "rm", "-rf", filepath.Join(filepath.Dir(root), "state")).Run()
	})
	path := filepath.Join(root, ".verilex", "frame", "doctor")
	give := `os.chmod(os.environ["VERILEX_EVIDENCE"], 0o755)
subprocess.run(["unshare", "--map-auto", "--map-root-user", "chown", "-R", "1:1", os.environ["VERILEX_EVIDENCE"]], check=True)
`
	write(t, path, strings.Replace(read(t, path), "sys.exit(0 if", give+"sys.exit(0 if", 1), 0755)
	done := verilex(t, root, nil, "run", "store-open")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, *record.Verdict, "inconclusive")
	equal(t, *record.Reason, "doctor refused the instance (exit 2)")
	equal(t, record.Frame[1].Unread, "exit")
	equal(t, len(record.Words), 0)
}

// A word that writes its own exit file and then gives its evidence directory away readable kept
// verilex from writing the exit code: the forged file cannot back the pass, so the word and a frame
// step that does the same are inconclusive.
func TestGivenAwayEvidenceWithItsOwnExitFile(t *testing.T) {
	otherUser(t)
	root := product(t)
	t.Cleanup(func() {
		exec.Command("unshare", "--map-auto", "--map-root-user", "rm", "-rf", filepath.Join(filepath.Dir(root), "state")).Run()
	})
	addWord(t, root, "store-glanced", `printf '99\n' > "$VERILEX_EVIDENCE/exit"
chmod 755 "$VERILEX_EVIDENCE"
unshare --map-auto --map-root-user chown -R 1:1 "$VERILEX_EVIDENCE"
echo '{"verdict": "pass", "observation": "fine"}'
`)
	done := verilex(t, root, nil, "run", "store-open | store-glanced")
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, verdicts(record), []string{"green", "inconclusive"})
	equal(t, record.Words[1].Reason, any("cannot scan evidence exit: permission denied"))
	equal(t, read(t, filepath.Join(record.Words[1].Evidence, "exit")), "99\n")

	frameRoot := product(t)
	t.Cleanup(func() {
		exec.Command("unshare", "--map-auto", "--map-root-user", "rm", "-rf", filepath.Join(filepath.Dir(frameRoot), "state")).Run()
	})
	path := filepath.Join(frameRoot, ".verilex", "frame", "doctor")
	give := `open(os.path.join(os.environ["VERILEX_EVIDENCE"], "exit"), "w").write("7\n")
os.chmod(os.environ["VERILEX_EVIDENCE"], 0o755)
subprocess.run(["unshare", "--map-auto", "--map-root-user", "chown", "-R", "1:1", os.environ["VERILEX_EVIDENCE"]], check=True)
`
	write(t, path, strings.Replace(read(t, path), "sys.exit(0 if", give+"sys.exit(0 if", 1), 0755)
	done = verilex(t, frameRoot, nil, "run", "store-open")
	equal(t, done.code, 2)
	record = lastRun(t, frameRoot)
	equal(t, *record.Reason, "doctor refused the instance (exit 2)")
	equal(t, record.Frame[1].Unread, "exit")
}

// A step that leaves a pipe as its exit file in a directory it gave away cannot make verilex wait
// on it: the word is inconclusive.
func TestGivenAwayExitPipeDoesNotHang(t *testing.T) {
	otherUser(t)
	root := product(t)
	t.Cleanup(func() {
		exec.Command("unshare", "--map-auto", "--map-root-user", "rm", "-rf", filepath.Join(filepath.Dir(root), "state")).Run()
	})
	addWord(t, root, "store-glanced", `mkfifo "$VERILEX_EVIDENCE/exit"
chmod 755 "$VERILEX_EVIDENCE"
unshare --map-auto --map-root-user chown -R 1:1 "$VERILEX_EVIDENCE"
echo '{"verdict": "pass", "observation": "fine"}'
`)
	run := start(t, root, nil, "run", "store-open | store-glanced")
	hung := time.AfterFunc(time.Minute, func() { run.cmd.Process.Kill() })
	done := run.wait(t)
	hung.Stop()
	equal(t, done.code, 2)
	record := lastRun(t, root)
	equal(t, verdicts(record), []string{"green", "inconclusive"})
	equal(t, record.Words[1].Reason, any("cannot scan evidence exit: permission denied"))
}

// The cleanup step may not hide a word's evidence: a stdout verilex could open before the step
// and cannot open after it did not survive, whatever the word's verdict, so the run is
// inconclusive.
func TestCleanupThatGivesAWordsEvidenceAway(t *testing.T) {
	otherUser(t)
	give := `os.chmod(word, 0o700)
subprocess.run(["unshare", "--map-auto", "--map-root-user", "chown", "-R", "1:1", word], check=True)
`
	changes := map[string]string{
		"given away": give,
		"gutted":     `os.remove(os.path.join(word, "stdout"))` + "\n" + give,
		"stdout given away": `os.chmod(os.path.join(word, "stdout"), 0o600)
subprocess.run(["unshare", "--map-auto", "--map-root-user", "chown", "1:1", os.path.join(word, "stdout")], check=True)
`,
	}
	reports := map[string]string{
		"green": `echo '{"verdict": "pass", "observation": "fine"}'` + "\n",
		"red":   `echo '{"verdict": "fail", "preconditions_held": true}'` + "\nexit 1\n",
	}
	for change, act := range changes {
		for want, report := range reports {
			t.Run(change+" "+want, func(t *testing.T) {
				root := product(t)
				t.Cleanup(func() {
					exec.Command("unshare", "--map-auto", "--map-root-user", "rm", "-rf", filepath.Join(filepath.Dir(root), "state")).Run()
				})
				path := filepath.Join(root, ".verilex", "frame", "cleanup")
				write(t, path, read(t, path)+`import subprocess
word = os.path.join(os.path.dirname(os.environ["VERILEX_EVIDENCE"]), "02-store-glanced")
`+act, 0755)
				addWord(t, root, "store-glanced", report)
				done := verilex(t, root, nil, "run", "store-open | store-glanced")
				equal(t, done.code, 2)
				record := lastRun(t, root)
				equal(t, verdicts(record), []string{"green", want})
				equal(t, *record.Reason, "evidence did not survive cleanup")
				equal(t, *record.EvidenceKept, false)
			})
		}
	}
}

// A word that leaves its own evidence directory or stdout closed, as `cp -a` from a read-only tree
// does, still gets its exit code written and its report read: the run is green and the evidence
// keeps its exit file and its modes.
func TestClosedEvidenceTheWordOwnsStillBacksAPass(t *testing.T) {
	cases := []struct{ closing, path, mode string }{
		{`chmod 500 "$VERILEX_EVIDENCE"`, "", "dr-x------"},
		{`chmod 000 "$VERILEX_EVIDENCE"`, "", "d---------"},
		{`chmod 000 "$VERILEX_EVIDENCE/stdout"`, "stdout", "----------"},
		{`printf '1\n' > "$VERILEX_EVIDENCE/exit"; chmod 400 "$VERILEX_EVIDENCE/exit"`, "exit", "-rw-------"},
		{`ln -s /etc/passwd "$VERILEX_EVIDENCE/exit"`, "exit", "-rw-------"},
	}
	for _, c := range cases {
		t.Run(c.closing, func(t *testing.T) {
			root := product(t)
			addWord(t, root, "store-glanced", "echo fine > \"$VERILEX_EVIDENCE/f.txt\"\n"+c.closing+"\necho '{\"verdict\": \"pass\", \"observation\": \"fine\"}'\n")
			done := verilex(t, root, nil, "run", "store-open | store-glanced")
			equal(t, done.code, 0)
			record := lastRun(t, root)
			equal(t, verdicts(record), []string{"green", "green"})
			evidence := record.Words[1].Evidence
			info, err := os.Stat(filepath.Join(evidence, c.path))
			if err != nil {
				t.Fatal(err)
			}
			equal(t, info.Mode().String(), c.mode)
			if err = os.Chmod(evidence, 0700); err != nil {
				t.Fatal(err)
			}
			exit, err := os.ReadFile(filepath.Join(evidence, "exit"))
			if err != nil {
				t.Fatal(err)
			}
			equal(t, string(exit), "0\n")
		})
	}
}

// verilex opens closed evidence only inside the evidence: a stdout link to a closed file
// elsewhere is not opened, so the report verilex never scanned cannot back a pass.
func TestClosedLinkTargetIsNotOpened(t *testing.T) {
	root := product(t)
	outside := filepath.Join(t.TempDir(), "report")
	write(t, outside, `{"verdict": "pass", "observation": "fine"}`+"\n", 0)
	addWord(t, root, "store-glanced", "rm \"$VERILEX_EVIDENCE/stdout\"\nln -s '"+outside+"' \"$VERILEX_EVIDENCE/stdout\"\n")
	done := verilex(t, root, nil, "run", "store-open | store-glanced")
	equal(t, done.code, 2)
	equal(t, verdicts(lastRun(t, root)), []string{"green", "inconclusive"})
	info, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, info.Mode().String(), "----------")
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
	contains(t, done.stderr, "no closing quotation")
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
