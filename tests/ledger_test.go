package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DereKk8/verilex/internal/runner"
)

// ledgerDir is the ledger a test product's runs keep by default, under their state home.
func ledgerDir(root string) string {
	return filepath.Join(filepath.Dir(root), "state", "tally", "ledger")
}

// recordedPass is one pass file of a ledger, as it lies on disk.
type recordedPass struct {
	path, ledger string
	fields       map[string]any
}

func (p recordedPass) evidence() string {
	return filepath.Join(p.ledger, filepath.FromSlash(p.fields["evidence"].(string)))
}

// passes reads every pass file of the ledger in dir.
func passes(t *testing.T, dir string) []recordedPass {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "passes", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	result := []recordedPass{}
	for _, path := range paths {
		p := recordedPass{path: path, ledger: dir}
		if err = json.Unmarshal([]byte(read(t, path)), &p.fields); err != nil {
			t.Fatal(err)
		}
		result = append(result, p)
	}
	return result
}

// passFor returns the one pass a ledger holds for a step.
func passFor(t *testing.T, dir, label string) recordedPass {
	t.Helper()
	found := []recordedPass{}
	for _, p := range passes(t, dir) {
		if p.fields["label"] == label {
			found = append(found, p)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d passes for %s", len(found), label)
	}
	return found[0]
}

// reseal rewrites a pass under the name its new content earns, as a writer that bypasses
// verilex would.
func reseal(t *testing.T, p recordedPass, change func(fields map[string]any)) {
	t.Helper()
	change(p.fields)
	data, err := json.Marshal(p.fields)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(p.path); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(filepath.Dir(p.path), p.fields["stamp"].(string)+"."+sha(string(data))+".json"), string(data), 0600)
}

// replace gives a file new content without touching the file it may share an inode with.
func replace(t *testing.T, path, text string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	write(t, path, text, 0600)
}

