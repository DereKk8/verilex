package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The sweep uses each claim's planned chain, including its providers, and changes no files.
func TestStaleListsOnlyUnprovenClaimsReadOnly(t *testing.T) {
	root := curated(t)
	green(t, root, nil, chain)
	before := tree(t, filepath.Dir(root))
	equal(t, verilex(t, root, nil, "stale"), output{})
	equal(t, tree(t, filepath.Dir(root)), before)

	appendTo(t, root+"/.verilex/words/item-listed/run", "# changed\n")
	before = tree(t, filepath.Dir(root))
	done := verilex(t, root, nil, "stale")
	equal(t, done, output{code: 1, stdout: "unproven  item-listed: item-listed apple: word changed\n"})
	equal(t, tree(t, filepath.Dir(root)), before)
	planned := planClaims(t, root, "--claim", "item-listed")
	equal(t, planned.Rerun, "item-listed apple: word changed")

	// A provider change invalidates the dependent claim's chain too, not just its own claim.
	appendTo(t, root+"/.verilex/words/item-stored/run", "# changed\n")
	done = verilex(t, root, nil, "stale")
	equal(t, done, output{code: 1, stdout: "unproven  item-added: item-stored apple: word changed\n" +
		"unproven  item-listed: item-stored apple: word changed\n"})
}

func TestStaleExpiryAndMissingProof(t *testing.T) {
	root := curated(t)
	first := green(t, root, nil, chain)
	age(t, ledgerDir(root), 6*24*time.Hour)
	equal(t, verilex(t, root, nil, "stale"), output{})
	age(t, ledgerDir(root), 8*24*time.Hour)
	before := tree(t, filepath.Dir(root))
	reason := "store-open: the green result from run " + first.Run + " expired (8d old; results stand 7d)"
	equal(t, verilex(t, root, nil, "stale"), output{code: 1, stdout: "unproven  item-added: " + reason + "\n" +
		"unproven  item-listed: " + reason + "\n" + "unproven  store-opened: " + reason + "\n"})
	equal(t, tree(t, filepath.Dir(root)), before)
	if err := os.RemoveAll(ledgerDir(root)); err != nil {
		t.Fatal(err)
	}
	before = tree(t, filepath.Dir(root))
	equal(t, verilex(t, root, nil, "stale"), output{code: 1, stdout: "unproven  item-added: store-open: no green result on record\n" +
		"unproven  item-listed: store-open: no green result on record\n" +
		"unproven  store-opened: store-open: no green result on record\n"})
	equal(t, tree(t, filepath.Dir(root)), before)
}

func TestStaleNeverTrustsProvisionalOrDriftSuspectWords(t *testing.T) {
	root := product(t)
	green(t, root, nil, chain)
	done := verilex(t, root, nil, "stale")
	equal(t, done, output{code: 1, stdout: "unproven  item-added: store-open: provisional; only admitted words are skipped\n" +
		"unproven  item-listed: store-open: provisional; only admitted words are skipped\n" +
		"unproven  store-opened: store-open: provisional; only admitted words are skipped\n"})

	root = curated(t)
	green(t, root, nil, chain)
	appendTo(t, root+"/.verilex/words/item-listed/run", "# changed\n")
	// A green at the changed stamp still cannot stand while the word is drift-suspect.
	green(t, root, nil, chain)
	done = verilex(t, root, nil, "stale")
	equal(t, done.code, 1)
	contains(t, done.stdout, "unproven  item-listed: item-listed apple: drift-suspect")
	equal(t, strings.Count(done.stdout, "\n"), 1)
}

func TestStaleReportsUndecidableClaimsWithoutHidingOthers(t *testing.T) {
	root := curated(t)
	proveOn(t, root, chain, "store-open", "item-stored", "item-listed")
	green(t, root, nil, chain)
	if err := os.RemoveAll(root + "/.verilex/words/item-stored"); err != nil {
		t.Fatal(err)
	}
	appendTo(t, root+"/.verilex/words/store-open/run", "# changed\n")
	before := tree(t, filepath.Dir(root))
	equal(t, verilex(t, root, nil, "stale"), output{code: 2, stdout: "inconclusive  item-added: claim item-added has no word; `verilex index item-added` lists its words\n" +
		"inconclusive  item-listed: item-listed requires item:apple; no word provides it\n" +
		"unproven  store-opened: store-open: word changed\n"})
	equal(t, tree(t, filepath.Dir(root)), before)
}

func TestStaleUnreadableProviderLedgerIsInconclusive(t *testing.T) {
	root := curated(t)
	green(t, root, nil, chain)
	p := passFor(t, ledgerDir(root), "store-open")
	dir := filepath.Dir(p.path)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "not a directory", 0600)
	before := tree(t, filepath.Dir(root))
	done := verilex(t, root, nil, "stale")
	equal(t, done.code, 2)
	equal(t, done.stderr, "")
	for _, name := range []string{"item-added", "item-listed", "store-opened"} {
		contains(t, done.stdout, "inconclusive  "+name+": the ledger is unreadable: ")
	}
	equal(t, strings.Count(done.stdout, "\n"), 3)
	equal(t, tree(t, filepath.Dir(root)), before)
}

func TestStaleKeepsAdmittedBindingsNotNewestRun(t *testing.T) {
	root := curated(t)
	proveOn(t, root, chain, "store-open", "item-stored", "item-listed")
	green(t, root, nil, chain)
	green(t, root, nil, "store-open | item-stored pear | item-listed pear")
	equal(t, verilex(t, root, nil, "stale"), output{})
	// Only pear's provider passes remain. They must not stand for the admitted apple input,
	// even though the final apple step still has its own matching pass.
	for _, p := range passes(t, ledgerDir(root)) {
		if p.fields["label"] == "item-stored apple" {
			if err := os.Remove(p.path); err != nil {
				t.Fatal(err)
			}
		}
	}
	equal(t, verilex(t, root, nil, "stale"), output{code: 1, stdout: "unproven  item-added: item-stored apple: no green result on record\n" +
		"unproven  item-listed: item-stored apple: no green result on record\n"})
}

func TestStaleDetectsDeclaredDependencyUpdates(t *testing.T) {
	root := curated(t)
	if err := os.Mkdir(root+"/lib", 0755); err != nil {
		t.Fatal(err)
	}
	write(t, root+"/lib/mode", "healthy", 0644)
	file := root + "/.verilex/words/item-stored/word.md"
	replace(t, file, strings.Replace(read(t, file), "depends:\n", "depends:\n  paths: [lib/mode]\n", 1))
	admitted(t, root, "item-stored")
	green(t, root, nil, chain)
	equal(t, verilex(t, root, nil, "stale"), output{})
	replace(t, root+"/lib/mode", "updated dependency")
	equal(t, verilex(t, root, nil, "stale"), output{code: 1, stdout: "unproven  item-added: item-stored apple: depends lib/mode changed\n" +
		"unproven  item-listed: item-stored apple: depends lib/mode changed\n"})
}

func TestStaleEmptyVocabularyAndInvalidProject(t *testing.T) {
	root := product(t)
	withoutClaims(t, root)
	if err := os.RemoveAll(root + "/.verilex/claims"); err != nil {
		t.Fatal(err)
	}
	before := tree(t, filepath.Dir(root))
	equal(t, verilex(t, root, nil, "stale"), output{})
	equal(t, tree(t, filepath.Dir(root)), before)
	write(t, root+"/.verilex/config.yaml", "[invalid", 0644)
	done := verilex(t, root, nil, "stale")
	equal(t, done.code, 2)
	contains(t, done.stderr, "verilex: refused:")
}
