package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func claimFile(root, name string) string {
	return filepath.Join(root, ".verilex", "claims", name+".yaml")
}

// pinOf is the claim's current version as a word pins it: <claim>@<version>.
func pinOf(t *testing.T, root, name string) string {
	t.Helper()
	done := verilex(t, root, nil, "claims", "--json")
	var claims []struct{ Claim, Version string }
	if err := json.Unmarshal([]byte(done.stdout), &claims); err != nil {
		t.Fatalf("%v: %+v", err, done)
	}
	for _, c := range claims {
		if c.Claim == name {
			return c.Claim + "@" + c.Version
		}
	}
	t.Fatalf("no claim %s: %s", name, done.stdout)
	return ""
}

// edit replaces old with new in a file, which must contain old.
func edit(t *testing.T, path, old, new string) {
	t.Helper()
	text := read(t, path)
	if !strings.Contains(text, old) {
		t.Fatalf("%s lacks %q", path, old)
	}
	write(t, path, strings.Replace(text, old, new, 1), 0644)
}

func stale(word, pinned, current string) string {
	return "verilex: refused: " + word + " pins claim " + pinned + ", which is now " + current + "; a pass proves only the version it ran against: check that " + word + " still proves the claim, then pin " + current + "\n"
}

// Rule: a claim's identity is a fingerprint of its sentence, evidence contract, preconditions,
// entry points and pinned states. Layout and order never change it; any change to what it
// says is a new version, and a word pinned to the old one is refused before anything starts.
func TestClaimFingerprintIsStableAndFollowsItsIdentity(t *testing.T) {
	root := product(t)
	path := claimFile(root, "item-added")
	original := read(t, path)
	v1 := pinOf(t, root, "item-added")
	equal(t, len(strings.TrimPrefix(v1, "item-added@")), 12)

	for name, text := range map[string]string{
		"wrapped sentence": strings.Replace(original, "sentence: A named item is in the store.", "sentence: >-\n  A named   item\n  is in the store.", 1),
		"reordered keys":   strings.Replace(original, "claim: item-added\n", "", 1) + "claim: item-added\n",
		"repeated entry":   strings.Replace(original, "entry: [cli]", "entry: [cli, cli]", 1),
		"another source":   strings.Replace(original, "sources:\n", "sources:\n  - ref: verify-tally/features/items.md#item-list\n    requirements: [Expect NAME on its own line.]\n", 1),
	} {
		write(t, path, text, 0644)
		if got := pinOf(t, root, "item-added"); got != v1 {
			t.Fatalf("%s changed the version: %s, was %s", name, got, v1)
		}
	}

	versions := map[string]string{v1: "original"}
	for name, change := range map[string][2]string{
		"sentence":      {"sentence: A named item is in the store.", "sentence: A named item is in the store once."},
		"action":        {"prints `added NAME`.", "prints `stored NAME`."},
		"observation":   {"observation: store.json lists NAME.", "observation: store.json lists NAME once."},
		"non-proofs":    {"    - \"`added NAME` alone", "    - \"exit 0 alone does not prove the item was stored.\"\n    - \"`added NAME` alone"},
		"preconditions": {"entry: [cli]", "entry: [cli]\npreconditions: [store opened read-only]"},
		"entry points":  {"entry: [cli]", "entry: [cli, api]"},
		"pinned states": {"requires: [store]", "requires: [store, signed-in]"},
	} {
		write(t, path, strings.Replace(original, change[0], change[1], 1), 0644)
		version := pinOf(t, root, "item-added")
		if seen, ok := versions[version]; ok {
			t.Fatalf("changing the %s kept version %s of the %s", name, version, seen)
		}
		versions[version] = name
	}
	// Non-proofs are a set: their order is not identity.
	write(t, path, original, 0644)
	edit(t, path, "\"`added NAME` alone does not prove the item was stored.\"\n", "\"`added NAME` alone does not prove the item was stored.\"\n    - \"exit 0 alone does not prove the item was stored.\"\n")
	equal(t, versions[pinOf(t, root, "item-added")], "non-proofs")

	write(t, path, strings.Replace(original, "observation: store.json lists NAME.", "observation: store.json lists NAME once.", 1), 0644)
	v2 := pinOf(t, root, "item-added")
	done := verilex(t, root, nil, "run", chain)
	equal(t, done.code, 2)
	equal(t, done.stderr, stale("item-stored", v1, v2))
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "state")); !os.IsNotExist(err) {
		t.Fatalf("a refused chain recorded state: %v", err)
	}
	equal(t, stores(t, root), []string{})
	equal(t, plan(t, root, chain).stderr, stale("item-stored", v1, v2))
	contains(t, verilex(t, root, nil, "claims").stdout, v2+"  A named item is in the store.\n  entry: cli  words: stale: item-stored pins "+v1+"\n")
	// A chain without the stale word still runs.
	equal(t, verilex(t, root, nil, "run", "store-open").code, 0)

	write(t, path, original, 0644)
	equal(t, pinOf(t, root, "item-added"), v1)
	write(t, path, strings.Replace(original, "non_proofs:", "non_proof:", 1), 0644)
	done = verilex(t, root, nil, "claims")
	equal(t, done.code, 2)
	contains(t, done.stderr, "malformed claim")
	contains(t, done.stderr, "field non_proof not found")

	// A mapped sentence that is not one requirement sentence could never match the verify skill,
	// so the claim is refused instead of needing review forever.
	for _, sentence := range []string{"Run `bin/tally add NAME`.", "Verified 2026-09-12: `tally add` exits 0.", "Expect exit 0. Expect `added NAME`.", "The store holds NAME."} {
		write(t, path, strings.Replace(original, `"Expect exit 0 and `+"`added NAME`; `store.json`"+` lists NAME."`, strconv.Quote(sentence), 1), 0644)
		done = verilex(t, root, nil, "claims")
		equal(t, done.code, 2)
		contains(t, done.stderr, "is not one requirement sentence")
	}
}

