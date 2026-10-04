package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DereKk8/verilex/internal/runner"
)

// runJSON runs a chain with --json and returns the process output and the record it printed.
func runJSON(t *testing.T, root string, env map[string]string, chain string, flags ...string) (output, runner.Record) {
	t.Helper()
	done := verilex(t, root, env, append([]string{"run", chain, "--json"}, flags...)...)
	var record runner.Record
	if err := json.Unmarshal([]byte(done.stdout), &record); err != nil {
		t.Fatalf("%v: %+v", err, done)
	}
	return done, record
}

// green runs a chain that must come out green and returns its record.
func green(t *testing.T, root string, env map[string]string, chain string, flags ...string) runner.Record {
	t.Helper()
	done, record := runJSON(t, root, env, chain, flags...)
	if done.code != 0 || *record.Verdict != "green" {
		t.Fatalf("not green: %+v", done)
	}
	return record
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	write(t, path, read(t, path)+text, info.Mode().Perm())
}

// ranLive asserts that every word of the chain ran in this run, with fresh evidence.
func ranLive(t *testing.T, record runner.Record) {
	t.Helper()
	if record.Skipped {
		t.Fatalf("run %s was skipped", record.Run)
	}
	equal(t, frames(record)[0], "launch")
	for _, word := range record.Words {
		if word.ReliesOn != "" || !strings.HasPrefix(word.Evidence, record.Dir+string(filepath.Separator)) {
			t.Fatalf("%s did not run live in %s: %+v", word.Word, record.Run, word)
		}
	}
}

// Rule: three verdicts, each with its own exit code; environment trouble is never red.
func TestThreeVerdictsAndExitCodes(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		verdict string
		code    int
	}{
		{"product works", nil, "green", 0},
		{"product broken", map[string]string{"TALLY_DEFECT": "drop-adds"}, "red", 1},
		{"environment trouble", map[string]string{"TALLY_SIMULATE_LOCK": "1"}, "inconclusive", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			done, record := runJSON(t, product(t), c.env, chain)
			equal(t, done.code, c.code)
			equal(t, string(*record.Verdict), c.verdict)
			for _, word := range record.Words {
				if word.Verdict != "green" && word.Verdict != "red" && word.Verdict != "inconclusive" {
					t.Fatalf("word verdict %q", word.Verdict)
				}
			}
		})
	}
}

func TestRunsRecordedWithLegacyLabelsReadAsThreeVerdicts(t *testing.T) {
	root := product(t)
	for id, label := range map[string]string{"1-a": "pass", "2-b": "fail", "3-c": "blocked", "4-d": "unverified"} {
		dir := filepath.Join(filepath.Dir(root), "state", "tally", "runs", id)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(dir, "run.json"), `{"run": "`+id+`", "chain": "store-open", "verdict": "`+label+`", "cleanup": "done", "words": [{"word": "store-open", "verdict": "`+label+`"}]}`, 0600)
	}
	equal(t, verilex(t, root, nil, "runs").stdout, "1-a  green  cleanup=done  store-open\n2-b  red  cleanup=done  store-open\n3-c  inconclusive  cleanup=done  store-open\n4-d  inconclusive  cleanup=done  store-open\n")
}

