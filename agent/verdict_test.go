package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A red from any verilex run is the verdict: the project is read-only, so a later green on other
// claims cannot undo a failure verilex found in the code under test. The brain here is the
// tester's F6 brain: it gets a red, then runs a narrower claim that is green.
func TestRedFromAnyRunIsTheVerdict(t *testing.T) {
	dir, product, ledger := changedProduct(t)
	for _, tc := range []struct {
		name, first, second string
		defect              bool
		args                []string
		exit                int
	}{
		{"intent only", "verilex run --claim item-listed", "verilex run --claim store-opened", true,
			[]string{"--intent", "prove a stored apple is listed"}, 1},
		{"with a diff", "verilex run --changed bin/tally", "verilex run --claim store-opened --changed bin/tally", true,
			[]string{"--diff", "HEAD"}, 1},
		{"honest, intent only", "verilex run --claim item-listed", "", false,
			[]string{"--intent", "prove a stored apple is listed"}, 0},
		{"honest, with a diff", "verilex run --changed bin/tally", "", false,
			[]string{"--diff", "HEAD"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.defect {
				t.Setenv("TALLY_DEFECT", "hide-lists")
			}
			body := "#!/bin/sh\n" + tc.first + " > \"$HOME/first\"\n"
			if tc.second != "" {
				body += tc.second + " > \"$HOME/second\"\n"
			}
			brain := writeBrain(t, filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")), body)
			args := append([]string{"--project", product, "--ledger", ledger, "--harness", "stub", "--model", "stub"}, tc.args...)
			run := launchKept(t, dir, coreBin, brain, args...)
			if run.code != tc.exit || run.stdout != run.home(t, "first") {
				t.Fatalf("exit %d, want %d and the first run's JSON\nstdout: %s\nstderr: %s", run.code, tc.exit, run.stdout, run.stderr)
			}
			var doc runDoc
			if err := json.Unmarshal([]byte(run.stdout), &doc); err != nil {
				t.Fatal(err)
			}
			if want := map[int]string{0: "green", 1: "red"}[tc.exit]; doc.Verdict != want {
				t.Fatalf("verdict %s, want %s", doc.Verdict, want)
			}
			if tc.second == "" {
				return
			}
			// The later run was a real green, so the launcher held it and chose the red.
			var later runDoc
			if err := json.Unmarshal([]byte(run.home(t, "second")), &later); err != nil || later.Verdict != "green" {
				t.Fatalf("the second run was not green: %v\n%s", err, run.home(t, "second"))
			}
		})
	}
}

// Every run the brain made counts, not only its last. A red stands, an inconclusive run's claims
// must be proved by the green that follows, and the last run is the verdict otherwise.
func TestEveryRunCounts(t *testing.T) {
	listed := []string{"store-opened", "item-added", "item-listed"}
	for _, tc := range []struct {
		name   string
		runs   []fakeRun
		exit   int
		stdout int // the run whose JSON the launcher prints, from 1; 0 for none
		stderr string
	}{
		{"red, then green", []fakeRun{
			{claimDoc("red", "r1", []string{"item-listed"}, listed), 1},
			{claimDoc("green", "r2", []string{"store-opened"}, listed[:1]), 0},
		}, 1, 1, ""},
		{"inconclusive, then a green that proves its claims", []fakeRun{
			{claimDoc("inconclusive", "r1", []string{"item-listed"}, listed), 2},
			{claimDoc("green", "r2", []string{"item-listed"}, listed), 0},
		}, 0, 2, ""},
		{"inconclusive, then a narrower green", []fakeRun{
			{claimDoc("inconclusive", "r1", []string{"item-listed"}, listed), 2},
			{claimDoc("green", "r2", []string{"store-opened"}, listed[:1]), 0},
		}, 2, 0, "inconclusive: run r2 is green but did not prove item-listed, item-added, which run r1 left inconclusive"},
		{"green, then inconclusive", []fakeRun{
			{claimDoc("green", "r1", []string{"store-opened"}, listed[:1]), 0},
			{claimDoc("inconclusive", "r2", []string{"store-opened"}, listed[:1]), 2},
		}, 2, 2, ""},
		{"an earlier exit that disagrees", []fakeRun{
			{claimDoc("red", "r1", []string{"item-listed"}, listed), 0},
			{claimDoc("green", "r2", []string{"store-opened"}, listed[:1]), 0},
		}, 2, 0, "inconclusive: run r1: verilex printed red but exited 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fake := writeFake(t, dir, fakeFiles{ticket: ticketJSON("stub", "stub"), runs: tc.runs})
			brain := writeBrain(t, dir, fmt.Sprintf("#!/bin/sh\nfor i in $(seq %d); do verilex run --named store-opened > /dev/null; done\n", len(tc.runs)))
			stdout, stderr, code := launch(t, dir, fake, brain, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub")
			want := ""
			if tc.stdout > 0 {
				want = string(tc.runs[tc.stdout-1].body)
			}
			if code != tc.exit || stdout != want || !strings.Contains(stderr, tc.stderr) {
				t.Fatalf("exit %d, want %d\nstdout: %s\nwant: %s\nstderr: %s", code, tc.exit, stdout, want, stderr)
			}
		})
	}
}

// The claims verilex index finds for the intent are a floor like --claim, so a brain that proves
// a narrower claim last gets no green. An intent that names no claim has no floor, so the
// launcher returns inconclusive before the brain runs, unless --claim names the floor.
func TestIntentNamesTheFloor(t *testing.T) {
	dir, product, ledger := admittedProduct(t)
	marker := filepath.Join(t.TempDir(), "brain-ran")
	t.Setenv("MARKER", marker)
	for _, tc := range []struct {
		name, intent, body, stderr string
		args                       []string
		exit                       int
	}{
		{"a narrower claim last", "prove a stored apple is listed",
			"verilex run --claim item-listed > /dev/null\nverilex run --claim store-opened > /dev/null\n",
			"is green but was not asked about claim item-listed, claim item-added", nil, 2},
		{"every floor claim", "prove a stored apple is listed",
			"verilex run --claim item-listed > /dev/null\n", "", nil, 0},
		{"no claim for the intent", "make the export faster",
			"verilex run --claim store-opened > /dev/null\n",
			`inconclusive: intent "make the export faster" names no claim (verilex index --intent found none)`, nil, 2},
		{"no claim for the intent, a claim given", "make the export faster",
			"verilex run --claim store-opened > /dev/null\n", "", []string{"--claim", "store-opened"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			os.Remove(marker)
			brain := writeBrain(t, filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")),
				"#!/bin/sh\ntouch \"$MARKER\"\ncp \"$VERILEX_AGENT_PROMPT\" \"$HOME/prompt\"\n"+tc.body)
			args := append([]string{"--project", product, "--ledger", ledger, "--intent", tc.intent, "--harness", "stub", "--model", "stub"}, tc.args...)
			run := launchKept(t, dir, coreBin, brain, args...)
			if run.code != tc.exit || !strings.Contains(run.stderr, tc.stderr) {
				t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", run.code, tc.exit, run.stdout, run.stderr)
			}
			_, err := os.Stat(marker)
			if strings.Contains(tc.stderr, "names no claim") {
				if !os.IsNotExist(err) {
					t.Fatal("the brain ran for an intent that names no claim")
				}
				return
			}
			if tc.intent == "prove a stored apple is listed" && !strings.Contains(run.home(t, "prompt"), "floor: item-listed\nfloor: item-added\n") {
				t.Fatalf("the prompt does not show the floor\n%s", run.home(t, "prompt"))
			}
		})
	}
}

// A red verilex printed before the time budget ended still stands; a green does not, because
// the brain might have gone on to a red.
func TestRedBeforeTheBudgetEndsStands(t *testing.T) {
	for _, tc := range []struct {
		verdict           string
		verilexExit, exit int
	}{{"red", 1, 1}, {"green", 0, 2}} {
		t.Run(tc.verdict, func(t *testing.T) {
			dir := t.TempDir()
			ticket := `{"intent":"prove the store opens","harness":"stub","model":"stub","time_budget":"2s"}` + "\n"
			body := claimDoc(tc.verdict, "r1", []string{"store-opened"}, []string{"store-opened"})
			fake := writeFake(t, dir, fakeFiles{ticket: []byte(ticket), run: body, runExit: tc.verilexExit})
			brain := writeBrain(t, dir, "#!/bin/sh\nverilex run --named store-opened > /dev/null\nsleep 30\n")
			start := time.Now()
			stdout, stderr, code := launch(t, dir, fake, brain, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub")
			if took := time.Since(start); took > 8*time.Second {
				t.Fatalf("launcher took %s", took)
			}
			if code != tc.exit {
				t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", code, tc.exit, stdout, stderr)
			}
			if tc.exit == 1 && stdout != string(body) {
				t.Fatalf("stdout is not verilex's red\n%s", stdout)
			}
			if tc.exit == 2 && (stdout != "" || !strings.Contains(stderr, "inconclusive: the time budget ended")) {
				t.Fatalf("stdout: %s\nstderr: %s", stdout, stderr)
			}
		})
	}
}

// claimDoc is a claim-run document a fake verilex prints: the claims it was asked for, and the
// claims it selected, each with the run's verdict.
func claimDoc(verdict, run string, requested, selected []string) []byte {
	claims := []map[string]string{}
	for _, name := range selected {
		claims = append(claims, map[string]string{"claim": name, "verdict": verdict})
	}
	doc := map[string]any{
		"format": "verilex-claim-run-1", "verdict": verdict, "run": run,
		"requested": map[string][]string{"claims": orEmpty(requested), "named": {}, "changed": {}},
		"claims":    claims,
	}
	data, _ := json.Marshal(doc)
	return append(data, '\n')
}