// Rule: a pass is evidence only for the claim version it ran against, on a fresh instance and
// on a kept one.
func TestPassOnOneClaimVersionIsNotReusedForTheNext(t *testing.T) {
	root := curated(t)
	v1 := pinOf(t, root, "item-added")
	first := green(t, root, nil, chain)
	equal(t, first.Words[1].Proves, v1)
	equal(t, first.Words[1].Entry, "cli")
	skipped := green(t, root, nil, chain)
	equal(t, skipped.Skipped, true)
	equal(t, skipped.Words[1].Proves, v1)
	kept := green(t, root, nil, chain, "--keep")

	path := claimFile(root, "item-added")
	edit(t, path, "observation: store.json lists NAME.", "observation: store.json lists NAME exactly once.")
	v2 := pinOf(t, root, "item-added")
	equal(t, plan(t, root, chain).stderr, stale("item-stored", v1, v2))

	// Pinning the new version is a change to the word, so it is admitted again.
	edit(t, filepath.Join(root, ".verilex", "words", "item-stored", "word.md"), "claim: "+v1, "claim: "+v2)
	// Uses that proved the old version are no evidence for the new one either.
	contains(t, verilex(t, root, nil, "propose", "item-stored").stderr, "item-stored has counted uses in 0 run(s)")
	admitted(t, root, "item-stored")
	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; item-stored apple: claim changed\n")
	done := plan(t, root, chain, "--continue", kept.Run)
	equal(t, done.code, 2)
	contains(t, done.stderr, "verilex: refused: item-stored apple: claim changed; the kept instance already holds the effects of item-stored apple from run "+kept.Run)

	record := green(t, root, nil, chain)
	equal(t, record.Rerun, "item-stored apple: claim changed")
	ranLive(t, record)
	equal(t, record.Words[1].Proves, v2)
	again := green(t, root, nil, chain)
	equal(t, again.Skipped, true)
	equal(t, again.Words[1].ReliesOn, record.Run)
	equal(t, verilex(t, root, nil, "cleanup", kept.Run).stdout, "cleanup: done\n")
}

