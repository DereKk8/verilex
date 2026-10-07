package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/grouping"
	"github.com/DereKk8/verilex/internal/onboarding"
)

// decision reads a word's onboarding decision from the product's grouping file.
func decision(t *testing.T, root, word string) grouping.Decision {
	t.Helper()
	project, err := dictionary.FindProject(root)
	if err != nil {
		t.Fatal(err)
	}
	g, err := grouping.Load(project)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := g.Sound(word)
	if !ok {
		t.Fatalf("%s has no sound decision in %v", word, g.Words)
	}
	return d
}

func groupingFile(root string) string { return filepath.Join(root, ".verilex", "grouping.yaml") }

// baseline onboards tally's three words for real: two uses of the default chain, then onboarding.
func baseline(t *testing.T) string {
	t.Helper()
	root := product(t)
	used(t, root, chain)
	used(t, root, chain)
	for _, word := range []string{"store-open", "item-stored", "item-listed"} {
		onboard(t, root, word)
	}
	return root
}

// claimLike writes a claim that copies item-added under another name, with edits, and returns
// its pin.
func claimLike(t *testing.T, root, name string, edits ...[2]string) string {
	t.Helper()
	text := strings.Replace(read(t, claimFile(root, "item-added")), "claim: item-added\n", "claim: "+name+"\n", 1)
	for _, e := range edits {
		if !strings.Contains(text, e[0]) {
			t.Fatalf("item-added.yaml lacks %q", e[0])
		}
		text = strings.Replace(text, e[0], e[1], 1)
	}
	write(t, claimFile(root, name), text, 0644)
	return pinOf(t, root, name)
}

// pyWord is a tally word's run: the shared helpers, the item name in `name`, then body.
func pyWord(body string) string {
	return "#!/usr/bin/env python3\nimport sys\nfrom pathlib import Path\n\nsys.dont_write_bytecode = True\nsys.path.insert(0, str(Path(__file__).resolve().parent.parent))\nfrom tally_word import result, stored_items, tally\n\nname = sys.argv[1]\n" + body + "\n"
}

// variant writes a word that pins a claim version and uses it in two runs, so it can be onboarded.
func variant(t *testing.T, root, word, pin, run string) {
	t.Helper()
	dir := filepath.Join(root, ".verilex", "words", word)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "word.md"), "---\nword: "+word+"\npromise: A named item is in the store.\nargs: [name]\nclaim: "+pin+"\nentry: cli\ninputs: [bin/tally]\nenv: [TALLY_DEFECT]\n---\n", 0644)
	write(t, filepath.Join(dir, "run"), run, 0755)
	if run != stub {
		used(t, root, "store-open | "+word+" apple")
		used(t, root, "store-open | "+word+" pear")
	}
}

// stub is a run for a word that is never used.
const stub = "#!/bin/sh\nexit 2\n"

func storedRun(t *testing.T, root string) string {
	return read(t, filepath.Join(root, ".verilex", "words", "item-stored", "run"))
}

