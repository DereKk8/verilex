package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/grouping"
	"github.com/DereKk8/verilex/internal/runner"
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
	Unmapped     []string       `json:"unmapped"`
	Unrun        []string       `json:"unrun"`
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

// A claimless word may implement a whole feature file, with no #<sub-feature>; an edit to that
// file touches the word, alone or beside a change a claim covers.
func TestWholeFileImplementsDependsOnTheFile(t *testing.T) {
	root := product(t)
	write(t, feature(root, "backup.md"), "# Backup\n\nA user backs the store up.\n", 0644)
	equal(t, verilex(t, root, nil, "new", "probe-backup", "--implements", "verify-tally/features/backup.md").code, 0)
	const file = ".cursor/skills/verify-tally/features/backup.md"
	alone := verilex(t, root, nil, "run", "--changed", file)
	equal(t, alone.code, 2)
	contains(t, alone.stdout, "probe-backup proves no claim and the diff touched it")
	green(t, root, nil, chain)
	mixed := verilex(t, root, nil, "plan", "--changed", file, "--changed", ".verilex/words/item-listed/run").stdout
	contains(t, mixed, "  unclaimed  probe-backup: proves no claim; verify it with the product verify skill\n")
	if strings.Contains(mixed, "unmapped") {
		t.Fatalf("a word depends on %s:\n%s", file, mixed)
	}
}

// A diff no claim covers is answered without reading the verify skill: only where its feature
// files are matters to a path. A feature file that blocks every read stands in for one too slow
// to parse.
func TestUncoveredDiffDoesNotReadTheVerifySkill(t *testing.T) {
	root := product(t)
	feature := filepath.Join(root, ".cursor", "skills", "verify-tally", "features", "items.md")
	if err := os.Remove(feature); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(feature, 0600); err != nil {
		t.Fatal(err)
	}
	within := func(args ...string) output {
		t.Helper()
		p := start(t, root, nil, args...)
		timer := time.AfterFunc(20*time.Second, func() { _ = p.cmd.Process.Kill() })
		defer timer.Stop()
		return p.wait(t)
	}
	for _, command := range []string{"run", "plan"} {
		done := within(command, "--changed", "README.md")
		equal(t, done.code, 2)
		contains(t, done.stdout, "inconclusive: no claim covers this change; fall back to the product verify skill\n")
	}
	equal(t, within("index", "--changed", "README.md").stdout, "changed: 0 claim(s); 0 of 0 known chain(s) must re-run\n")
	equal(t, claimsOf(changed(t, root, ".cursor/skills/verify-tally/features/items.md")), []string{"item-added", "item-listed"})
}

