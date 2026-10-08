package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DereKk8/verilex/agent/internal/cli"
	"github.com/DereKk8/verilex/agent/internal/prompt"
	agentrun "github.com/DereKk8/verilex/agent/internal/run"
)

var (
	agentBin string
	coreBin  string
	repo     string
)

func TestMain(m *testing.M) {
	// A launcher that a test runs in this process (launchTimed) starts this binary as its sandbox
	// helper and as the brain's verilex relay, as the shipped launcher starts itself.
	if len(os.Args) > 1 && (os.Args[1] == "sandbox-init" || os.Args[1] == "relay") {
		os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr, agentrun.WallClock))
	}
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	repo = wd
	if _, err = os.Stat(filepath.Join(repo, "go.work")); err != nil {
		repo = filepath.Dir(wd)
	}
	dir, err := os.MkdirTemp("", "verilex-agent-build-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	agentBin = filepath.Join(dir, "verilex-agent")
	coreBin = filepath.Join(dir, "verilex")
	build := func(out, pkg string, extra ...string) {
		args := append([]string{"build", "-o", out}, extra...)
		args = append(args, pkg)
		cmd := exec.Command("go", args...)
		cmd.Dir = repo
		if text, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintln(os.Stderr, string(text), err)
			os.RemoveAll(dir)
			os.Exit(1)
		}
	}
	build(agentBin, "./agent/cmd/verilex-agent")
	build(coreBin, "./cmd/verilex")
	if _, err = exec.LookPath(sandboxTool()); err != nil {
		fmt.Fprintf(os.Stderr, "the agent tests run each brain in the launcher's sandbox and need %s on PATH\n", sandboxTool())
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestLauncherReturnsVerilexJSONNotTheBrain(t *testing.T) {
	runJSON := []byte("{\"format\":\"verilex-claim-run-1\",\"verdict\":\"red\",\"run\":\"r1\",\"requested\":{\"claims\":[\"item-listed\"],\"named\":[],\"changed\":[],\"chain\":null},\"missed_claims\":[\"item-added\"],\"note\":\"keep me\"}\n")
	dir := t.TempDir()
	fake := writeFake(t, dir, fakeFiles{run: runJSON, runExit: 1, ticket: ticketJSON("stub", "stub")})
	brain := writeBrain(t, dir, "#!/bin/sh\nprintf '%s\\n' 'BRAIN SAYS GREEN'\nverilex run --json --claim item-listed\nprintf '%s\\n' 'still green'\n")
	stdout, stderr, code := launch(t, dir, fake, brain, "--intent", "prove a stored apple is listed", "--harness", "stub", "--model", "stub")
	if code != 1 {
		t.Fatalf("exit %d, want 1\nstderr: %s\nstdout: %s", code, stderr, stdout)
	}
	if stdout != string(runJSON) {
		t.Fatalf("stdout = %q, want verilex JSON %q", stdout, runJSON)
	}
	if strings.Contains(stdout, "BRAIN") || strings.Contains(stderr, "BRAIN") {
		t.Fatalf("brain text leaked\nstdout: %s\nstderr: %s", stdout, stderr)
	}
}

// A claim run that does not say who wrote its chain comes from a verilex older than the launcher.
// Its chain may be the brain's, so it is inconclusive, never a verdict.
func TestClaimRunWithoutRequestedChainIsInconclusive(t *testing.T) {
	runJSON := []byte(`{"format":"verilex-claim-run-1","verdict":"green","run":"r1","requested":{"claims":[],"named":["store-opened"],"changed":["notes.txt"]}}` + "\n")
	dir := t.TempDir()
	fake := writeFake(t, dir, fakeFiles{run: runJSON, ticket: ticketJSON("stub", "stub")})
	brain := writeBrain(t, dir, "#!/bin/sh\nverilex run --json --named store-opened --changed notes.txt\n")
	stdout, stderr, code := launch(t, dir, fake, brain, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub")
	if code != 2 || stdout != "" || !strings.Contains(stderr, "verilex-agent: inconclusive: verilex printed a claim run without requested.chain") {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
}

func TestBrainExitDoesNotReplaceTheVerdict(t *testing.T) {
	runJSON := claimRun("green", "r9", []string{"store-opened"}, nil)
	dir := t.TempDir()
	fake := writeFake(t, dir, fakeFiles{run: runJSON, ticket: ticketJSON("stub", "stub")})
	brain := writeBrain(t, dir, "#!/bin/sh\nverilex run --json --named store-opened\nexit 1\n")
	stdout, stderr, code := launch(t, dir, fake, brain, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub")
	if code != 0 || stdout != string(runJSON) {
		t.Fatalf("exit %d stdout %q stderr %s", code, stdout, stderr)
	}
}

func TestSkillIntentAndSuggestionsReachTheBrain(t *testing.T) {
	dir := t.TempDir()
	gitRepo(t, filepath.Join(dir, "project"), "notes.txt")
	skill := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skill, []byte("SKILL-MARKER-7\nwhen to use verilex\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := writeFake(t, dir, fakeFiles{
		ticket: []byte("{\"diff\":\"HEAD\",\"harness\":\"stub\",\"model\":\"stub\",\"effort\":\"low\"}\n"),
		run:    claimRun("green", "r4", []string{"store-opened"}, []string{"notes.txt"}),
	})
	brain := writeBrain(t, dir, "#!/bin/sh\nif [ \"$VERILEX_AGENT_PHASE\" = suggest ]; then\n  cp \"$VERILEX_AGENT_PROMPT\" \"$HOME/suggest\"\n  printf '%s\\n' '{\"claims\":[\"item-added\"]}'\n  exit 0\nfi\ncat > \"$HOME/stdin\"\ncp \"$VERILEX_AGENT_PROMPT\" \"$HOME/prompt\"\nverilex run --named store-opened --changed notes.txt\n")
	run := launchKept(t, dir, fake, brain, "--skill", skill, "--suggest", "--diff", "HEAD", "--claim", "store-opened", "--harness", "stub", "--model", "stub")
	stdout := run.stdout
	if run.code != 0 {
		t.Fatalf("exit %d stdout %s stderr %s", run.code, stdout, run.stderr)
	}
	text := run.home(t, "prompt")
	if stdin := run.home(t, "stdin"); stdin != text {
		t.Fatalf("stdin is not the prompt:\n%s", stdin)
	}
	for _, want := range []string{
		"SKILL-MARKER-7",
		"intent: prove nothing this change touched broke",
		"diff: HEAD",
		"floor: store-opened",
		"changed: notes.txt",
		"suggestion: item-added",
		prompt.Rule,
		"# Task\nProve the intent now.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("prompt missing %q\n%s", want, text)
		}
	}
	// The suggest phase asks for claims only: it carries no task to run verilex.
	if suggest := run.home(t, "suggest"); !strings.Contains(suggest, "# Suggest\n") || !strings.Contains(suggest, "Do not call verilex.") || strings.Contains(suggest, "# Task") {
		t.Fatalf("suggest prompt:\n%s", suggest)
	}
	if strings.Contains(stdout, "item-added") {
		t.Fatalf("suggestion JSON became the verdict: %s", stdout)
	}
}

func TestUnreadableDiffIsRefusedBeforeTheBrain(t *testing.T) {
	dir := t.TempDir()
	gitRepo(t, filepath.Join(dir, "project"), "notes.txt")
	started := filepath.Join(dir, "started")
	t.Setenv("STARTED", started)
	fake := writeFake(t, dir, fakeFiles{ticket: []byte("{\"diff\":\"no-such-rev\",\"harness\":\"stub\",\"model\":\"stub\"}\n")})
	brain := writeBrain(t, dir, "#!/bin/sh\ntouch \"$STARTED\"\n")
	stdout, stderr, code := launch(t, dir, fake, brain, "--diff", "no-such-rev", "--harness", "stub", "--model", "stub")
	if code != 2 || stdout != "" || !strings.Contains(stderr, "refused: diff no-such-rev") {
		t.Fatalf("exit %d stdout %q stderr %s", code, stdout, stderr)
	}
	if _, err := os.Stat(started); !os.IsNotExist(err) {
		t.Fatal("the brain started on a diff git could not read")
	}
}

func TestRealHarnessStaysOptIn(t *testing.T) {
	dir := t.TempDir()
	touched := filepath.Join(dir, "touched")
	driver := writeBrain(t, dir, "#!/bin/sh\ntouch \"$TOUCHED\"\n")
	t.Setenv("TOUCHED", touched)
	templates := filepath.Join(dir, "harnesses.json")
	if err := os.WriteFile(templates, []byte(`{"claude-code":["`+driver+`"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := writeFake(t, dir, fakeFiles{ticket: ticketJSON("claude-code", "claude-opus")})
	_, stderr, code := launch(t, dir, fake, "", "--harnesses", templates, "--intent", "prove the store opens", "--harness", "claude-code", "--model", "claude-opus")
	if code != 2 || !strings.Contains(stderr, "verilex-agent: refused: harness claude-code is not started unless --allow-harness") {
		t.Fatalf("exit %d stderr %s", code, stderr)
	}
	if _, err := os.Stat(touched); !os.IsNotExist(err) {
		t.Fatal("harness ran without --allow-harness")
	}
}

func TestSameHomeIsRefused(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	fake := writeFake(t, dir, fakeFiles{ticket: ticketJSON("stub", "stub"), run: claimRun("green", "hold", []string{"store-opened"}, nil)})
	brain := writeBrain(t, dir, holdBrain+"verilex run --json --named store-opened\n")
	held := launchHeld(t, dir, fake, brain, "--home", home, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub")
	_, stderr, code := launch(t, dir, fake, brain, "--home", home, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub")
	first := held.release(t)
	if first.code != 0 {
		t.Fatalf("first exit %d: %s", first.code, first.stderr)
	}
	if code != 2 || !strings.Contains(stderr, "in use") {
		t.Fatalf("exit %d stderr %s", code, stderr)
	}
}

func TestParallelRunsKeepSeparateHomesAndSharedLedger(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	stores := t.TempDir()
	t.Setenv("TALLY_STORES", stores)
	dir := t.TempDir()
	product := copyProduct(t, dir)
	ledger := filepath.Join(dir, "ledger")
	admit(t, product, ledger)
	brain := writeBrain(t, dir, "#!/bin/sh\nprintf '%s\\n' 'BRAIN SAYS GREEN'\nverilex run --claim item-listed\n")
	launchOne := func(home string) (runDoc, error) {
		stdout, stderr, code, err := launchRaw(dir, coreBin, brain,
			"--project", product, "--home", home, "--ledger", ledger,
			"--intent", "prove a stored apple is listed", "--harness", "stub", "--model", "stub")
		if err != nil {
			return runDoc{}, err
		}
		if code != 0 || strings.Contains(stdout, "BRAIN") {
			return runDoc{}, fmt.Errorf("exit %d\n%s\n%s", code, stdout, stderr)
		}
		var doc runDoc
		if err = json.Unmarshal([]byte(stdout), &doc); err != nil {
			return runDoc{}, fmt.Errorf("%v\n%s", err, stdout)
		}
		if doc.Verdict != "green" || doc.Run == "" {
			return runDoc{}, fmt.Errorf("verdict %+v", doc)
		}
		return doc, nil
	}
	homes := []string{filepath.Join(dir, "home-a"), filepath.Join(dir, "home-b")}
	docs := make([]runDoc, len(homes))
	errc := make(chan error, len(homes))
	for i, home := range homes {
		go func() {
			var err error
			docs[i], err = launchOne(home)
			errc <- err
		}()
	}
	for range homes {
		if err := <-errc; err != nil {
			t.Fatal(err)
		}
	}
	for i, home := range homes {
		if docs[i].Skipped {
			t.Fatalf("run %d skipped before any pass of this word admission was recorded", i)
		}
		if got := runIDs(t, home); len(got) != 1 || got[0] != docs[i].Run {
			t.Fatalf("home %d holds runs %v, want only %s", i, got, docs[i].Run)
		}
	}
	if docs[0].Run == docs[1].Run {
		t.Fatalf("parallel runs share run %s", docs[0].Run)
	}
	third := filepath.Join(dir, "home-c")
	doc, err := launchOne(third)
	if err != nil {
		t.Fatal(err)
	}
	if !doc.Skipped {
		t.Fatalf("a run on a new home did not skip through the shared ledger: %+v", doc)
	}
	record := readRecord(t, third, doc.Run)
	if record["instance"] != nil {
		t.Fatalf("skipped run launched an instance: %v", record["instance"])
	}
	if _, err = os.Stat(filepath.Join(stores, "tally-"+doc.Run)); !os.IsNotExist(err) {
		t.Fatalf("skipped run left a store: %v", err)
	}
}

func TestBoundaryPassesOnThisRepoAndFailsOnAHarnessNameOrImport(t *testing.T) {
	boundary := func(root string) (string, int) {
		t.Helper()
		cmd := exec.Command("go", "run", filepath.Join(repo, "scripts", "boundary.go"), root)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if exit, ok := err.(*exec.ExitError); ok {
			return string(out), exit.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), 0
	}
	fixture := func(files map[string]string) string {
		t.Helper()
		root := t.TempDir()
		for name, body := range files {
			path := filepath.Join(root, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}
	for _, tc := range []struct {
		name  string
		root  string
		exit  int
		wants string
	}{
		{"this repository", repo, 0, "boundary: ok"},
		{"identifiers and skill directories are not harnesses", fixture(map[string]string{
			"internal/x.go": "package x\n\nvar dirs = []string{\".cursor/skills\", \".claude/skills\"}\n\nfunc f() int {\n\tpi := 0\n\treturn pi\n}\n",
		}), 0, "boundary: ok"},
		{"a harness name in a string", fixture(map[string]string{
			"internal/driver.go": "package internal\n\nvar harness = \"claude-code\"\n",
		}), 1, "internal/driver.go:3: claude-code"},
		{"a harness name in a raw string", fixture(map[string]string{
			"cmd/x/main.go": "package main\n\nvar argv = `\nexec codex exec\n`\n",
		}), 1, "core names a specific harness"},
		{"an import of the agent module", fixture(map[string]string{
			"cmd/main.go": "package main\n\nimport _ \"github.com/DereKk8/verilex/agent/internal/cli\"\n",
		}), 1, "core imports the agent module"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, exit := boundary(tc.root)
			if exit != tc.exit || !strings.Contains(out, tc.wants) {
				t.Fatalf("exit %d, want %d\n%s", exit, tc.exit, out)
			}
		})
	}
}

func TestTimeBudgetWithoutAVerdictIsInconclusive(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	ticket := filepath.Join(dir, "ticket.yaml")
	if err := os.WriteFile(ticket, []byte("intent: prove the store opens\nharness: stub\nmodel: stub\neffort: low\ntime_budget: 200ms\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	brain := writeBrain(t, dir, "#!/bin/sh\nsleep 30\n")
	start := time.Now()
	stdout, stderr, code := launch(t, dir, coreBin, brain, "--project", copyProduct(t, dir), "--ticket", ticket)
	if time.Since(start) > 2*time.Second {
		t.Fatalf("budget did not stop the brain after %s", time.Since(start))
	}
	if code != 2 || !strings.Contains(stderr, "time budget") {
		t.Fatalf("exit %d stdout %s stderr %s", code, stdout, stderr)
	}
	if strings.Contains(stdout, "green") {
		t.Fatalf("budget expiry reported green: %s", stdout)
	}
}

type fakeFiles struct {
	ticket  []byte
	plan    []byte
	run     []byte
	runExit int
	// runs, when set, answers each verilex run in turn instead of run.
	runs []fakeRun
	// index answers verilex index; by default the intent names store-opened.
	index []byte
}

type fakeRun struct {
	body []byte
	exit int
}

// fakeScript is a verilex that answers each subcommand from the files the FAKE_* variables name.
const fakeScript = `#!/bin/sh
cmd=
prev=
for arg in "$@"; do
  case "$prev" in
    --project|--continue|--ticket|--intent|--changed|--implements|--verdict|--same-as|--claim|--claims|--diff) prev=; continue ;;
  esac
  case "$arg" in
    --project|--continue|--ticket|--intent|--changed|--implements|--verdict|--same-as|--claim|--claims|--diff) prev=$arg; continue ;;
    --*) continue ;;
    *) cmd=$arg; break ;;
  esac
done
case "$cmd" in
  ticket) cat "$FAKE_TICKET"; exit 0 ;;
  index) cat "$FAKE_INDEX"; exit 0 ;;
  plan) cat "$FAKE_PLAN"; exit 0 ;;
  run)
    if [ -n "$FAKE_RUNS" ]; then
      n=$(($(cat "$FAKE_RUNS/count" 2>/dev/null || echo 0) + 1))
      echo "$n" > "$FAKE_RUNS/count"
      cat "$FAKE_RUNS/$n.json"
      exit "$(cat "$FAKE_RUNS/$n.exit")"
    fi
    cat "$FAKE_RUN"; exit "$FAKE_RUN_EXIT" ;;
  runs) echo '[]'; exit 0 ;;
  *) echo "fake verilex: unexpected $cmd" >&2; exit 2 ;;
