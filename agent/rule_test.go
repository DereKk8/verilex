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

// TestVerdictRule checks docs/verdict-rule.md row by row: subtest NN is row NN, with that row's
// spec, the brain's runs and the verdict. Change the two together.
func TestVerdictRule(t *testing.T) {
	_, product, _ := changedProduct(t)
	for _, row := range verdictRule {
		t.Run(row.name, func(t *testing.T) { row.run(t, product) })
	}
}

// The docs table and verdictRule hold the same rows in the same order, with the same verdict,
// exit and printed run, so neither can change alone.
func TestVerdictRuleMatchesDocs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repo, "docs", "verdict-rule.md"))
	if err != nil {
		t.Fatal(err)
	}
	var table [][]string
	for _, line := range strings.Split(string(data), "\n") {
		// An escaped pipe is part of a chain inside a cell.
		cells := strings.Split(strings.ReplaceAll(line, `\|`, ""), " | ")
		if strings.HasPrefix(line, "| ") && cells[0] != "| #" {
			table = append(table, cells)
		}
	}
	if len(table) != len(verdictRule) {
		t.Fatalf("docs/verdict-rule.md has %d rows, verdictRule %d", len(table), len(verdictRule))
	}
	for i, row := range verdictRule {
		cells := table[i]
		if len(cells) != 6 {
			t.Fatalf("docs/verdict-rule.md row %d has %d cells, want 6: %q", i+1, len(cells), cells)
		}
		verdict := []string{"green", "red", "inconclusive"}[row.exit] + ", no JSON"
		if row.stdout != "" {
			verdict = []string{"green", "red", "inconclusive"}[row.exit] + ", run " + row.stdout
		}
		if !strings.HasPrefix(row.name, fmt.Sprintf("%02d ", i+1)) || cells[0] != fmt.Sprintf("| %d", i+1) || cells[3] != verdict || cells[4] != fmt.Sprint(row.exit) {
			t.Errorf("row %d: docs %q, test %q with %q and exit %d", i+1, strings.Join(cells, " | "), row.name, verdict, row.exit)
		}
	}
}