// An uncovered runbook reference needs no feature text. A blocked feature file must not stop
// index, plan or run from returning the same no-coverage answer as an ordinary file.
func TestUncoveredRunbookDoesNotReadTheVerifySkill(t *testing.T) {
	root := product(t)
	const ref = "runbook:verify-tally/features/items.md#item-add"
	equal(t, verilex(t, root, nil, "index", "--changed", ref).stdout,
		"changed: 1 claim(s); 0 of 0 known chain(s) must re-run\nitem-added  A named item is in the store.  (dormant)\n")
	feature := filepath.Join(root, ".cursor", "skills", "verify-tally", "features", "items.md")
	if err := os.Remove(feature); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(feature, 0600); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{
		"runbook:verify-tally/features/items.md#no-such",
		"runbook:verify-tally/features/nothing.md#x",
		"runbook:verify-tally/features/items.md#no-such@0123456789ab",
	} {
		for _, command := range []string{"index", "plan", "run"} {
			t.Run(command+"/"+change, func(t *testing.T) {
				p := start(t, root, nil, command, "--changed", change)
				timer := time.AfterFunc(5*time.Second, func() { _ = p.cmd.Process.Kill() })
				defer timer.Stop()
				done := p.wait(t)
				if command == "index" {
					equal(t, done.code, 0)
					equal(t, done.stdout, "changed: 0 claim(s); 0 of 0 known chain(s) must re-run\n")
				} else {
					equal(t, done.code, 2)
					prefix := ""
					if command == "plan" {
						prefix = "plan: "
					}
					got := done.stdout
					if command == "run" {
						// The usage ledger records the uncovered run and names it on a second line.
						head, id, named := strings.Cut(got, "\nrun: ")
						equal(t, named && id != "\n" && strings.Index(id, "\n") == len(id)-1, true)
						got = head + "\n"
					}
					equal(t, got, prefix+"inconclusive: no claim covers this change; fall back to the product verify skill\n")
				}
				equal(t, done.stderr, "")
			})
		}
	}
	equal(t, verilex(t, root, nil, "runs").stdout, "")
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

// Rule: requested.chain says who wrote the chain: null when verilex planned the run from claims
// and changes, also for an empty chain argument, and the caller's chain when the caller gave one.
func TestRequestNamesTheCallersChain(t *testing.T) {
	root := curated(t)
	green(t, root, nil, chain)
	for _, tc := range []struct {
		args  []string
		chain any
	}{
		{[]string{"--claim", "item-listed"}, nil},
		{[]string{"--claim", "item-listed", ""}, nil},
		{[]string{"--changed", "bin/tally"}, nil},
		{[]string{"--changed", "bin/tally", "store-open"}, "store-open"},
	} {
		out := verilex(t, root, nil, append([]string{"run", "--json"}, tc.args...)...)
		var record map[string]any
		if err := json.Unmarshal([]byte(out.stdout), &record); err != nil {
			t.Fatalf("%q: %v\n%s%s", tc.args, err, out.stdout, out.stderr)
		}
		requested, _ := record["requested"].(map[string]any)
		if chain, ok := requested["chain"]; !ok || chain != tc.chain {
			t.Fatalf("%q: requested.chain %#v (present %v), want %#v", tc.args, chain, ok, tc.chain)
		}
	}
}

// Rule: with --no-chain, plan and run refuse any chain argument, an empty one too, before anything
// runs or any run record is written. Claims, named claims, a diff and --fresh still plan and run.
func TestNoChainRefusesEveryChainArgument(t *testing.T) {
	root := curated(t)
	green(t, root, nil, chain)
	before := runCount(t, root)
	for _, command := range []string{"plan", "run"} {
		for _, args := range [][]string{
			{"store-open"},
			{""},
			{"--claim", "item-listed", ""},
			{"--changed", "bin/tally", "store-open"},
			{"--", "store-open"},
		} {
			out := verilex(t, root, nil, append([]string{command, "--no-chain", "--json"}, args...)...)
			if out.code != 2 || out.stdout != "" || !strings.Contains(out.stderr, "--no-chain refuses a chain argument, an empty one too") {
				t.Fatalf("%s %q: exit %d\n%s%s", command, args, out.code, out.stdout, out.stderr)
			}
		}
	}
	bare := verilex(t, root, nil, "run", "--no-chain", "--json")
	if bare.code != 2 || !strings.Contains(bare.stderr, "--no-chain needs --claim, --named or --changed") {
		t.Fatalf("bare run: exit %d\n%s%s", bare.code, bare.stdout, bare.stderr)
	}
	if after := runCount(t, root); after != before {
		t.Fatalf("refused runs wrote run records: %d before, %d after", before, after)
	}
	if plan := planClaims(t, root, "--no-chain", "--claim", "item-listed"); plan.Chain != chain {
		t.Fatalf("plan --no-chain --claim item-listed planned %q, want %q", plan.Chain, chain)
	}
	for _, args := range [][]string{
		{"--claim", "item-listed"},
		{"--named", "item-listed"},
		{"--changed", "bin/tally"},
		{"--fresh", "--claim", "item-listed"},
	} {
		out := verilex(t, root, nil, append([]string{"run", "--no-chain", "--json"}, args...)...)
		var record map[string]any
		if err := json.Unmarshal([]byte(out.stdout), &record); err != nil || out.code != 0 || record["verdict"] != "green" {
			t.Fatalf("run %q: exit %d %v\n%s%s", args, out.code, err, out.stdout, out.stderr)
		}
		if args[0] == "--fresh" && record["skipped"] == true {
			t.Fatalf("run %q skipped, but --fresh runs live", args)
		}
	}
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

// Rule: every matching runbook entry is mapped, even beside another dependency that hits
// the same claim. Unrelated entries still warn beside a mapped path.
func TestRunbookChangeMapsBesideClaimDependencies(t *testing.T) {
	const claimRef = "runbook:verify-tally/features/items.md#item-add"
	const wordRef = "runbook:verify-tally/features/items.md#word-only"
	const feature = ".cursor/skills/verify-tally/features/items.md"
	const claim = ".verilex/claims/item-added.yaml"
	for _, tc := range []struct {
		name, path, ref string
		unmapped        []string
	}{
		{"feature-and-source", feature, claimRef, nil},
		{"claim-and-source", claim, claimRef, nil},
		{"feature-and-word-ref", feature, wordRef, nil},
		{"claim-and-word-ref", claim, wordRef, nil},
		{"config-and-word-ref", "config:tally.store", wordRef, nil},
		{"unrelated-runbook", claim, "runbook:verify-tally/features/items.md#unrelated", []string{"runbook:verify-tally/features/items.md#unrelated"}},
		{"unrelated-path", claim, "unrelated.txt", []string{"unrelated.txt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := product(t)
			word := filepath.Join(root, ".verilex", "words", "item-stored", "word.md")
			replace(t, word, strings.Replace(read(t, word), "depends:\n", "depends:\n  runbook: [verify-tally/features/items.md#word-only]\n", 1))
			admitted(t, root)
			green(t, root, nil, chain)
			green(t, root, nil, chain)
			alone := planClaims(t, root, "--changed", wordRef)
			equal(t, alone.Selected, []string{"item-added"})
			equal(t, alone.Warning, "")
			changes := []string{"--changed", tc.path, "--changed", tc.ref}
			warning := ""
			if len(tc.unmapped) != 0 {
				warning = "1 change no word covers"
			}
			plan := planClaims(t, root, changes...)
			contains(t, strings.Join(plan.Selected, ","), "item-added")
			equal(t, plan.Unmapped, tc.unmapped)
			equal(t, plan.Warning, warning)
			for _, command := range []string{"plan", "run"} {
				human := verilex(t, root, nil, append([]string{command}, changes...)...)
				equal(t, human.code, 0)
				if warning != "" {
					contains(t, human.stdout, warning)
					contains(t, human.stdout, "unmapped  "+tc.ref)
				} else if strings.Contains(human.stdout, "unmapped") || strings.Contains(human.stdout, "change no word covers") {
					t.Fatalf("false warning: %s", human.stdout)
				}
			}
			type runRecord struct {
				Run, Verdict, Warning string
				Unmapped              []string
			}
			run := verilex(t, root, nil, append([]string{"run", "--json"}, changes...)...)
			equal(t, run.code, 0)
			var record runRecord
			if err := json.Unmarshal([]byte(run.stdout), &record); err != nil {
				t.Fatal(err)
			}
			equal(t, record.Verdict, "green")
			equal(t, record.Unmapped, tc.unmapped)
			equal(t, record.Warning, warning)
			runs := verilex(t, root, nil, "runs", "--json")
			equal(t, runs.code, 0)
			var records []runRecord
			if err := json.Unmarshal([]byte(runs.stdout), &records); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, saved := range records {
				if saved.Run == record.Run {
					found = true
					equal(t, saved.Verdict, "green")
					equal(t, saved.Warning, warning)
				}
			}
			if !found {
				t.Fatalf("run %s missing from runs: %s", record.Run, runs.stdout)
			}
		})
	}
}

// Rule: a diff entry no word depends on, a touched word with no claim, and a touched word the
// chain does not drive are each counted in the verdict's warning, never dropped beside a
// mapped entry. The tally here reads its defect from lib/mode, which no word declares.
func TestMixedDiffGapsWarn(t *testing.T) {
	root := curated(t)
	tally := filepath.Join(root, "bin", "tally")
	reads := strings.Replace(read(t, tally), `defect = os.environ.get("TALLY_DEFECT")`,
		`defect = os.environ.get("TALLY_DEFECT") or (Path(__file__).parent.parent / "lib" / "mode").read_text().strip()`, 1)
	if err := os.Remove(tally); err != nil {
		t.Fatal(err)
	}
	write(t, tally, reads, 0755)
	if err := os.MkdirAll(filepath.Join(root, "lib"), 0755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "lib", "mode"), "", 0644)
	green(t, root, nil, chain)
	green(t, root, nil, chain)
	replace(t, filepath.Join(root, "lib", "mode"), "drop-adds\n")

	equal(t, verilex(t, root, nil, "run", "--changed", "lib/mode").code, 2)
	mixed := verilex(t, root, nil, "run", "--changed", "lib/mode", "--changed", "runbook:verify-tally/features/items.md#item-add")
	contains(t, mixed.stdout, "with 1 change no word covers; run ")
	contains(t, mixed.stdout, "  unmapped  lib/mode: no word depends on it")
	asJSON := verilex(t, root, nil, "run", "--json", "--changed", "lib/mode", "--changed", "runbook:verify-tally/features/items.md#item-add")
	var record map[string]any
	if err := json.Unmarshal([]byte(asJSON.stdout), &record); err != nil {
		t.Fatal(err)
	}
	equal(t, record["unmapped"], any([]any{"lib/mode"}))
	equal(t, record["warning"], "1 change no word covers")
	contains(t, verilex(t, root, nil, "runs").stdout, "warning: 1 change no word covers")
	equal(t, planClaims(t, root, "--changed", "lib/mode", "--changed", "runbook:verify-tally/features/items.md#item-add").Unmapped, []string{"lib/mode"})

	typo := verilex(t, root, nil, "run", "--claim", "item-listed", "--changed", "config:tally.lsit")
	contains(t, typo.stdout, "with 1 change no word covers; run ")
	contains(t, typo.stdout, "  unmapped  config:tally.lsit")
	contains(t, verilex(t, root, nil, "run", "--changed", "config:tally.lsit", "--changed", "config:tally.open").stdout, "with 1 change no word covers; run ")

	verilex(t, root, nil, "new", "probe-word", "--implements", "verify-tally/features/items.md#item-add")
	stub := verilex(t, root, nil, "run", "--changed", ".verilex/words/probe-word/run", "--changed", ".verilex/words/item-listed/run")
	contains(t, stub.stdout, "with 1 touched word with no claim; run ")
	contains(t, verilex(t, root, nil, "runs").stdout, "warning: 1 touched word with no claim")
	chained := verilex(t, root, nil, "run", "--json", "store-open | probe-word", "--changed", ".verilex/words/probe-word/run")
	if strings.Contains(chained.stdout, `"unclaimed"`) {
		t.Fatalf("a claimless word the chain ran was reported as a gap: %s", chained.stdout)
	}
}

// Rule: plan and run share one force rule. A config key hit on a claim's word the chain does not
// use leaves the plan and the run agreeing, and the warning names that word as not run.
func TestForcedHitOnOtherWordOfClaim(t *testing.T) {
	root := curated(t)
	green(t, root, nil, chain)
	src, dst := filepath.Join(root, ".verilex", "words", "item-listed"), filepath.Join(root, ".verilex", "words", "item-listed-b")
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dst, "run"), read(t, filepath.Join(src, "run")), 0755)
	word := strings.Replace(read(t, filepath.Join(src, "word.md")), "word: item-listed\n", "word: item-listed-b\n", 1)
	write(t, filepath.Join(dst, "word.md"), strings.Replace(word, "config_keys: [tally.list]", "config_keys: [tally.listb]", 1), 0644)

	plan := planClaims(t, root, "--changed", "config:tally.listb")
	equal(t, plan.Selected, []string{"item-listed"})
	equal(t, plan.Unrun, []string{"item-listed-b"})
	equal(t, plan.Warning, "1 touched word not run")
	if len(plan.Run) != 0 || plan.Rerun != "" {
		t.Fatalf("plan says run while no chain word changed: run=%+v rerun=%q", plan.Run, plan.Rerun)
	}
	done := verilex(t, root, nil, "run", "--json", "--changed", "config:tally.listb")
	var record map[string]any
	if err := json.Unmarshal([]byte(done.stdout), &record); err != nil {
		t.Fatal(err)
	}
	equal(t, record["skipped"], true)
	equal(t, record["warning"], "1 touched word not run")
	equal(t, record["unrun"], any([]any{"item-listed-b"}))
	contains(t, verilex(t, root, nil, "run", "--changed", "config:tally.listb").stdout,
		"  unrun  item-listed-b: proves a claim this chain proves through another word; run a chain that uses it\n")
}