// Rule: a claim is anchored on its sub-feature id and the requirement sentences it maps to. A
// change to one of them flags the claim for review, and no word that proves it skips until the
// claim is reviewed; edits around them flag nothing. A review that leaves the claim's identity
// alone keeps its version, so every pass recorded for it stands.
func TestChangedSourceRequirementFlagsClaimForReview(t *testing.T) {
	root := curated(t)
	v1 := pinOf(t, root, "item-added")
	first := green(t, root, nil, chain)
	labels := []string{"store-open", "item-stored apple", "item-listed apple"}
	items := feature(root, "items.md")
	original, claim := read(t, items), read(t, claimFile(root, "item-added"))

	edit(t, items, "Run `bin/tally --store \"$STORE\" add NAME`.", "Run `bin/tally --store \"$STORE\" --quiet add NAME`.")
	edit(t, items, "A user adds named items to an open store and lists them.", "A user adds named items to an open store, then lists them.")
	edit(t, items, "Expect exit 0 and `added NAME`; `store.json` lists NAME.", "**Expect** exit 0 and\n  `added NAME`;   `store.json` lists NAME. Verified 2026-09-12: it exits 0.")
	write(t, items, read(t, items)+"\nVerified 2026-09-12: `tally add` exits 0 on a fresh store.\n", 0644)
	equal(t, strings.Contains(verilex(t, root, nil, "claims").stdout, "review:"), false)
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (3 admitted)\n")
	equal(t, plan(t, root, chain).stdout, "plan: skip 3, run 0\n"+skips(first.Run, labels...))

	write(t, items, original, 0644)
	reworded := "Expect exit 0 and `added NAME`, and expect `store.json` to list NAME."
	edit(t, items, addRequirement, reworded)
	review := itemAdd + ": requirement changed or gone: " + addRequirement + "; " + itemAdd + ": requirement no claim maps: " + reworded
	equal(t, verilex(t, root, nil, "claims").stdout, ""+
		v1+"  A named item is in the store.\n  entry: cli  words: item-stored\n"+
		"  review: "+itemAdd+": requirement changed or gone: "+addRequirement+"\n"+
		"  review: "+itemAdd+": requirement no claim maps: "+reworded+"\n"+
		pinOf(t, root, "item-listed")+"  A stored item shows up when a user lists the store.\n  entry: cli  words: item-listed\n"+
		pinOf(t, root, "store-opened")+"  A new, empty store is open and ready for items.\n  entry: cli  words: store-open\n")
	equal(t, pinOf(t, root, "item-added"), v1)
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 3 admitted drift-suspect; they always run\n  item-stored: claim item-added needs review: "+review+"\n")
	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; "+headline("item-stored apple: drift-suspect: claim item-added needs review: "+review)+"\n")

	// The reviewer judges that the claim still says the same and maps it to the new sentence.
	// The version and the passes recorded for it stand, but no pass was judged against the new
	// mapping, so every word that proves the claim runs live once before it may skip again.
	edit(t, claimFile(root, "item-added"), addRequirement, reworded)
	equal(t, pinOf(t, root, "item-added"), v1)
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (3 admitted)\n")
	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; item-stored apple: claim sources changed\n")
	remapped := green(t, root, nil, chain)
	equal(t, remapped.Rerun, "item-stored apple: claim sources changed")
	ranLive(t, remapped)
	equal(t, remapped.Words[1].Proves, v1)
	equal(t, plan(t, root, chain).stdout, "plan: skip 3, run 0\n"+skips(remapped.Run, labels...))

	// A requirement added to the sub-feature that no claim maps asks for review and is named;
	// mapping it there clears the review and, again, the next run goes live.
	write(t, items, strings.Replace(read(t, items), reworded, reworded+" Expect exit 2 and `exists NAME` when NAME is already stored.", 1), 0644)
	unmapped := itemAdd + ": requirement no claim maps: Expect exit 2 and `exists NAME` when NAME is already stored."
	contains(t, verilex(t, root, nil, "claims").stdout, "  entry: cli  words: item-stored\n  review: "+unmapped+"\n")
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 3 admitted drift-suspect; they always run\n  item-stored: claim item-added needs review: "+unmapped+"\n")
	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; item-stored apple: drift-suspect: claim item-added needs review: "+unmapped+"\n")
	edit(t, claimFile(root, "item-added"), "      - "+strconv.Quote(reworded), "      - "+strconv.Quote(reworded)+"\n      - \"Expect exit 2 and `exists NAME` when NAME is already stored.\"")
	equal(t, pinOf(t, root, "item-added"), v1)
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (3 admitted)\n")
	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; item-stored apple: claim sources changed\n")
	write(t, items, original, 0644)
	write(t, claimFile(root, "item-added"), claim, 0644)
	equal(t, green(t, root, nil, chain).Rerun, "item-stored apple: claim sources changed")

	write(t, items, strings.ReplaceAll(original, "`item-add`", "`item-put`"), 0644)
	contains(t, verilex(t, root, nil, "claims").stdout, "  review: "+itemAdd+": sub-feature item-add is gone\n")
	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; item-stored apple: drift-suspect: claim item-added needs review: "+itemAdd+": sub-feature item-add is gone\n")
}

