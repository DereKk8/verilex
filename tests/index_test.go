package tests

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DereKk8/verilex/internal/index"
)

const tier1 = "index tally: 3 active claim(s); `verilex index <claim>` lists a claim's words\n" +
	"item-added  A named item is in the store.\n" +
	"item-listed  A stored item shows up when a user lists the store.\n" +
	"store-opened  A new, empty store is open and ready for items.\n"

func firstLines(text string, n int) string {
	lines := strings.SplitAfter(text, "\n")
	return strings.Join(lines[:min(n, len(lines))], "")
}

// Done-when (PER-231): tier 1 is unchanged in size after adding many dormant variants. Agents pay
// for the product's active claims; words the product never uses stay dormant and out of tiers 1
// and 2, however many there are.
func TestFirstTierUnchangedByDormantVariants(t *testing.T) {
	root := curated(t)
	equal(t, verilex(t, root, nil, "index").stdout, tier1)
	v := pinOf(t, root, "item-added")
	second := verilex(t, root, nil, "index", "item-added").stdout
	equal(t, second, v+"  A named item is in the store.\n  entry: cli  requires: store  provides: item:{name}\n  item-stored <name>\n")
	asJSON := verilex(t, root, nil, "index", "--json").stdout

	// Variants never onboarded, claims and words nobody onboarded, and words onboarded only for
	// another product.
	for i := range 30 {
		variant(t, root, fmt.Sprintf("item-v%02d", i), v, stub)
	}
	colors := []string{"amber", "beige", "coral", "denim", "ebony", "fawn", "garnet", "hazel", "indigo", "jade"}
	for _, color := range colors {
		pin := claimLike(t, root, "thing-"+color, [2]string{"sentence: A named item is in the store.", "sentence: A " + color + " thing is counted."})
		variant(t, root, "count-"+color, pin, stub)
	}
	admittedFor(t, root, "elsewhere", "item-v00", "item-v01", "count-amber")
	equal(t, strings.Count(verilex(t, root, nil, "words").stdout, "  status:   "), 3+30+10)

	equal(t, verilex(t, root, nil, "index").stdout, tier1)
	equal(t, verilex(t, root, nil, "index", "--json").stdout, asJSON)
	equal(t, verilex(t, root, nil, "index", "item-added").stdout, second)

	// A dormant claim is still found by intent, and its tier 2 lists its dormant words.
	equal(t, firstLines(verilex(t, root, nil, "index", "--intent", "a jade thing gets counted").stdout, 2), "intent: 1 claim(s) for 'a jade thing gets counted'\nthing-jade  A jade thing is counted.  (dormant)\n")
	contains(t, verilex(t, root, nil, "index", "thing-amber").stdout, "\n  count-amber <name>  (dormant)\n")
	contains(t, verilex(t, root, nil, "index", "thing-jade").stdout, "\n  count-jade <name>  (provisional)\n")
}

