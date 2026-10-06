package e2e_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	agentBin string
	coreBin  string
	repo     string
)

func TestMain(m *testing.M) {
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
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestLauncherReturnsVerilexJSONNotTheBrain(t *testing.T) {
	runJSON := []byte("{\"verdict\":\"red\",\"run\":\"r1\",\"missed_claims\":[\"item-added\"],\"note\":\"keep me\"}\n")
	dir := t.TempDir()
	fake := writeFake(t, dir, fakeFiles{run: runJSON, runExit: 1, ticket: ticketJSON("stub", "stub")})
	brain := writeBrain(t, dir, "#!/bin/sh\nprintf '%s\\n' 'BRAIN SAYS GREEN'\nverilex run --json 'store-open'\nprintf '%s\\n' 'still green'\n")
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

func TestBrainExitDoesNotReplaceTheVerdict(t *testing.T) {
	runJSON := []byte("{\"verdict\":\"green\",\"run\":\"r9\"}\n")
	dir := t.TempDir()
	fake := writeFake(t, dir, fakeFiles{run: runJSON, ticket: ticketJSON("stub", "stub")})
	brain := writeBrain(t, dir, "#!/bin/sh\nverilex run --json 'store-open'\nexit 1\n")
	stdout, stderr, code := launch(t, dir, fake, brain, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub")
	if code != 0 || stdout != string(runJSON) {
		t.Fatalf("exit %d stdout %q stderr %s", code, stdout, stderr)
	}
}

func TestNoVerilexRunIsNotGreen(t *testing.T) {
	dir := t.TempDir()
	fake := writeFake(t, dir, fakeFiles{ticket: ticketJSON("stub", "stub")})
	brain := writeBrain(t, dir, "#!/bin/sh\nprintf '%s\\n' 'green'\nexit 0\n")
	stdout, stderr, code := launch(t, dir, fake, brain, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub")
	if code != 2 {
		t.Fatalf("exit %d, want 2\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if strings.Contains(stdout, "green") {
		t.Fatalf("stdout reported green without a verilex run: %s", stdout)
	}
	if !strings.Contains(stderr, "no verdict") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestMissingWarningRefusesGreen(t *testing.T) {
	dir := t.TempDir()
	fake := writeFake(t, dir, fakeFiles{
		ticket:  ticketJSON("stub", "stub"),
		plan:    []byte("{\"format\":\"claim-plan\",\"unpicked\":[\"item-added\"],\"warning\":\"1 touched claim not covered\"}"),
		run:     []byte("{\"verdict\":\"green\",\"run\":\"r2\"}"),
		runExit: 0,
	})
	brain := writeBrain(t, dir, "#!/bin/sh\nverilex plan --json 'store-open'\nverilex run --json 'store-open'\n")
	stdout, stderr, code := launch(t, dir, fake, brain, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub")
	if code != 2 {
		t.Fatalf("exit %d, want 2\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "item-added") || !strings.Contains(stderr, "missed-claim") {
		t.Fatalf("stderr = %q", stderr)
	}
	if !strings.Contains(stdout, `"verdict":"green"`) {
		t.Fatalf("stdout dropped verilex JSON: %s", stdout)
	}
}

func TestWarningInVerdictIsReturned(t *testing.T) {
	runJSON := []byte("{\"verdict\":\"green\",\"run\":\"r3\",\"uncovered\":[{\"claim\":\"item-added\",\"next\":\"verilex run --claim item-added\"}],\"warning\":\"1 touched claim not covered\"}\n")
	dir := t.TempDir()
	fake := writeFake(t, dir, fakeFiles{
		ticket: ticketJSON("stub", "stub"),
		plan:   []byte("{\"unpicked\":[\"item-added\"]}"),
		run:    runJSON,
	})
	brain := writeBrain(t, dir, "#!/bin/sh\nverilex plan --json\nverilex run --json 'store-open'\nprintf '%s\\n' 'I decided green'\n")
	stdout, stderr, code := launch(t, dir, fake, brain, "--intent", "prove the store opens", "--claim", "store-opened", "--harness", "stub", "--model", "stub")
	if code != 0 || stdout != string(runJSON) {
		t.Fatalf("exit %d stdout %q stderr %s", code, stdout, stderr)
	}
}

func TestSkillIntentAndSuggestionsReachTheBrain(t *testing.T) {
	dir := t.TempDir()
	skill := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skill, []byte("SKILL-MARKER-7\nwhen to use verilex\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	seen := filepath.Join(dir, "seen-prompt")
	fake := writeFake(t, dir, fakeFiles{ticket: []byte("{\"diff\":\"HEAD\",\"harness\":\"stub\",\"model\":\"stub\",\"effort\":\"low\"}\n"), run: []byte("{\"verdict\":\"green\",\"run\":\"r4\"}\n")})
	brain := writeBrain(t, dir, "#!/bin/sh\nif [ \"$VERILEX_AGENT_PHASE\" = suggest ]; then\n  printf '%s\\n' '{\"claims\":[\"item-added\"]}'\n  exit 0\nfi\ncp \"$VERILEX_AGENT_PROMPT\" \"$PROMPT_COPY\"\nverilex run --json 'store-open'\n")
	t.Setenv("PROMPT_COPY", seen)
	stdout, stderr, code := launch(t, dir, fake, brain, "--skill", skill, "--suggest", "--diff", "HEAD", "--claim", "store-opened", "--harness", "stub", "--model", "stub")
	if code != 0 {
		t.Fatalf("exit %d stdout %s stderr %s", code, stdout, stderr)
	}
	body, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"SKILL-MARKER-7",
		"intent: prove nothing this change touched broke",
		"diff: HEAD",
		"floor: store-opened",
		"suggestion: item-added",
		"The launcher ignores your message and returns verilex's own JSON verdict.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("prompt missing %q\n%s", want, text)
		}
	}
	if strings.Contains(stdout, "item-added") && strings.Contains(stdout, "claims") {
		t.Fatalf("suggestion JSON became the verdict: %s", stdout)
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
	if code != 2 || !strings.Contains(stderr, "--allow-harness") {
		t.Fatalf("exit %d stderr %s", code, stderr)
	}
	if _, err := os.Stat(touched); !os.IsNotExist(err) {
		t.Fatal("harness ran without --allow-harness")
	}
}

func TestSameHomeIsRefused(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	fake := writeFake(t, dir, fakeFiles{ticket: ticketJSON("stub", "stub"), run: []byte("{\"verdict\":\"green\",\"run\":\"hold\"}\n")})
	hold := filepath.Join(dir, "hold")
	release := filepath.Join(dir, "release")
	brain := writeBrain(t, dir, "#!/bin/sh\ntouch \"$HOLD\"\nwhile [ ! -f \"$RELEASE\" ]; do sleep 0.02; done\nverilex run --json 'store-open'\n")
	t.Setenv("HOLD", hold)
	t.Setenv("RELEASE", release)
	firstErr := make(chan error, 1)
	go func() {
		_, stderr, code, err := launchRaw(dir, fake, brain, "--home", home, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub")
		if err != nil {
			firstErr <- err
			return
		}
		if code != 0 {
			firstErr <- fmt.Errorf("first exit %d: %s", code, stderr)
			return
		}
		firstErr <- nil
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(hold); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(hold); err != nil {
		t.Fatal("first run did not start")
	}
	_, stderr, code := launch(t, dir, fake, brain, "--home", home, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub")
	os.WriteFile(release, []byte("go\n"), 0o600)
	if err := <-firstErr; err != nil {
		t.Fatal(err)
	}
	if code != 2 || !strings.Contains(stderr, "in use") {
		t.Fatalf("exit %d stderr %s", code, stderr)
	}
}

func TestParallelRunsKeepSeparateHomesAndSharedLedger(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Fatal("python3 is required for the tally product")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	product := copyProduct(t, dir)
	ledger := filepath.Join(dir, "ledger")
	if err := os.MkdirAll(ledger, 0o700); err != nil {
		t.Fatal(err)
	}
	admit(t, product, ledger)
	homes := []string{filepath.Join(dir, "home-a"), filepath.Join(dir, "home-b")}
	brains := []string{
		writeBrain(t, filepath.Join(dir, "brain-a"), "#!/bin/sh\nprintf '%s\\n' 'BRAIN SAYS GREEN'\nverilex run --json 'store-open | item-stored apple | item-listed apple'\n"),
		writeBrain(t, filepath.Join(dir, "brain-b"), "#!/bin/sh\nprintf '%s\\n' 'BRAIN SAYS GREEN'\nverilex run --json 'store-open | item-stored apple | item-listed apple'\n"),
	}
	errc := make(chan error, 2)
	for i, home := range homes {
		home, brain, i := home, brains[i], i
		go func() {
			stdout, stderr, code, err := launchRaw(dir, coreBin, brain,
				"--project", product, "--home", home, "--ledger", ledger,
				"--intent", "prove a stored apple is listed", "--harness", "stub", "--model", "stub")
			if err != nil {
				errc <- err
				return
			}
			if code != 0 {
				errc <- fmt.Errorf("run %d exit %d\n%s\n%s", i, code, stdout, stderr)
				return
			}
			if strings.Contains(stdout, "BRAIN") {
				errc <- fmt.Errorf("run %d leaked the brain: %s", i, stdout)
				return
			}
			var doc struct {
				Verdict string `json:"verdict"`
				Run     string `json:"run"`
				Skipped bool   `json:"skipped"`
			}
			if err = json.Unmarshal([]byte(stdout), &doc); err != nil {
				errc <- fmt.Errorf("run %d: %v\n%s", i, err, stdout)
				return
			}
			if doc.Verdict != "green" || doc.Run == "" {
				errc <- fmt.Errorf("run %d verdict %+v", i, doc)
				return
			}
			record := filepath.Join(home, "tally", "runs", doc.Run, "run.json")
			body, err := os.ReadFile(record)
			if err != nil {
				errc <- fmt.Errorf("run %d record: %v", i, err)
				return
			}
			if !strings.Contains(string(body), `"verdict": "green"`) || !strings.Contains(string(body), doc.Run) {
				errc <- fmt.Errorf("run %d record does not match stdout", i)
				return
			}
			if !doc.Skipped && !strings.Contains(string(body), filepath.Join(home, "stores")) {
				errc <- fmt.Errorf("run %d instance is not under its home: %s", i, body)
				return
			}
			errc <- nil
		}()
	}
	for range 2 {
		if err := <-errc; err != nil {
			t.Fatal(err)
		}
	}
	other := filepath.Join(dir, "home-c")
	brain := writeBrain(t, dir, "#!/bin/sh\nprintf '%s\\n' 'BRAIN SAYS RED'\nverilex run --json 'store-open | item-stored apple | item-listed apple'\n")
	stdout, stderr, code := launch(t, dir, coreBin, brain,
		"--project", product, "--home", other, "--ledger", ledger,
		"--intent", "prove a stored apple is listed", "--harness", "stub", "--model", "stub")
	if code != 0 {
		t.Fatalf("skip run exit %d\n%s\n%s", code, stdout, stderr)
	}
	var doc struct {
		Skipped bool   `json:"skipped"`
		Verdict string `json:"verdict"`
		Run     string `json:"run"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("skip JSON: %v\n%s", err, stdout)
	}
	if !doc.Skipped || doc.Verdict != "green" {
		t.Fatalf("third run did not skip: %+v\n%s", doc, stdout)
	}
	stores, err := os.ReadDir(filepath.Join(other, "stores"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stores) != 0 {
		t.Fatalf("skipped run launched a store: %v", stores)
	}
	if strings.Contains(stdout, "BRAIN") {
		t.Fatalf("brain text in skip verdict: %s", stdout)
	}
}

func TestBoundaryPassesOnThisRepoAndFailsOnAHarnessNameOrImport(t *testing.T) {
	script := filepath.Join(repo, "scripts", "boundary")
	out, err := exec.Command(script, repo).CombinedOutput()
	if err != nil {
		t.Fatalf("repo boundary: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "boundary: ok") {
		t.Fatalf("stdout = %s", out)
	}
	bad := t.TempDir()
	if err = os.MkdirAll(filepath.Join(bad, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(bad, "internal", "driver.go"), []byte("package internal\n\nvar harness = \"claude-code\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = exec.Command(script, bad).CombinedOutput()
	if err == nil || !strings.Contains(string(out), "claude-code") {
		t.Fatalf("harness fixture err=%v\n%s", err, out)
	}
	imp := t.TempDir()
	if err = os.MkdirAll(filepath.Join(imp, "cmd"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(imp, "cmd", "main.go"), []byte("package main\nimport _ \"github.com/DereKk8/verilex/agent/internal/cli\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = exec.Command(script, imp).CombinedOutput()
	if err == nil || !strings.Contains(string(out), "imports the agent module") {
		t.Fatalf("import fixture err=%v\n%s", err, out)
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
}

func writeFake(t *testing.T, dir string, files fakeFiles) string {
	t.Helper()
	path := filepath.Join(dir, "fake-verilex")
	script := "#!/bin/sh\ncmd=\nprev=\nfor arg in \"$@\"; do\n  case \"$prev\" in\n    --project|--continue|--ticket|--intent|--changed|--implements|--verdict|--same-as|--claim|--claims|--diff) prev=; continue ;;\n  esac\n  case \"$arg\" in\n    --project|--continue|--ticket|--intent|--changed|--implements|--verdict|--same-as|--claim|--claims|--diff) prev=$arg; continue ;;\n    --*) continue ;;\n    *) cmd=$arg; break ;;\n  esac\ndone\ncase \"$cmd\" in\n  ticket) cat \"$FAKE_TICKET\"; exit 0 ;;\n  plan) cat \"$FAKE_PLAN\"; exit 0 ;;\n  run) cat \"$FAKE_RUN\"; exit \"$FAKE_RUN_EXIT\" ;;\n  *) echo \"fake verilex: unexpected $cmd\" >&2; exit 2 ;;\nesac\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
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
	t.Setenv("FAKE_TICKET", write("ticket.json", files.ticket))
	t.Setenv("FAKE_PLAN", write("plan.json", files.plan))
	t.Setenv("FAKE_RUN", write("run.json", files.run))
	t.Setenv("FAKE_RUN_EXIT", fmt.Sprint(files.runExit))
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
	skill := filepath.Join(dir, "skill.md")
	if _, err := os.Stat(skill); err != nil {
		if err = os.WriteFile(skill, []byte("skill text\n"), 0o600); err != nil {
			return "", "", 0, err
		}
	}
	argv := []string{"--verilex", verilex, "--skill", skill, "--project", dir}
	if brain != "" {
		argv = append(argv, "--brain", brain)
	}
	argv = append(argv, args...)
	cmd := exec.Command(agentBin, argv...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err == nil {
		return string(out), "", 0, nil
	}
	exit, ok := err.(*exec.ExitError)
	if !ok {
		return string(out), "", 0, err
	}
	return string(out), string(exit.Stderr), exit.ExitCode(), nil
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