// result reads the onboarding record a run of `verilex onboard` printed.
func result(t *testing.T, done output) onboarding.Result {
	t.Helper()
	m := regexp.MustCompile(`(?m)^  record: (\S+)$`).FindStringSubmatch(done.stdout)
	if m == nil {
		t.Fatalf("no record in %q", done.stdout)
	}
	var r onboarding.Result
	if err := json.Unmarshal([]byte(read(t, filepath.Join(m[1], "record.json"))), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// trials lists each trial as "<state> <word> <verdict>".
func trialsOf(r onboarding.Result) []string {
	result := []string{}
	for _, trial := range r.Trials {
		result = append(result, trial.State+" "+trial.Word+" "+trial.Verdict)
	}
	return result
}

// Done-when (PER-230): a variant is grouped by the mechanical match, with no model call. Its claim
// says the same as a grouped claim in its own file, so it becomes an alias of that claim, after
// the behavioral check shows the word and the claim's words agree on every state.
func TestVariantGroupedByMechanicalMatchWithoutModelCall(t *testing.T) {
	root := baseline(t)
	v := pinOf(t, root, "item-added")
	equal(t, decision(t, root, "item-stored").Match, grouping.New)

	alias := claimLike(t, root, "item-put-done", [2]string{"    - \"`added NAME` alone", "    - \"exit 0 alone does not prove the item was stored.\"\n    - \"`added NAME` alone"})
	if strings.TrimPrefix(alias, "item-put-done@") == strings.TrimPrefix(v, "item-added@") {
		t.Fatal("the alias claim should be another claim version")
	}
	variant(t, root, "item-put", alias, storedRun(t, root))
	before := read(t, groupingFile(root))
	done := onboard(t, root, "item-put")
	equal(t, strings.SplitN(done.stdout, "\n", 2)[0], "onboarded item-put: variant of "+v+" through claim item-put-done, matched mechanically; caught dropped-add")
	d := decision(t, root, "item-put")
	equal(t, d.Claim, v)
	equal(t, d.Proves, alias)
	equal(t, d.Match, grouping.Mechanical)
	equal(t, d.Chain, "store-open | item-put pear")
	equal(t, len(d.Uses["tally"]), 2)
	equal(t, d.Defects, map[string]string{"dropped-add": decision(t, root, "item-stored").Defects["dropped-add"]})
	r := result(t, done)
	equal(t, len(r.Candidates), 1)
	equal(t, r.Candidates[0].Kind, "match")
	equal(t, r.Candidates[0].Words, []string{"item-stored"})
	equal(t, trialsOf(r), []string{"healthy item-put green", "dropped-add item-put red", "healthy item-stored green", "dropped-add item-stored red"})
	equal(t, d.Pending, []grouping.Pending(nil))
	if read(t, groupingFile(root)) == before {
		t.Fatal("onboarding did not record its decision")
	}

	// A word that pins the grouped claim itself needs no match at all.
	variant(t, root, "item-filed", v, storedRun(t, root))
	equal(t, strings.SplitN(onboard(t, root, "item-filed").stdout, "\n", 2)[0], "onboarded item-filed: variant of "+v+"; caught dropped-add")
	equal(t, decision(t, root, "item-filed").Match, grouping.Same)
	equal(t, decision(t, root, "item-filed").Pending, []grouping.Pending(nil))
	equal(t, verilex(t, root, nil, "index", "item-added").stdout, v+"  A named item is in the store.\n  entry: cli  requires: store  provides: item:{name}\n  item-filed <name>\n  item-put <name>  (via item-put-done)\n  item-stored <name>\n")
	equal(t, status(t, root, "item-put"), "admitted")
	equal(t, d.GroupDefects, map[string]string{"dropped-add": d.Defects["dropped-add"]})

	// A planted defect the grouped claim declares later holds the alias word too, which was
	// never compared under it. Onboarding it again, with no grouped word left to compare, holds
	// it to what the grouped claim's gate demands.
	edit(t, claimFile(root, "item-added"), "  dropped-add: {TALLY_DEFECT: drop-adds}\n", "  dropped-add: {TALLY_DEFECT: drop-adds}\n  lost-add: {TALLY_DEFECT: drop-adds}\n")
	equal(t, verilex(t, root, nil, "check").stdout, "check: 3 of 5 admitted drift-suspect; they always run\n"+
		"  item-filed: claim item-added declares planted defect lost-add, which item-filed was never gated against\n"+
		"  item-put: grouped claim item-added declares planted defect lost-add, which item-put was never compared under\n"+
		"  item-stored: claim item-added declares planted defect lost-add, which item-stored was never gated against\n")
	done = onboard(t, root, "item-put")
	equal(t, strings.SplitN(done.stdout, "\n", 2)[0], "onboarded item-put: variant of "+v+" through claim item-put-done, matched mechanically; caught dropped-add")
	equal(t, result(t, done).Candidates[0].Words, []string(nil))
	equal(t, trialsOf(result(t, done)), []string{"healthy item-put green", "dropped-add item-put red", "lost-add item-put red"})
	equal(t, status(t, root, "item-put"), "admitted")
}

// Done-when (PER-230): a behavioral disagreement is reported as a finding. The proposal's claim
// reads like item-added, and its word passes its own claim's gate, but it behaves unlike
// item-added's word under each claim's planted defect: one of them is wrong, or the claims differ.
func TestBehavioralDisagreementIsReportedAsFinding(t *testing.T) {
	root := baseline(t)
	noted := claimLike(t, root, "item-noted", [2]string{"dropped-add: {TALLY_DEFECT: drop-adds}", "muted-add: {TALLY_DEFECT: mute-adds}"})
	variant(t, root, "item-logged", noted, pyWord(`added = tally("add", name)
if added.stdout.strip() != f"added {name}":
    result("fail", preconditions_held=True, detail=f"tally add printed {added.stdout.strip()!r}")
result("pass", observation=f"tally confirmed the add of {name}")`))
	before := read(t, groupingFile(root))
	done := verilex(t, root, nil, "onboard", "item-logged")
	equal(t, done.code, 1)
	contains(t, done.stdout, "rejected item-logged: item-logged behaves differently from the words of claim item-added: one of them is wrong, or the claims differ\n")
	contains(t, done.stdout, "  finding  on planted defect dropped-add, store-open | item-logged pear is green and store-open | item-stored pear (claim item-added) is red\n")
	contains(t, done.stdout, "  finding  on planted defect muted-add, store-open | item-logged pear is red and store-open | item-stored pear (claim item-added) is green\n")
	r := result(t, done)
	equal(t, len(r.Findings), 2)
	for _, finding := range r.Findings {
		for _, evidence := range finding.Evidence {
			if _, err := os.Stat(filepath.Join(evidence, "stdout")); err != nil {
				t.Fatalf("finding evidence %s: %v", evidence, err)
			}
		}
	}
	equal(t, read(t, groupingFile(root)), before)
	equal(t, status(t, root, "item-logged"), "provisional")

	// With no grouped word left to compare, the word is held to what item-added's gate demands.
	appendTo(t, filepath.Join(root, ".verilex", "words", "item-stored", "word.md"), "\n")
	done = verilex(t, root, nil, "onboard", "item-logged")
	equal(t, done.code, 1)
	contains(t, done.stdout, "  finding  on planted defect dropped-add, store-open | item-logged pear is green, and claim item-added requires red there; none of its words could be compared\n")
	equal(t, len(result(t, done).Findings), 1)
	equal(t, read(t, groupingFile(root)), before)
}

// Done-when (PER-230): an ambiguous proposal goes back to the calling agent; verilex itself calls
// no model. Its claim takes the same states and evidence as item-added in other words, and its
// word behaves like item-added's on every state, so only the agent can tell. Until it decides,
// the word joins as a claim of its own.
func TestAmbiguousProposalGoesBackToTheAgent(t *testing.T) {
	root := baseline(t)
	v := pinOf(t, root, "item-added")
	kept := claimLike(t, root, "item-kept", [2]string{"sentence: A named item is in the store.", "sentence: A user's named item is kept in the store."})
	variant(t, root, "item-keep", kept, storedRun(t, root))
	done := onboard(t, root, "item-keep")
	equal(t, done.stdout, "undecided item-keep: claim "+kept+" joins the vocabulary as a claim of its own for now; caught dropped-add\n"+
		"  decide: does claim item-kept say the same as "+v+" (compared: item-stored)? item-keep behaved like them on every trial state.\n"+
		"    verilex onboard item-keep --same-as item-added\n    verilex onboard item-keep --distinct\n"+
		"  record: "+result(t, done).Record+"\n")
	r := result(t, done)
	equal(t, r.Outcome, onboarding.Undecided)
	equal(t, r.Request.Proposal.Claim, kept)
	equal(t, len(r.Request.Candidates), 1)
	equal(t, r.Request.Candidates[0].Claim, v)
	contains(t, r.Request.Candidates[0].Text, "sentence: A named item is in the store.")
	equal(t, trialsOf(r), []string{"healthy item-keep green", "dropped-add item-keep red", "healthy item-stored green", "dropped-add item-stored red"})
	d := decision(t, root, "item-keep")
	equal(t, d.Claim, kept)
	equal(t, d.Match, grouping.New)
	equal(t, d.Pending, []grouping.Pending{{Claim: v, Words: []string{"item-stored"}, Defects: map[string]string{"dropped-add": decision(t, root, "item-stored").Defects["dropped-add"]}}})
	equal(t, status(t, root, "item-keep"), "admitted")
	equal(t, verilex(t, root, nil, "index", "item-kept").stdout, kept+"  A user's named item is kept in the store.\n  entry: cli  requires: store  provides: item:{name}\n  item-keep <name>\n")
	contains(t, verilex(t, root, nil, "onboard", "item-keep").stdout, "onboarded item-keep already: it proves "+kept+" for tally, and nothing it was judged on changed\n  decide: does claim item-kept say the same as ")

	// A choice the question does not offer is refused, and the agent's answer groups the word.
	refused := verilex(t, root, nil, "onboard", "item-keep", "--same-as", "item-listed")
	equal(t, refused.code, 2)
	equal(t, refused.stderr, "verilex: refused: item-keep's claim may say the same only as item-added, not item-listed\n")
	equal(t, onboard(t, root, "item-keep", "--same-as", "item-added").stdout, "onboarded item-keep: variant of "+v+" through claim item-kept, as the agent decided\n")
	d = decision(t, root, "item-keep")
	equal(t, d.Claim, v)
	equal(t, d.Proves, kept)
	equal(t, d.Match, grouping.Agent)
	equal(t, d.Pending, []grouping.Pending(nil))
	equal(t, d.GroupDefects, map[string]string{"dropped-add": decision(t, root, "item-stored").Defects["dropped-add"]})
	contains(t, verilex(t, root, nil, "index", "item-added").stdout, "  item-keep <name>  (via item-kept)\n")
	refused = verilex(t, root, nil, "onboard", "item-keep", "--distinct")
	equal(t, refused.code, 2)
	contains(t, refused.stderr, "item-keep has no open grouping question: it is grouped under "+v)

	// Another undecided word the agent keeps apart stays a claim of its own.
	held := claimLike(t, root, "item-held", [2]string{"sentence: A named item is in the store.", "sentence: The store holds a user's named item."})
	variant(t, root, "item-hold", held, storedRun(t, root))
	equal(t, strings.SplitN(onboard(t, root, "item-hold").stdout, "\n", 2)[0], "undecided item-hold: claim "+held+" joins the vocabulary as a claim of its own for now; caught dropped-add")
	equal(t, onboard(t, root, "item-hold", "--distinct").stdout, "onboarded item-hold: claim "+held+" stays a claim of its own, as the agent decided\n")
	d = decision(t, root, "item-hold")
	equal(t, d.Claim, held)
	equal(t, d.Match, grouping.New)
	equal(t, d.Pending, []grouping.Pending(nil))
}

// Every word of an undecided claim waits for the same answer: a second word that pins the claim
// is compared with the pending claim's words too and carries the question, identical words get
// identical decisions, the index reads the same on every call, and the agent's one answer moves
// all of them, so the claim is never a group and an alias at once.
func TestEveryWordOfAnUndecidedClaimWaitsForTheAgent(t *testing.T) {
	root := baseline(t)
	v := pinOf(t, root, "item-added")
	held := claimLike(t, root, "item-held", [2]string{"sentence: A named item is in the store.", "sentence: A user's named item is held in the store."})
	for _, word := range []string{"item-hold", "item-hold2", "item-hold3", "item-hold4"} {
		variant(t, root, word, held, storedRun(t, root))
	}
	equal(t, strings.SplitN(onboard(t, root, "item-hold").stdout, "\n", 2)[0], "undecided item-hold: claim "+held+" joins the vocabulary as a claim of its own for now; caught dropped-add")
	second := onboard(t, root, "item-hold2")
	equal(t, second.stdout, "undecided item-hold2: variant of "+held+", which stays a claim of its own for now; caught dropped-add\n"+
		"  decide: does claim item-held say the same as "+v+" (compared: item-stored)? item-hold2 behaved like them on every trial state.\n"+
		"    verilex onboard item-hold2 --same-as item-added\n    verilex onboard item-hold2 --distinct\n"+
		"  record: "+result(t, second).Record+"\n")
	equal(t, trialsOf(result(t, second)), []string{"healthy item-hold2 green", "dropped-add item-hold2 red", "healthy item-hold green", "dropped-add item-hold red", "healthy item-stored green", "dropped-add item-stored red"})
	pending := []grouping.Pending{{Claim: v, Words: []string{"item-stored"}, Defects: map[string]string{"dropped-add": decision(t, root, "item-stored").Defects["dropped-add"]}}}
	for _, word := range []string{"item-hold3", "item-hold4"} {
		onboard(t, root, word)
	}
	for _, word := range []string{"item-hold2", "item-hold3", "item-hold4"} {
		d := decision(t, root, word)
		equal(t, []string{d.Claim, d.Proves, d.Match}, []string{held, "", grouping.Same})
		equal(t, d.Pending, pending)
	}
	tier1, tier2 := verilex(t, root, nil, "index").stdout, verilex(t, root, nil, "index", "item-held").stdout
	contains(t, tier1, "\nitem-held  A user's named item is held in the store.\n")
	for range 20 {
		equal(t, verilex(t, root, nil, "index").stdout, tier1)
		equal(t, verilex(t, root, nil, "index", "item-held").stdout, tier2)
	}

	// A word whose files changed is onboarded again into the same open question.
	write(t, filepath.Join(root, ".verilex", "words", "item-hold3", "run"), storedRun(t, root)+"# changed\n", 0755)
	used(t, root, "store-open | item-hold3 fig")
	used(t, root, "store-open | item-hold3 kiwi")
	equal(t, strings.SplitN(onboard(t, root, "item-hold3").stdout, "\n", 2)[0], "undecided item-hold3: variant of "+held+", which stays a claim of its own for now; caught dropped-add")
	equal(t, decision(t, root, "item-hold3").Pending, pending)

	// One answer moves every word of the claim.
	equal(t, onboard(t, root, "item-hold", "--same-as", "item-added").stdout, "onboarded item-hold: variant of "+v+" through claim item-held, as the agent decided; item-hold2, item-hold3, item-hold4 moved with it\n")
	for _, word := range []string{"item-hold", "item-hold2", "item-hold3", "item-hold4"} {
		d := decision(t, root, word)
		equal(t, []string{d.Claim, d.Proves, d.Match}, []string{v, held, grouping.Agent})
		equal(t, d.Pending, []grouping.Pending(nil))
	}
	tier1 = verilex(t, root, nil, "index").stdout
	equal(t, strings.Contains(tier1, "item-held"), false)
	tier2 = verilex(t, root, nil, "index", "item-held").stdout
	contains(t, tier2, v+"  A named item is in the store.\n")
	contains(t, tier2, "  item-hold4 <name>  (via item-held)\n")
	for range 20 {
		equal(t, verilex(t, root, nil, "index").stdout, tier1)
		equal(t, verilex(t, root, nil, "index", "item-held").stdout, tier2)
	}
}

// repin points a word at another claim version and uses it in two new runs.
func repin(t *testing.T, root, word, pin string) {
	t.Helper()
	path := filepath.Join(root, ".verilex", "words", word, "word.md")
	text := regexp.MustCompile(`(?m)^claim: .*$`).ReplaceAllString(read(t, path), "claim: "+pin)
	write(t, path, text, 0644)
	used(t, root, "store-open | "+word+" fig")
	used(t, root, "store-open | "+word+" kiwi")
}

// A new claim version is a new meaning, so the agent's answer for the old version never carries
// over: neither when the alias claim changes nor when the claim it was grouped under changes.
func TestAnswerForAnOldClaimVersionDoesNotCarryOver(t *testing.T) {
	root := baseline(t)
	held := claimLike(t, root, "item-held", [2]string{"sentence: A named item is in the store.", "sentence: A user's named item is held in the store."})
	variant(t, root, "h1", held, storedRun(t, root))
	variant(t, root, "h2", held, storedRun(t, root))
	onboard(t, root, "h1")
	onboard(t, root, "h2")
	contains(t, onboard(t, root, "h1", "--same-as", "item-added").stdout, "h2 moved with it")

	// The alias claim changes its meaning; h2 still holds a decision for the old version.
	edit(t, claimFile(root, "item-held"), "is held in the store.", "is deleted from the store.")
	deleted := pinOf(t, root, "item-held")
	repin(t, root, "h1", deleted)
	equal(t, strings.SplitN(onboard(t, root, "h1").stdout, "\n", 2)[0], "undecided h1: claim "+deleted+" joins the vocabulary as a claim of its own for now; caught dropped-add")
	equal(t, decision(t, root, "h1").Claim, deleted)
	equal(t, verilex(t, root, nil, "index", "item-held").stdout, deleted+"  A user's named item is deleted from the store.\n  entry: cli  requires: store  provides: item:{name}\n  h1 <name>\n")

	// The grouped claim changes its meaning; h2 was grouped under its old version.
	edit(t, claimFile(root, "item-added"), "sentence: A named item is in the store.", "sentence: A named item is removed from the store.")
	repin(t, root, "h2", deleted)
	equal(t, strings.SplitN(onboard(t, root, "h2").stdout, "\n", 2)[0], "onboarded h2: variant of "+deleted+"; caught dropped-add")
	d := decision(t, root, "h2")
	equal(t, []string{d.Claim, d.Proves, d.Match}, []string{deleted, "", grouping.Same})
}

// A decision of a word that no longer exists places nothing: a word that pins the claim it was
// grouped under is matched and asked again.
func TestDecisionOfARemovedWordPlacesNothing(t *testing.T) {
	root := baseline(t)
	gone := claimLike(t, root, "item-gone", [2]string{"sentence: A named item is in the store.", "sentence: A user's named item is still in the store."})
	variant(t, root, "g1", gone, storedRun(t, root))
	onboard(t, root, "g1")
	onboard(t, root, "g1", "--same-as", "item-added")
	if err := os.RemoveAll(filepath.Join(root, ".verilex", "words", "g1")); err != nil {
		t.Fatal(err)
	}
	variant(t, root, "g2", gone, storedRun(t, root))
	equal(t, strings.SplitN(onboard(t, root, "g2").stdout, "\n", 2)[0], "undecided g2: claim "+gone+" joins the vocabulary as a claim of its own for now; caught dropped-add")
}

// Two answers to one open question at once: the grouping file's lock lets one apply, and the
// other is refused as an answer to a closed question, never a crash.
func TestConcurrentAnswersApplyOnce(t *testing.T) {
	root := baseline(t)
	held := claimLike(t, root, "item-held", [2]string{"sentence: A named item is in the store.", "sentence: A user's named item is held in the store."})
	variant(t, root, "item-hold", held, storedRun(t, root))
	variant(t, root, "item-hold2", held, storedRun(t, root))
	onboard(t, root, "item-hold")
	onboard(t, root, "item-hold2")
	open := read(t, groupingFile(root))
	for round := range 8 {
		write(t, groupingFile(root), open, 0644)
		answers := [][]string{{"onboard", "item-hold", "--same-as", "item-added"}, {"onboard", "item-hold2", "--distinct"}}
		if round%2 == 1 {
			answers[1] = []string{"onboard", "item-hold2", "--same-as", "item-added"}
		}
		results := make([]output, len(answers))
		var wg sync.WaitGroup
		for i, args := range answers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cmd, stdout, stderr := command(root, nil, args...)
				code := 0
				if err := cmd.Run(); err != nil {
					code = 2
					if e, ok := err.(*exec.ExitError); ok {
						code = e.ExitCode()
					}
				}
				results[i] = output{code, stdout.String(), stderr.String()}
			}()
		}
		wg.Wait()
		applied := 0
		for _, r := range results {
			switch r.code {
			case 0:
				applied++
			case 2:
				contains(t, r.stderr, "verilex: refused: ")
				contains(t, r.stderr, "has no open grouping question")
			default:
				t.Fatalf("round %d: %+v", round, r)
			}
		}
		equal(t, applied, 1)
		equal(t, decision(t, root, "item-hold").Pending, []grouping.Pending(nil))
		equal(t, decision(t, root, "item-hold2").Pending, []grouping.Pending(nil))
	}
}

