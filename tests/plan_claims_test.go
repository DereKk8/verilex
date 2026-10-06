package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// claimPlan is the verilex-claim-plan-1 JSON a launcher binds to.
type claimPlan struct {
	Format       string         `json:"format"`
	Intent       string         `json:"intent"`
	Selected     []string       `json:"selected"`
	Skip         []skippedClaim `json:"skip"`
	Run          []liveClaim    `json:"run"`
	Order        []string       `json:"order"`
	Chain        string         `json:"chain"`
	Touched      []string       `json:"touched"`
	Unpicked     []string       `json:"unpicked"`
	Warning      string         `json:"warning"`
	Rerun        string         `json:"rerun"`
	Inconclusive string         `json:"inconclusive"`
	Unclaimed    []string       `json:"unclaimed"`
}

type skippedClaim struct {
	Claim        string            `json:"claim"`
	Word         string            `json:"word"`
	Step         string            `json:"step"`
	Fingerprints map[string]string `json:"fingerprints"`
	Stamp        string            `json:"stamp"`
	ReliesOn     string            `json:"relies_on"`
}

type liveClaim struct {
	Claim  string `json:"claim"`
	Word   string `json:"word"`
	Step   string `json:"step"`
	Reason string `json:"reason"`
}

func planClaims(t *testing.T, root string, args ...string) claimPlan {
	t.Helper()
	before := runCount(t, root)
	done := verilex(t, root, nil, append([]string{"plan", "--json"}, args...)...)
	if done.code != 0 {
		t.Fatalf("plan failed: %+v", done)
	}
	if runCount(t, root) != before {
		t.Fatal("plan recorded a run")
	}
	var plan claimPlan
	if err := json.Unmarshal([]byte(done.stdout), &plan); err != nil {
		t.Fatalf("%v: %s", err, done.stdout)
	}
	if plan.Format != "verilex-claim-plan-1" {
		t.Fatalf("format %q", plan.Format)
	}
	return plan
}

// Rule: with no intent the plan is the diff-affected claim set, in dependency order, and a
// standing pass is cited by the fingerprints that prove it.
func TestNoIntentPlanIsTheDiff(t *testing.T) {
	root := curated(t)
	first := green(t, root, nil, chain)
	plan := planClaims(t, root, "--changed", ".verilex/words/item-listed/run")
	equal(t, plan.Intent, "prove nothing this change touched broke")
	equal(t, plan.Selected, []string{"item-listed"})
	equal(t, plan.Touched, []string{"item-listed"})
	equal(t, plan.Unpicked, []string{})
	equal(t, plan.Order, []string{"store-opened", "item-added", "item-listed"})
	equal(t, plan.Chain, chain)
	if len(plan.Skip) != 1 || plan.Skip[0].Claim != "item-listed" || plan.Skip[0].ReliesOn != first.Run {
		t.Fatalf("skip: %+v", plan.Skip)
	}
	pass := passFor(t, ledgerDir(root), "item-listed apple")
	prints, _ := pass.fields["components"].(map[string]any)
	for key, value := range plan.Skip[0].Fingerprints {
		if prints[key] != value {
			t.Fatalf("fingerprint %s: plan %v ledger %v", key, value, prints[key])
		}
	}
	if plan.Skip[0].Fingerprints["claim"] == "" || plan.Skip[0].Stamp == "" {
		t.Fatalf("missing proof: %+v", plan.Skip[0])
	}
	human := verilex(t, root, nil, "plan", "--changed", ".verilex/words/item-listed/run")
	contains(t, human.stdout, "prove nothing this change touched broke")
	contains(t, human.stdout, "skip  item-listed  item-listed apple  relies on run "+first.Run)
	contains(t, human.stdout, "order: store-opened, item-added, item-listed")
}