// Rule: a claim plan binds each word's arguments from the admitted onboarding chain. A newer run on
// other inputs never changes what the plan skips or runs, so the plan and the run cannot go green on them.
func TestPlanBindsAdmittedChainNotNewestRun(t *testing.T) {
	root := curated(t)
	proveOn(t, root, chain, "store-open", "item-stored", "item-listed")
	green(t, root, nil, chain)
	green(t, root, nil, "store-open | item-stored pear | item-listed pear")
	plan := planClaims(t, root, "--claim", "item-listed")
	equal(t, plan.Chain, chain)
	if len(plan.Skip) != 1 || plan.Skip[0].Word != "item-listed" {
		t.Fatalf("skip: %+v", plan.Skip)
	}
	contains(t, plan.Skip[0].Step, "item-listed apple")
}

// Rule: a word binds from its own admitted chain before another word's. item-stored is admitted on apple
// and item-listed on pear, so a run of item-added meets the apple defect and cannot go green on pear.
func TestRunClaimBindsEachWordFromItsOwnAdmittedChain(t *testing.T) {
	root := curated(t)
	pear := "store-open | item-stored pear | item-listed pear"
	proveOn(t, root, "store-open | item-stored apple", "store-open", "item-stored")
	proveOn(t, root, pear, "item-listed")
	green(t, root, nil, pear)
	plan := planClaims(t, root, "--claim", "item-added")
	equal(t, plan.Chain, "store-open | item-stored apple")
	done := verilex(t, root, map[string]string{"TALLY_DEFECT": "drop-apple"}, "run", "--claim", "item-added")
	if done.code != 1 {
		t.Fatalf("run --claim item-added with apple broken: exit %d\n%s", done.code, done.stdout)
	}
	contains(t, done.stdout, "red  item-added: tally said 'added apple' but store.json lacks apple")
}