// A claim the agent puts under a claim whose own question is still open keeps the pending claims
// it was compared with, so the open claim's answer can still move it.
func TestClaimMovedUnderAnUndecidedClaimFollowsItsAnswer(t *testing.T) {
	root := baseline(t)
	v := pinOf(t, root, "item-added")
	held := claimLike(t, root, "item-held", [2]string{"sentence: A named item is in the store.", "sentence: A user's named item is held in the store."})
	variant(t, root, "h1", held, storedRun(t, root))
	onboard(t, root, "h1")
	retained := claimLike(t, root, "item-retained", [2]string{"sentence: A named item is in the store.", "sentence: The store retains each item a user names."})
	variant(t, root, "r1", retained, storedRun(t, root))
	onboard(t, root, "r1")
	pending := []string{}
	for _, p := range decision(t, root, "r1").Pending {
		pending = append(pending, p.Claim)
	}
	equal(t, pending, []string{v, held})
	equal(t, onboard(t, root, "r1", "--same-as", "item-held").stdout, "onboarded r1: variant of "+held+" through claim item-retained, as the agent decided\n")
	equal(t, decision(t, root, "r1").Pending[0].Claim, v)
	equal(t, onboard(t, root, "h1", "--same-as", "item-added").stdout, "onboarded h1: variant of "+v+" through claim item-held, as the agent decided; r1 moved with it\n")
	d := decision(t, root, "r1")
	equal(t, []string{d.Claim, d.Proves, d.Match}, []string{v, retained, grouping.Agent})
	equal(t, d.Pending, []grouping.Pending(nil))
}