func sha(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Rule: stateless instances, each with a state home of its own, share one ledger through
// VERILEX_LEDGER. Concurrent runs record every pass they prove, with none lost and none forged,
// and any instance pointed at the ledger reuses them.
func TestStatelessInstancesShareOneLedger(t *testing.T) {
	root := curated(t)
	shared := filepath.Join(t.TempDir(), "ledger")
	instance := func(name string) map[string]string {
		return map[string]string{"VERILEX_HOME": filepath.Join(filepath.Dir(root), "instances", name), "VERILEX_LEDGER": shared}
	}
	fruits := []string{"apple", "pear", "plum"}
	chainOf := func(fruit string) string { return "store-open | item-stored " + fruit + " | item-listed " + fruit }
	started := []*process{}
	for i := range 6 {
		started = append(started, start(t, root, instance(fmt.Sprint(i)), "run", chainOf(fruits[i%3]), "--json"))
	}
	type proof struct{ run, label, stamp, stdout string }
	proven := []proof{}
	byFruit := map[string][]string{}
	for i, p := range started {
		done := p.wait(t)
		var record runner.Record
		if err := json.Unmarshal([]byte(done.stdout), &record); err != nil {
			t.Fatalf("%v: %+v", err, done)
		}
		equal(t, done.code, 0)
		ranLive(t, record)
		byFruit[fruits[i%3]] = append(byFruit[fruits[i%3]], record.Run)
		for _, word := range record.Words {
			proven = append(proven, proof{record.Run, strings.Join(append([]string{word.Word}, word.Args...), " "), word.Stamp, read(t, filepath.Join(word.Evidence, "stdout"))})
		}
	}
	recorded := []proof{}
	slots := map[string]int{}
	for _, p := range passes(t, filepath.Join(shared, "tally")) {
		equal(t, filepath.Base(p.path), p.fields["stamp"].(string)+"."+sha(read(t, p.path))+".json")
		equal(t, p.fields["owner"], p.fields["run"])
		recorded = append(recorded, proof{p.fields["run"].(string), p.fields["label"].(string), p.fields["stamp"].(string), read(t, filepath.Join(p.evidence(), "stdout"))})
		slots[filepath.Base(filepath.Dir(p.path))]++
	}
	order := func(a, b proof) int { return strings.Compare(a.run+a.label, b.run+b.label) }
	slices.SortFunc(proven, order)
	slices.SortFunc(recorded, order)
	equal(t, len(proven), 18)
	equal(t, recorded, proven)
	counts := []int{}
	for _, n := range slots {
		counts = append(counts, n)
	}
	slices.Sort(counts)
	// store-open's slot holds a pass from each of the six runs, side by side.
	equal(t, counts, []int{2, 2, 2, 2, 2, 2, 6})

	// A new instance reuses those passes and launches nothing.
	for _, fruit := range fruits {
		record := green(t, root, instance("new"), chainOf(fruit))
		equal(t, record.Skipped, true)
		equal(t, len(record.Frame), 0)
		for _, word := range record.Words[1:] {
			if !slices.Contains(byFruit[fruit], word.ReliesOn) {
				t.Fatalf("%s relies on %s, not a run of its chain %v", word.Word, word.ReliesOn, byFruit[fruit])
			}
		}
	}
	equal(t, stores(t, root), []string{})
	// An instance outside the shared ledger keeps a ledger of its own, which proves nothing yet.
	alone := map[string]string{"VERILEX_HOME": filepath.Join(filepath.Dir(root), "instances", "alone")}
	equal(t, verilex(t, root, alone, "plan", chainOf("apple")).stdout, "plan: skip 0, run 3; store-open: no green result on record\n")
}

// Rule: a reader trusts no pass file. A pass stands only when it matches its digest, proved the
// step's claim version on an instance its run launched, and its evidence still meets the
// evidence contract. A refused pass makes the chain run live, and the fresh pass then stands.
func TestPassesThatDoNotStandAreRefused(t *testing.T) {
	cases := []struct {
		name   string
		tamper func(t *testing.T, root string, p recordedPass)
		reason func(run, pin string) string
	}{
		{"recorded on an instance its run did not launch", func(t *testing.T, root string, p recordedPass) {
			reseal(t, p, func(fields map[string]any) { fields["owner"] = "1767225600-a1b2c3d4e5f6" })
		}, func(run, pin string) string {
			return "the pass from run " + run + " was recorded on an instance run 1767225600-a1b2c3d4e5f6 launched; only a run that launched its own instance records a pass"
		}},
		{"another claim version", func(t *testing.T, root string, p recordedPass) {
			reseal(t, p, func(fields map[string]any) { fields["claim"] = "item-added@000000000000" })
		}, func(run, pin string) string {
			return "the pass from run " + run + " proved item-added@000000000000, not " + pin + "; a pass proves only the claim version it ran against"
		}},
		{"evidence without a second observation", func(t *testing.T, root string, p recordedPass) {
			stdout := `{"verdict": "pass"}` + "\n"
			replace(t, filepath.Join(p.evidence(), "stdout"), stdout)
			reseal(t, p, func(fields map[string]any) { fields["result"] = sha(stdout) })
		}, func(run, pin string) string {
			return "the pass from run " + run + " misses the evidence contract: pass without a second observation"
		}},
		{"evidence whose exit code disagrees", func(t *testing.T, root string, p recordedPass) {
			replace(t, filepath.Join(p.evidence(), "exit"), "1\n")
		}, func(run, pin string) string {
			return "the pass from run " + run + " misses the evidence contract: exit 1 disagrees with verdict pass"
		}},
		{"evidence changed after recording", func(t *testing.T, root string, p recordedPass) {
			replace(t, filepath.Join(p.evidence(), "stdout"), `{"verdict": "pass", "observation": "trust me"}`+"\n")
		}, func(run, pin string) string {
			return "the evidence of the pass from run " + run + " changed after it was recorded"
		}},
		{"pass edited in place", func(t *testing.T, root string, p recordedPass) {
			write(t, p.path, strings.Replace(read(t, p.path), `"recorded": "`, `"recorded": "2`, 1), 0600)
		}, func(run, pin string) string {
			return "a pass on record is damaged: its content does not match its digest"
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := curated(t)
			first := green(t, root, nil, chain)
			c.tamper(t, root, passFor(t, ledgerDir(root), "item-stored apple"))
			reason := "item-stored apple: " + c.reason(first.Run, pinOf(t, root, "item-added"))
			equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; "+reason+"\n")
			record := green(t, root, nil, chain)
			equal(t, record.Rerun, reason)
			ranLive(t, record)
			again := green(t, root, nil, chain)
			equal(t, again.Skipped, true)
			equal(t, again.Words[1].ReliesOn, record.Run)
		})
	}
}

func TestUnreadableLedgerRunsLive(t *testing.T) {
	root := curated(t)
	green(t, root, nil, chain)
	passesDir := filepath.Join(ledgerDir(root), "passes")
	if err := os.RemoveAll(passesDir); err != nil {
		t.Fatal(err)
	}
	write(t, passesDir, "not a directory\n", 0600)
	done := plan(t, root, chain)
	contains(t, done.stdout, "plan: skip 0, run 3; the ledger is unreadable: open "+passesDir+string(filepath.Separator))
	contains(t, done.stdout, ": not a directory\n")
	_, record := runJSON(t, root, nil, chain)
	equal(t, *record.Verdict, "green")
	ranLive(t, record)
}

// stranded plants a temporary entry of the shape verilex names its own, holding a file, that is
// old enough for prune to sweep.
func stranded(t *testing.T, dir string) string {
	t.Helper()
	return strandedAs(t, dir, ".tmp-4242")
}

// strandedAs plants an old temporary entry named name, holding a file, and returns that file.
func strandedAs(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Join(path, "keep"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(path, "keep", "f"), "not verilex's\n", 0600)
	old := time.Now().Add(-48 * time.Hour)
	for _, p := range []string{filepath.Join(path, "keep", "f"), filepath.Join(path, "keep"), path} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(path, "keep", "f")
}

// Rule: pruning deletes only inside the ledger's real directories. When the ledger, its
// evidence/ or its passes/ is a symlink, verilex still reads and writes through it, but prune
// follows no link, so old temporary entries where it points are kept.
func TestPruneNeverFollowsALinkOutOfTheLedger(t *testing.T) {
	for _, linked := range []string{"evidence", "passes", "."} {
		t.Run(linked, func(t *testing.T) {
			root := curated(t)
			shared := filepath.Join(t.TempDir(), "ledger")
			env := map[string]string{"VERILEX_LEDGER": shared}
			green(t, root, env, chain)
			ledger := filepath.Join(shared, "tally")
			inside, outside := filepath.Join(ledger, linked), filepath.Join(t.TempDir(), "elsewhere")
			if err := os.Rename(inside, outside); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, inside); err != nil {
				t.Fatal(err)
			}
			kept := []string{stranded(t, outside)}
			if linked == "passes" {
				// Every real directory under a linked passes/ looks like a slot.
				if err := os.Mkdir(filepath.Join(outside, "x"), 0700); err != nil {
					t.Fatal(err)
				}
				kept = append(kept, stranded(t, filepath.Join(outside, "x")))
			}
			// The ledger's own real root is still swept.
			swept := stranded(t, ledger)
			green(t, root, env, "store-open | item-stored pear | item-listed pear")
			for _, path := range kept {
				equal(t, read(t, path), "not verilex's\n")
			}
			if _, err := os.Stat(swept); linked != "." && !os.IsNotExist(err) {
				t.Fatalf("prune kept %s inside the ledger: %v", swept, err)
			}
			equal(t, green(t, root, env, chain).Skipped, true)
		})
	}
}