// Rule: a plan whose claims were admitted on different inputs runs each claim's word on its own
// input. item-stored is admitted on apple and item-listed on pear, so the chain stores both: apple
// to prove item-added, pear only to provide the item item-listed lists.
func TestPlanRunsEachClaimOnItsOwnAdmittedInput(t *testing.T) {
	root := curated(t)
	proveOn(t, root, "store-open | item-stored apple", "store-open", "item-stored")
	proveOn(t, root, "store-open | item-stored pear | item-listed pear", "item-listed")
	both := "store-open | item-stored apple | item-stored pear | item-listed pear"
	plan := planClaims(t, root, "--claim", "item-added", "--claim", "item-listed")
	equal(t, plan.Chain, both)
	equal(t, plan.Order, []string{"store-opened", "item-added", "item-listed"})
	equal(t, planClaims(t, root, "--changed", "bin/tally").Chain, both)
	done := verilex(t, root, map[string]string{"TALLY_DEFECT": "drop-apple"}, "run", "--changed", "bin/tally")
	if done.code != 1 {
		t.Fatalf("run --changed bin/tally with apple broken: exit %d\n%s%s", done.code, done.stdout, done.stderr)
	}
	contains(t, done.stdout, "red  item-added: tally said 'added apple' but store.json lacks apple")
	if done = verilex(t, root, nil, "run", "--changed", "bin/tally"); done.code != 0 {
		t.Fatalf("healthy run --changed bin/tally: exit %d\n%s%s", done.code, done.stdout, done.stderr)
	}
}