// Done-when (PER-230): a word that misses its planted defect is refused. Correctness is a
// universal gate: green on the healthy product, red under every planted defect, no false pass.
func TestWordThatMissesItsPlantedDefectIsRefused(t *testing.T) {
	root := baseline(t)
	v := pinOf(t, root, "item-added")
	variant(t, root, "item-trusted", v, pyWord(`added = tally("add", name)
if added.returncode != 0:
    result("fail", preconditions_held=True, detail=f"tally add exited {added.returncode}")
result("pass", observation=f"tally add exited 0 for {name}")`))
	before := read(t, groupingFile(root))
	done := verilex(t, root, nil, "onboard", "item-trusted")
	equal(t, done.code, 1)
	m := regexp.MustCompile(`^rejected item-trusted: item-trusted gives a false pass under planted defect dropped-add: store-open \| item-trusted pear is green
  trial  dropped-add  store-open \| item-trusted pear: green, expected red
    evidence: (\S+)
  record: \S+
$`).FindStringSubmatch(done.stdout)
	if m == nil {
		t.Fatalf("unexpected output %q", done.stdout)
	}
	contains(t, read(t, filepath.Join(m[1], "actions.log")), "$ tally add pear\nexit 0\nadded pear\n")
	equal(t, read(t, groupingFile(root)), before)
	equal(t, status(t, root, "item-trusted"), "provisional")
	equal(t, plan(t, root, "store-open | item-trusted apple").stdout, "plan: skip 0, run 2; item-trusted apple: provisional; only admitted words are skipped\n")

	// A planted defect added to the claim later holds every word never gated against it.
	edit(t, claimFile(root, "item-added"), "  dropped-add: {TALLY_DEFECT: drop-adds}\n", "  dropped-add: {TALLY_DEFECT: drop-adds}\n  lost-add: {TALLY_DEFECT: drop-adds}\n")
	equal(t, pinOf(t, root, "item-added"), v)
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 3 admitted drift-suspect; they always run\n  item-stored: claim item-added declares planted defect lost-add, which item-stored was never gated against\n")
	equal(t, strings.SplitN(onboard(t, root, "item-stored").stdout, "\n", 2)[0], "onboarded item-stored: claim "+v+" joins the vocabulary; caught dropped-add, lost-add")
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (3 admitted)\n")

	// A claim without a planted defect cannot show that any word catches one.
	bare := claimLike(t, root, "item-bare", [2]string{"defects:\n  dropped-add: {TALLY_DEFECT: drop-adds}\n  lost-add: {TALLY_DEFECT: drop-adds}\n", ""})
	variant(t, root, "item-bared", bare, storedRun(t, root))
	done = verilex(t, root, nil, "onboard", "item-bared")
	equal(t, done.code, 2)
	equal(t, done.stderr, "verilex: refused: claim item-bare declares no planted defect, so onboarding cannot show that item-bared catches one: add 'defects' to "+claimFile(root, "item-bare")+"\n")

	// A planted defect that stops the chain before the word runs proves nothing: inconclusive, never red.
	early := claimLike(t, root, "item-early", [2]string{"  lost-add: {TALLY_DEFECT: drop-adds}\n", "  dirty-open: {TALLY_DEFECT: dirty-opens}\n"})
	variant(t, root, "item-earl", early, storedRun(t, root))
	done = verilex(t, root, nil, "onboard", "item-earl")
	equal(t, done.code, 2)
	contains(t, done.stdout, "inconclusive item-earl: planted defect dirty-open stops store-open | item-earl pear before item-earl runs (store-open: a new store holds ['ghost']): a planted defect must leave the states the claim requires intact\n")
	equal(t, status(t, root, "item-earl"), "provisional")
}

