package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DereKk8/verilex/internal/runner"
)

func runCount(t *testing.T, root string) int {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(filepath.Dir(root), "state", "tally", "runs", "*", "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	return len(paths)
}

func recordOf(t *testing.T, root, id string) runner.Record {
	t.Helper()
	record, err := runner.ReadRecord(filepath.Join(filepath.Dir(root), "state", "tally", "runs", id, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	return record
}

// plan prints the plan for a chain and checks that it ran and recorded nothing.
func plan(t *testing.T, root string, chain string, flags ...string) output {
	t.Helper()
	before := runCount(t, root)
	done := verilex(t, root, nil, append([]string{"plan", chain}, flags...)...)
	if runCount(t, root) != before {
		t.Fatal("plan recorded a run")
	}
	return done
}

func skips(id string, labels ...string) string {
	text := ""
	for _, label := range labels {
		text += "  skip  " + label + "  relies on run " + id + "\n"
	}
	return text
}

// age rewrites when every result in a JSON file was recorded: the ledger's entries, or a kept
// run's instance history.
func age(t *testing.T, path string, by time.Duration) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(read(t, path)), &doc); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-by).UTC().Format(time.RFC3339)
	switch entries := doc["entries"].(type) {
	case map[string]any:
		for _, entry := range entries {
			entry.(map[string]any)["recorded"] = at
		}
	default:
		for _, entry := range doc["history"].([]any) {
			entry.(map[string]any)["recorded"] = at
		}
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	write(t, path, string(data), 0600)
}

func ledgerPath(root string) string {
	return filepath.Join(filepath.Dir(root), "state", "tally", "runs", "ledger.json")
}

// Rule: plan runs the same skip decision as run and runs nothing. A change no stamp covers, such
// as docs, still skips, and the plan cites the run each step relies on.
func TestDocsOnlyChangeSkipsWithCitations(t *testing.T) {
	root := curated(t)
	first := green(t, root, nil, chain)
	appendTo(t, filepath.Join(root, ".cursor", "skills", "verify-tally", "SKILL.md"), "\nA note for maintainers.\n")
	write(t, filepath.Join(root, "README.md"), "# tally\n", 0644)

	done := plan(t, root, chain)
	equal(t, done.code, 0)
	equal(t, done.stdout, "plan: skip 3, run 0\n"+skips(first.Run, "store-open", "item-stored apple", "item-listed apple"))
	done = plan(t, root, chain, "--json")
	var decided runner.Plan
	if err := json.Unmarshal([]byte(done.stdout), &decided); err != nil {
		t.Fatal(err)
	}
	equal(t, decided.Skip, []runner.Skip{{Step: "store-open", Skipped: true, ReliesOn: first.Run}, {Step: "item-stored apple", Skipped: true, ReliesOn: first.Run}, {Step: "item-listed apple", Skipped: true, ReliesOn: first.Run}})
	equal(t, decided.Rerun, "")

	record := green(t, root, nil, chain)
	equal(t, record.Skipped, true)
	equal(t, len(record.Frame), 0)
	for _, word := range record.Words {
		equal(t, word.ReliesOn, first.Run)
	}
}

func TestWordScriptChangeRunsLive(t *testing.T) {
	root := curated(t)
	green(t, root, nil, chain)
	appendTo(t, filepath.Join(root, ".verilex", "words", "item-listed", "run"), "# edited\n")
	admitted(t, root, "item-listed")

	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; item-listed apple: word changed\n")
	record := green(t, root, nil, chain)
	equal(t, record.Rerun, "item-listed apple: word changed")
	ranLive(t, record)
	equal(t, plan(t, root, chain).stdout, "plan: skip 3, run 0\n"+skips(record.Run, "store-open", "item-stored apple", "item-listed apple"))
}

// Rule: a recorded green result older than seven days is never reused.
func TestExpiredResultRunsLive(t *testing.T) {
	root := curated(t)
	first := green(t, root, nil, chain)
	age(t, ledgerPath(root), 6*24*time.Hour)
	equal(t, plan(t, root, chain).stdout, "plan: skip 3, run 0\n"+skips(first.Run, "store-open", "item-stored apple", "item-listed apple"))

	age(t, ledgerPath(root), 8*24*time.Hour)
	reason := "store-open: the green result from run " + first.Run + " expired (8d old; results stand 7d)"
	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; "+reason+"\n")
	record := green(t, root, nil, chain)
	equal(t, record.Rerun, reason)
	ranLive(t, record)
	equal(t, green(t, root, nil, chain).Skipped, true)

	// An instance kept longer than that proves nothing either, and its effects stay in place,
	// so continuing it is refused.
	kept := green(t, root, nil, chain, "--keep")
	age(t, filepath.Join(kept.Dir, "run.json"), 8*24*time.Hour)
	done := verilex(t, root, nil, "run", chain, "--continue", kept.Run)
	equal(t, done.code, 2)
	contains(t, done.stderr, "verilex: refused: store-open: the green result from run "+kept.Run+" expired (8d old; results stand 7d); the kept instance already holds the effects of store-open")
	equal(t, verilex(t, root, nil, "cleanup", kept.Run).stdout, "cleanup: done\n")
}

// Rule: --continue refreshes a kept instance, asks the doctor, skips what the instance already
// proves and runs only the rest on it.
func TestContinueRunsOnlyTheChangedWord(t *testing.T) {
	root := curated(t)
	kept := green(t, root, nil, chain, "--keep")
	store := stores(t, root)
	equal(t, len(store), 1)
	appendTo(t, filepath.Join(root, ".verilex", "words", "item-listed", "run"), "# edited\n")
	admitted(t, root, "item-listed")

	equal(t, plan(t, root, chain, "--continue", kept.Run).stdout,
		"plan: skip 2, run 1 on the instance kept by "+kept.Run+"; item-listed apple: word changed\n"+skips(kept.Run, "store-open", "item-stored apple"))
	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; item-listed apple: word changed\n")

	continued := green(t, root, nil, chain, "--continue", kept.Run, "--keep")
	equal(t, continued.Continues, kept.Run)
	equal(t, continued.Rerun, "item-listed apple: word changed")
	equal(t, frames(continued), []string{"refresh", "doctor"})
	equal(t, verdicts(continued), []string{"green", "green", "green"})
	equal(t, continued.Words[0].ReliesOn, kept.Run)
	equal(t, continued.Words[1].ReliesOn, kept.Run)
	equal(t, continued.Words[0].Evidence, kept.Words[0].Evidence)
	equal(t, continued.Words[2].ReliesOn, "")
	if !strings.HasPrefix(continued.Words[2].Evidence, continued.Dir+string(filepath.Separator)) {
		t.Fatalf("item-listed did not run in %s: %s", continued.Run, continued.Words[2].Evidence)
	}
	equal(t, read(t, filepath.Join(continued.Words[2].Evidence, "actions.log")), "$ tally list\nexit 0\napple\n\n")
	equal(t, stores(t, root), store)

	// The kept instance now belongs to the continuing run.
	equal(t, recordOf(t, root, kept.Run).Cleanup, "continued")
	done := verilex(t, root, nil, "cleanup", kept.Run)
	equal(t, done.code, 2)
	equal(t, done.stderr, "verilex: refused: "+kept.Run+" was continued by "+continued.Run+"; clean up that run instead\n")
	done = verilex(t, root, nil, "run", chain, "--continue", kept.Run)
	equal(t, done.stderr, "verilex: refused: "+kept.Run+" was continued by "+continued.Run+"; continue that run instead\n")

	// A result proven on a continued instance never stands for a fresh one.
	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; item-listed apple: word changed\n")

	// Continuing again runs no word, then cleans the instance up.
	done = verilex(t, root, nil, "run", chain, "--continue", continued.Run)
	equal(t, done.code, 0)
	again := lastRunOf(t, root, func(r runner.Record) bool { return r.Continues == continued.Run })
	equal(t, done.stdout, "green: 3 green, continued "+continued.Run+", 3 skipped: proven on its instance by runs "+kept.Run+", "+continued.Run+"; run "+again.Run+"\n")
	equal(t, frames(again), []string{"refresh", "doctor", "cleanup"})
	equal(t, again.Cleanup, "done")
	equal(t, stores(t, root), []string{})
}

// A word that changes state is never run twice on one instance: when such a word must run live
// on a kept instance that already holds its effects, --continue refuses before touching anything.
func TestContinueRefusesToRunAStateChangingWordAgain(t *testing.T) {
	root := curated(t)
	kept := green(t, root, nil, chain, "--keep")
	appendTo(t, filepath.Join(root, ".verilex", "words", "item-stored", "run"), "# edited\n")
	admitted(t, root, "item-stored")
	refusal := "verilex: refused: item-stored apple: word changed; the kept instance already holds the effects of item-stored apple from run " + kept.Run + ", and a word that changes state never runs twice or out of order on one instance: run without --continue\n"
	done := plan(t, root, chain, "--continue", kept.Run)
	equal(t, done.code, 2)
	equal(t, done.stderr, refusal)
	before := runCount(t, root)
	done = verilex(t, root, nil, "run", chain, "--continue", kept.Run)
	equal(t, done.code, 2)
	equal(t, done.stderr, refusal)
	equal(t, runCount(t, root), before)
	equal(t, recordOf(t, root, kept.Run).Cleanup, "kept")

	equal(t, verilex(t, root, nil, "cleanup", kept.Run).stdout, "cleanup: done\n")

	// A chain that only adds words after what the instance holds runs them on it.
	root = curated(t)
	kept = green(t, root, nil, "store-open", "--keep")
	longer := green(t, root, nil, chain, "--continue", kept.Run)
	equal(t, longer.Rerun, "item-stored apple: not run on the kept instance yet")
	equal(t, longer.Words[0].ReliesOn, kept.Run)
	equal(t, longer.Words[1].ReliesOn, "")
	equal(t, longer.Words[2].ReliesOn, "")
	equal(t, stores(t, root), []string{})
}

// Rule: a provisional or drift-suspect word never skips, in plan, run or --continue.
func TestDriftSuspectWordNeverSkips(t *testing.T) {
	root := curated(t)
	kept := green(t, root, nil, chain, "--keep")
	items := feature(root, "items.md")
	original := read(t, items)
	changeAddRequirement(t, root)
	equal(t, status(t, root, "item-stored"), "drift-suspect")
	held := "item-stored apple: drift-suspect: " + reviewAdd

	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; "+held+"\n")
	done := plan(t, root, chain, "--continue", kept.Run)
	equal(t, done.code, 2)
	contains(t, done.stderr, "verilex: refused: "+held+"; the kept instance already holds the effects of item-stored apple")
	record := green(t, root, nil, chain)
	equal(t, record.Rerun, held)
	ranLive(t, record)
	equal(t, plan(t, root, chain).stdout, "plan: skip 0, run 3; "+held+"\n")

	// A read-only word that must run live (here provisional again) runs on the kept instance:
	// it changes nothing there.
	write(t, items, original, 0644)
	equal(t, status(t, root, "item-stored"), "admitted")
	if err := os.Remove(filepath.Join(root, ".verilex", "words", "item-listed", "admission.json")); err != nil {
		t.Fatal(err)
	}
	continued := green(t, root, nil, chain, "--continue", kept.Run)
	equal(t, continued.Rerun, "item-listed apple: word changed")
	equal(t, continued.Words[1].ReliesOn, kept.Run)
	equal(t, continued.Words[2].ReliesOn, "")
}

func TestContinueRefusalsAndRefreshFailure(t *testing.T) {
	root := curated(t)
	done := verilex(t, root, nil, "run", chain, "--continue", "no-such-run")
	equal(t, done.stderr, "verilex: refused: no-such-run is not a run of tally\n")
	done = verilex(t, root, nil, "plan", chain, "--continue", "../tally")
	equal(t, done.stderr, "verilex: refused: ../tally is not a run of tally\n")
	first := green(t, root, nil, chain)
	done = verilex(t, root, nil, "run", chain, "--continue", first.Run)
	equal(t, done.stderr, "verilex: refused: "+first.Run+" kept no instance (cleanup=done); run with --keep first\n")

	kept := green(t, root, nil, chain, "--keep")
	done = verilex(t, root, nil, "run", chain, "--continue", kept.Run, "--fresh")
	equal(t, done.stderr, "verilex: refused: --fresh would run every word again on the kept instance; run without --continue\n")
	refresh := filepath.Join(root, ".verilex", "frame", "refresh")
	saved := read(t, refresh)
	if err := os.Remove(refresh); err != nil {
		t.Fatal(err)
	}
	done = verilex(t, root, nil, "run", chain, "--continue", kept.Run)
	equal(t, done.stderr, "verilex: refused: missing executable frame step "+refresh+"; --continue needs it\n")

	// A refresh that fails makes the run inconclusive, and cleanup still tears the instance down.
	write(t, refresh, saved, 0755)
	write(t, filepath.Join(filepath.Dir(root), "stores", stores(t, root)[0], "store.json"), "{not json", 0600)
	done = verilex(t, root, nil, "run", chain, "--continue", kept.Run)
	equal(t, done.code, 2)
	record := lastRunOf(t, root, func(r runner.Record) bool { return r.Continues == kept.Run })
	equal(t, *record.Reason, "refresh exited 1")
	equal(t, frames(record), []string{"refresh", "cleanup"})
	equal(t, done.stdout, "inconclusive: 0 green, 3 not run, continued "+kept.Run+"; run "+record.Run+"\n"+
		"  inconclusive  refresh: exit 1\n    evidence: "+record.Frame[0].Evidence+"\n")
	equal(t, record.Cleanup, "done")
	equal(t, stores(t, root), []string{})
}

func TestReadOnlyWordProvidesNothing(t *testing.T) {
	root := product(t)
	path := filepath.Join(root, ".verilex", "words", "item-stored", "word.md")
	write(t, path, strings.Replace(read(t, path), "entry: cli", "entry: cli\nprovides: [shelf]\nread_only: true", 1), 0644)
	done := verilex(t, root, nil, "words")
	equal(t, done.code, 2)
	contains(t, done.stderr, "a 'read_only' word changes nothing, so it provides no states")
}