// Rule: quiet output. Verdict first, then only steps that are not green, each with one cause
// line and its evidence path; green steps are only counted.
func TestQuietOutputShowsVerdictThenFailuresOnly(t *testing.T) {
	t.Run("red", func(t *testing.T) {
		root := product(t)
		done := verilex(t, root, map[string]string{"TALLY_DEFECT": "drop-adds"}, "run", chain)
		record := lastRun(t, root)
		equal(t, done.stdout, "red: 1 green, 1 red, 1 not run; run "+record.Run+"\n"+
			"  red  item-stored apple: tally said 'added apple' but store.json lacks apple\n"+
			"    evidence: "+record.Words[1].Evidence+"\n"+
			"    verify skill: verify-tally/features/items.md#item-add\n")
		equal(t, done.stderr, "")
	})
	t.Run("inconclusive word", func(t *testing.T) {
		root := product(t)
		done := verilex(t, root, map[string]string{"TALLY_SIMULATE_LOCK": "1"}, "run", chain)
		record := lastRun(t, root)
		equal(t, done.stdout, "inconclusive: 1 inconclusive, 2 not run; run "+record.Run+"\n"+
			"  inconclusive  store-open: "+record.Words[0].Reason.(string)+"\n"+
			"    evidence: "+record.Words[0].Evidence+"\n"+
			"    verify skill: verify-tally/features/store.md#store-open\n")
	})
	t.Run("inconclusive frame", func(t *testing.T) {
		root := product(t)
		other := filepath.Join(t.TempDir(), "dev-store")
		if err := os.Mkdir(other, 0755); err != nil {
			t.Fatal(err)
		}
		done := verilex(t, root, map[string]string{"TALLY_ADOPT_STORE": other}, "run", chain)
		record := lastRun(t, root)
		equal(t, done.stdout, "inconclusive: 0 green, 3 not run; run "+record.Run+"\n"+
			"  inconclusive  doctor: refused the instance (exit 1)\n"+
			"    evidence: "+record.Frame[1].Evidence+"\n")
	})
	t.Run("cause without a failing step", func(t *testing.T) {
		root := product(t)
		write(t, filepath.Join(root, ".verilex", "frame", "launch"), "#!/bin/sh\necho launched\n", 0755)
		done := verilex(t, root, nil, "run", chain)
		equal(t, done.code, 2)
		record := lastRun(t, root)
		equal(t, done.stdout, "inconclusive: 0 green, 3 not run; run "+record.Run+"\n"+
			"  cause: launch printed no JSON object with an 'instance'\n")
	})
	t.Run("multi-line causes stay on one line", func(t *testing.T) {
		root := product(t)
		addWord(t, root, "store-glanced", `printf '%s\n' '{"verdict": "fail", "preconditions_held": true, "detail": "first\nsecond"}'`+"\nexit 1\n")
		done := verilex(t, root, nil, "run", "store-open | store-glanced")
		contains(t, done.stdout, "  red  store-glanced: first second\n")
	})
	t.Run("kept instance names its cleanup", func(t *testing.T) {
		root := product(t)
		done := verilex(t, root, nil, "run", "store-open", "--keep")
		id := lastRun(t, root).Run
		equal(t, done.stdout, "green: 1 green; run "+id+"\nkept: tear down with `verilex cleanup "+id+"`\n")
		verilex(t, root, nil, "cleanup", id)
	})
	t.Run("json stays complete", func(t *testing.T) {
		root := product(t)
		_, record := runJSON(t, root, nil, chain)
		equal(t, frames(record), []string{"launch", "doctor", "cleanup"})
		equal(t, verdicts(record), []string{"green", "green", "green"})
		equal(t, record.Steps, 3)
		for _, word := range record.Words {
			if word.Observation == nil || word.Evidence == "" || len(word.Stamp) != 64 || word.Claim != "pass" {
				t.Fatalf("incomplete word record: %+v", word)
			}
		}
		equal(t, record.Rerun, "store-open: no green result on record")
	})
}