// Per-product active set: a vocabulary shared by two products is active for each only where it
// was used, and an alias whose words a product never used is pruned from that product's index.
func TestWordsAProductNeverUsedStayDormantForIt(t *testing.T) {
	root := baseline(t)
	v := pinOf(t, root, "item-added")
	alias := claimLike(t, root, "item-put-done", [2]string{"    - \"`added NAME` alone", "    - \"exit 0 alone does not prove the item was stored.\"\n    - \"`added NAME` alone"})
	variant(t, root, "item-put", alias, storedRun(t, root))
	onboard(t, root, "item-put")
	both := v + "  A named item is in the store.\n  entry: cli  requires: store  provides: item:{name}\n  item-put <name>  (via item-put-done)\n  item-stored <name>\n"
	equal(t, verilex(t, root, nil, "index", "item-added").stdout, both)
	equal(t, verilex(t, root, nil, "index", "item-put-done").stdout, both)

	config := filepath.Join(root, ".verilex", "config.yaml")
	edit(t, config, "project: tally", "project: tally-mobile")
	equal(t, verilex(t, root, nil, "index").stdout, "index tally-mobile: no active claims; find one with `verilex index --intent '<what to prove>'`\n")
	equal(t, firstLines(verilex(t, root, nil, "index", "--intent", "a named item is in the store").stdout, 2), "intent: 1 claim(s) for 'a named item is in the store'\nitem-added  A named item is in the store.  (dormant)\n")

	used(t, root, "store-open | item-stored apple")
	used(t, root, "store-open | item-stored pear")
	equal(t, onboard(t, root, "item-stored").stdout, "onboarded item-stored for tally-mobile: it proves "+v+", and its correctness gate already holds\n")
	equal(t, slices.Sorted(func(yield func(string) bool) {
		for product := range decision(t, root, "item-stored").Uses {
			yield(product)
		}
	}), []string{"tally", "tally-mobile"})
	equal(t, verilex(t, root, nil, "index").stdout, "index tally-mobile: 1 active claim(s); `verilex index <claim>` lists a claim's words\nitem-added  A named item is in the store.\n")
	equal(t, verilex(t, root, nil, "index", "item-added").stdout, v+"  A named item is in the store.\n  entry: cli  requires: store  provides: item:{name}\n  item-stored <name>\n")

	edit(t, config, "project: tally-mobile", "project: tally")
	equal(t, verilex(t, root, nil, "index").stdout, tier1)
	equal(t, verilex(t, root, nil, "index", "item-added").stdout, both)
}

// Done-when (PER-231): the intent lookup returns the right claim.
func TestIntentLookupReturnsTheRightClaim(t *testing.T) {
	root := curated(t)
	for intent, claim := range map[string]string{
		"prove a stored item shows up when the store is listed": "item-listed  A stored item shows up when a user lists the store.\n",
		"make sure an item gets added to the store":             "item-added  A named item is in the store.\n",
		"open a new empty store":                                "store-opened  A new, empty store is open and ready for items.\n",
		"item-add":                                              "item-added  A named item is in the store.\n",
	} {
		lines := strings.SplitAfter(verilex(t, root, nil, "index", "--intent", intent).stdout, "\n")
		equal(t, lines[1], claim)
	}
	equal(t, verilex(t, root, nil, "index", "--intent", "prove tenants still provision").stdout, "intent: 0 claim(s) for 'prove tenants still provision'\n")
	var found []index.Claim
	if err := json.Unmarshal([]byte(verilex(t, root, nil, "index", "--intent", "items are listed", "--json").stdout), &found); err != nil {
		t.Fatal(err)
	}
	equal(t, found[0].Claim, "item-listed")
	equal(t, found[0].Words, []index.Word{{Word: "item-listed", Args: []string{"name"}, Entry: "cli", State: index.Active}})
	done := verilex(t, root, nil, "index", "--intent", "x", "item-added")
	equal(t, done.code, 2)
	contains(t, done.stderr, "look up one thing at a time")
}

// changed runs the change lookup and checks every chain it reports against `verilex plan`.
func changed(t *testing.T, root string, files ...string) index.Change {
	t.Helper()
	args := []string{"index", "--json"}
	for _, file := range files {
		args = append(args, "--changed", file)
	}
	var change index.Change
	if err := json.Unmarshal([]byte(verilex(t, root, nil, args...).stdout), &change); err != nil {
		t.Fatal(err)
	}
	for _, c := range change.Chains {
		done := plan(t, root, c.Chain)
		first := strings.SplitN(done.stdout, "\n", 2)[0]
		switch {
		case c.Refused:
			equal(t, done.code, 2)
			equal(t, done.stderr, "verilex: refused: "+c.Reason+"\n")
		case c.Run:
			equal(t, done.code, 0)
			equal(t, strings.HasPrefix(first, "plan: skip 0, run "), true)
			var p struct{ Rerun string }
			if err := json.Unmarshal([]byte(plan(t, root, c.Chain, "--json").stdout), &p); err != nil {
				t.Fatal(err)
			}
			equal(t, p.Rerun, c.Reason)
		default:
			equal(t, done.code, 0)
			equal(t, first, fmt.Sprintf("plan: skip %d, run 0", strings.Count(c.Chain, "|")+1))
			contains(t, done.stdout, "relies on run "+c.ReliesOn+"\n")
		}
	}
	return change
}

