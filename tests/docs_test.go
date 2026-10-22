package tests

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/DereKk8/verilex/internal/index"
)

// guide is a doc with claim anchors: under a heading, two in one passage, and one in a fenced
// block, which is an example and ties nothing.
const guide = "# Guide\n\nHow tally works.\n\n" +
	"## Adding items\n<!-- claims: item-added -->\n\n`tally add NAME` stores NAME.\n\n" +
	"## Listing\n<!-- claims: item-listed -->\n\n`tally list` shows what is stored.\n<!-- claims: item-added -->\n\n" +
	"```markdown\n<!-- claims: no-such-claim -->\n```\n"

// Done-when (PER-321): a code change names the doc passages tied to the claims it touches, and a
// doc change names the claims its passages are tied to, without re-running anything.
func TestChangeLookupNamesTiedDocPassages(t *testing.T) {
	root := product(t)
	writeDoc(t, filepath.Join(root, "docs", "guide.md"), guide)
	writeDoc(t, filepath.Join(root, "docs", "notes", "plain.md"), "# Plain\n\nNo anchors.\n")

	change := changed(t, root, ".verilex/words/item-listed/run")
	equal(t, claimsOf(change), []string{"item-listed"})
	equal(t, change.Passages, []index.Passage{{File: "docs/guide.md", Heading: "Listing", Claims: []string{"item-listed"}}})
	equal(t, change.Named, []index.Passage{})
	equal(t, verilex(t, root, nil, "index", "--changed", ".verilex/words/item-listed/run").stdout,
		"changed: 1 claim(s); 0 of 0 known chain(s) must re-run; 1 doc passage(s) may now be false\n"+
			"item-listed  A stored item shows up when a user lists the store.  (dormant)\n"+
			"  doc  docs/guide.md: Listing  (item-listed)\n")

	change = changed(t, root, "bin/tally")
	equal(t, change.Passages, []index.Passage{
		{File: "docs/guide.md", Heading: "Adding items", Claims: []string{"item-added"}},
		{File: "docs/guide.md", Heading: "Listing", Claims: []string{"item-listed", "item-added"}},
	})

	change = changed(t, root, "docs/guide.md")
	equal(t, claimsOf(change), []string{})
	equal(t, len(change.Chains), 0)
	equal(t, change.Passages, []index.Passage{})
	equal(t, change.Named, []index.Passage{
		{File: "docs/guide.md", Heading: "Adding items", Claims: []string{"item-added"}},
		{File: "docs/guide.md", Heading: "Listing", Claims: []string{"item-listed", "item-added"}},
	})
	equal(t, verilex(t, root, nil, "index", "--changed", "docs").stdout,
		"changed: 0 claim(s); 0 of 0 known chain(s) must re-run; the changed docs name 2 claim(s)\n"+
			"  names  docs/guide.md: Adding items  (item-added)\n"+
			"  names  docs/guide.md: Listing  (item-listed, item-added)\n")
	equal(t, verilex(t, root, nil, "index", "--changed", "docs/notes/plain.md").stdout, "changed: 0 claim(s); 0 of 0 known chain(s) must re-run\n")
}

// Done-when (PER-321): an anchor that names a claim that does not exist, or that ties no passage,
// is refused, so a doc never points at a claim nobody proves.
func TestChangeLookupRefusesABrokenDocAnchor(t *testing.T) {
	root := product(t)
	writeDoc(t, filepath.Join(root, "docs", "guide.md"), "# Guide\n\n## Adding items\n\n<!-- claims: item-added item-addded -->\n")
	done := verilex(t, root, nil, "index", "--changed", "bin/tally")
	equal(t, done.code, 2)
	equal(t, done.stdout, "")
	equal(t, done.stderr, "verilex: refused: docs/guide.md:5: the anchor names claim item-addded, which does not exist; `verilex claims` lists the claims\n")

	writeDoc(t, filepath.Join(root, "docs", "guide.md"), "<!-- claims: item-added -->\n# Guide\n")
	done = verilex(t, root, nil, "index", "--changed", "bin/tally")
	equal(t, done.code, 2)
	equal(t, done.stderr, "verilex: refused: docs/guide.md:1: the anchor sits above the first heading; put it under the heading of the passage it describes\n")

	writeDoc(t, filepath.Join(root, "docs", "guide.md"), "# Guide\n<!-- claims: -->\n")
	done = verilex(t, root, nil, "index", "--changed", "bin/tally")
	equal(t, done.code, 2)
	equal(t, done.stderr, "verilex: refused: docs/guide.md:2: write an anchor as `<!-- claims: <claim> ... -->`, naming at least one claim\n")

	// A near-miss anchor is refused, not silently ignored (issue #49 S8).
	for _, anchor := range []string{"<!-- claim: item-listed -->", "<!-- Claims: item-listed -->", "<!-- claims item-listed -->", "<!--claims=item-listed-->",
		"<!-- claims; item-listed -->", "<!-- claims, item-listed -->", "<!-- claims- item-listed -->", "<!-- claims -->", "<!-- claims item_listed -->", "<!-- Claims Item_listed -->"} {
		writeDoc(t, filepath.Join(root, "docs", "guide.md"), "# Guide\n\n## Listing\n"+anchor+"\n")
		done = verilex(t, root, nil, "index", "--changed", "docs/guide.md")
		equal(t, done.code, 2)
		equal(t, done.stdout, "")
		equal(t, done.stderr, "verilex: refused: docs/guide.md:4: write an anchor as `<!-- claims: <claim> ... -->`, naming at least one claim\n")
	}
	// A note that opens with the word but goes on with a plain word is no anchor. So is a mistyped
	// anchor that reads as one, such as <!-- claims item listed -->: it ties nothing (documented).
	for _, comment := range []string{"<!-- claims item listed -->", "<!-- claims for item-listed -->",
		"<!-- claimed by nobody -->", "<!-- Claims below were reviewed in Q3 -->", "<!-- claim numbers come from the audit -->",
		"<!-- claim-free section -->", "<!-- claim/owner map in README -->", "<!-- claims.md lists them -->"} {
		writeDoc(t, filepath.Join(root, "docs", "guide.md"), "# Guide\n\n## Listing\n"+comment+"\n")
		done = verilex(t, root, nil, "index", "--changed", "bin/tally")
		equal(t, done.stderr, "")
		equal(t, done.code, 0)
	}
}

