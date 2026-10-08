package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestVerdictRule checks README "Verdict rule" row by row: subtest NN is row NN, with that row's
// spec, the brain's runs and the verdict. Change the two together.
func TestVerdictRule(t *testing.T) {
	_, product, _ := changedProduct(t)
	for _, row := range verdictRule {
		t.Run(row.name, func(t *testing.T) { row.run(t, product) })
	}
}

// The README table and verdictRule hold the same rows in the same order, with the same verdict,
// exit and printed run, so neither can change alone.
func TestVerdictRuleMatchesREADME(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repo, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, _ := strings.Cut(string(data), "\n### Verdict rule\n")
	section, _, _ = strings.Cut(section, "\n### ")
	var table [][]string
	for _, line := range strings.Split(section, "\n") {
		// An escaped pipe is part of a chain inside a cell.
		cells := strings.Split(strings.ReplaceAll(line, `\|`, ""), " | ")
		if strings.HasPrefix(line, "| ") && cells[0] != "| #" {
			table = append(table, cells)
		}
	}
	if len(table) != len(verdictRule) {
		t.Fatalf("README has %d rows, verdictRule %d", len(table), len(verdictRule))
	}
	for i, row := range verdictRule {
		cells := table[i]
		if len(cells) != 6 {
			t.Fatalf("README row %d has %d cells, want 6: %q", i+1, len(cells), cells)
		}
		verdict := []string{"green", "red", "inconclusive"}[row.exit] + ", no JSON"
		if row.stdout != "" {
			verdict = []string{"green", "red", "inconclusive"}[row.exit] + ", run " + row.stdout
		}
		if !strings.HasPrefix(row.name, fmt.Sprintf("%02d ", i+1)) || cells[0] != fmt.Sprintf("| %d", i+1) || cells[3] != verdict || cells[4] != fmt.Sprint(row.exit) {
			t.Errorf("row %d: README %q, test %q with %q and exit %d", i+1, strings.Join(cells, " | "), row.name, verdict, row.exit)
		}
	}
}

const (
	apple    = "prove a stored apple is listed"
	store    = "prove the store opens"
	nothing  = "make the export faster"
	dupChain = "store-open | store-open"
	f6Chain  = "store-open | item-stored apple | item-listed apple"
)