func claimsOf(change index.Change) []string {
	names := []string{}
	for _, c := range change.Claims {
		names = append(names, c.Claim)
	}
	return names
}

// Done-when (PER-231): the change lookup returns the claims whose dependencies cover the touched
// files, and agrees with `plan` on what must re-run.
func TestChangeLookupAgreesWithPlan(t *testing.T) {
	root := curated(t)
	first := green(t, root, nil, chain)
	equal(t, green(t, root, nil, "store-open | item-stored apple").Skipped, true)

	change := changed(t, root, "README.md")
	equal(t, claimsOf(change), []string{})
	equal(t, len(change.Chains), 0)
	equal(t, verilex(t, root, nil, "index", "--changed", "README.md").stdout, "changed: 0 claim(s); 0 of 0 known chain(s) must re-run\n")

	// Every word reads bin/tally. Touched but unchanged, it re-runs nothing plan would skip.
	change = changed(t, root, "bin/tally")
	equal(t, claimsOf(change), []string{"item-added", "item-listed", "store-opened"})
	equal(t, change.Chains, []index.Rerun{
		{Chain: "store-open | item-stored apple", ReliesOn: first.Run},
		{Chain: chain, ReliesOn: first.Run},
	})

	appendTo(t, filepath.Join(root, ".verilex", "words", "item-listed", "run"), "\n")
	change = changed(t, root, ".verilex/words/item-listed/run")
	equal(t, claimsOf(change), []string{"item-listed"})
	equal(t, change.Chains, []index.Rerun{{Chain: chain, Run: true, Reason: "item-listed apple: word changed"}})
	equal(t, verilex(t, root, nil, "index", "--changed", ".verilex/words/item-listed/run").stdout, "changed: 1 claim(s); 1 of 1 known chain(s) must re-run\nitem-listed  A stored item shows up when a user lists the store.\n  run  "+chain+": item-listed apple: word changed\n")

	// A changed requirement in the verify skill holds the claim's words; a new claim version
	// refuses their chains, in the lookup as in plan.
	changeAddRequirement(t, root)
	change = changed(t, root, ".cursor/skills/verify-tally/features/items.md")
	equal(t, claimsOf(change), []string{"item-added", "item-listed"})
	equal(t, change.Chains[0], index.Rerun{Chain: "store-open | item-stored apple", Run: true, Reason: "item-stored apple: drift-suspect: " + reviewAdd})
	edit(t, claimFile(root, "item-added"), "observation: store.json lists NAME.", "observation: store.json lists NAME once.")
	change = changed(t, root, ".verilex/claims/item-added.yaml")
	equal(t, claimsOf(change), []string{"item-added"})
	for _, c := range change.Chains {
		equal(t, c.Refused, true)
	}
}

// copyTree copies a product into dir, writing its files in reverse order at another time.
// The change lookup follows a word grouped under another claim to that claim's file: an edit to
// the grouped claim makes plan re-run the alias word's chains, through a planted defect or a new
// version, so the lookup lists them too.
func TestChangeLookupFollowsAliasWordsToTheirGroupedClaim(t *testing.T) {
	root := baseline(t)
	alias := claimLike(t, root, "item-put-done", [2]string{"    - \"`added NAME` alone", "    - \"exit 0 alone does not prove the item was stored.\"\n    - \"`added NAME` alone"})
	variant(t, root, "item-put", alias, storedRun(t, root))
	onboard(t, root, "item-put")
	used(t, root, "store-open | item-put pear")
	equal(t, strings.SplitN(plan(t, root, "store-open | item-put pear").stdout, "\n", 2)[0], "plan: skip 2, run 0")
	original := read(t, claimFile(root, "item-added"))
	reruns := func() []string {
		names := []string{}
		for _, c := range changed(t, root, claimFile(root, "item-added")).Chains {
			if c.Run {
				names = append(names, c.Chain)
			}
		}
		return names
	}
	edit(t, claimFile(root, "item-added"), "  dropped-add: {TALLY_DEFECT: drop-adds}\n", "  dropped-add: {TALLY_DEFECT: drop-adds}\n  lost-add: {TALLY_DEFECT: drop-adds}\n")
	contains(t, strings.Join(reruns(), "\n"), "store-open | item-put pear")
	write(t, claimFile(root, "item-added"), original, 0644)
	edit(t, claimFile(root, "item-added"), "sentence: A named item is in the store.", "sentence: A named item is kept in the store.")
	contains(t, strings.Join(reruns(), "\n"), "store-open | item-put pear")
}