// R13: a proposal whose evidence shows only a declared non-proof is rejected, before any trial.
func TestEvidenceShowingOnlyANonProofIsRejected(t *testing.T) {
	root := baseline(t)
	v := pinOf(t, root, "item-added")
	variant(t, root, "item-echoed", v, pyWord(`added = tally("add", name)
if added.stdout.strip() != f"added {name}":
    result("fail", preconditions_held=True, detail="no confirmation")
result("pass", observation=f"tally printed added {name}")`))
	done := verilex(t, root, nil, "onboard", "item-echoed")
	equal(t, done.code, 1)
	m := regexp.MustCompile(`^rejected item-echoed: item-echoed's evidence in run (\S+) \("tally printed added apple"\) shows only a declared non-proof of claim item-added: ` + "`added NAME` alone does not prove the item was stored.\n  record: \\S+\n$").FindStringSubmatch(done.stdout)
	if m == nil {
		t.Fatalf("unexpected output %q", done.stdout)
	}
	equal(t, len(result(t, done).Trials), 0)

	// A proposed claim whose own observation is only that non-proof is rejected the same way.
	said := claimLike(t, root, "item-said", [2]string{"observation: store.json lists NAME.", "observation: \"`added NAME` is printed.\""})
	variant(t, root, "item-sayer", said, storedRun(t, root))
	done = verilex(t, root, nil, "onboard", "item-sayer")
	equal(t, done.code, 1)
	contains(t, done.stdout, "rejected item-sayer: claim item-said's observation (\"`added NAME` is printed.\") shows only a declared non-proof of claim item-")
	equal(t, status(t, root, "item-sayer"), "provisional")
}