// Rule: a claim's requirement sentences count only inside the text its sub-feature id names: the
// list items, paragraphs and table rows that open with the id, or a heading named by it. A
// sentence moved to another step, an id left only as a mention, or a claim mapped to a sentence
// of another sub-feature asks for review, so no word that proves the claim skips.
func TestClaimRequirementsStayInsideTheirSubFeature(t *testing.T) {
	root := curated(t)
	green(t, root, nil, chain)
	green(t, root, nil, chain, "--fresh")
	items := feature(root, "items.md")
	original, claim := read(t, items), read(t, claimFile(root, "item-added"))
	itemList := "verify-tally/features/items.md#item-list"

	// The item-add requirement moves into the item-list step.
	edit(t, items, " Expect exit 0 and `added NAME`; `store.json` lists NAME.", "")
	edit(t, items, "Expect NAME on its own line.", "Expect NAME on its own line. "+addRequirement)
	claims := verilex(t, root, nil, "claims").stdout
	contains(t, claims, "  entry: cli  words: item-stored\n  review: "+itemAdd+": requirement is outside sub-feature item-add: "+addRequirement+"\n")
	contains(t, claims, "  entry: cli  words: item-listed\n  review: "+itemList+": requirement no claim maps: "+addRequirement+"\n")
	equal(t, verilex(t, root, nil, "check").stdout, "check: 2 of 3 admitted drift-suspect; they always run\n"+
		"  item-listed: claim item-listed needs review: "+itemList+": requirement no claim maps: "+addRequirement+"\n"+
		"  item-stored: claim item-added needs review: "+itemAdd+": requirement is outside sub-feature item-add: "+addRequirement+"\n")
	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; item-stored apple: drift-suspect: claim item-added needs review: "+itemAdd+": requirement is outside sub-feature item-add: "+addRequirement+"\n")

	// The id stays only as a mention in prose, which names no sub-feature.
	write(t, items, strings.ReplaceAll(original, "`item-add`", "`item-put`")+"\nNever drive `item-add` through the store file.\n", 0644)
	contains(t, verilex(t, root, nil, "claims").stdout, "  entry: cli  words: item-stored\n  review: "+itemAdd+": sub-feature item-add is gone\n")
	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; item-stored apple: drift-suspect: claim item-added needs review: "+itemAdd+": sub-feature item-add is gone\n")

	// Mapping the claim to a sentence of another sub-feature never clears a review.
	write(t, items, original, 0644)
	edit(t, claimFile(root, "item-added"), "      - \""+addRequirement+"\"", "      - Expect NAME on its own line.")
	review := itemAdd + ": requirement is outside sub-feature item-add: Expect NAME on its own line.; " + itemAdd + ": requirement no claim maps: " + addRequirement
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 3 admitted drift-suspect; they always run\n  item-stored: claim item-added needs review: "+review+"\n")
	done := verilex(t, root, nil, "propose", "item-stored")
	equal(t, done.code, 2)
	contains(t, done.stderr, "verilex: refused: ")
	contains(t, done.stderr, "claim item-added needs review: "+review+"; bring its sources in line with the verify skill first")
	write(t, claimFile(root, "item-added"), claim, 0644)
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (3 admitted)\n")
}

