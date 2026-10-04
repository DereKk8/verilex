package stamp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DereKk8/verilex/internal/dictionary"
)

func project(t *testing.T) (dictionary.Project, []dictionary.Step) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		".verilex/config.yaml":     "project: demo\n",
		".verilex/frame/launch":    "#!/bin/sh\n",
		".verilex/words/a/run":     "#!/bin/sh\n",
		".verilex/words/a/word.md": "---\nword: a\n---\n",
		".verilex/words/b/run":     "#!/bin/sh\n",
		".verilex/words/b/word.md": "---\nword: b\n---\n",
		".verilex/words/help.sh":   "# shared\n",
		"product":                  "v1\n",
	}
	for name, text := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0700); err != nil {
			t.Fatal(err)
		}
	}
	p := dictionary.Project{Root: root, Name: "demo"}
	word := func(name string) dictionary.Word {
		return dictionary.Word{Name: name, Path: filepath.Join(p.Dir(), "words", name), Inputs: []string{"product"}, InputsDeclared: true, Env: []string{"DEMO_MODE"}}
	}
	return p, []dictionary.Step{{Word: word("a")}, {Word: word("b"), Argv: []string{"x"}}}
}

func TestStampsChainSoUpstreamChangesReachDependents(t *testing.T) {
	p, steps := project(t)
	before := Chain(p, steps)
	if before[0].Digest == "" || before[1].Digest == "" {
		t.Fatalf("unstamped: %+v", before)
	}
	if err := os.WriteFile(filepath.Join(p.Dir(), "words", "a", "run"), []byte("#!/bin/sh\n# edited\n"), 0700); err != nil {
		t.Fatal(err)
	}
	after := Chain(p, steps)
	if before[1].Components["word"] != after[1].Components["word"] || before[1].Digest == after[1].Digest {
		t.Fatal("b's own files are unchanged, yet its stamp must change with a's")
	}
	if got := Diff(before[1].Components, after[1].Components); got != "upstream changed" {
		t.Fatalf("diff: %q", got)
	}
	if got := Diff(before[0].Components, after[0].Components); got != "word changed" {
		t.Fatalf("diff: %q", got)
	}
}

func TestEnvironmentValuesAreStamped(t *testing.T) {
	p, steps := project(t)
	t.Setenv("DEMO_MODE", "")
	empty := Chain(p, steps)
	if err := os.Unsetenv("DEMO_MODE"); err != nil {
		t.Fatal(err)
	}
	unset := Chain(p, steps)
	t.Setenv("DEMO_MODE", "broken")
	broken := Chain(p, steps)
	if empty[0].Digest == unset[0].Digest || unset[0].Digest == broken[0].Digest || empty[0].Digest == broken[0].Digest {
		t.Fatal("environment values must each give a distinct stamp")
	}
	if got := Diff(unset[0].Components, broken[0].Components); got != "env DEMO_MODE changed" {
		t.Fatalf("diff: %q", got)
	}
}

func TestUnclearFootprintGivesNoStamp(t *testing.T) {
	p, steps := project(t)
	steps[0].Word.InputsDeclared = false
	stamps := Chain(p, steps)
	if stamps[0].Digest != "" || stamps[0].Unclear != "declares no inputs" {
		t.Fatalf("%+v", stamps[0])
	}
	if stamps[1].Digest != "" || stamps[1].Unclear != "upstream a has no stamp" {
		t.Fatalf("%+v", stamps[1])
	}
	p, steps = project(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(p.Dir(), "words", "a", "linked")); err != nil {
		t.Fatal(err)
	}
	if stamps = Chain(p, steps); stamps[0].Digest != "" {
		t.Fatal("a symlinked directory must leave the word unstamped")
	}
}

func TestSlotsNameTheChainPrefix(t *testing.T) {
	p, steps := project(t)
	full := Chain(p, steps)
	alone := Chain(p, steps[:1])
	if full[0].Slot != alone[0].Slot || full[0].Digest != alone[0].Digest {
		t.Fatal("a chain's first step must stamp the same alone")
	}
	steps[1].Argv = []string{"y"}
	if Chain(p, steps)[1].Slot == full[1].Slot {
		t.Fatal("arguments must change the slot")
	}
}
