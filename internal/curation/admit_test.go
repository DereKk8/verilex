package curation_test

import (
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/DereKk8/verilex/internal/cli"
	"github.com/DereKk8/verilex/internal/curation"
	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/lifecycle"
)

// These tests drive the real CLI in this process, so an edit can land at a seam of propose or
// admit: while the command runs, at the one moment that matters, every time.

// The curator judged the packet, so admit records the word's files as the packet held them, or
// nothing. Here the word on disk is not the judged one when admit starts, and the judged files
// come back right after admit read the word: an admission of the files the curator never judged
// would turn clean once they are restored.
func TestAdmitRecordsOnlyTheWordFilesTheCuratorJudged(t *testing.T) {
	root := product(t)
	run := glanced(t, root)
	packet := propose(t, root, "store-glanced")
	file := verdict(t, root, packet.ID)
	judged := read(t, run)
	unjudged := judged + "\n# concurrent edit A\n"
	write(t, run, unjudged)
	curation.SetAfterRead(t, func() { write(t, run, judged) })

	done := verilex(t, root, "admit", "store-glanced", "--verdict", file)
	if a := admission(t, root, "store-glanced"); a != nil && a.WordDigest != packet.WordDigest {
		t.Fatalf("admit recorded word digest %s, not %s of the packet the curator judged: %+v", a.WordDigest, packet.WordDigest, done)
	}
	write(t, run, unjudged)
	if state := status(t, root, "store-glanced"); state == "admitted" {
		t.Fatalf("the word the curator never judged is admitted: %+v", done)
	}
	equal(t, done, output{code: 2, stderr: "verilex: refused: store-glanced changed since packet " + packet.ID + "; propose it again\n"})

	// Without the overlapping edit, the judged files are admitted, and the other files drift.
	curation.SetAfterRead(t, nil)
	write(t, run, judged)
	equal(t, verilex(t, root, "admit", "store-glanced", "--verdict", file), output{stdout: "admitted store-glanced (curator curator-model-1, 2 runs)\n"})
	write(t, run, unjudged)
	equal(t, verilex(t, root, "check").stdout, "check: 1 of 1 admitted drift-suspect; they always run\n  store-glanced: the word's files changed since admission\n")
}

// The same for the frame, which every word runs with: admit records the binding the packet held,
// or nothing. The frame is not the judged one when admit starts, and comes back right after admit
// read the binding.
func TestAdmitRecordsOnlyTheFrameTheCuratorJudged(t *testing.T) {
	root := product(t)
	glanced(t, root)
	packet := propose(t, root, "store-glanced")
	file := verdict(t, root, packet.ID)
	launch := filepath.Join(root, ".verilex", "frame", "launch")
	judged := read(t, launch)
	unjudged := judged + "\n# concurrent frame edit A\n"
	write(t, launch, unjudged)
	curation.SetAfterRead(t, func() { write(t, launch, judged) })

	done := verilex(t, root, "admit", "store-glanced", "--verdict", file)
	if a := admission(t, root, "store-glanced"); a != nil && !maps.Equal(a.Binding, packet.Binding) {
		t.Fatalf("admit recorded binding %v, not %v of the packet the curator judged: %+v", a.Binding, packet.Binding, done)
	}
	write(t, launch, unjudged)
	if state := status(t, root, "store-glanced"); state == "admitted" {
		t.Fatalf("the word is admitted on a frame the curator never judged: %+v", done)
	}
	equal(t, done, output{code: 2, stderr: "verilex: refused: store-glanced changed since packet " + packet.ID + "; propose it again\n"})

	curation.SetAfterRead(t, nil)
	write(t, launch, judged)
	equal(t, verilex(t, root, "admit", "store-glanced", "--verdict", file), output{stdout: "admitted store-glanced (curator curator-model-1, 2 runs)\n"})
	write(t, launch, unjudged)
	equal(t, verilex(t, root, "check").stdout, "check: 1 of 1 admitted drift-suspect; they always run\n  store-glanced: the frame changed since admission (.verilex/frame)\n")
}