// Rule: a claim pins the prose of its sub-feature too. Any change there outside code spans,
// even inside a Run sentence, asks for review, so a rule written outside a requirement sentence
// is never missed. The reviewer pins the new prose; the version stays and the next run is live.
func TestProseChangeInSubFeatureFlagsClaimForReview(t *testing.T) {
	root := curated(t)
	green(t, root, nil, chain)
	items := feature(root, "items.md")
	original := read(t, items)

	edit(t, items, "add NAME`. Expect", "add NAME`, which must exit 2 when NAME is stored. Expect")
	done := verilex(t, root, nil, "claims", "--json")
	var claims []struct {
		Claim   string
		Sources []struct{ Prose string }
	}
	if err := json.Unmarshal([]byte(done.stdout), &claims); err != nil {
		t.Fatal(err)
	}
	prose := claims[0].Sources[0].Prose
	equal(t, claims[0].Claim, "item-added")
	equal(t, len(prose), 12)
	review := itemAdd + ": prose changed: check that the claim still holds, then pin prose " + prose
	contains(t, verilex(t, root, nil, "claims").stdout, "  entry: cli  words: item-stored\n  review: "+review+"\n")
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 3 admitted drift-suspect; they always run\n  item-stored: claim item-added needs review: "+review+"\n")
	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; item-stored apple: drift-suspect: claim item-added needs review: "+review+"\n")

	// The reviewer judges that the claim still holds and pins the new prose.
	edit(t, claimFile(root, "item-added"), "prose: 2948a95bd310", "prose: "+prose)
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (3 admitted)\n")
	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; item-stored apple: claim sources changed\n")
	ranLive(t, green(t, root, nil, chain))
	equal(t, plan(t, root, chain).code, 0)
	contains(t, plan(t, root, chain).stdout, "plan: skip 3, run 0\n")

	// Prose of another sub-feature flags only the claims anchored there.
	write(t, items, strings.Replace(original, "lists every stored item, one per line.", "lists every stored item, one per line, sorted.", 1), 0644)
	edit(t, claimFile(root, "item-added"), "prose: "+prose, "prose: 2948a95bd310")
	check := verilex(t, root, nil, "check").stdout
	contains(t, check, "check: 1 of 3 admitted drift-suspect; they always run\n  item-listed: claim item-listed needs review: verify-tally/features/items.md#item-list: prose changed")
}