// Rule: a word is skipped only when every stamp component matches a recorded green result.
func TestUnchangedChainIsSkippedCitingThePastRun(t *testing.T) {
	root := curated(t)
	first := green(t, root, nil, chain)
	ranLive(t, first)
	done := verilex(t, root, nil, "run", chain)
	equal(t, done.code, 0)
	second := lastRunOf(t, root, func(r runner.Record) bool { return r.Run != first.Run })
	equal(t, done.stdout, "green: 3 green, skipped: stamps match run "+first.Run+"; run "+second.Run+"\n")
	equal(t, second.Skipped, true)
	equal(t, len(second.Frame), 0)
	equal(t, second.Cleanup, "none")
	for i, word := range second.Words {
		equal(t, word.ReliesOn, first.Run)
		equal(t, word.Evidence, first.Words[i].Evidence)
		equal(t, word.Stamp, first.Words[i].Stamp)
	}
	if _, err := os.Stat(filepath.Join(second.Dir, "frame-launch")); !os.IsNotExist(err) {
		t.Fatalf("a skipped run launched: %v", err)
	}
	equal(t, verilex(t, root, nil, "cleanup", second.Run).stdout, "verilex: "+second.Run+" launched nothing; it relied on stamps\n")
	// A prefix of a proven chain ran in the same order from a fresh instance, so it is proven too.
	prefix := green(t, root, nil, "store-open | item-stored apple")
	equal(t, prefix.Skipped, true)
	equal(t, prefix.Words[1].ReliesOn, first.Run)
}

// A false skip is the worst defect: every change that can alter a result forces a live re-run.
func TestAnyChangedStampComponentForcesRerun(t *testing.T) {
	cases := []struct {
		name   string
		change func(t *testing.T, root string) map[string]string
		chain  string
		rerun  string
	}{
		{"word script", func(t *testing.T, root string) map[string]string {
			appendTo(t, filepath.Join(root, ".verilex", "words", "item-listed", "run"), "# edited\n")
			admitted(t, root, "item-listed")
			return nil
		}, chain, "item-listed apple: word changed"},
		{"word permissions", func(t *testing.T, root string) map[string]string {
			if err := os.Chmod(filepath.Join(root, ".verilex", "words", "item-stored", "run"), 0700); err != nil {
				t.Fatal(err)
			}
			return nil
		}, chain, "item-stored apple: word changed"},
		{"word contract", func(t *testing.T, root string) map[string]string {
			appendTo(t, filepath.Join(root, ".verilex", "words", "store-open", "word.md"), "More words.\n")
			admitted(t, root, "store-open")
			return nil
		}, chain, "store-open: word changed"},
		{"declared input", func(t *testing.T, root string) map[string]string {
			appendTo(t, filepath.Join(root, "bin", "tally"), "# edited\n")
			return nil
		}, chain, "store-open: input bin/tally changed"},
		{"shared word helper", func(t *testing.T, root string) map[string]string {
			appendTo(t, filepath.Join(root, ".verilex", "words", "tally_word.py"), "# edited\n")
			return nil
		}, chain, "store-open: shared changed"},
		{"shared helper directory", func(t *testing.T, root string) map[string]string {
			dir := filepath.Join(root, ".verilex", "words", "lib")
			if err := os.Mkdir(dir, 0755); err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(dir, "helper.py"), "# new helper\n", 0644)
			return nil
		}, chain, "store-open: shared changed"},
		{"frame step", func(t *testing.T, root string) map[string]string {
			appendTo(t, filepath.Join(root, ".verilex", "frame", "doctor"), "# edited\n")
			return nil
		}, chain, "store-open: frame changed"},
		{"project config", func(t *testing.T, root string) map[string]string {
			appendTo(t, filepath.Join(root, ".verilex", "config.yaml"), "secret_patterns: ['NEVER-[0-9]+']\n")
			return nil
		}, chain, "store-open: frame changed"},
		{"declared environment", func(t *testing.T, root string) map[string]string {
			return map[string]string{"TALLY_SIMULATE_LOCK": ""}
		}, chain, "store-open: env TALLY_SIMULATE_LOCK changed"},
		{"different argument", func(t *testing.T, root string) map[string]string { return nil },
			"store-open | item-stored pear | item-listed pear", "item-stored pear: no green result on record"},
		{"longer chain", func(t *testing.T, root string) map[string]string { return nil },
			chain + " | item-stored pear", "item-stored pear: no green result on record"},
		{"evidence gone", func(t *testing.T, root string) map[string]string {
			runs, _ := filepath.Glob(filepath.Join(filepath.Dir(root), "state", "tally", "runs", "*", "02-item-stored"))
			if len(runs) != 1 {
				t.Fatalf("evidence: %v", runs)
			}
			if err := os.RemoveAll(runs[0]); err != nil {
				t.Fatal(err)
			}
			return nil
		}, chain, "item-stored apple: evidence from run %s is gone"},
		{"unreadable ledger", func(t *testing.T, root string) map[string]string {
			write(t, filepath.Join(filepath.Dir(root), "state", "tally", "runs", "ledger.json"), "{not json", 0600)
			return nil
		}, chain, "the ledger is unreadable: invalid character 'n' looking for beginning of object key string"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := curated(t)
			first := green(t, root, nil, chain)
			env := c.change(t, root)
			_, record := runJSON(t, root, env, c.chain)
			equal(t, record.Rerun, strings.Replace(c.rerun, "%s", first.Run, 1))
			ranLive(t, record)
			equal(t, string(*record.Verdict), "green")
			// The re-run proved the new state, so the next identical run is skipped again.
			again := green(t, root, env, c.chain)
			equal(t, again.Skipped, true)
			equal(t, again.Words[0].ReliesOn, record.Run)
		})
	}
}

