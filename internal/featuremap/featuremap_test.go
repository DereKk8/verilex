package featuremap_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
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
	"- **`item-add`.** Run `tally add NAME`. Expect exit 0 and `added NAME`;\n  `store.json` lists NAME.\n" +
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

// pinner writes items.md under a skill directory and pins its `item-add` sub-feature, with the
// prose fingerprint of accepted as the one a reviewer accepted.
func pinner(t *testing.T, accepted string) func(text string, requirements []string, covered ...string) featuremap.Anchor {
	root := t.TempDir()
	dir := filepath.Join(root, "skills", "verify-x")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	pin := func(text, prose string, requirements, covered []string) featuremap.Anchor {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "items.md"), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
		a, err := featuremap.Pin(root, []string{"skills"}, featuremap.Source{Ref: "verify-x/items.md#item-add", Requirements: requirements, Prose: prose, Covered: covered})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	prose := pin(accepted, "", nil, nil).Prose
	if len(prose) != featuremap.ProseDigits {
		t.Fatalf("prose %q", prose)
	}
	return func(text string, requirements []string, covered ...string) featuremap.Anchor {
		t.Helper()
		return pin(text, prose, requirements, covered)
	}
}

func reviews(t *testing.T, a featuremap.Anchor, want ...string) {
	t.Helper()
	if !slices.Equal(a.Review, want) {
		t.Fatalf("review %q, want %q", a.Review, want)
	}
}

func changed(a featuremap.Anchor) string {
	return "prose changed: check that the claim still holds, then pin prose " + a.Prose
}

const added = "Expect exit 0 and `added NAME`; `store.json` lists NAME."

// R1: an anchor asks for review only when its sub-feature id or one of its requirement
// sentences changes; commands and run history around them never do.
func TestPinFlagsOnlyChangedIdsAndRequirements(t *testing.T) {
	pin := pinner(t, runbook)
	base := pin(runbook, []string{added})
	if len(base.Review) != 0 || base.File != filepath.Join("skills", "verify-x", "items.md") || len(base.Hash) != 64 {
		t.Fatalf("%#v", base)
	}
	noise := strings.NewReplacer("Run `tally add NAME`.", "Run `tally --quiet add NAME`.", "Items\n", "Items we keep\n", "2026-09-12", "2026-10-01").Replace(runbook)
	if a := pin(noise, []string{added}); len(a.Review) != 0 || a.Hash != base.Hash || a.Prose != base.Prose {
		t.Fatalf("noise asked for review: %#v", a)
	}
	reviews(t, pin(strings.Replace(runbook, "`added NAME`", "`stored NAME`", 1), []string{added}),
		"requirement changed or gone: "+added, "requirement no claim maps: Expect exit 0 and `stored NAME`; `store.json` lists NAME.")
	reviews(t, pin(strings.ReplaceAll(runbook, "`item-add`", "`item-put`"), []string{added}), "sub-feature item-add is gone")
	reviews(t, pin(runbook, []string{"Run `tally add NAME`.", added}), "requirement changed or gone: Run `tally add NAME`.")
	missing, err := featuremap.Pin(t.TempDir(), []string{"skills"}, featuremap.Source{Ref: "verify-x/items.md#item-add", Requirements: []string{added}})
	if err != nil || len(missing.Review) != 1 || !strings.HasPrefix(missing.Review[0], "no feature file") {
		t.Fatalf("%#v %v", missing, err)
	}
}

