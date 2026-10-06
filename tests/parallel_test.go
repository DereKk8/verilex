package tests

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DereKk8/verilex/internal/runner"
)

// addHeldWord adds store-held, a word that reports which store it drove and whose owner label
// it found there, then holds its run until the test releases it, so several runs are in flight
// at once. It marks itself held by touching $HOLDING/<run> and waits for $HOLDING/<run>.go.
func addHeldWord(t *testing.T, root string) {
	t.Helper()
	addWord(t, root, "store-held", `store=$(python3 -c 'import json, os; print(json.loads(os.environ["VERILEX_INSTANCE"])["store"])')
owner=$(cat "$store/owner")
touch "$HOLDING/$VERILEX_RUN"
while [ ! -e "$HOLDING/$VERILEX_RUN.go" ]; do sleep 0.02; done
echo "{\"verdict\": \"pass\", \"observation\": \"drove $store owned by $owner\"}"
`)
	path := filepath.Join(root, ".verilex", "words", "store-held", "word.md")
	write(t, path, strings.Replace(read(t, path), "promise: A test word.", "promise: A test word.\ntimeout: 60", 1), 0644)
}

// held waits until n runs hold in store-held and returns their ids in order.
func held(t *testing.T, holding string, n int) []string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		names, err := filepath.Glob(filepath.Join(holding, "*"))
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, name := range names {
			if !strings.HasSuffix(name, ".go") {
				ids = append(ids, filepath.Base(name))
			}
		}
		if len(ids) == n {
			slices.Sort(ids)
			return ids
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d runs hold: %v", len(ids), n, ids)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func release(t *testing.T, holding, id string) {
	t.Helper()
	write(t, filepath.Join(holding, id+".go"), "", 0644)
}

func storeOf(root, id string) string {
	return filepath.Join(filepath.Dir(root), "stores", "tally-"+id)
}

// Rule: concurrent runs against one product each launch and drive an instance of their own. No
// run drives, takes over or tears down an instance another run holds, and one run's cleanup
// leaves every other run's instance alone.
func TestConcurrentRunsEachDriveOnlyTheirOwnInstance(t *testing.T) {
	root := product(t)
	addHeldWord(t, root)
	holding := t.TempDir()
	env := map[string]string{"HOLDING": holding}
	const n = 4
	runs := []*process{}
	for range n {
		runs = append(runs, start(t, root, env, "run", "store-open | store-held | item-stored apple", "--json"))
	}
	ids := held(t, holding, n)
	stored := []string{}
	for _, id := range ids {
		stored = append(stored, "tally-"+id)
		equal(t, read(t, filepath.Join(storeOf(root, id), "owner")), id+"\n")
	}
	equal(t, stores(t, root), stored)

	// While a run goes, tearing its instance down or taking it over is refused before anything runs.
	victim := ids[0]
	busy := "verilex: refused: " + victim + " is still running and owns its instance; wait until it finishes\n"
	before := runCount(t, root)
	done := verilex(t, root, nil, "cleanup", victim)
	equal(t, done.code, 2)
	equal(t, done.stderr, busy)
	done = verilex(t, root, nil, "run", "store-open | store-held", "--continue", victim)
	equal(t, done.code, 2)
	equal(t, done.stderr, busy)
	equal(t, runCount(t, root), before)

	// A launch that hands back another run's store is refused by the doctor before any word runs,
	// and its cleanup leaves that store alone.
	done, adopted := runJSON(t, root, map[string]string{"TALLY_ADOPT_STORE": storeOf(root, victim)}, "store-open | item-stored apple")
	equal(t, done.code, 2)
	equal(t, *adopted.Reason, "doctor refused the instance (exit 1)")
	equal(t, len(adopted.Words), 0)
	equal(t, frames(adopted), []string{"launch", "doctor", "cleanup"})
	equal(t, read(t, filepath.Join(adopted.Frame[2].Evidence, "stdout")), "left "+storeOf(root, victim)+" alone: not owned by run "+adopted.Run+"\n")
	equal(t, read(t, filepath.Join(storeOf(root, victim), "store.json")), `{"items": []}`)
	equal(t, stores(t, root), stored)

	// Releasing one run lets it finish and tear down its own store; every other run keeps its own.
	release(t, holding, victim)
	deadline := time.Now().Add(30 * time.Second)
	for recordOf(t, root, victim).Cleanup != "done" {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not finish", victim)
		}
		time.Sleep(20 * time.Millisecond)
	}
	equal(t, stores(t, root), stored[1:])
	for _, id := range ids[1:] {
		equal(t, read(t, filepath.Join(storeOf(root, id), "owner")), id+"\n")
		equal(t, read(t, filepath.Join(storeOf(root, id), "store.json")), `{"items": []}`)
		if recordOf(t, root, id).Verdict != nil {
			t.Fatalf("%s finished before it was released", id)
		}
	}

	for _, id := range ids[1:] {
		release(t, holding, id)
	}
	finished := []string{}
	for _, p := range runs {
		done := p.wait(t)
		equal(t, done.code, 0)
		var record runner.Record
		if err := json.Unmarshal([]byte(done.stdout), &record); err != nil {
			t.Fatal(err)
		}
		equal(t, verdicts(record), []string{"green", "green", "green"})
		equal(t, record.Words[1].Observation, any("drove "+storeOf(root, record.Run)+" owned by "+record.Run))
		equal(t, record.Cleanup, "done")
		finished = append(finished, record.Run)
	}
	slices.Sort(finished)
	equal(t, finished, ids)
	equal(t, stores(t, root), []string{})
}