// A word re-pinned to another claim after onboarding is not onboarded for that claim, so the index
// lists it there as provisional and the claim does not become active.
func TestRepinnedWordIsNotActiveUnderItsNewClaim(t *testing.T) {
	root := baseline(t)
	noted := claimLike(t, root, "item-noted", [2]string{"sentence: A named item is in the store.", "sentence: A named item is noted in the store."})
	tier1 := verilex(t, root, nil, "index").stdout
	edit(t, filepath.Join(root, ".verilex", "words", "item-stored", "word.md"), "claim: "+pinOf(t, root, "item-added"), "claim: "+noted)
	equal(t, verilex(t, root, nil, "index").stdout, strings.NewReplacer("3 active", "2 active", "item-added  A named item is in the store.\n", "").Replace(tier1))
	equal(t, verilex(t, root, nil, "index", "item-noted").stdout, noted+"  A named item is noted in the store.\n  entry: cli  requires: store  provides: item:{name}\n  item-stored <name>  (provisional)\n")
}

func copyTree(t *testing.T, from, dir string) {
	t.Helper()
	paths := []string{}
	err := filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err == nil {
			paths = append(paths, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(paths)
	for _, path := range paths {
		rel, _ := filepath.Rel(from, path)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(dir, rel)
		if info.IsDir() {
			if err = os.MkdirAll(dest, info.Mode().Perm()); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			t.Fatal(err)
		}
		write(t, dest, read(t, path), info.Mode().Perm())
	}
}

// Done-when (PER-231): regenerating the index from the records is deterministic. The index is
// generated on every call from the claim files, the word contracts and the grouping file; the
// same records give the same bytes, whatever the run history, file order or file times.
func TestIndexRegenerationIsDeterministic(t *testing.T) {
	root := baseline(t)
	for _, color := range []string{"amber", "beige", "coral"} {
		pin := claimLike(t, root, "thing-"+color, [2]string{"sentence: A named item is in the store.", "sentence: A " + color + " thing is counted."})
		variant(t, root, "count-"+color, pin, stub)
	}
	lookups := [][]string{
		{"index"}, {"index", "--json"}, {"index", "item-added"}, {"index", "item-added", "--json"},
		{"index", "item-added", "item-stored"}, {"index", "item-added", "item-stored", "--json"},
		{"index", "--intent", "a thing is counted"}, {"index", "--intent", "an item is added", "--json"},
	}
	generate := func(root string) []string {
		result := []string{}
		for _, args := range lookups {
			done := verilex(t, root, nil, args...)
			equal(t, done.code, 0)
			result = append(result, done.stdout)
		}
		return result
	}
	before := generate(root)
	equal(t, before[4], "item-stored <name>  proves "+pinOf(t, root, "item-added")+" through cli; admitted\n  requires: store  provides: item:{name}\n  inputs: bin/tally  env: TALLY_DEFECT, TALLY_SIMULATE_LOCK  timeout: 1800s\n  proven on: store-open | item-stored apple\n")
	equal(t, generate(root), before)
	used(t, root, "store-open | item-stored fig")
	equal(t, generate(root), before)

	elsewhere := filepath.Join(t.TempDir(), "tally")
	copyTree(t, root, elsewhere)
	equal(t, generate(elsewhere), before)
}