var verdictRule = []rule{
	{
		name: "01 the intent names no claim", spec: intent(nothing), neverStarts: true,
		exit: 2, stderr: []string{`inconclusive: intent "make the export faster" names no claim`},
	},
	{
		name: "02 the diff changes no file, and no intent or --claim names a claim", spec: []string{"--diff", "HEAD..HEAD"}, neverStarts: true,
		exit: 2, stderr: []string{"inconclusive: diff HEAD..HEAD changes no file under ", ", so there is nothing to prove"},
	},
	{
		name: "03 verilex index fails", spec: intent(apple), index: "fail", neverStarts: true,
		exit: 2, stderr: []string{"inconclusive: verilex index --intent failed, so the intent's claims are unknown: the index is broken"},
	},
	{
		name: "04 verilex index does not answer before the time budget ends", ticket: ticket(apple, "1s"), index: "hang", neverStarts: true,
		within: 10 * time.Second,
		exit:   2, stderr: []string{"inconclusive: the time budget ended before verilex index --intent answered"},
	},
	{
		name: "05 the sandbox tool fails", spec: intent(store), neverStarts: true,
		setup: fakeSandboxTool("#!/bin/sh\necho '" + sandboxTool() + ": setting up uid map: Permission denied' >&2\nexit 1\n"),
		exit:  2, stderr: []string{"inconclusive: the brain runs only in a sandbox, and the sandbox is not available here: " + sandboxTool() + " could not start the sandbox: " + sandboxTool() + ": setting up uid map: Permission denied"},
		check: projectUnchanged,
	},
	{
		name: "06 the sandbox tool starts the brain without a sandbox", spec: intent(store), neverStarts: true,
		setup: fakeSandboxTool(passthrough()),
		exit:  2, stderr: []string{"inconclusive: the brain runs only in a sandbox, and the sandbox is not available here: the sandbox did not hold: the brain could write the project"},
		check: projectUnchanged,
	},
	{
		name: "07 the project changes during the run (F1)", spec: intent(store),
		brain: "verilex run --claim store-opened > /dev/null\n",
		during: func(t *testing.T, r *ruleRun) {
			path := filepath.Join(r.product, "bin", "tally")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.WriteFile(path, before, 0o755) })
			appendLine(t, path, "# changed while the brain ran\n")
		},
		exit: 2, stderr: []string{"inconclusive: the project changed during the run", "bin/tally"},
	},
	{
		name: "08 a run around the launcher in its home (F5)", spec: intent(store),
		brain: "verilex run --claim store-opened > /dev/null\n",
		during: func(t *testing.T, r *ruleRun) {
			cmd := exec.Command(coreBin, "--project", r.product, "run", "--keep", "--claim", "store-opened")
			cmd.Env = append(os.Environ(), "VERILEX_HOME="+r.home, "VERILEX_LEDGER="+filepath.Join(r.dir, "host-ledger"))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		},
		exit: 2, stderr: []string{"did not come through the launcher", "kept its instance; the launcher tore it down"},
		check: func(t *testing.T, r *ruleRun, got kept) {
			if rows := instancesDown(t, r.product, r.home); len(rows) != 2 {
				t.Fatalf("home runs %+v, want the host's and the brain's", rows)
			}
		},
	},
	{
		name: "09 a run whose exit disagrees with its JSON", spec: intent(store),
		fake: []fakeRun{
			{claimDoc("red", "r1", []string{"item-listed"}, []string{"store-opened", "item-added", "item-listed"}), 0},
			{claimDoc("green", "r2", []string{"store-opened"}, []string{"store-opened"}), 0},
		},
		brain: "verilex run --named store-opened > \"$HOME/1\"\nverilex run --named store-opened > \"$HOME/2\"\n",
		exit:  2, stderr: []string{"inconclusive: run r1: verilex printed red but exited 0"},
	},
	{
		name: "10 a planned red", spec: intent(apple), plan: []string{"defect"},
		brain: "verilex run --claim item-listed > \"$HOME/1\"\n",
		exit:  1, stdout: "1",
	},
	{
		name: "11 a planned red, then a green on another claim (F6)", spec: intent(apple), plan: []string{"defect", "defect"},
		brain: "verilex run --claim item-listed > \"$HOME/1\"\nverilex run --claim store-opened > \"$HOME/2\"\n",
		exit:  1, stdout: "1", check: green("2"),
	},
	{
		name: "12 a planned red on the diff, then a narrower green (F6)", spec: diff(), plan: []string{"defect", "defect"},
		brain: "verilex run --changed bin/tally > \"$HOME/1\"\nverilex run --claim store-opened --changed bin/tally > \"$HOME/2\"\n",
		exit:  1, stdout: "1", check: green("2"),
	},
	{
		name: "13 a planned red, a forged pass, the same red again (F2)", spec: intent(apple), plan: []string{"defect", "defect"},
		setup: func(t *testing.T, r *ruleRun) {
			ledger := filepath.Join(r.dir, "ledger")
			r.args = append(r.args, "--ledger", ledger)
			r.env = append(r.env, "LEDGER="+ledger)
		},
		// The write is judged on the host: on Linux a path under /tmp lands in the brain's own /tmp.
		brain: "verilex run --claim item-listed > /dev/null\n" +
			"mkdir -p \"$LEDGER/tally/passes/forged\" 2> /dev/null && echo '{\"run\": \"forged\"}' > \"$LEDGER/tally/passes/forged/pass.json\"\n" +
			"verilex run --claim item-listed > \"$HOME/2\"\n",
		exit: 1, stdout: "2",
		check: func(t *testing.T, r *ruleRun, got kept) {
			var doc runDoc
			if err := json.Unmarshal([]byte(got.stdout), &doc); err != nil || doc.Skipped {
				t.Fatalf("the rerun skipped: %v\n%s", err, got.stdout)
			}
			if _, err := os.Stat(filepath.Join(r.dir, "ledger", "tally", "passes", "forged")); !os.IsNotExist(err) {
				t.Fatalf("the brain wrote a forged pass into the ledger: %v", err)
			}
		},
	},
	{
		name: "14 a planned red, then the time budget ends (F4)", ticket: ticket(apple, "2s"), plan: []string{"defect"},
		brain:  "verilex run --claim item-listed > \"$HOME/1\"\nsleep 30\n",
		within: 8 * time.Second,
		exit:   1, stdout: "1",
	},
	{
		name: "15 a green, then the time budget ends (F4)", ticket: ticket(apple, "2s"),
		brain:  "verilex run --claim item-listed > \"$HOME/1\"\nsleep 30\n",
		within: 8 * time.Second,
		exit:   2, stderr: []string{"inconclusive: the time budget ended before the brain finished"},
	},
	{
		name: "16 a red brain chain, then the time budget ends (F4)", ticket: ticket(store, "2s"),
		brain:  "verilex run '" + dupChain + "' > \"$HOME/1\"\nsleep 30\n",
		within: 8 * time.Second,
		exit:   2, stderr: []string{"inconclusive: the time budget ended before the brain finished"},
		check: verdictOf("1", "red"),
	},
	{
		name: "17 no run, only a refused run and a plan", spec: intent(store),
		brain: "printf '%s\\n' green\nverilex run --keep --claim store-opened 2> /dev/null\nverilex plan --claim store-opened > /dev/null\n",
		exit:  2, stderr: []string{"inconclusive: the brain exited without a verilex run, so there is no verdict"},
	},
	{
		name: "18 a red brain chain, last", spec: intent(store),
		brain: "verilex run '" + dupChain + "' > \"$HOME/1\"\n",
		exit:  1, stdout: "1",
	},
	{
		name: "19 a green, then an inconclusive, last", spec: intent(apple), plan: []string{"", "lock"},
		brain: "verilex run --claim item-listed > \"$HOME/1\"\nverilex run --claim item-listed > \"$HOME/2\"\n",
		exit:  2, stdout: "2", check: verdictOf("1", "green"),
	},
	{
		name: "20 a red brain chain, then a green that proves its red word's claim (F7)", spec: intent(store),
		brain: "verilex run '" + dupChain + "' > \"$HOME/1\"\nverilex run --claim store-opened > \"$HOME/2\"\n",
		exit:  0, stdout: "2", check: verdictOf("1", "red"),
	},
	{
		name: "21 a red brain chain, then a green that does not prove its red word's claim (F6 as a chain)", spec: intent(apple), plan: []string{"defect", "defect"},
		brain: "verilex run '" + f6Chain + "' > \"$HOME/1\"\nverilex run --claim store-opened > \"$HOME/2\"\n",
		exit:  2, stderr: []string{"is green but did not prove item-listed, which run ", " left red"},
		check: verdictOf("1", "red"),
	},
	{
		name: "22 a red brain chain on the diff, then a green that does not prove its red word's claim (F6 as a chain)", spec: diff(), plan: []string{"defect", "defect"},
		brain: "verilex run --changed bin/tally '" + f6Chain + "' > \"$HOME/1\"\nverilex run --claim store-opened --changed bin/tally > \"$HOME/2\"\n",
		exit:  2, stderr: []string{"is green but did not prove item-listed, which run ", " left red"},
		check: func(t *testing.T, r *ruleRun, got kept) {
			var doc runDoc
			if err := json.Unmarshal([]byte(got.home(t, "1")), &doc); err != nil || doc.Verdict != "red" || doc.Format != "verilex-claim-run-1" {
				t.Fatalf("the chain run is not a red claim run: %v %+v", err, doc)
			}
		},
	},
	{
		name: "23 an inconclusive, then a green that proves its claims", spec: intent(apple), plan: []string{"lock", ""},
		brain: "verilex run --claim item-listed > \"$HOME/1\"\nverilex run --claim item-listed > \"$HOME/2\"\n",
		exit:  0, stdout: "2", check: verdictOf("1", "inconclusive"),
	},
	{
		name: "24 an inconclusive, then a narrower green", spec: intent(store),
		fake: []fakeRun{
			{claimDoc("inconclusive", "r1", []string{"item-listed"}, []string{"store-opened", "item-added", "item-listed"}), 2},
			{claimDoc("green", "r2", []string{"store-opened"}, []string{"store-opened"}), 0},
		},
		brain: "verilex run --named store-opened > \"$HOME/1\"\nverilex run --named store-opened > \"$HOME/2\"\n",
		exit:  2, stderr: []string{"inconclusive: run r2 is green but did not prove item-listed, item-added, which run r1 left inconclusive"},
	},
	{
		name: "25 a green that covers the spec", spec: intent(apple),
		brain: "cp \"$VERILEX_AGENT_PROMPT\" \"$HOME/prompt\"\nverilex run --claim item-listed > \"$HOME/1\"\n",
		exit:  0, stdout: "1",
		check: func(t *testing.T, r *ruleRun, got kept) {
			if prompt := got.home(t, "prompt"); !strings.Contains(prompt, "floor: item-listed\nfloor: item-added\n") {
				t.Fatalf("the prompt does not list the floor:\n%s", prompt)
			}
		},
	},
	{
		name: "26 a green below the intent's floor", spec: intent(apple),
		brain: "verilex run --claim item-listed > \"$HOME/1\"\nverilex run --claim store-opened > \"$HOME/2\"\n",
		exit:  2, stderr: []string{"is green but was not asked about claim item-listed, claim item-added"},
	},
	{
		name: "27 a green below the --claim floor", spec: append(intent(store), "--claim", "item-listed"),
		brain: "verilex run --claim store-opened > \"$HOME/1\"\n",
		exit:  2, stderr: []string{"is green but was not asked about claim item-listed"},
	},
	{
		name: "28 the intent names no claim, and --claim stands for it", spec: append(intent(nothing), "--claim", "store-opened"),
		brain: "verilex run --claim store-opened > \"$HOME/1\"\n",
		exit:  0, stdout: "1",
	},
	{
		name: "29 a green not asked about the diff", spec: diff(),
		brain: "verilex run --claim store-opened > \"$HOME/1\"\n",
		exit:  2, stderr: []string{"is green but was not asked about change bin/tally"},
	},
	{
		name: "30 a green on the diff that proves every touched claim", spec: diff(),
		brain: "verilex run --changed bin/tally > \"$HOME/1\"\n",
		exit:  0, stdout: "1", check: warning(""),
	},
	{
		name: "31 a green on the diff, with verilex's warning", spec: diff(),
		brain: "verilex run --claim store-opened --changed bin/tally > \"$HOME/1\"\n",
		exit:  0, stdout: "1", check: warning("2 touched claims not covered"),
	},
	{
		name: "32 a green on a diff with a rename and a non-ASCII name (F3)", spec: append(intent(store), "--diff", "HEAD"),
		setup: renamedProduct,
		brain: "cp \"$VERILEX_AGENT_PROMPT\" \"$HOME/prompt\"\nset --\n" +
			"for path in $(sed -n 's/^changed: //p' \"$VERILEX_AGENT_PROMPT\"); do set -- \"$@\" --changed \"$path\"; done\n" +
			"verilex run --claim store-opened \"$@\" > \"$HOME/1\"\n",
		exit: 0, stdout: "1",
		check: func(t *testing.T, r *ruleRun, got kept) {
			changed := []string{".verilex/words/item-listed/café.txt", ".verilex/words/item-listed/notes.txt", "notes-moved.txt"}
			var doc runDoc
			if err := json.Unmarshal([]byte(got.stdout), &doc); err != nil {
				t.Fatal(err)
			}
			if got := slices.Sorted(slices.Values(doc.Requested.Changed)); !slices.Equal(got, changed) {
				t.Fatalf("requested.changed %q, want %q", got, changed)
			}
			if len(doc.Uncovered) != 1 || doc.Uncovered[0].Claim != "item-listed" {
				t.Fatalf("uncovered %+v, warning %q", doc.Uncovered, doc.Warning)
			}
			prompt := got.home(t, "prompt")
			for _, path := range changed {
				if !strings.Contains(prompt, "changed: "+path+"\n") {
					t.Fatalf("the prompt lacks %s\n%s", path, prompt)
				}
			}
		},
	},
	{
		name: "33 a green brain chain on the diff", spec: append(intent(store), "--diff", "HEAD"),
		brain: "verilex run --changed bin/tally 'store-open' > \"$HOME/1\"\n",
		exit:  0, stdout: "1", check: warning("2 touched claims not covered"),
	},
	{
		name: "34 a green brain chain without a diff", spec: intent(store),
		brain: "verilex run 'store-open' > \"$HOME/1\"\n",
		exit:  2, stderr: []string{"is green but is not a claim run"},
	},
}