// Rule: a word that runs on other arguments than its admitted ones only provides a state; it proves
// no claim. item-listed's plan stores pear, so neither it nor its run reports item-added, which
// only apple proves.
func TestProviderOnOtherInputProvesNoClaim(t *testing.T) {
	root := curated(t)
	pear := "store-open | item-stored pear | item-listed pear"
	proveOn(t, root, "store-open | item-stored apple", "store-open", "item-stored")
	proveOn(t, root, pear, "item-listed")
	plan := planClaims(t, root, "--claim", "item-listed")
	equal(t, plan.Chain, pear)
	equal(t, plan.Order, []string{"store-opened", "item-listed"})
	done := verilex(t, root, map[string]string{"TALLY_DEFECT": "drop-apple"}, "run", "--json", "--claim", "item-listed")
	var record runner.Record
	if err := json.Unmarshal([]byte(done.stdout), &record); err != nil || done.code != 0 {
		t.Fatalf("run --claim item-listed: exit %d %v\n%s", done.code, err, done.stdout)
	}
	var claims []string
	for _, c := range record.Claims {
		claims = append(claims, c.Claim+" "+c.Step+" "+string(c.Verdict))
	}
	equal(t, claims, []string{"store-opened store-open green", "item-listed item-listed pear green"})
}