// Rule: prune sweeps only the temporary names verilex itself creates. Old .tmp- entries another
// tool keeps in the ledger's directories survive every recording run.
func TestPruneKeepsTemporaryEntriesVerilexDidNotName(t *testing.T) {
	root := curated(t)
	shared := filepath.Join(t.TempDir(), "ledger")
	env := map[string]string{"VERILEX_LEDGER": shared}
	green(t, root, env, chain)
	ledger := filepath.Join(shared, "tally")
	slots, err := filepath.Glob(filepath.Join(ledger, "passes", "*"))
	if err != nil || len(slots) == 0 {
		t.Fatalf("no slot: %v", err)
	}
	kept, swept := []string{}, []string{}
	for _, dir := range []string{ledger, filepath.Join(ledger, "evidence"), slots[0]} {
		for _, name := range []string{".tmp-cache", ".tmp-", ".tmp-12x", ".tmp-clock-", ".tmp-clock-a1"} {
			kept = append(kept, strandedAs(t, dir, name))
		}
		swept = append(swept, stranded(t, dir), strandedAs(t, dir, ".tmp-clock-77"))
	}
	green(t, root, env, "store-open | item-stored pear | item-listed pear")
	for _, path := range kept {
		equal(t, read(t, path), "not verilex's\n")
	}
	for _, path := range swept {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("prune kept %s: %v", path, err)
		}
	}
}

// Rule: a project name is a plain name of one directory: . and .. are refused, so the ledger
// never lands on, or prunes, the directory around VERILEX_LEDGER.
func TestProjectNamedDotIsRefused(t *testing.T) {
	for _, name := range []string{".", ".."} {
		t.Run(name, func(t *testing.T) {
			root := curated(t)
			shared := filepath.Join(t.TempDir(), "ledger")
			// Where the project's ledger would land: VERILEX_LEDGER itself, or the directory around it.
			victim := stranded(t, filepath.Join(shared, name))
			config := filepath.Join(root, ".verilex", "config.yaml")
			edit(t, config, "project: tally", "project: "+name)
			done := verilex(t, root, map[string]string{"VERILEX_LEDGER": shared}, "run", chain)
			equal(t, done.code, 2)
			equal(t, done.stderr, "verilex: refused: "+config+": 'project' must be a plain name\n")
			equal(t, read(t, victim), "not verilex's\n")
		})
	}
}