// R19: onboarding checks the agent's step-to-claim mapping: the steps a claim's source maps
// must be what its evidence contract proves.
func TestStepToClaimMappingIsChecked(t *testing.T) {
	root := baseline(t)
	write(t, feature(root, "audit.md"), "# Audit\n\nA user audits an item.\n\n## Sub-features\n\n- `item-audit` confirms that a named item is stored.\n- `item-store` stores a named item.\n\n## Driving it with the tally CLI\n\n- `item-audit`: Run `bin/tally --store \"$STORE\" list`. Expect NAME on its own line.\n- `item-store`: Run `bin/tally --store \"$STORE\" add NAME`. Expect exit 0 and `added NAME`; `store.json` lists NAME.\n", 0644)
	audited := claimLike(t, root, "item-audited", [2]string{"  - ref: " + itemAdd + "\n    prose: 2948a95bd310\n    requirements:\n      - \"" + addRequirement + "\"", "  - ref: verify-tally/features/audit.md#item-audit\n    prose: 000000000000\n    requirements:\n      - Expect NAME on its own line."})
	pinProse(t, root, "item-audited")
	variant(t, root, "item-audit", audited, storedRun(t, root))
	done := verilex(t, root, nil, "onboard", "item-audit")
	equal(t, done.code, 1)
	contains(t, done.stdout, "rejected item-audit: claim item-audited maps verify-tally/features/audit.md#item-audit to \"Expect NAME on its own line.\", which shares no term with the claim's sentence or evidence contract: map the step that proves the claim\n")
	equal(t, len(result(t, done).Trials), 0)

	edit(t, claimFile(root, "item-audited"), "#item-audit\n", "#item-store\n")
	edit(t, claimFile(root, "item-audited"), "      - Expect NAME on its own line.", "      - \""+addRequirement+"\"")
	pinProse(t, root, "item-audited")
	contains(t, onboard(t, root, "item-audit").stdout, "onboarded item-audit: variant of "+pinOf(t, root, "item-added")+" through claim item-audited, matched mechanically")
}