// Rule: an admitted word binds its own step of its onboarded chain, even after another word of that
// chain is gone. A newer run on pear never rebinds it, so apple's defect is still found.
func TestAdmittedWordKeepsItsInputWhenItsChainLosesAWord(t *testing.T) {
	root := curated(t)
	proveOn(t, root, "store-open | item-stored apple | item-peek | item-listed apple", "store-open", "item-stored", "item-listed")
	green(t, root, nil, "store-open | item-stored pear | item-listed pear")
	equal(t, verilex(t, root, nil, "check").stdout, "check: no drift (3 admitted)\n")
	equal(t, planClaims(t, root, "--claim", "item-listed").Chain, chain)
	done := verilex(t, root, map[string]string{"TALLY_DEFECT": "hide-apple"}, "run", "--claim", "item-listed")
	if done.code != 1 {
		t.Fatalf("run --claim item-listed with apple hidden: exit %d\n%s%s", done.code, done.stdout, done.stderr)
	}
	contains(t, done.stdout, "red  item-listed")
}

// Rule: an admitted word whose onboarded chain no longer binds its arguments is drift-suspect, and a
// plan that selects it is refused: no other chain or run record may bind it instead.
func TestAdmittedWordWhoseChainNoLongerBindsItIsNeverRebound(t *testing.T) {
	root := curated(t)
	proveOn(t, root, "store-open | item-stored | item-listed apple", "item-stored")
	green(t, root, nil, "store-open | item-stored pear | item-listed pear")
	equal(t, verilex(t, root, nil, "check").stdout, "check: 1 of 3 admitted drift-suspect; they always run\n"+
		"  item-stored: its onboarded chain no longer binds its arguments, so claim plans refuse it; onboard it again\n")
	done := verilex(t, root, nil, "plan", "--claim", "item-added")
	equal(t, done.code, 2)
	equal(t, done.stderr, "verilex: refused: item-stored: its onboarded chain no longer binds its arguments; onboard it again\n")
	equal(t, planClaims(t, root, "--claim", "item-listed").Order, []string{"store-opened", "item-listed"})
}

// proveOn records the chain onboarding proved each named admitted word on.
func proveOn(t *testing.T, root, onChain string, words ...string) {
	t.Helper()
	project, err := dictionary.FindProject(root)
	if err != nil {
		t.Fatal(err)
	}
	g, err := grouping.Load(project)
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range words {
		d := g.Words[word]
		d.Chain = onChain
		g.Set(word, d)
	}
	if err = grouping.Save(project, g); err != nil {
		t.Fatal(err)
	}
}

// Rule: a plan whose provider would provide its own requirement by running again on other arguments
// ends in a refusal, never a loop. item-derived provides item:{name} and requires item:{name}x, so
// each step of it would need another.
func TestPlanRefusesAProviderThatOnlyItselfCanFeed(t *testing.T) {
	root := curated(t)
	proveOn(t, root, chain, "store-open", "item-stored", "item-listed")
	dir := filepath.Join(root, ".verilex", "words", "item-derived")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "word.md"), "---\nword: item-derived\npromise: An item derived from another item.\nargs: [name]\n"+
		"implements: [verify-tally/features/items.md#item-list]\nentry: cli\ninputs: [bin/tally]\n"+
		"requires: [\"item:{name}x\"]\nprovides: [\"item:{name}\"]\n---\n", 0644)
	write(t, filepath.Join(dir, "run"), "#!/bin/sh\ncat >/dev/null\nexit 1\n", 0755)
	for _, args := range [][]string{{"plan", "--claim", "item-listed"}, {"run", "--claim", "item-listed"}} {
		p := start(t, root, nil, args...)
		timer := time.AfterFunc(10*time.Second, func() { _ = p.cmd.Process.Kill() })
		done := p.wait(t)
		timer.Stop()
		equal(t, done, output{code: 2, stderr: "verilex: refused: word dependencies cycle: item-derived apple requires item:applex, " +
			"which item-derived would provide again on other arguments; no chain order is safe\n"})
	}
	equal(t, runCount(t, root), 0)
}