// An anchor shown as an example in an indented code block (four spaces or a tab) ties nothing, as
// on the site, which shows it; and a UTF-8 byte order mark does not hide the first heading.
func TestChangeLookupReadsOnlyTheAnchorsTheSiteHides(t *testing.T) {
	root := product(t)
	writeDoc(t, filepath.Join(root, "docs", "guide.md"), "\ufeff# Guide\n<!-- claims: item-added -->\n")
	done := verilex(t, root, nil, "index", "--changed", "docs")
	equal(t, done.stderr, "")
	equal(t, done.stdout, "changed: 0 claim(s); 0 of 0 known chain(s) must re-run; the changed docs name 1 claim(s)\n"+
		"  names  docs/guide.md: Guide  (item-added)\n")

	writeDoc(t, filepath.Join(root, "docs", "guide.md"), "# Guide\n\nWrite an anchor like this:\n\n    <!-- claims: no-such-claim -->\n\n"+
		"or under a tab:\n\n\t<!-- claims: item-listed -->\n\n## Adding items\n<!-- claims: item-added -->\n")
	done = verilex(t, root, nil, "index", "--changed", "docs")
	equal(t, done.stderr, "")
	equal(t, done.stdout, "changed: 0 claim(s); 0 of 0 known chain(s) must re-run; the changed docs name 1 claim(s)\n"+
		"  names  docs/guide.md: Adding items  (item-added)\n")
	equal(t, changed(t, root, "bin/tally").Passages, []index.Passage{{File: "docs/guide.md", Heading: "Adding items", Claims: []string{"item-added"}}})
}

// Only regular files under docs/ are read: a dangling symlink cannot refuse the lookup, a symlink
// to a file outside the product is not read, and a FIFO cannot block it.
func TestChangeLookupReadsOnlyRegularDocs(t *testing.T) {
	root := product(t)
	docs := filepath.Join(root, "docs")
	writeDoc(t, filepath.Join(docs, "guide.md"), "# Guide\n<!-- claims: item-added -->\n")
	outside := filepath.Join(t.TempDir(), "outside.md")
	writeDoc(t, outside, "# Outside\n<!-- claims: no-such-claim -->\n")
	for link, target := range map[string]string{"gone.md": filepath.Join(docs, "missing.md"), "linked.md": outside} {
		if err := os.Symlink(target, filepath.Join(docs, link)); err != nil {
			t.Fatal(err)
		}
	}
	if err := syscall.Mkfifo(filepath.Join(docs, "pipe.md"), 0600); err != nil {
		t.Fatal(err)
	}
	p := start(t, root, nil, "index", "--changed", "docs")
	timer := time.AfterFunc(10*time.Second, func() { _ = p.cmd.Process.Kill() })
	defer timer.Stop()
	done := p.wait(t)
	equal(t, done.code, 0)
	equal(t, done.stderr, "")
	equal(t, done.stdout, "changed: 0 claim(s); 0 of 0 known chain(s) must re-run; the changed docs name 1 claim(s)\n"+
		"  names  docs/guide.md: Guide  (item-added)\n")
}

func writeDoc(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	write(t, path, text, 0644)
}