func TestPlantedDefectAfterGreenRunIsNeverSkipped(t *testing.T) {
	root := curated(t)
	green(t, root, nil, chain)
	done, record := runJSON(t, root, map[string]string{"TALLY_DEFECT": "drop-adds"}, chain)
	equal(t, done.code, 1)
	equal(t, string(*record.Verdict), "red")
	equal(t, record.Rerun, "store-open: env TALLY_DEFECT changed")
	ranLive(t, record)
	// A red result is never reused: the same broken chain runs again, past the green store-open.
	_, again := runJSON(t, root, map[string]string{"TALLY_DEFECT": "drop-adds"}, chain)
	equal(t, again.Rerun, "item-stored apple: env TALLY_DEFECT changed")
	ranLive(t, again)
	equal(t, string(*again.Verdict), "red")
	root = curated(t)
	runJSON(t, root, map[string]string{"TALLY_DEFECT": "drop-adds"}, chain)
	_, third := runJSON(t, root, map[string]string{"TALLY_DEFECT": "drop-adds"}, chain)
	equal(t, third.Rerun, "item-stored apple: no green result on record")
	ranLive(t, third)
}

func TestUpstreamRerunForcesDependentRerun(t *testing.T) {
	t.Run("upstream stamp changed", func(t *testing.T) {
		root := curated(t)
		first := green(t, root, nil, chain)
		// Re-prove only the first two words under a changed script; item-listed keeps its old record.
		appendTo(t, filepath.Join(root, ".verilex", "words", "item-stored", "run"), "# edited\n")
		admitted(t, root, "item-stored")
		prefix := green(t, root, nil, "store-open | item-stored apple")
		ranLive(t, prefix)
		_, record := runJSON(t, root, nil, chain)
		equal(t, record.Rerun, "item-listed apple: upstream changed")
		ranLive(t, record)
		if record.Words[2].Stamp == first.Words[2].Stamp {
			t.Fatal("item-listed kept its stamp although item-stored changed")
		}
	})
	t.Run("upstream re-runs with an unchanged stamp", func(t *testing.T) {
		root := product(t)
		first := green(t, root, nil, chain)
		if err := os.RemoveAll(first.Words[0].Evidence); err != nil {
			t.Fatal(err)
		}
		_, record := runJSON(t, root, nil, chain)
		equal(t, record.Rerun, "store-open: evidence from run "+first.Run+" is gone")
		ranLive(t, record)
		equal(t, record.Words[2].Stamp, first.Words[2].Stamp)
	})
}