const (
	apple    = "prove a stored apple is listed"
	store    = "prove the store opens"
	nothing  = "make the export faster"
	dupChain = "store-open | store-open"
	// pearChain stores and lists pear, which hide-apple leaves alone.
	pearChain = "store-open | item-stored pear | item-listed pear"
	noRun     = "made no verilex run, so there is no verdict"
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
		name: "10 a run that verilex did not plan", spec: intent(store),
		fake: []fakeRun{
			{chainDoc("green", "r1", "store-open"), 0},
			{claimDoc("green", "r2", []string{"store-opened"}, []string{"store-opened"}), 0},
		},
		brain: "verilex run --named store-opened > \"$HOME/1\"\nverilex run --named store-opened > \"$HOME/2\"\n",
		exit:  2, stderr: []string{"inconclusive: run r1 ran a chain although the launcher passes --no-chain, so verilex did not plan it"},
	},
	{
		name: "11 a red, then a green on another claim (F6)", spec: intent(apple), plan: []string{"hide-lists", "hide-lists"},
		brain: "verilex run --claim item-listed > \"$HOME/1\"\nverilex run --claim store-opened > \"$HOME/2\"\n",
		exit:  1, stdout: "1", check: green("2"),
	},
	{
		name: "12 an empty chain argument is refused (F8)", spec: diff(), plan: []string{"hide-lists", "hide-lists"},
		brain: "verilex run --changed bin/tally '' > \"$HOME/1\" 2>> \"$HOME/refused\"\nverilex run --changed bin/tally > \"$HOME/2\"\n",
		exit:  1, stdout: "2", refused: 1,
	},
	{
		name: "13 a red, a forged pass, the same red again (F2)", spec: intent(apple), plan: []string{"hide-lists", "hide-lists"},
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
		name: "14 a red, then the time budget ends (F4)", ticket: ticket(apple, "2s"), plan: []string{"hide-lists"},
		brain:     "verilex run --claim item-listed > \"$HOME/1\"\ntouch \"$HOME/ran\"\nsleep 30\n",
		endsAfter: "ran", within: 8 * time.Second,
		exit: 1, stdout: "1",
	},
	{
		name: "15 a green, then the time budget ends (F4)", ticket: ticket(apple, "2s"),
		brain:     "verilex run --claim item-listed > \"$HOME/1\"\ntouch \"$HOME/ran\"\nsleep 30\n",
		endsAfter: "ran", within: 8 * time.Second,
		exit: 2, stderr: []string{"inconclusive: the time budget ended before the brain finished\n"}, check: verdictOf("1", "green"),
	},
	{
		// The wall clock ends this budget: the sandbox tool never answers, so the brain never starts.
		name: "16 the time budget ends before the brain starts (F4)", ticket: "diff: HEAD\nharness: stub\nmodel: stub\ntime_budget: 1s\n", neverStarts: true,
		setup: fakeSandboxTool("#!/bin/sh\nexec sleep 30\n"),
		exit:  2, stderr: []string{"inconclusive: the time budget ended before the brain finished\n"},
	},
	{
		name: "17 no run, only a refused run and a plan", spec: intent(store),
		brain: "printf '%s\\n' green\nverilex run --keep --claim store-opened 2> /dev/null\nverilex plan --claim store-opened > /dev/null\n",
		exit:  2, stderr: []string{"inconclusive: the brain " + noRun},
	},
	{
		name: "18 a chain alone is refused (F10)", spec: intent(store),
		brain: "verilex run '" + dupChain + "' > \"$HOME/1\" 2>> \"$HOME/refused\"\n",
		exit:  2, refused: 1, stderr: []string{"inconclusive: brain exited: exit status 2, and " + noRun},
	},
	{
		name: "19 a chain on other inputs is refused, so the claim plan keeps the admitted inputs (F9, F11)", spec: intent(apple), plan: []string{"hide-apple", "hide-apple"},
		brain: "verilex run --changed bin/tally '" + pearChain + "' > \"$HOME/1\" 2>> \"$HOME/refused\"\nverilex run --claim item-listed > \"$HOME/2\"\n",
		exit:  1, stdout: "2", refused: 1, check: listedApple("2"),
	},
	{
		name: "20 the same with --fresh (F11)", spec: intent(apple), plan: []string{"hide-apple", "hide-apple"},
		brain: "verilex run '" + pearChain + "' > \"$HOME/1\" 2>> \"$HOME/refused\"\nverilex run --fresh --claim item-listed > \"$HOME/2\"\n",
		exit:  1, stdout: "2", refused: 1, check: listedApple("2"),
	},
	{
		name: "21 a green, then an inconclusive, last", spec: intent(apple), plan: []string{"", "lock"},
		brain: "verilex run --claim item-listed > \"$HOME/1\"\nverilex run --claim item-listed > \"$HOME/2\"\n",
		exit:  2, stdout: "2", check: verdictOf("1", "green"),
	},
	{
		name: "22 a chain is refused, then a green covers the spec (F7)", spec: intent(store),
		brain: "verilex run '" + dupChain + "' > \"$HOME/1\" 2>> \"$HOME/refused\"\nverilex run --claim store-opened > \"$HOME/2\"\n",
		exit:  0, stdout: "2", refused: 1,
	},
	{
		name: "23 a green covers the spec, then a chain is refused (F10)", spec: intent(store),
		brain: "verilex run --claim store-opened > \"$HOME/1\"\nverilex run '" + dupChain + "' > \"$HOME/2\" 2>> \"$HOME/refused\"\n",
		exit:  0, stdout: "1", refused: 1,
	},
	{
		name: "24 an inconclusive, then a green that proves its claims", spec: intent(apple), plan: []string{"lock", ""},
		brain: "verilex run --claim item-listed > \"$HOME/1\"\nverilex run --claim item-listed > \"$HOME/2\"\n",
		exit:  0, stdout: "2", check: verdictOf("1", "inconclusive"),
	},
	{
		name: "25 an inconclusive, then a narrower green", spec: intent(store),
		fake: []fakeRun{
			{claimDoc("inconclusive", "r1", []string{"item-listed"}, []string{"store-opened", "item-added", "item-listed"}), 2},
			{claimDoc("green", "r2", []string{"store-opened"}, []string{"store-opened"}), 0},
		},
		brain: "verilex run --named store-opened > \"$HOME/1\"\nverilex run --named store-opened > \"$HOME/2\"\n",
		exit:  2, stderr: []string{"inconclusive: run r2 is green but did not prove item-listed, item-added, which run r1 left inconclusive"},
	},
	{
		name: "26 a green that covers the spec", spec: intent(apple),
		brain: "cp \"$VERILEX_AGENT_PROMPT\" \"$HOME/prompt\"\nverilex run --claim item-listed > \"$HOME/1\"\n",
		exit:  0, stdout: "1",
		check: func(t *testing.T, r *ruleRun, got kept) {
			if prompt := got.home(t, "prompt"); !strings.Contains(prompt, "floor: item-listed\nfloor: item-added\n") {
				t.Fatalf("the prompt does not list the floor:\n%s", prompt)
			}
		},
	},
	{
		name: "27 a green below the intent's floor", spec: intent(apple),
		brain: "verilex run --claim item-listed > \"$HOME/1\"\nverilex run --claim store-opened > \"$HOME/2\"\n",
		exit:  2, stderr: []string{"is green but was not asked about claim item-listed, claim item-added"},
	},
	{
		name: "28 a green below the --claim floor", spec: append(intent(store), "--claim", "item-listed"),
		brain: "verilex run --claim store-opened > \"$HOME/1\"\n",
		exit:  2, stderr: []string{"is green but was not asked about claim item-listed"},
	},
	{
		name: "29 the intent names no claim, and --claim stands for it", spec: append(intent(nothing), "--claim", "store-opened"),
		brain: "verilex run --claim store-opened > \"$HOME/1\"\n",
		exit:  0, stdout: "1",
	},
	{
		name: "30 a green not asked about the diff", spec: diff(),
		brain: "verilex run --claim store-opened > \"$HOME/1\"\n",
		exit:  2, stderr: []string{"is green but was not asked about change bin/tally"},
	},
	{
		name: "31 a green on the diff that proves every touched claim", spec: diff(),
		brain: "verilex run --changed bin/tally > \"$HOME/1\"\n",
		exit:  0, stdout: "1", check: warning(""),
	},
	{
		name: "32 a green on the diff, with verilex's warning", spec: diff(),
		brain: "verilex run --claim store-opened --changed bin/tally > \"$HOME/1\"\n",
		exit:  0, stdout: "1", check: warning("2 touched claims not covered"),
	},
	{
		name: "33 a green on a diff with a rename and a non-ASCII name (F3)", spec: append(intent(store), "--diff", "HEAD"),
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
}

