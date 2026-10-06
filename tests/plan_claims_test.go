package tests

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// claimPlan is the verilex-claim-plan-1 JSON a launcher binds to.
type claimPlan struct {
	Format   string         `json:"format"`
	Intent   string         `json:"intent"`
	Selected []string       `json:"selected"`
	Skip     []skippedClaim `json:"skip"`
	Run      []liveClaim    `json:"run"`
	Order    []string       `json:"order"`
	Chain    string         `json:"chain"`
	Touched  []string       `json:"touched"`
	Unpicked []string       `json:"unpicked"`
	Warning  string         `json:"warning"`
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