esac
`

func writeFake(t *testing.T, dir string, files fakeFiles) string {
	t.Helper()
	path := filepath.Join(dir, "fake-verilex")
	if err := os.WriteFile(path, []byte(fakeScript), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, body []byte) string {
		p := filepath.Join(dir, name)
		if body == nil {
			body = []byte("{}\n")
		}
		if err := os.WriteFile(p, body, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if files.index == nil {
		files.index = []byte(`[{"claim":"store-opened"}]` + "\n")
	}
	t.Setenv("FAKE_TICKET", write("ticket.json", files.ticket))
	t.Setenv("FAKE_INDEX", write("index.json", files.index))
	t.Setenv("FAKE_PLAN", write("plan.json", files.plan))
	t.Setenv("FAKE_RUN", write("run.json", files.run))
	t.Setenv("FAKE_RUN_EXIT", fmt.Sprint(files.runExit))
	t.Setenv("FAKE_RUNS", "")
	if len(files.runs) > 0 {
		runs := filepath.Join(dir, "runs")
		if err := os.MkdirAll(runs, 0o700); err != nil {
			t.Fatal(err)
		}
		for i, run := range files.runs {
			write(filepath.Join("runs", fmt.Sprintf("%d.json", i+1)), run.body)
			write(filepath.Join("runs", fmt.Sprintf("%d.exit", i+1)), []byte(fmt.Sprintf("%d\n", run.exit)))
		}
		t.Setenv("FAKE_RUNS", runs)
	}
	return path
}

func writeBrain(t *testing.T, dir, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "brain")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func ticketJSON(harness, model string) []byte {
	return []byte(fmt.Sprintf("{\"intent\":\"prove the store opens\",\"harness\":%q,\"model\":%q,\"effort\":\"low\",\"from\":{\"harness\":\"ticket\",\"model\":\"ticket\"}}\n", harness, model))
}

func launch(t *testing.T, dir, verilex, brain string, args ...string) (string, string, int) {
	t.Helper()
	stdout, stderr, code, err := launchRaw(dir, verilex, brain, args...)
	if err != nil {
		t.Fatal(err)
	}
	return stdout, stderr, code
}

func launchRaw(dir, verilex, brain string, args ...string) (string, string, int, error) {
	return launchEnv(dir, verilex, brain, nil, args...)
}

// launchEnv runs the launcher with env added to the test's environment.
func launchEnv(dir, verilex, brain string, env []string, args ...string) (string, string, int, error) {
	argv, err := launcherArgs(dir, verilex, brain, args...)
	if err != nil {
		return "", "", 0, err
	}
	cmd := exec.Command(agentBin, argv...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0, nil
	}
	exit, ok := err.(*exec.ExitError)
	if !ok {
		return stdout.String(), stderr.String(), 0, err
	}
	return stdout.String(), stderr.String(), exit.ExitCode(), nil
}

// launcherArgs is a test's launcher command line, with a skill and, unless args name one, a
// project: dir/project, a git repository beside the test's fake files.
func launcherArgs(dir, verilex, brain string, args ...string) ([]string, error) {
	skill := filepath.Join(dir, "skill.md")
	if _, err := os.Stat(skill); err != nil {
		if err = os.WriteFile(skill, []byte("skill text\n"), 0o600); err != nil {
			return nil, err
		}
	}
	project := filepath.Join(dir, "project")
	if _, err := os.Stat(filepath.Join(project, ".git")); err != nil && !slices.Contains(args, "--project") {
		if err = os.MkdirAll(project, 0o700); err != nil {
			return nil, err
		}
		if err = commitAll(project); err != nil {
			return nil, err
		}
	}
	argv := []string{"--verilex", verilex, "--skill", skill, "--project", project}
	if brain != "" {
		argv = append(argv, "--brain", brain)
	}
	return append(argv, args...), nil
}

// kept is a launcher run whose run directory the test reads afterwards.
type kept struct {
	stdout, stderr string
	code           int
	root           string
}

// home reads a file the brain wrote in its own home, the one place its sandbox lets it write.
func (k kept) home(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(k.root, "brain", "home", name))
	if err != nil {
		t.Fatalf("%v\nstdout: %s\nstderr: %s", err, k.stdout, k.stderr)
	}
	return string(data)
}

// launchKept runs the launcher with --keep-work and a TMPDIR of its own, so the test finds the
// run directory.
func launchKept(t *testing.T, dir, verilex, brain string, args ...string) kept {
	t.Helper()
	return launchKeptEnv(t, dir, verilex, brain, nil, args...)
}

// launchKeptEnv is launchKept with env added to the test's environment.
func launchKeptEnv(t *testing.T, dir, verilex, brain string, env []string, args ...string) kept {
	t.Helper()
	tmp := runTemp(t)
	stdout, stderr, code, err := launchEnv(dir, verilex, brain, append([]string{"TMPDIR=" + tmp}, env...), append(args, "--keep-work")...)
	if err != nil {
		t.Fatal(err)
	}
	return kept{stdout: stdout, stderr: stderr, code: code, root: runRoot(t, tmp)}
}

// holdBrain is the start of a brain that waits, after it touched hold in its home, until the
// test writes release there.
const holdBrain = "#!/bin/sh\ntouch \"$HOME/hold\"\nwhile [ ! -f \"$HOME/release\" ]; do sleep 0.02; done\n"

// held is a launcher run whose brain waits in holdBrain.
type held struct {
	root string
	done chan kept
}

// launchHeld starts the launcher and returns once its brain is holding.
func launchHeld(t *testing.T, dir, verilex, brain string, args ...string) held {
	t.Helper()
	return launchHeldEnv(t, dir, verilex, brain, nil, args...)
}

// launchHeldEnv is launchHeld with env added to the test's environment.
func launchHeldEnv(t *testing.T, dir, verilex, brain string, env []string, args ...string) held {
	t.Helper()
	tmp := runTemp(t)
	done := make(chan kept, 1)
	go func() {
		stdout, stderr, code, err := launchEnv(dir, verilex, brain, append([]string{"TMPDIR=" + tmp}, env...), append(args, "--keep-work")...)
		if err != nil {
			stderr += err.Error()
			code = -1
		}
		done <- kept{stdout: stdout, stderr: stderr, code: code}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if matches, _ := filepath.Glob(filepath.Join(tmp, "verilex-agent-*", "brain", "home", "hold")); len(matches) == 1 {
			return held{root: filepath.Dir(filepath.Dir(filepath.Dir(matches[0]))), done: done}
		}
		select {
		case run := <-done:
			t.Fatalf("the brain never held: exit %d\n%s\n%s", run.code, run.stdout, run.stderr)
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("the brain never held")
	return held{}
}

// release lets the held brain go on and waits for the launcher.
func (h held) release(t *testing.T) kept {
	t.Helper()
	if err := os.WriteFile(filepath.Join(h.root, "brain", "home", "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	run := <-h.done
	run.root = h.root
	return run
}

// testTimer is an agentrun.Timer that the test ends itself, never the wall clock. Like the context
// of agentrun.WallClock when the budget runs out, its context then ends with context.DeadlineExceeded.
type testTimer struct {
	done chan struct{}
	once sync.Once
	// budget is what the launcher started the timer with, and ended is when the test ended it.
	budget  time.Duration
	started bool
	ended   time.Time
}

func newTestTimer() *testTimer { return &testTimer{done: make(chan struct{})} }

// start is the agentrun.Timer. The launcher's own cancel also ends the budget, as WallClock's does.
func (tt *testTimer) start(budget time.Duration) (context.Context, context.CancelFunc) {
	tt.budget, tt.started = budget, true
	return timerContext{tt.done}, tt.stop
}

func (tt *testTimer) stop() { tt.once.Do(func() { close(tt.done) }) }

// timerContext is the testTimer's context: no deadline and no values, and DeadlineExceeded once
// the test ends the budget.
type timerContext struct{ done chan struct{} }

func (c timerContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c timerContext) Done() <-chan struct{}       { return c.done }
func (c timerContext) Value(any) any               { return nil }
func (c timerContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// launchTimed runs the launcher in this process, with --keep-work, env set for the run, and a
// testTimer that the test ends once the brain has touched marker in its home. So "the brain's run,
// then the end of the budget" happens in that order however long the launcher takes to set up.
func launchTimed(t *testing.T, dir, verilex, brain string, env []string, marker string, args ...string) (kept, *testTimer) {
	t.Helper()
	tmp := runTemp(t)
	for _, entry := range append([]string{"TMPDIR=" + tmp}, env...) {
		key, value, _ := strings.Cut(entry, "=")
		t.Setenv(key, value)
	}
	argv, err := launcherArgs(dir, verilex, brain, append(args, "--keep-work")...)
	if err != nil {
		t.Fatal(err)
	}
	timer := newTestTimer()
	returned := make(chan struct{})
	var watch sync.WaitGroup
	watch.Add(1)
	go func() {
		defer watch.Done()
		for {
			select {
			case <-returned:
				return
			default:
			}
			if matches, _ := filepath.Glob(filepath.Join(tmp, "verilex-agent-*", "brain", "home", marker)); len(matches) == 1 {
				timer.ended = time.Now()
				timer.stop()
				return
			}
			select {
			case <-returned:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	var stdout, stderr strings.Builder
	code := cli.Main(argv, &stdout, &stderr, timer.start)
	close(returned)
	watch.Wait()
	return kept{stdout: stdout.String(), stderr: stderr.String(), code: code, root: runRoot(t, tmp)}, timer
}

// runTemp is a short TMPDIR for one launcher run: its run directory lands there.
func runTemp(t *testing.T) string {
	t.Helper()
	tmp, err := os.MkdirTemp("/tmp", "vxt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	return tmp
}

func runRoot(t *testing.T, tmp string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(tmp, "verilex-agent-*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("run directories in %s: %v", tmp, matches)
	}
	return matches[0]
}

// sandboxTool is what the launcher sandboxes the brain with on this platform.
func sandboxTool() string {
	if runtime.GOOS == "darwin" {
		return "sandbox-exec"
	}
	return "bwrap"
}

func copyProduct(t *testing.T, dir string) string {
	t.Helper()
	root := filepath.Join(dir, "tally")
	src := filepath.Join(repo, "tests", "fixtures", "tally")
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(root, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(dest, info.Mode().Perm())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = commitAll(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func admit(t *testing.T, product, ledger string) {
	t.Helper()
	home := t.TempDir()
	chain := "store-open | item-stored apple | item-listed apple"
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(coreBin, append([]string{"--project", product}, args...)...)
		cmd.Env = append(os.Environ(), "VERILEX_HOME="+home, "VERILEX_LEDGER="+ledger, "TALLY_STORES="+filepath.Join(home, "stores"))
		if err := os.MkdirAll(filepath.Join(home, "stores"), 0o700); err != nil {
			t.Fatal(err)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", args, err, out)
		}
	}
	run("run", chain)
	run("run", chain)
	for _, word := range []string{"store-open", "item-stored", "item-listed"} {
		run("onboard", word)
	}
}