// The curator reads the files a packet shows, and admit matches the packet's word digest, so the
// digest is of the files shown. An edit that lands right after propose read them never puts the
// digest of other files beside them, and those other files are never admitted on the packet.
func TestProposeDigestsTheWordFilesItShows(t *testing.T) {
	root := product(t)
	run := glanced(t, root)
	shown := read(t, run)
	curation.SetAfterFiles(t, func() { write(t, run, shown+"\n# edit after propose read the files\n") })
	packet := propose(t, root, "store-glanced")
	curation.SetAfterFiles(t, nil)
	equal(t, packet.Files["run"], shown)

	// The digest of exactly the files the packet shows, with the modes they have.
	copied := filepath.Join(t.TempDir(), "store-glanced")
	if err := os.Mkdir(copied, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, text := range packet.Files {
		info, err := os.Stat(filepath.Join(filepath.Dir(run), rel))
		if err != nil {
			t.Fatal(err)
		}
		writeMode(t, filepath.Join(copied, rel), text, info.Mode().Perm())
	}
	digest, err := lifecycle.WordDigest(dictionary.Word{Name: "store-glanced", Path: copied})
	if err != nil {
		t.Fatal(err)
	}
	if packet.WordDigest != digest {
		t.Fatalf("the packet shows the curator files with digest %s but holds word digest %s", digest, packet.WordDigest)
	}

	file := verdict(t, root, packet.ID)
	equal(t, verilex(t, root, "admit", "store-glanced", "--verdict", file), output{code: 2, stderr: "verilex: refused: store-glanced changed since packet " + packet.ID + "; propose it again\n"})
	write(t, run, shown)
	equal(t, verilex(t, root, "admit", "store-glanced", "--verdict", file), output{stdout: "admitted store-glanced (curator curator-model-1, 2 runs)\n"})
}

type output struct {
	code           int
	stdout, stderr string
}

// verilex runs the CLI on the product in this process.
func verilex(t *testing.T, root string, args ...string) output {
	t.Helper()
	var stdout, stderr strings.Builder
	code := cli.Main(append([]string{"--project", root}, args...), &stdout, &stderr)
	return output{code, stdout.String(), stderr.String()}
}

// product copies the tally sample product into a fresh directory with its own state, stores and
// config home, and returns its root.
func product(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "tally")
	if err = os.CopyFS(root, os.DirFS(filepath.Join("..", "..", "tests", "fixtures", "tally"))); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(base, "stores"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, value := range os.Environ() {
		if key, _, _ := strings.Cut(value, "="); strings.HasPrefix(key, "TALLY_") || strings.HasPrefix(key, "VERILEX_") {
			t.Setenv(key, "")
			os.Unsetenv(key)
		}
	}
	t.Setenv("VERILEX_HOME", filepath.Join(base, "state"))
	t.Setenv("TALLY_STORES", filepath.Join(base, "stores"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
	return root
}

// glanced adds store-glanced, a word without a claim, uses it in two runs, and returns its run file.
func glanced(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, ".verilex", "words", "store-glanced")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMode(t, filepath.Join(dir, "word.md"), "---\nword: store-glanced\npromise: A test word.\nrequires: [store]\nimplements: [verify-tally/features/store.md#store-open]\n---\n", 0o644)
	writeMode(t, filepath.Join(dir, "run"), "#!/bin/sh\ncat >/dev/null\necho '{\"verdict\": \"pass\", \"observation\": \"fine\"}'\n", 0o755)
	for range 2 {
		if done := verilex(t, root, "run", "store-open | store-glanced"); done.code != 0 {
			t.Fatalf("run: %+v", done)
		}
	}
	return filepath.Join(dir, "run")
}

func propose(t *testing.T, root, word string) curation.Packet {
	t.Helper()
	done := verilex(t, root, "propose", word)
	m := regexp.MustCompile(`packet: (\S+)`).FindStringSubmatch(done.stdout)
	if done.code != 0 || m == nil {
		t.Fatalf("propose %s: %+v", word, done)
	}
	var packet curation.Packet
	if err := json.Unmarshal([]byte(read(t, m[1])), &packet); err != nil {
		t.Fatal(err)
	}
	return packet
}

// verdict writes an admit verdict for the packet and returns its path.
func verdict(t *testing.T, root, packet string) string {
	t.Helper()
	path := filepath.Join(filepath.Dir(root), "verdict.json")
	writeMode(t, path, `{"word": "store-glanced", "packet": "`+packet+`", "verdict": "admit", "curator": "curator-model-1", "reason": "One real moment, proven twice."}`, 0o644)
	return path
}

// admission reads the word's admission record, or nil when there is none.
func admission(t *testing.T, root, word string) *lifecycle.Admission {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".verilex", "words", word, "admission.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var a lifecycle.Admission
	if err = json.Unmarshal(data, &a); err != nil {
		t.Fatal(err)
	}
	return &a
}

func status(t *testing.T, root, word string) string {
	t.Helper()
	listing := verilex(t, root, "words").stdout
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(word) + `\b[^\n]*\n(?:  [^\n]*\n)*?  status:   (\S+)`).FindStringSubmatch(listing)
	if m == nil {
		t.Fatalf("%s not listed:\n%s", word, listing)
	}
	return m[1]
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// write replaces a file's contents and keeps its mode.
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

func equal[T any](t *testing.T, actual, expected T) {
	t.Helper()
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("got %#v, want %#v", actual, expected)
	}
}