// rule is one row of docs/verdict-rule.md.
type rule struct {
	name string
	// spec is the launcher's run spec flags; ticket is a ticket file used instead, for a time budget.
	spec   []string
	ticket string
	// plan is the product's environment for each verilex run in turn: "" as it is, "lock" for a
	// store another process holds, which verilex calls inconclusive, or a TALLY_DEFECT value such
	// as hide-lists.
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
	// endsAfter is the file the brain touches in its home once its runs are done. The row then runs
	// the launcher in this process with a timer that the test ends only after that file appears,
	// so the budget ends after the brain's runs however long the launcher takes to set up.
	endsAfter string
	// within bounds how long the launcher takes: from its start, or with endsAfter, from the end
	// of the budget.
	within time.Duration
	exit   int
	// stdout names the file in the brain's home that holds the launcher's stdout; "" is none.
	stdout string
	stderr []string
	// refused is how many chains verilex refused for the brain. Each chain run appends its stderr
	// to refused in the brain's home.
	refused int
	check   func(t *testing.T, r *ruleRun, got kept)
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
	switch {
	case row.during != nil:
		held := launchHeldEnv(t, r.dir, verilex, brain, r.env, args...)
		row.during(t, r)
		got = held.release(t)
	case row.endsAfter != "":
		var timer *testTimer
		got, timer = launchTimed(t, r.dir, verilex, brain, r.env, row.endsAfter, args...)
		if want := ticketBudget(t, row.ticket); !timer.started || timer.budget != want {
			t.Fatalf("the launcher started the budget timer: %v, with %s, want %s\nstderr: %s", timer.started, timer.budget, want, got.stderr)
		}
		if timer.ended.IsZero() {
			t.Fatalf("the brain never touched %s, so the test never ended the budget\nexit %d\nstdout: %s\nstderr: %s", row.endsAfter, got.code, got.stdout, got.stderr)
		}
		start = timer.ended
	default:
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
	refusals, _ := os.ReadFile(filepath.Join(got.root, "brain", "home", "refused"))
	if n := strings.Count(string(refusals), "--no-chain refuses a chain argument, an empty one too"); n != row.refused {
		t.Fatalf("verilex refused %d chains, want %d\nrefused: %s\nstderr: %s", n, row.refused, refusals, got.stderr)
	}
	plannedOnly(t, r.home)
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
  line=$(sed -n "${n}p" "$RULE_STATE/plan")
  case "$line" in
    lock) export TALLY_SIMULATE_LOCK=1 ;;
    ?*) export TALLY_DEFECT="$line" ;;
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