// Rule: a step that only provides a state proves no claim, but its failure is still a failure: the
// quiet output prints it, labelled, with its evidence, and leaves the claim it fed not run.
func TestFailingProviderStepIsPrintedAsAFailure(t *testing.T) {
	root := curated(t)
	proveOn(t, root, "store-open | item-stored apple", "store-open", "item-stored")
	proveOn(t, root, "store-open | item-stored pear | item-listed pear", "item-listed")
	done := verilex(t, root, map[string]string{"TALLY_DEFECT": "drop-adds"}, "run", "--claim", "item-listed")
	record := lastRun(t, root)
	equal(t, done.code, 1)
	equal(t, done.stdout, "red: 1 green, 1 red, 1 not run; run "+record.Run+"\n"+
		"  red  item-stored pear (only provides a state): tally said 'added pear' but store.json lacks pear\n"+
		"    evidence: "+record.Words[1].Evidence+"\n"+
		"  inconclusive  item-listed: not run\n"+
		"    next: verilex run --fresh --claim 'item-listed'\n")
	equal(t, record.Words[1].ProvidesOnly, true)
	var claims []string
	for _, c := range record.Claims {
		claims = append(claims, c.Claim+" "+string(c.Verdict))
	}
	equal(t, claims, []string{"store-opened green", "item-listed inconclusive"})
}

// Rule: an admitted word whose claim has a newer version is refused with that reason, the one
// onboarding gives too, not as a word to onboard again.
func TestPlanRefusesAStaleAdmittedWordWithItsPin(t *testing.T) {
	root := curated(t)
	proveOn(t, root, chain, "store-open", "item-stored", "item-listed")
	v1 := pinOf(t, root, "item-listed")
	edit(t, claimFile(root, "item-listed"), "observation: The listing matches store.json.", "observation: The listing matches store.json exactly.")
	v2 := pinOf(t, root, "item-listed")
	for _, cmd := range []string{"plan", "run"} {
		done := verilex(t, root, nil, cmd, "--claim", "item-listed")
		equal(t, done, output{code: 2, stderr: stale("item-listed", v1, v2)})
	}
	equal(t, verilex(t, root, nil, "onboard", "item-listed").stderr, stale("item-listed", v1, v2))
}

// Rule: a claim plan writes each argument the way the chain parser reads it, so an argument with a
// space stays one argument. item-stored onboarded on 'big apple' plans, runs and is then skipped on it.
func TestPlanKeepsAnArgumentWithASpaceWhole(t *testing.T) {
	root := product(t)
	onChain := "store-open | item-stored 'big apple'"
	used(t, root, onChain)
	used(t, root, onChain)
	onboard(t, root, "store-open")
	onboard(t, root, "item-stored")
	plan := planClaims(t, root, "--claim", "item-added")
	equal(t, plan.Chain, onChain)
	equal(t, plan.Order, []string{"store-opened", "item-added"})
	equal(t, plan.Run, []liveClaim{{Claim: "item-added", Word: "item-stored", Step: "item-stored big apple", Reason: "word admission changed"}})
	done := verilex(t, root, nil, "run", "--json", "--claim", "item-added")
	var record runner.Record
	if err := json.Unmarshal([]byte(done.stdout), &record); err != nil || done.code != 0 {
		t.Fatalf("run --claim item-added: exit %d %v\n%s%s", done.code, err, done.stdout, done.stderr)
	}
	equal(t, record.Chain, onChain)
	equal(t, string(*record.Verdict), "green")
	equal(t, record.Words[1].Args, []string{"big apple"})
	plan = planClaims(t, root, "--claim", "item-added")
	if len(plan.Skip) != 1 || plan.Skip[0].Claim != "item-added" || plan.Skip[0].ReliesOn != record.Run || len(plan.Run) != 0 {
		t.Fatalf("plan after green: skip %+v run %+v", plan.Skip, plan.Run)
	}
	done = verilex(t, root, nil, "run", "--claim", "item-added")
	equal(t, done.code, 0)
	contains(t, done.stdout, "green: 2 green, skipped: stamps match run "+record.Run+"; run ")
}