// Rule: a plan lists skip, run, topological order and touched claims the agent did not pick.
// Named claims always appear, and they do not drop claims the agent derived.
func TestPlanSkipRunOrderAndUnpicked(t *testing.T) {
	root := curated(t)
	first := green(t, root, nil, chain)
	appendTo(t, root+"/.verilex/words/item-stored/run", "\n")
	plan := planClaims(t, root,
		"--claim", "item-added", "--named", "store-opened",
		"--changed", ".verilex/words/item-stored/run", "--changed", ".verilex/words/item-listed/run")
	equal(t, plan.Intent, "given")
	equal(t, plan.Selected, []string{"item-added", "store-opened"})
	equal(t, plan.Unpicked, []string{"item-listed"})
	equal(t, plan.Warning, "1 touched claim not picked")
	equal(t, plan.Order, []string{"store-opened", "item-added"})
	equal(t, plan.Chain, "store-open | item-stored apple")
	if len(plan.Skip) != 1 || plan.Skip[0].Claim != "store-opened" || plan.Skip[0].ReliesOn != first.Run {
		t.Fatalf("named claim missing from skip: %+v", plan.Skip)
	}
	if len(plan.Run) != 1 || plan.Run[0].Claim != "item-added" || !strings.Contains(plan.Run[0].Reason, "drift-suspect") {
		t.Fatalf("run: %+v", plan.Run)
	}
	human := verilex(t, root, nil, "plan", "--claim", "item-added", "--named", "store-opened",
		"--changed", ".verilex/words/item-listed/run", "--changed", ".verilex/words/item-stored/run")
	contains(t, human.stdout, "unpicked  item-listed")
	contains(t, human.stdout, "1 touched claim not picked")
}

// Rule: a run whose plan missed a touched claim carries that warning in human and JSON form.
// A config key or image pin the stamp does not fingerprint runs live, not as a skip.
func TestMissedClaimWarningAndDependencyKinds(t *testing.T) {
	root := curated(t)
	green(t, root, nil, chain)

	missed := verilex(t, root, nil, "run", "--json", "--claim", "store-opened", "--named", "item-added", "--changed", ".verilex/words/item-listed/run")
	if missed.code != 0 {
		t.Fatalf("missed run: %+v", missed)
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(missed.stdout), &record); err != nil {
		t.Fatal(err)
	}
	equal(t, record["verdict"], "green")
	equal(t, record["warning"], "1 touched claim not covered")
	uncovered, _ := record["uncovered"].([]any)
	if len(uncovered) != 1 || uncovered[0].(map[string]any)["claim"] != "item-listed" {
		t.Fatalf("uncovered: %#v", record["uncovered"])
	}
	human := verilex(t, root, nil, "run", "--claim", "store-opened", "--changed", ".verilex/words/item-listed/run")
	contains(t, human.stdout, "green:")
	contains(t, human.stdout, "with 1 touched claim not covered")
	contains(t, human.stdout, "uncovered  item-listed")
	contains(t, human.stdout, "next: verilex run --claim 'item-listed'")

	lookup := verilex(t, root, nil, "index", "--changed", "config:tally.list")
	contains(t, lookup.stdout, "item-listed  A stored item shows up when a user lists the store.")
	if strings.Contains(lookup.stdout, "item-added") || strings.Contains(lookup.stdout, "store-opened") {
		t.Fatalf("config key touched other claims:\n%s", lookup.stdout)
	}
	listed := planClaims(t, root, "--changed", "config:tally.list")
	equal(t, listed.Selected, []string{"item-listed"})
	if len(listed.Run) != 1 || listed.Run[0].Reason != "config key tally.list changed" {
		t.Fatalf("config key should force a run: %+v", listed.Run)
	}
	live := verilex(t, root, nil, "run", "--json", "--changed", "config:tally.list")
	var ran map[string]any
	if err := json.Unmarshal([]byte(live.stdout), &ran); err != nil {
		t.Fatal(err)
	}
	if ran["skipped"] == true {
		t.Fatal("config key change was skipped")
	}
	store := planClaims(t, root, "--changed", "image:ghcr.io/example/store:1")
	equal(t, store.Selected, []string{"store-opened"})
	if len(store.Run) != 1 || !strings.HasPrefix(store.Run[0].Reason, "image pin ") {
		t.Fatalf("image pin: %+v", store.Run)
	}
	runbook := planClaims(t, root, "--changed", "runbook:verify-tally/features/items.md#item-add")
	equal(t, runbook.Selected, []string{"item-added"})
	paths := planClaims(t, root, "--changed", "bin/tally")
	equal(t, paths.Selected, []string{"item-added", "item-listed", "store-opened"})
}