// A requirement sentence in a Sub-features bullet starts with its own id. The id names the step,
// so it is no expected value: a claim that names the sentence's real values maps it.
func TestLeadingSubFeatureLabelIsNoExpectedValue(t *testing.T) {
	root := baseline(t)
	write(t, feature(root, "label.md"), "# Label\n\nA user stores a named item.\n\n## Sub-features\n\n- `item-label` must print `added NAME`.\n\n## Driving it with the tally CLI\n\n- `item-label`: Run `bin/tally --store \"$STORE\" add NAME`. Expect exit 0 and `added NAME`; `store.json` lists NAME.\n", 0644)
	labelled := claimLike(t, root, "item-labelled", [2]string{"  - ref: " + itemAdd + "\n    prose: 2948a95bd310\n    requirements:\n      - \"" + addRequirement + "\"", "  - ref: verify-tally/features/label.md#item-label\n    prose: 000000000000\n    requirements:\n      - \"`item-label` must print `added NAME`.\"\n      - \"" + addRequirement + "\""})
	pinProse(t, root, "item-labelled")
	variant(t, root, "item-label", labelled, storedRun(t, root))
	done := verilex(t, root, nil, "onboard", "item-label")
	if strings.Contains(done.stdout, "expected values") {
		t.Fatalf("the sub-feature label was counted as an expected value: %q", done.stdout)
	}
	contains(t, done.stdout, "onboarded item-label: variant of "+pinOf(t, root, "item-added")+" through claim item-labelled, matched mechanically")
}

// pinProse pins each of a claim's sources to its sub-feature's prose as it is now, as the
// claim's reviewer does.
func pinProse(t *testing.T, root, name string) {
	t.Helper()
	var claims []struct {
		Claim   string
		Sources []struct{ Ref, Prose string }
	}
	if err := json.Unmarshal([]byte(verilex(t, root, nil, "claims", "--json").stdout), &claims); err != nil {
		t.Fatal(err)
	}
	text := read(t, claimFile(root, name))
	for _, c := range claims {
		for _, source := range c.Sources {
			if c.Claim == name {
				text = regexp.MustCompile(`(- ref: `+regexp.QuoteMeta(source.Ref)+`\n\s+prose: )\S+`).ReplaceAllString(text, "${1}"+source.Prose)
			}
		}
	}
	write(t, claimFile(root, name), text, 0644)
}