// Rule: a kept instance goes to exactly one continuing run. Every other attempt to take it over
// is refused before anything starts and leaves no run behind.
func TestKeptInstanceGoesToExactlyOneContinuingRun(t *testing.T) {
	root := curated(t)
	kept := green(t, root, nil, chain, "--keep")
	before := runCount(t, root)
	const n = 6
	attempts := []*process{}
	for range n {
		attempts = append(attempts, start(t, root, nil, "run", chain, "--continue", kept.Run))
	}
	results := []output{}
	for _, p := range attempts {
		results = append(results, p.wait(t))
	}
	equal(t, runCount(t, root), before+1)
	winner := lastRunOf(t, root, func(r runner.Record) bool { return r.Continues == kept.Run })
	equal(t, *winner.Verdict, "green")
	equal(t, winner.Cleanup, "done")
	refusals := []string{
		"verilex: refused: " + kept.Run + " was continued by " + winner.Run + "; continue that run instead\n",
		"verilex: refused: " + kept.Run + "'s instance is in use by another verilex command; try again once it finishes\n",
	}
	won := 0
	for _, done := range results {
		if done.code == 0 {
			won++
			equal(t, done.stdout, "green: 3 green, continued "+kept.Run+", 3 skipped: proven on its instance by run "+kept.Run+"; run "+winner.Run+"\n")
			continue
		}
		if done.code != 2 || !slices.Contains(refusals, done.stderr) {
			t.Fatalf("unexpected refusal: %+v", done)
		}
	}
	equal(t, won, 1)
	equal(t, recordOf(t, root, kept.Run).ContinuedBy, winner.Run)
	equal(t, stores(t, root), []string{})
}

// Liveness is the run's own process, not its record: once a run dies mid-chain, its instance is
// nobody's, and `verilex cleanup` may tear it down.
func TestCleanupTearsDownTheInstanceOfARunThatDied(t *testing.T) {
	root := product(t)
	addHeldWord(t, root)
	holding := t.TempDir()
	p := start(t, root, map[string]string{"HOLDING": holding}, "run", "store-open | store-held")
	id := held(t, holding, 1)[0]
	equal(t, verilex(t, root, nil, "cleanup", id).stderr, "verilex: refused: "+id+" is still running and owns its instance; wait until it finishes\n")
	if err := p.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	p.wait(t)
	release(t, holding, id)
	record := recordOf(t, root, id)
	equal(t, record.Cleanup, "pending")
	equal(t, stores(t, root), []string{"tally-" + id})

	done := verilex(t, root, nil, "cleanup", id)
	equal(t, done.code, 0)
	equal(t, done.stdout, "cleanup: done\n")
	equal(t, stores(t, root), []string{})
	equal(t, recordOf(t, root, id).Cleanup, "done")
}