// Rule: an aged-out pass does not prove a claim, and a red claim names expected against got.
func TestAgeOutAndFailingLink(t *testing.T) {
	root := curated(t)
	green(t, root, nil, chain)
	age(t, ledgerDir(root), 8*24*time.Hour)
	aged := planClaims(t, root, "--claim", "store-opened")
	if len(aged.Run) != 1 || aged.Run[0].Claim != "store-opened" || !strings.Contains(aged.Run[0].Reason, "expired") {
		t.Fatalf("aged pass still proved the claim: %+v", aged.Run)
	}

	root = curated(t)
	green(t, root, nil, chain)
	done := verilex(t, root, map[string]string{"TALLY_DEFECT": "drop-adds"}, "run", "--claim", "item-added")
	equal(t, done.code, 1)
	contains(t, done.stdout, "red:")
	contains(t, done.stdout, "red  item-added:")
	contains(t, done.stdout, "expected: store.json lists NAME.")
	contains(t, done.stdout, "got: tally said 'added apple' but store.json lacks apple")
	contains(t, done.stdout, "next: verilex run --fresh --claim 'item-added'")
	asJSON := verilex(t, root, map[string]string{"TALLY_DEFECT": "drop-adds"}, "run", "--json", "--fresh", "--claim", "item-added")
	var record map[string]any
	if err := json.Unmarshal([]byte(asJSON.stdout), &record); err != nil {
		t.Fatal(err)
	}
	equal(t, record["verdict"], "red")
	claims, _ := record["claims"].([]any)
	var added map[string]any
	for _, item := range claims {
		row := item.(map[string]any)
		if row["claim"] == "item-added" {
			added = row
		}
	}
	if added == nil || added["expected"] != "store.json lists NAME." || added["got"] != "tally said 'added apple' but store.json lacks apple" || added["evidence"] == "" || added["next"] == "" {
		t.Fatalf("failing link: %#v", record["claims"])
	}
}

// Rule: --continue must not skip a config key or image pin hit from a kept instance's older
// history, in claim mode or chain mode.
func TestContinueDoesNotSkipForcedDependency(t *testing.T) {
	root := curated(t)
	kept := green(t, root, nil, chain, "--keep")
	for _, change := range []string{"config:tally.list", "image:ghcr.io/example/store:1"} {
		for _, command := range []string{"run", "plan"} {
			done := verilex(t, root, nil, command, "--continue", kept.Run, "--claim", "item-listed", "--changed", change)
			equal(t, done.code, 2)
			equal(t, done.stderr, "verilex: refused: --continue is not used with a claim plan; run without --continue\n")
			equal(t, done.stdout, "")
			done = verilex(t, root, nil, command, "--continue", kept.Run, chain, "--changed", change)
			equal(t, done.code, 2)
			contains(t, done.stderr, "changed; a kept instance cannot be reused for it: run without --continue")
			equal(t, done.stdout, "")
		}
	}
	if runCount(t, root) != 1 {
		t.Fatal("refused continue recorded a run")
	}
}

// Rule: a chain run given a diff that hits a config key or image pin runs live, and plan says so.
func TestChainWithForcedDependencyRunsLive(t *testing.T) {
	root := curated(t)
	green(t, root, nil, chain)
	plan := verilex(t, root, nil, "plan", chain, "--changed", "config:tally.list")
	equal(t, plan.stdout, "plan: skip 0, run 3; item-listed apple: config key tally.list changed\n")
	done := verilex(t, root, nil, "run", "--json", chain, "--changed", "config:tally.list")
	var record map[string]any
	if err := json.Unmarshal([]byte(done.stdout), &record); err != nil {
		t.Fatal(err)
	}
	equal(t, record["skipped"], nil)
	equal(t, record["rerun"], "item-listed apple: config key tally.list changed")
	equal(t, record["verdict"], "green")
}