// Rule: a requirement sentence counts as mapped only when a claim that a word proves maps it. A
// claim no word proves (or only a word pinned to an older version) covers nothing, so it can
// never clear another claim's review.
func TestClaimWithoutWordCoversNothing(t *testing.T) {
	root := curated(t)
	green(t, root, nil, chain)
	items := feature(root, "items.md")
	twice := "Expect exit 2 and `exists NAME` when NAME is already stored."
	edit(t, items, "`store.json` lists NAME.\n", "`store.json` lists NAME. "+twice+"\n")
	review := "claim item-added needs review: " + itemAdd + ": requirement no claim maps: " + twice
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 3 admitted drift-suspect; they always run\n  item-stored: "+review+"\n")

	ghost := "claim: item-kept-once\nsentence: A name is stored at most once.\nentry: [cli]\nevidence:\n  action: \"`tally add NAME` twice exits 2 the second time.\"\n  observation: store.json lists NAME once.\n" +
		"sources:\n  - ref: " + itemAdd + "\n    prose: 2948a95bd310\n    requirements: [\"" + twice + "\"]\n"
	write(t, claimFile(root, "item-kept-once"), ghost, 0644)
	pin := pinOf(t, root, "item-kept-once")
	contains(t, verilex(t, root, nil, "claims").stdout, pin+"  A name is stored at most once.\n  entry: cli  words: -\n")
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 3 admitted drift-suspect; they always run\n  item-stored: "+review+"\n")
	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; item-stored apple: drift-suspect: "+review+"\n")

	// Once a word proves the claim, the sentence is covered.
	dir := filepath.Join(root, ".verilex", "words", "item-added-twice")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "word.md"), "---\nword: item-added-twice\npromise: A name added twice is stored once.\nclaim: "+pin+"\nentry: cli\ninputs: [bin/tally]\n---\n", 0644)
	write(t, filepath.Join(dir, "run"), read(t, filepath.Join(root, ".verilex", "words", "item-stored", "run")), 0755)
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (3 admitted)\n")

	// A word pinned to an older version proves nothing now.
	edit(t, claimFile(root, "item-kept-once"), "A name is stored at most once.", "A name is stored once at most.")
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 3 admitted drift-suspect; they always run\n  item-stored: "+review+"\n")
}

// Rule: a claim pins the order of reality for every word that proves it. A variant word that
// declares no states of its own still cannot run before the states its claim requires.
func TestChainBreakingAPinnedRuleIsRefusedBeforeLaunch(t *testing.T) {
	root := product(t)
	pin := pinOf(t, root, "item-added")
	dir := filepath.Join(root, ".verilex", "words", "item-put")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	contract := "---\nword: item-put\npromise: A user puts a named item in the store.\nargs: [name]\nclaim: " + pin + "\nentry: cli\ninputs: [bin/tally]\n---\n"
	write(t, filepath.Join(dir, "word.md"), contract, 0644)
	write(t, filepath.Join(dir, "run"), read(t, filepath.Join(root, ".verilex", "words", "item-stored", "run")), 0755)

	done := verilex(t, root, nil, "run", "item-put apple | store-open")
	equal(t, done.code, 2)
	equal(t, done.stderr, "verilex: refused: item-put apple requires store, pinned by claim item-added; nothing earlier provides it\n")
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "state")); !os.IsNotExist(err) {
		t.Fatalf("a refused chain recorded state: %v", err)
	}
	equal(t, stores(t, root), []string{})
	equal(t, plan(t, root, "item-put apple | store-open").code, 2)

	// The claim provides item:{name} for the variant too, so a chain may list the item after it.
	record := green(t, root, nil, "store-open | item-put apple | item-listed apple")
	equal(t, record.Words[1].Proves, pin)
	equal(t, record.Words[1].Provides, []string{"item:apple"})
	contains(t, verilex(t, root, nil, "claims").stdout, pin+"  A named item is in the store.\n  entry: cli  words: item-put, item-stored\n")

	for change, refusal := range map[[2]string]string{
		{"entry: cli", "entry: api"}:                                "'entry' must name the entry point the word exercises, one of claim item-added's: cli",
		{"args: [name]", "args: [item]"}:                            "claim item-added uses {name}, so the word must take arg 'name'",
		{"entry: cli", "entry: cli\nread_only: true"}:               "a 'read_only' word changes nothing, so it cannot prove claim item-added, which provides states",
		{"claim: " + pin, "claim: item-sold@" + pin[11:]}:           "no claim 'item-sold' in .verilex/claims",
		{"claim: " + pin, "claim: item-added"}:                      "'claim' must pin a claim version, <claim>@<version> as `verilex claims` prints it",
		{"entry: cli", "entry: cli\nimplements: [" + itemAdd + "]"}: "a word that proves a claim takes its feature-map sources from the claim; drop 'implements'",
	} {
		write(t, filepath.Join(dir, "word.md"), strings.Replace(contract, change[0], change[1], 1), 0644)
		done := verilex(t, root, nil, "words")
		equal(t, done.code, 2)
		contains(t, done.stderr, refusal)
	}
}