// ticketBudget is the time_budget a row's ticket gives.
func ticketBudget(t *testing.T, text string) time.Duration {
	t.Helper()
	_, rest, _ := strings.Cut(text, "time_budget: ")
	value, _, _ := strings.Cut(rest, "\n")
	budget, err := time.ParseDuration(value)
	if err != nil {
		t.Fatalf("ticket time_budget %q: %v", value, err)
	}
	return budget
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

// plannedOnly checks that every run record in home is a claim run that verilex planned: no chain
// ran, so no chain set the word arguments of a later claim plan (F11).
func plannedOnly(t *testing.T, home string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(home, "*", "runs", "*", "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		var record struct {
			Format    string `json:"format"`
			Requested *struct {
				Chain *string `json:"chain"`
			} `json:"requested"`
		}
		data, err := os.ReadFile(path)
		if err != nil || json.Unmarshal(data, &record) != nil || record.Format != "verilex-claim-run-1" || record.Requested == nil || record.Requested.Chain != nil {
			t.Fatalf("%s is not a run that verilex planned: %v\n%s", path, err, data)
		}
	}
}

// listedApple checks that the run whose JSON the brain kept in file listed apple, the input of
// the admitted chain, and not an input that a chain chose.
func listedApple(file string) func(t *testing.T, r *ruleRun, got kept) {
	return func(t *testing.T, r *ruleRun, got kept) {
		var doc struct {
			Words []struct {
				Word string   `json:"word"`
				Args []string `json:"args"`
			} `json:"words"`
		}
		if err := json.Unmarshal([]byte(got.home(t, file)), &doc); err != nil {
			t.Fatal(err)
		}
		for _, word := range doc.Words {
			if word.Word == "item-listed" && slices.Equal(word.Args, []string{"apple"}) {
				return
			}
		}
		t.Fatalf("run %s did not run item-listed apple: %+v", file, doc.Words)
	}
}

// chainDoc is a claim-run document of a chain the caller gave, which a fake verilex prints.
func chainDoc(verdict, run, chain string) []byte {
	doc := map[string]any{
		"format": "verilex-claim-run-1", "verdict": verdict, "run": run,
		"requested": map[string]any{"claims": []string{}, "named": []string{}, "changed": []string{"bin/tally"}, "chain": chain},
	}
	data, _ := json.Marshal(doc)
	return append(data, '\n')
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
		"requested": map[string]any{"claims": orEmpty(requested), "named": []string{}, "changed": []string{}, "chain": nil},
		"claims":    claims,
	}
	data, _ := json.Marshal(doc)
	return append(data, '\n')
}