// Rule: a depends.paths change must not skip from a pass recorded before the change. The tally
// here reads its defect from lib/mode, which item-stored declares in depends.paths.
func TestDependsPathChangeDoesNotSkip(t *testing.T) {
	root := curated(t)
	tally := filepath.Join(root, "bin", "tally")
	reads := strings.Replace(read(t, tally), `defect = os.environ.get("TALLY_DEFECT")`,
		`defect = os.environ.get("TALLY_DEFECT") or (Path(__file__).parent.parent / "lib" / "mode").read_text().strip()`, 1)
	if err := os.Remove(tally); err != nil {
		t.Fatal(err)
	}
	write(t, tally, reads, 0755)
	word := filepath.Join(root, ".verilex", "words", "item-stored", "word.md")
	replace(t, word, strings.Replace(read(t, word), "depends:\n", "depends:\n  paths: [lib/mode]\n", 1))
	if err := os.MkdirAll(filepath.Join(root, "lib"), 0755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "lib", "mode"), "", 0644)
	admitted(t, root, "item-stored")
	green(t, root, nil, chain)
	equal(t, planClaims(t, root, "--changed", "lib/mode").Rerun, "")
	replace(t, filepath.Join(root, "lib", "mode"), "drop-adds\n")

	plan := planClaims(t, root, "--changed", "lib/mode")
	if len(plan.Run) != 1 || plan.Run[0].Claim != "item-added" || plan.Run[0].Reason != "depends lib/mode changed" {
		t.Fatalf("stale pass still proved the claim: %+v skip=%+v", plan.Run, plan.Skip)
	}
	for range 2 {
		done := verilex(t, root, nil, "run", "--changed", "lib/mode")
		equal(t, done.code, 1)
		contains(t, done.stdout, "red  item-added:")
	}
}

// Rule: a diff that touches no claim is inconclusive, not an evidence-free green.
func TestUnmappedDiffIsInconclusive(t *testing.T) {
	root := product(t)
	for _, change := range []string{"README.md", "does/not/exist.txt", "config:tally.lsit", "bin/taly"} {
		done := verilex(t, root, nil, "run", "--changed", change)
		equal(t, done.code, 2)
		if strings.HasPrefix(done.stdout, "green") {
			t.Fatalf("%s was a false green: %s", change, done.stdout)
		}
		contains(t, done.stdout, "inconclusive: no claim covers this change; fall back to the product verify skill")
	}
	if runCount(t, root) != 0 {
		t.Fatal("unmapped diff recorded a run")
	}
	asJSON := verilex(t, root, nil, "run", "--json", "--changed", "does/not/exist.txt")
	equal(t, asJSON.code, 2)
	var doc map[string]any
	if err := json.Unmarshal([]byte(asJSON.stdout), &doc); err != nil {
		t.Fatal(err)
	}
	equal(t, doc["format"], "verilex-claim-run-1")
	equal(t, doc["verdict"], "inconclusive")
	planned := verilex(t, root, nil, "plan", "--json", "--changed", "config:tally.lsit")
	equal(t, planned.code, 2)
	var plan claimPlan
	if err := json.Unmarshal([]byte(planned.stdout), &plan); err != nil {
		t.Fatal(err)
	}
	equal(t, plan.Format, "verilex-claim-plan-1")
	equal(t, plan.Inconclusive, "no claim covers this change; fall back to the product verify skill")
	equal(t, plan.Chain, "")
	equal(t, verilex(t, root, nil, "plan", "--changed", "bin/taly").stdout, "plan: inconclusive: no claim covers this change; fall back to the product verify skill\n")

	verilex(t, root, nil, "new", "probe-word", "--implements", "verify-tally/features/items.md#item-add")
	stub := verilex(t, root, nil, "run", "--changed", ".verilex/words/probe-word/run")
	equal(t, stub.code, 2)
	if strings.HasPrefix(stub.stdout, "green") {
		t.Fatalf("claimless word was a false green: %s", stub.stdout)
	}
	contains(t, stub.stdout, "probe-word proves no claim and the diff touched it")
	green(t, root, nil, chain)
	mixed := planClaims(t, root, "--changed", ".verilex/words/probe-word/run", "--changed", ".verilex/words/item-listed/run")
	equal(t, mixed.Selected, []string{"item-listed"})
	equal(t, mixed.Unclaimed, []string{"probe-word"})
	contains(t, verilex(t, root, nil, "plan", "--changed", ".verilex/words/probe-word/run", "--changed", ".verilex/words/item-listed/run").stdout,
		"  unclaimed  probe-word: proves no claim; verify it with the product verify skill\n")
}

