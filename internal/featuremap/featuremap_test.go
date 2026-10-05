package featuremap_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DereKk8/verilex/internal/featuremap"
)

const feature = "# Store\n\nIntro.\n\n## Open a store\n\nRun `open`.\n\n```sh\n# not a heading\n```\n\n### Gotchas\n\nOpening twice exits 1.\n\n## Close a store\n\nRun `close`.\n"

func TestResolveSelectsHeadingSectionsAndFeatureIDs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skills", "verify-x", "features")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "store.md"), []byte(feature), 0644); err != nil {
		t.Fatal(err)
	}
	dirs := []string{".missing", "skills"}
	for ref, want := range map[string]string{
		"verify-x/features/store.md#open-a-store":  "## Open a store\n\nRun `open`.\n\n```sh\n# not a heading\n```\n\n### Gotchas\n\nOpening twice exits 1.",
		"verify-x/features/store.md#close-a-store": "## Close a store\n\nRun `close`.",
		"verify-x/features/store.md#close":         feature[:len(feature)-1],
		"verify-x/features/store.md":               feature[:len(feature)-1],
	} {
		section, err := featuremap.Resolve(root, dirs, ref)
		if err != nil || section.Text != want || len(section.Hash) != 64 || section.File != filepath.Join("skills", "verify-x", "features", "store.md") {
			t.Fatalf("%s: %#v %v", ref, section, err)
		}
	}
	for _, ref := range []string{"verify-x/features/store.md#delete", "verify-x/features/other.md#open", "../store.md#open", "#open"} {
		var missing *featuremap.MissingError
		if _, err := featuremap.Resolve(root, dirs, ref); !errors.As(err, &missing) {
			t.Fatalf("%s resolved: %v", ref, err)
		}
	}
}

const runbook = "# Items\n\n## Sub-features\n\n- `item-add` stores a named item.\n\n## Steps\n\n" +
	"- **Add.** Run `tally add NAME`. Expect exit 0 and `added NAME`;\n  `store.json` lists NAME.\n" +
	"- Run `tally add NAME` again; it must exit 0.\n" +
	"- Verified 2026-09-12: `tally add` exits 0.\n" +
	"- The CLI returns `{\"items\": []}` for an empty store. Then a user adds more.\n" +
	"- Prose that mentions `must` only in code.\n\n" +
	"```sh\ntally add x # must exit 0\n```\n\n" +
	"| Step | Result |\n|---|---|\n| list | Expect NAME on its own line. |\n"

// R2: requirement sentences drop action sentences (and so their commands), dated run history,
// fenced code and layout, and keep literal values in code spans.
func TestRequirementsKeepOnlyNormalizedRequirementSentences(t *testing.T) {
	got := featuremap.Requirements(runbook)
	want := []string{
		"Expect exit 0 and `added NAME`; `store.json` lists NAME.",
		"The CLI returns `{\"items\": []}` for an empty store.",
		"Expect NAME on its own line.",
	}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %q, want %q", got[i], want[i])
		}
	}
	if s := featuremap.Normalize("- Expect  exit 0 and\n `added NAME`;  `store.json` lists NAME."); s != want[0] {
		t.Fatalf("normalized %q", s)
	}
}

// R1: an anchor asks for review only when its sub-feature id or one of its requirement
// sentences changes; prose, commands and run history around them never do.
func TestPinFlagsOnlyChangedIdsAndRequirements(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skills", "verify-x")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "items.md")
	pin := func(text string, requirements ...string) featuremap.Anchor {
		t.Helper()
		if err := os.WriteFile(file, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
		a, err := featuremap.Pin(root, []string{"skills"}, "verify-x/items.md#item-add", requirements)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	sentence := "Expect exit 0 and `added NAME`; `store.json` lists NAME."
	base := pin(runbook, sentence)
	if len(base.Review) != 0 || base.File != filepath.Join("skills", "verify-x", "items.md") || len(base.Hash) != 64 {
		t.Fatalf("%#v", base)
	}
	noise := strings.NewReplacer("Run `tally add NAME`.", "Run `tally --quiet add NAME`.", "stores a named item", "keeps a named item", "2026-09-12", "2026-10-01").Replace(runbook)
	if a := pin(noise, sentence); len(a.Review) != 0 || a.Hash != base.Hash {
		t.Fatalf("noise asked for review: %#v", a)
	}
	if a := pin(strings.Replace(runbook, "`added NAME`", "`stored NAME`", 1), sentence); len(a.Review) != 1 || a.Review[0] != "requirement changed or gone: "+sentence {
		t.Fatalf("%#v", a)
	}
	if a := pin(strings.Replace(runbook, "`item-add`", "`item-put`", 1), sentence); len(a.Review) != 1 || a.Review[0] != "sub-feature item-add is gone" {
		t.Fatalf("%#v", a)
	}
	if a := pin(runbook, "Run `tally add NAME`."); len(a.Review) != 1 {
		t.Fatalf("an action sentence was accepted as a requirement: %#v", a)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if a, err := featuremap.Pin(root, []string{"skills"}, "verify-x/items.md#item-add", []string{sentence}); err != nil || len(a.Review) != 1 || !strings.HasPrefix(a.Review[0], "no feature file") {
		t.Fatalf("%#v %v", a, err)
	}
}