func TestUnclearFootprintIsNeverSkipped(t *testing.T) {
	t.Run("word declares no inputs", func(t *testing.T) {
		root := curated(t)
		addWord(t, root, "store-glanced", `echo '{"verdict": "pass", "observation": "fine"}'`+"\n")
		green(t, root, nil, "store-open | store-glanced")
		record := green(t, root, nil, "store-open | store-glanced")
		equal(t, record.Rerun, "store-glanced: declares no inputs")
		ranLive(t, record)
	})
	t.Run("word declares an empty inputs list", func(t *testing.T) {
		root := curated(t)
		addWord(t, root, "store-glanced", `echo '{"verdict": "pass", "observation": "fine"}'`+"\n")
		path := filepath.Join(root, ".verilex", "words", "store-glanced", "word.md")
		write(t, path, strings.Replace(read(t, path), "requires: [store]", "requires: [store]\ninputs: []", 1), 0644)
		green(t, root, nil, "store-open | store-glanced")
		record := green(t, root, nil, "store-open | store-glanced")
		equal(t, record.Rerun, "store-glanced: declares no inputs")
		ranLive(t, record)
		if record.Words[1].Stamp != "" {
			t.Fatal("a word with an empty inputs list was stamped")
		}
	})
	t.Run("declared input is missing", func(t *testing.T) {
		root := product(t)
		path := filepath.Join(root, ".verilex", "words", "store-open", "word.md")
		write(t, path, strings.Replace(read(t, path), "inputs: [bin/tally]", "inputs: [bin/tally, bin/missing]", 1), 0644)
		green(t, root, nil, "store-open")
		record := green(t, root, nil, "store-open")
		equal(t, record.Rerun, "store-open: input bin/missing: "+filepath.Join(root, "bin", "missing")+" is missing")
		ranLive(t, record)
	})
	t.Run("input changes while the word runs", func(t *testing.T) {
		root := curated(t)
		write(t, filepath.Join(root, "counter"), "0\n", 0644)
		addWord(t, root, "store-counted", `echo 1 >> "$VERILEX_PROJECT_ROOT/counter"`+"\n"+`echo '{"verdict": "pass", "observation": "counted"}'`+"\n")
		path := filepath.Join(root, ".verilex", "words", "store-counted", "word.md")
		write(t, path, strings.Replace(read(t, path), "requires: [store]", "requires: [store]\ninputs: [counter]", 1), 0644)
		green(t, root, nil, "store-open | store-counted")
		record := green(t, root, nil, "store-open | store-counted")
		equal(t, record.Rerun, "store-counted: no green result on record")
	})
	t.Run("inconclusive run proves nothing", func(t *testing.T) {
		root := product(t)
		path := filepath.Join(root, ".verilex", "frame", "cleanup")
		appendTo(t, path, "\nif os.environ.get(\"LEAK\"):\n    (Path(os.environ[\"VERILEX_EVIDENCE\"]) / \"key.pem\").write_text(\"-----BEGIN PRIVATE KEY-----\\n\")\n")
		done, leaked := runJSON(t, root, map[string]string{"LEAK": "1"}, chain)
		equal(t, done.code, 2)
		equal(t, verdicts(leaked), []string{"green", "green", "green"})
		record := green(t, root, nil, chain)
		equal(t, record.Rerun, "store-open: no green result on record")
	})
}

func TestFreshAndKeepAlwaysRunLive(t *testing.T) {
	root := product(t)
	green(t, root, nil, chain)
	fresh := green(t, root, nil, chain, "--fresh")
	equal(t, fresh.Rerun, "--fresh asked for a live run")
	ranLive(t, fresh)
	kept := green(t, root, nil, chain, "--keep")
	equal(t, kept.Rerun, "--keep needs a live instance")
	ranLive(t, kept)
	equal(t, len(stores(t, root)), 1)
	equal(t, verilex(t, root, nil, "cleanup", kept.Run).stdout, "cleanup: done\n")
}

func lastRunOf(t *testing.T, root string, match func(runner.Record) bool) runner.Record {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(filepath.Dir(root), "state", "tally", "runs", "*", "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		record, err := runner.ReadRecord(path)
		if err != nil {
			t.Fatal(err)
		}
		if match(record) {
			return record
		}
	}
	t.Fatal("no matching run")
	return runner.Record{}
}