// Rule: every selected claim has a verdict, a red claim is not also uncovered, and the record names what was asked.
func TestClaimReportAndRunContract(t *testing.T) {
	root := curated(t)
	green(t, root, nil, chain)
	locked := verilex(t, root, map[string]string{"TALLY_SIMULATE_LOCK": "1"}, "run", "--json", "--claim", "item-listed", "--named", "store-opened")
	var record map[string]any
	if err := json.Unmarshal([]byte(locked.stdout), &record); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, item := range record["claims"].([]any) {
		names[item.(map[string]any)["claim"].(string)] = true
	}
	if !names["item-listed"] || !names["store-opened"] {
		t.Fatalf("selected claim missing: %#v", record["claims"])
	}
	human := verilex(t, root, map[string]string{"TALLY_SIMULATE_LOCK": "1"}, "run", "--claim", "item-listed", "--named", "store-opened")
	contains(t, human.stdout, "  inconclusive  item-listed: not run\n    next: verilex run --fresh --claim 'item-listed'\n")

	red := verilex(t, root, map[string]string{"TALLY_DEFECT": "drop-adds"}, "run", "--json", "--claim", "item-listed", "--changed", "bin/tally")
	if err := json.Unmarshal([]byte(red.stdout), &record); err != nil {
		t.Fatal(err)
	}
	equal(t, record["verdict"], "red")
	var added map[string]any
	for _, item := range record["claims"].([]any) {
		row := item.(map[string]any)
		if row["claim"] == "item-added" {
			added = row
		}
	}
	if added == nil || added["verdict"] != "red" {
		t.Fatalf("red claim missing: %#v", record["claims"])
	}
	if uncovered, ok := record["uncovered"].([]any); ok {
		for _, item := range uncovered {
			if item.(map[string]any)["claim"] == "item-added" {
				t.Fatal("a claim that ran red was also uncovered")
			}
		}
	}
	equal(t, record["format"], "verilex-claim-run-1")
	requested := record["requested"].(map[string]any)
	equal(t, requested["claims"].([]any)[0], "item-listed")
	if len(record["touched"].([]any)) == 0 {
		t.Fatal("touched was empty")
	}
	missed := verilex(t, root, nil, "run", "--json", "--claim", "store-opened", "--changed", ".verilex/words/item-listed/run")
	contains(t, missed.stdout, "1 touched claim not covered")
	runs := verilex(t, root, nil, "runs", "--json")
	contains(t, runs.stdout, "1 touched claim not covered")
}

// Rule: when the chain runs live, the plan says so, so a standing pass is not read as a step the
// run omits. A provider step whose image pin changed forces the chain live too.
func TestPlanSaysChainRunsLive(t *testing.T) {
	root := curated(t)
	first := green(t, root, nil, chain)
	plan := planClaims(t, root, "--changed", "config:tally.list")
	equal(t, plan.Rerun, "item-listed apple: config key tally.list changed")
	human := verilex(t, root, nil, "plan", "--claim", "item-listed", "--named", "item-added", "--changed", "config:tally.list")
	contains(t, human.stdout, "plan: whole chain runs live; run 1, proven 1; item-listed apple: config key tally.list changed\n")
	contains(t, human.stdout, "  proven  item-added  item-stored apple  relies on run "+first.Run+"\n")

	provider := planClaims(t, root, "--claim", "item-listed", "--changed", "image:ghcr.io/example/store:1")
	equal(t, provider.Unpicked, []string{"store-opened"})
	equal(t, provider.Rerun, "store-open: image pin ghcr.io/example/store:1 changed")
	done := verilex(t, root, nil, "run", "--json", "--claim", "item-listed", "--changed", "image:ghcr.io/example/store:1")
	var record map[string]any
	if err := json.Unmarshal([]byte(done.stdout), &record); err != nil {
		t.Fatal(err)
	}
	equal(t, record["skipped"], nil)
	equal(t, record["rerun"], any(provider.Rerun))
	for _, word := range record["words"].([]any) {
		if word.(map[string]any)["relies_on"] != nil {
			t.Fatalf("a step skipped under a changed image pin: %#v", word)
		}
	}
}