// rule is one row of README "Verdict rule".
type rule struct {
	name string
	// spec is the launcher's run spec flags; ticket is a ticket file used instead, for a time budget.
	spec   []string
	ticket string
	// plan is the product's environment for each verilex run in turn: "" as it is, "defect" for
	// TALLY_DEFECT=hide-lists, "lock" for a store another process holds, which verilex calls
	// inconclusive.
	plan []string
	// index fakes verilex index: "fail" exits 2, "hang" never answers.
	index string
	// fake replaces verilex with one that prints these runs in turn.
	fake []fakeRun
	// brain is the brain after #!/bin/sh. A run that the row reads writes its JSON to $HOME/<n>.
	brain string
	// neverStarts says the brain must not start. Every brain first touches started in its home.
	neverStarts bool
	// setup prepares the row; during runs while the brain holds, before its first run.
	setup, during func(t *testing.T, r *ruleRun)
	within        time.Duration
	exit          int
	// stdout names the file in the brain's home that holds the launcher's stdout; "" is none.
	stdout string
	stderr []string
	check  func(t *testing.T, r *ruleRun, got kept)
}

// ruleRun is one row's launch: its directory, the project and home the launcher uses, and the
// launcher flags and environment the row's setup adds.
type ruleRun struct {
	dir, product, home, status string
	args, env                  []string
}