// Done-when (PER-230): the grouping file changes only through onboarding. Every other command
// leaves it as it is, a refused or rejected proposal changes nothing, and a decision edited by
// hand no longer matches its seal, so verilex ignores it until the word is onboarded again.
func TestGroupingFileChangesOnlyThroughOnboarding(t *testing.T) {
	root := baseline(t)
	before := read(t, groupingFile(root))
	for _, args := range [][]string{
		{"run", chain}, {"run", chain}, {"plan", chain}, {"words"}, {"claims"}, {"check"}, {"runs"},
		{"index"}, {"index", "item-added"}, {"index", "item-added", "item-stored"}, {"index", "--intent", "an item is added"},
		{"index", "--changed", "bin/tally"}, {"gap", "A user renames an item"}, {"onboard", "item-stored"},
	} {
		verilex(t, root, nil, args...)
		equal(t, read(t, groupingFile(root)), before)
	}
	for _, word := range []string{"item-stored", "store-open"} {
		done := verilex(t, root, nil, "propose", word)
		equal(t, done.code, 2)
		contains(t, done.stderr, "proves claim")
		contains(t, done.stderr, "so it joins the vocabulary through `verilex onboard "+word+"`, not propose and admit")
	}
	done := verilex(t, root, nil, "admit", "item-stored", "--verdict", verdict(t, root, `{"word": "item-stored", "packet": "0123456789abcdef", "verdict": "admit", "curator": "m"}`))
	equal(t, done.code, 2)
	contains(t, done.stderr, "not propose and admit")
	equal(t, read(t, groupingFile(root)), before)
	equal(t, verilex(t, root, nil, "onboard", "item-stored").stdout, "onboarded item-stored already: it proves "+pinOf(t, root, "item-added")+" for tally, and nothing it was judged on changed\n")

	// A hand edit voids the decision it touches: the word drifts, leaves the index and never skips.
	edited := strings.Replace(before, "match: new\n    defects:\n      dropped-add", "match: same\n    defects:\n      dropped-add", 1)
	if edited == before {
		t.Fatal("found no item-stored decision to edit")
	}
	write(t, groupingFile(root), edited, 0644)
	why := "its grouping decision does not match its seal: it was edited outside `verilex onboard`; onboard it again"
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 3 admitted drift-suspect; they always run\n  item-stored: "+why+"\n")
	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; item-stored apple: drift-suspect: "+why+"\n")
	equal(t, strings.Contains(verilex(t, root, nil, "index").stdout, "item-added"), false)
	// A decision copied by hand to another word does not match that word's seal either.
	copied := edited + strings.Replace(before[strings.Index(before, "  item-stored:\n"):strings.Index(before, "  store-open:\n")], "  item-stored:\n", "  item-put:\n", 1)
	write(t, groupingFile(root), copied, 0644)
	variant(t, root, "item-put", pinOf(t, root, "item-added"), stub)
	equal(t, status(t, root, "item-put"), "drift-suspect")

	write(t, groupingFile(root), edited, 0644)
	onboard(t, root, "item-stored")
	equal(t, status(t, root, "item-stored"), "admitted")
	equal(t, decision(t, root, "item-stored").Match, grouping.New)
}

// R17: a claim source in another repository is resolved there, and onboarding records the
// repository and the commit it checked the mapping against.
func TestCrossRepoSourceRecordsRepoAndCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := product(t)
	runbooks := filepath.Join(filepath.Dir(root), "runbooks")
	skill := filepath.Join(runbooks, ".cursor", "skills", "verify-tally", "features")
	if err := os.MkdirAll(skill, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(skill, "items.md"), read(t, feature(root, "items.md")), 0644)
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", runbooks, "-c", "user.name=verilex", "-c", "user.email=verilex@example.com"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-q", "-m", "runbooks")
	commit := git("rev-parse", "HEAD")
	if err := os.Remove(feature(root, "items.md")); err != nil {
		t.Fatal(err)
	}
	edit(t, claimFile(root, "item-added"), "  - ref: "+itemAdd+"\n", "  - ref: "+itemAdd+"\n    repo: ../runbooks\n")
	edit(t, claimFile(root, "item-listed"), "  - ref: verify-tally/features/items.md#item-list\n", "  - ref: verify-tally/features/items.md#item-list\n    repo: ../runbooks\n")
	equal(t, strings.Contains(verilex(t, root, nil, "claims").stdout, "review:"), false)
	used(t, root, "store-open | item-stored apple")
	used(t, root, "store-open | item-stored pear")

	write(t, filepath.Join(skill, "items.md"), read(t, filepath.Join(skill, "items.md"))+"\nMore prose.\n", 0644)
	done := verilex(t, root, nil, "onboard", "item-stored")
	equal(t, done.code, 2)
	equal(t, done.stderr, "verilex: refused: claim item-added: .cursor/skills/verify-tally/features/items.md has uncommitted changes in ../runbooks; commit them, so the recorded commit holds what onboarding checks\n")
	git("commit", "-q", "-am", "prose")
	commit2 := git("rev-parse", "HEAD")
	if commit2 == commit {
		t.Fatal("no new commit")
	}
	onboard(t, root, "item-stored")
	equal(t, decision(t, root, "item-stored").Sources, []grouping.Source{{Ref: itemAdd, Repo: "../runbooks", Commit: commit2}})

	// The other repository's runbook is the claim's source: a changed requirement there flags it.
	edit(t, filepath.Join(skill, "items.md"), addRequirement, "Expect exit 0 and `stored NAME`; `store.json` lists NAME.")
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 1 admitted drift-suspect; they always run\n  item-stored: "+reviewAdd+"\n")
	if !slices.Contains(strings.Split(verilex(t, root, nil, "claims").stdout, "\n"), "  review: "+itemAdd+": requirement changed or gone: "+addRequirement) {
		t.Fatal("the claim does not ask for review")
	}
}

// Onboarding runs its trials in parallel, each on its own instance. This test drives the
// onboarding entrypoint in this process, so `go test -race` watches those trials too.
func TestOnboardingTrialsRunInParallelWithoutRaces(t *testing.T) {
	root := baseline(t)
	variant(t, root, "item-filed", pinOf(t, root, "item-added"), storedRun(t, root))
	t.Setenv("VERILEX_HOME", filepath.Join(filepath.Dir(root), "state"))
	t.Setenv("TALLY_STORES", filepath.Join(filepath.Dir(root), "stores"))
	project, err := dictionary.FindProject(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := onboarding.Onboard(project, "item-filed", onboarding.Choice{})
	if err != nil {
		t.Fatal(err)
	}
	equal(t, r.Outcome, onboarding.Onboarded)
	equal(t, trialsOf(r), []string{"healthy item-filed green", "dropped-add item-filed red", "healthy item-stored green", "dropped-add item-stored red"})
	equal(t, stores(t, root), []string{})
}