// Any change to the sub-feature's prose outside code spans asks for review, the words of a Run
// sentence included, so a rule written outside a requirement sentence is never missed. Command
// edits, dated run history and layout do not.
func TestPinFlagsProseChangesButNotCommands(t *testing.T) {
	pin := pinner(t, runbook)
	base := pin(runbook, []string{added})
	for name, edit := range map[string][2]string{
		"run sentence": {"Run `tally add NAME`.", "Run `tally add NAME`, which must exit 2 when NAME is stored."},
		"definition":   {"stores a named item", "keeps a named item"},
		"new sentence": {"lists NAME.\n", "lists NAME. Never add a NAME twice.\n"},
	} {
		a := pin(strings.Replace(runbook, edit[0], edit[1], 1), []string{added})
		if a.Prose == base.Prose {
			t.Fatalf("%s: prose %s did not change", name, a.Prose)
		}
		reviews(t, a, changed(a))
	}
	for name, edit := range map[string][2]string{
		"command":     {"Run `tally add NAME`.", "Run `tally --store \"$STORE\" add NAME`."},
		"run history": {"lists NAME.\n", "lists NAME. Verified 2026-09-12: it exits 0.\n"},
		"layout":      {"Run `tally add NAME`. Expect", "Run   `tally add NAME`.\n  Expect"},
		"emphasis":    {"stores a named item", "stores a **named** item"},
	} {
		if a := pin(strings.Replace(runbook, edit[0], edit[1], 1), []string{added}); len(a.Review) != 0 {
			t.Fatalf("%s: %#v", name, a)
		}
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills", "verify-x"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "verify-x", "items.md"), []byte(runbook), 0644); err != nil {
		t.Fatal(err)
	}
	unpinned, err := featuremap.Pin(root, []string{"skills"}, featuremap.Source{Ref: "verify-x/items.md#item-add", Requirements: []string{added}})
	if err != nil {
		t.Fatal(err)
	}
	reviews(t, unpinned, "prose is not pinned: check that the claim holds, then pin prose "+base.Prose)
}

// R1: a sub-feature is the text its id names, so its requirement sentences count only there. A
// sentence moved to another step, an id left only as a mention elsewhere, or a claim mapped to a
// sentence of another step asks for review.
func TestPinScopesRequirementsToTheirSubFeature(t *testing.T) {
	pin := pinner(t, runbook)
	moved := strings.Replace(runbook, " Expect exit 0 and `added NAME`;\n  `store.json` lists NAME.\n", "\n", 1) + "| add | " + added + " |\n"
	reviews(t, pin(moved, []string{added}), "requirement is outside sub-feature item-add: "+added)
	mention := strings.ReplaceAll(runbook, "`item-add`", "`item-put`") + "\nUse `item-add` only through the CLI.\n"
	reviews(t, pin(mention, []string{added}), "sub-feature item-add is gone")
	wrapped := strings.Replace(runbook, "**`item-add`.**", "**Add.**", 1) + "\nText that wraps onto the next line\n`item-add` there does not open a block.\n"
	a := pin(wrapped, []string{added})
	reviews(t, a, "requirement is outside sub-feature item-add: "+added, changed(a))
	reviews(t, pin(runbook, []string{added, "Expect NAME on its own line."}), "requirement is outside sub-feature item-add: Expect NAME on its own line.")

	// A heading, a table row, and the lines and fences nested under a list item all belong to it.
	for name, text := range map[string]string{
		"heading":   "# Items\n\n## Steps\n\n### item-add\n\nRun `tally add NAME`.\n\n#### Result\n\n" + added + "\n\n### item-list\n\nExpect NAME on its own line.\n",
		"table row": "| Step | Result |\n|---|---|\n| `item-add` | " + added + " |\n| `item-list` | Expect NAME on its own line. |\n",
		"nested":    "- `item-add`: Run `tally add NAME`.\n\n  ```sh\n  tally add x # must exit 0\n  ```\n\n  " + added + "\n- `item-list`: Expect NAME on its own line.\n",
	} {
		if a := pinner(t, text)(text, []string{added}); len(a.Review) != 0 {
			t.Fatalf("%s: %#v", name, a)
		}
	}
	// A heading inside the sub-feature is prose too.
	heading := "# Items\n\n### item-add\n\nRun `tally add NAME`.\n\n#### Result\n\n" + added + "\n"
	a = pinner(t, heading)(strings.Replace(heading, "#### Result", "#### Outcome", 1), []string{added})
	reviews(t, a, changed(a))
}

// A requirement the sub-feature states that no claim maps asks for review and is named, unless
// a claim maps it in the same sub-feature.
func TestPinFlagsRequirementsNoClaimMaps(t *testing.T) {
	pin := pinner(t, runbook)
	refused := "Expect exit 2 and `exists NAME` when NAME is already stored."
	text := strings.Replace(runbook, "`store.json` lists NAME.\n", "`store.json` lists NAME. "+refused+"\n", 1)
	reviews(t, pin(text, []string{added}), "requirement no claim maps: "+refused)
	reviews(t, pin(text, []string{added}, added, "- "+refused))
	reviews(t, pin(text, []string{added, refused}))
}