func (row rule) run(t *testing.T, product string) {
	r := &ruleRun{dir: t.TempDir(), product: product}
	r.home = filepath.Join(r.dir, "home")
	if row.setup != nil {
		row.setup(t, r)
	}
	verilex := row.verilex(t, r)
	body := "#!/bin/sh\ntouch \"$HOME/started\"\n"
	if row.during != nil {
		body = holdBrain + "touch \"$HOME/started\"\n"
	}
	brain := writeBrain(t, filepath.Join(r.dir, "brain-dir"), body+row.brain)
	args := append([]string{"--home", r.home}, r.args...)
	if r.product != "" {
		args = append(args, "--project", r.product)
	}
	if row.ticket != "" {
		path := filepath.Join(r.dir, "ticket.yaml")
		if err := os.WriteFile(path, []byte(row.ticket), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, "--ticket", path)
	} else {
		args = append(args, "--harness", "stub", "--model", "stub")
	}
	args = append(args, row.spec...)
	start := time.Now()
	var got kept
	if row.during != nil {
		held := launchHeldEnv(t, r.dir, verilex, brain, r.env, args...)
		row.during(t, r)
		got = held.release(t)
	} else {
		got = launchKeptEnv(t, r.dir, verilex, brain, r.env, args...)
	}
	if row.within > 0 && time.Since(start) > row.within {
		t.Fatalf("the launcher took %s, more than %s", time.Since(start), row.within)
	}
	want := ""
	if row.stdout != "" {
		want = got.home(t, row.stdout)
	}
	if got.code != row.exit || got.stdout != want {
		t.Fatalf("exit %d, want %d\nstdout: %s\nwant: %s\nstderr: %s", got.code, row.exit, got.stdout, want, got.stderr)
	}
	// No JSON is the launcher's own inconclusive, never a refusal.
	if row.stdout == "" && !strings.Contains(got.stderr, "verilex-agent: inconclusive: ") {
		t.Fatalf("stderr lacks verilex-agent: inconclusive:\nstderr: %s", got.stderr)
	}
	for _, part := range row.stderr {
		if !strings.Contains(got.stderr, part) {
			t.Fatalf("stderr lacks %q\nstderr: %s", part, got.stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(got.root, "brain", "home", "started")); row.neverStarts != os.IsNotExist(err) {
		t.Fatalf("the brain started: %v, want %v\nstderr: %s", !os.IsNotExist(err), !row.neverStarts, got.stderr)
	}
	if row.check != nil {
		row.check(t, r, got)
	}
}

// verilex is the row's verilex: a fake, or the real one through a wrapper that sets each run's
// product environment from the plan and fakes verilex index on request.
func (row rule) verilex(t *testing.T, r *ruleRun) string {
	if row.fake != nil {
		r.product = ""
		return writeFake(t, r.dir, fakeFiles{ticket: ticketJSON("stub", "stub"), runs: row.fake})
	}
	state := filepath.Join(r.dir, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "plan"), []byte(strings.Join(row.plan, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.dir, "verilex")
	if err := os.WriteFile(path, []byte(planVerilex), 0o700); err != nil {
		t.Fatal(err)
	}
	r.env = append(r.env, "RULE_STATE="+state, "RULE_VERILEX="+coreBin, "RULE_INDEX="+row.index)
	return path
}

// planVerilex runs the real verilex. Each run gets the environment its line in the plan names.
const planVerilex = `#!/bin/sh
unset TALLY_DEFECT TALLY_SIMULATE_LOCK
case "$3" in
run)
  n=$(($(cat "$RULE_STATE/n" 2> /dev/null || echo 0) + 1))
  echo "$n" > "$RULE_STATE/n"
  case "$(sed -n "${n}p" "$RULE_STATE/plan")" in
    defect) export TALLY_DEFECT=hide-lists ;;
    lock) export TALLY_SIMULATE_LOCK=1 ;;
  esac ;;
index)
  case "$RULE_INDEX" in
    fail) echo "verilex: refused: the index is broken" >&2; exit 2 ;;
    hang) exec sleep 30 ;;
  esac ;;
esac
exec "$RULE_VERILEX" "$@"
`

func intent(text string) []string { return []string{"--intent", text} }

func diff() []string { return []string{"--diff", "HEAD"} }

func ticket(intent, budget string) string {
	return "intent: " + intent + "\nharness: stub\nmodel: stub\ntime_budget: " + budget + "\n"
}

// fakeSandboxTool puts body first on PATH as the sandbox tool.
func fakeSandboxTool(body string) func(t *testing.T, r *ruleRun) {
	return func(t *testing.T, r *ruleRun) {
		fakes := filepath.Join(r.dir, "fakes")
		if err := os.MkdirAll(fakes, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(fakes, sandboxTool()), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
		r.env = append(r.env, "PATH="+fakes+string(os.PathListSeparator)+os.Getenv("PATH"))
		r.status = gitStatus(t, r.product)
	}
}

// passthrough is a sandbox tool that starts the command without a sandbox.
func passthrough() string {
	if sandboxTool() == "sandbox-exec" {
		return "#!/bin/sh\nshift 2\nexec \"$@\"\n"
	}
	return "#!/bin/sh\nwhile [ \"$1\" != -- ]; do shift; done\nshift\nexec \"$@\"\n"
}

func projectUnchanged(t *testing.T, r *ruleRun, got kept) {
	if status := gitStatus(t, r.product); status != r.status {
		t.Fatalf("the project changed:\n%s\nwas:\n%s", status, r.status)
	}
}

// renamedProduct is an admitted tally copy whose working tree renames a file in a word directory
// and changes another whose name is not ASCII.
func renamedProduct(t *testing.T, r *ruleRun) {
	product := copyProduct(t, r.dir)
	words := filepath.Join(product, ".verilex", "words", "item-listed")
	for _, name := range []string{"notes.txt", "café.txt"} {
		if err := os.WriteFile(filepath.Join(words, name), []byte("note\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	admit(t, product, filepath.Join(r.dir, "admit-ledger"))
	if err := commitAll(product); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "mv", ".verilex/words/item-listed/notes.txt", "notes-moved.txt")
	cmd.Dir = product
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(words, "café.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.product = product
}

// green checks that the run whose JSON the brain kept in file was green.
func green(file string) func(t *testing.T, r *ruleRun, got kept) {
	return verdictOf(file, "green")
}

// verdictOf checks the verdict of the run whose JSON the brain kept in file.
func verdictOf(file, verdict string) func(t *testing.T, r *ruleRun, got kept) {
	return func(t *testing.T, r *ruleRun, got kept) {
		var doc runDoc
		if err := json.Unmarshal([]byte(got.home(t, file)), &doc); err != nil || doc.Verdict != verdict {
			t.Fatalf("run %s is not %s: %v\n%s", file, verdict, err, got.home(t, file))
		}
	}
}

// warning checks verilex's own missed-claim warning on the stdout.
func warning(text string) func(t *testing.T, r *ruleRun, got kept) {
	return func(t *testing.T, r *ruleRun, got kept) {
		var doc runDoc
		if err := json.Unmarshal([]byte(got.stdout), &doc); err != nil || doc.Warning != text {
			t.Fatalf("warning %q, want %q: %v", doc.Warning, text, err)
		}
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
