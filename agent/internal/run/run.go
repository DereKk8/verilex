// Package run launches one brain and returns the verdict verilex itself printed.
package run

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/DereKk8/verilex/agent/internal/harness"
	"github.com/DereKk8/verilex/agent/internal/prompt"
	"github.com/DereKk8/verilex/agent/internal/proxy"
	"github.com/DereKk8/verilex/agent/internal/sandbox"
	"github.com/DereKk8/verilex/agent/internal/tree"
	"github.com/DereKk8/verilex/agent/internal/verdict"
)

// Options is one launcher invocation. JSON is the default output.
type Options struct {
	Project, Verilex, Ticket, Intent, Diff string
	Claims                                 []string
	Harness, Model, Effort, Profile        string
	Ledger, Home, Skill, Brain, Harnesses  string
	Suggest, AllowHarness, JSON, KeepWork  bool
}

// Result is what the command prints. Stdout is verilex's document, never the brain's message.
// Work is the run's directory when KeepWork kept it, also when the run returns an error.
type Result struct {
	Stdout []byte
	Exit   int
	Work   string
}

// Inconclusive is a run that produced no verdict the launcher may return. It is exit 2, like
// verilex's own inconclusive, and never a product failure.
type Inconclusive struct{ Reason string }

func (e *Inconclusive) Error() string { return e.Reason }

func inconclusive(format string, args ...any) error {
	return &Inconclusive{Reason: fmt.Sprintf(format, args...)}
}

type budgetError struct{}

func (e *budgetError) Error() string { return "the time budget ended before the brain finished" }

func isBudget(err error) bool {
	var target *budgetError
	return errors.As(err, &target)
}

type ticket struct {
	Intent     string `json:"intent"`
	Diff       string `json:"diff"`
	Harness    string `json:"harness"`
	Model      string `json:"model"`
	Effort     string `json:"effort"`
	TimeBudget string `json:"time_budget"`
}

// dirs is one run's directory. Everything the run creates lives under root, so one removal
// cleans it up and one listing audits it. The brain may read share and sock, may write only
// brain, and cannot see the rest.
type dirs struct {
	root    string
	share   string // prompt, the brain's verilex command
	brain   string // the brain's home and temporary files
	log     string // the brain's output, its verilex commands and its network requests
	sock    string // the launcher's verilex socket
	sandbox string // each phase's sandbox profile and egress socket
	home    string // the verilex home when --home is not given
}

func newDirs() (dirs, error) {
	root, err := os.MkdirTemp(shortTemp(), "verilex-agent-")
	if err != nil {
		return dirs{}, err
	}
	d := dirs{
		root: root, share: filepath.Join(root, "share"), brain: filepath.Join(root, "brain"),
		log: filepath.Join(root, "log"), sock: filepath.Join(root, "sock"),
		sandbox: filepath.Join(root, "sandbox"), home: filepath.Join(root, "home"),
	}
	for _, dir := range []string{d.share, d.brain, d.log, d.sock, d.sandbox} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return d, err
		}
	}
	return d, nil
}

// Run resolves the spec through the verilex CLI, starts the brain in the sandbox, and returns
// verilex's verdict. It applies rows 1-8 of README "Verdict rule", and verdict.Decide the rest.
func Run(opts Options) (result Result, err error) {
	project, err := abs(opts.Project)
	if err != nil {
		return Result{}, err
	}
	opts.Project = project
	verilex, err := verilexPath(opts.Verilex)
	if err != nil {
		return Result{}, err
	}
	skill, err := loadSkill(opts.Skill, project)
	if err != nil {
		return Result{}, err
	}
	d, err := newDirs()
	if err != nil {
		return Result{}, err
	}
	if opts.KeepWork {
		defer func() { result.Work = d.root }()
	} else {
		defer os.RemoveAll(d.root)
	}
	spec, ticketPath, err := resolve(verilex, project, d.root, opts)
	if err != nil {
		return Result{}, err
	}
	brain, err := harness.Resolve(harness.Spec{
		Name: spec.Harness, Model: spec.Model, Effort: spec.Effort,
		Brain: opts.Brain, AllowHarness: opts.AllowHarness, TemplatesPath: opts.Harnesses,
	})
	if err != nil {
		return Result{}, err
	}
	intent := spec.Intent
	if intent == "" {
		intent = prompt.DefaultIntent
	}
	changed, err := changedFiles(project, spec.Diff)
	if err != nil {
		return Result{}, err
	}
	if spec.Diff != "" && len(changed) == 0 && spec.Intent == "" && len(opts.Claims) == 0 {
		return Result{}, inconclusive("diff %s changes no file under %s, so there is nothing to prove", spec.Diff, project)
	}
	budget, err := time.ParseDuration(or(spec.TimeBudget, "0s"))
	if err != nil {
		return Result{}, fmt.Errorf("time_budget: %v", err)
	}
	// One deadline covers the intent lookup and both brain phases, so none can stretch the budget.
	ctx, cancel := context.WithCancel(context.Background())
	if budget > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), budget)
	}
	defer cancel()
	named, err := floor(ctx, verilex, project, spec.Intent, opts.Claims)
	if err != nil {
		return Result{}, err
	}
	home, err := homeDir(opts.Home, d.home)
	if err != nil {
		return Result{}, err
	}
	if opts.Ledger != "" {
		// The ledger exists before the brain starts, so the sandbox can hide it.
		if err = os.MkdirAll(opts.Ledger, 0o700); err != nil {
			return Result{}, err
		}
	}
	before, err := tree.Snapshot(project)
	if err != nil {
		return Result{}, err
	}
	l, err := newLaunch(d, opts, brain, verilex, home, named)
	if err != nil {
		return Result{}, err
	}
	defer l.close()
	suggestions, err := l.suggest(ctx, skill, intent, spec.Diff, changed)
	if err != nil {
		if edited := tree.Changed(before, snapshotOrNil(project)); len(edited) > 0 {
			return Result{}, changedProject(edited)
		}
		return Result{}, phaseError(err)
	}
	text := prompt.Verify(prompt.Input{
		Skill: skill, Intent: intent, Diff: spec.Diff,
		Named: named, Changed: changed, Suggestions: suggestions,
	})
	promptPath := filepath.Join(d.share, "prompt.txt")
	if err = os.WriteFile(promptPath, []byte(text), 0o600); err != nil {
		return Result{}, err
	}
	release, err := lockHome(home)
	if err != nil {
		return Result{}, err
	}
	defer release()
	px, err := proxy.Listen(d.sock)
	if err != nil {
		return Result{}, err
	}
	defer px.Close()
	px.Verilex = verilex
	px.Project = project
	px.Home = home
	px.Ledger = opts.Ledger
	px.Ticket = ticketPath
	px.Env = os.Environ()
	px.Log = l.verilexLog
	go px.Serve()
	bin := filepath.Join(d.share, "bin")
	if err = writeWrapper(bin, px.Socket()); err != nil {
		return Result{}, err
	}
	brainErr := l.brain(ctx, phase{name: "verify", prompt: promptPath, bin: bin, sockets: []string{px.Socket()}})
	// Stop the proxy first: no verilex run may finish after the checks below.
	px.Close()
	var unavailable *sandbox.Unavailable
	if errors.As(brainErr, &unavailable) {
		return Result{}, phaseError(brainErr)
	}
	// The brain cannot write the project, but anything else on the host can: a verdict on code
	// that changed during the run is not the verdict on the code under test.
	if edited := tree.Changed(before, snapshotOrNil(project)); len(edited) > 0 {
		cleanHome(px)
		return Result{}, changedProject(edited)
	}
	if err = cleanHome(px); err != nil {
		return Result{}, inconclusive("%v", err)
	}
	var unfinished error
	if isBudget(brainErr) {
		unfinished = brainErr
	}
	doc, code, err := verdict.Decide(px.Runs(), verdict.Want{Project: project, Named: named, Changed: changed}, unfinished)
	if errors.Is(err, verdict.ErrNoRun) && brainErr != nil {
		return Result{}, inconclusive("%v, and made no verilex run, so there is no verdict", brainErr)
	}
	if err != nil {
		return Result{}, inconclusive("%v", err)
	}
	return output(doc, code, opts.JSON), nil
}

// output prints doc as verilex printed it, or its quiet human form.
func output(doc verdict.Doc, code int, asJSON bool) Result {
	if !asJSON {
		return Result{Stdout: []byte(verdict.Summary(doc)), Exit: code}
	}
	return Result{Stdout: doc.Raw, Exit: code}
}

// phaseError is the answer when a brain phase could not finish: the sandbox could not be
// established, or the budget ended.
func phaseError(err error) error {
	var unavailable *sandbox.Unavailable
	switch {
	case errors.As(err, &unavailable):
		return inconclusive("the brain runs only in a sandbox, and the sandbox is not available here: %s", unavailable.Reason)
	case isBudget(err):
		return inconclusive("%v", err)
	}
	return err
}

// changedProject is the answer when the project changed during the run: whatever verilex printed
// may be about code that is no longer the code under test.
func changedProject(paths []string) error {
	shown := paths
	if len(shown) > 10 {
		shown = append(slices.Clone(shown[:10]), fmt.Sprintf("and %d more", len(paths)-10))
	}
	return inconclusive("the project changed during the run, so no verdict covers the code under test: %s", strings.Join(shown, ", "))
}

func snapshotOrNil(project string) tree.State {
	state, err := tree.Snapshot(project)
	if err != nil {
		return tree.State{"(project unreadable)": err.Error()}
	}
	return state
}

// cleanHome tears down every instance left in the run's home and refuses a home the launcher
// did not fill on its own: a kept instance outlives the run, and a run that did not come
// through the proxy was not checked by it.
func cleanHome(px *proxy.Proxy) error {
	out, stderr, code := px.Exec("runs", "--json")
	if code != 0 {
		return fmt.Errorf("verilex runs in the run's home: %s", oneLine(string(stderr)))
	}
	var rows []struct {
		Run     string `json:"run"`
		Cleanup string `json:"cleanup"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return fmt.Errorf("verilex runs JSON: %v", err)
	}
	started := px.Started()
	var problems []string
	for _, row := range rows {
		if row.Cleanup == "kept" || row.Cleanup == "pending" {
			if _, stderr, code := px.Exec("cleanup", row.Run); code != 0 {
				problems = append(problems, fmt.Sprintf("run %s left an instance that cleanup could not tear down: %s", row.Run, oneLine(string(stderr))))
				continue
			}
		}
		if !started[row.Run] {
			problems = append(problems, fmt.Sprintf("run %s did not come through the launcher", row.Run))
		}
		if row.Cleanup == "kept" {
			problems = append(problems, fmt.Sprintf("run %s kept its instance; the launcher tore it down", row.Run))
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// launch holds what both brain phases share: the brain command, what it may read and what it
// must not reach.
type launch struct {
	d          dirs
	argv       []string
	read       []string
	files      map[string]string
	hide       []string
	project    string
	named      []string
	suggestOn  bool
	verilexLog *os.File
	egressLog  *os.File
}

func newLaunch(d dirs, opts Options, brain harness.Brain, verilex, home string, named []string) (*launch, error) {
	l := &launch{d: d, argv: brain.Argv, project: opts.Project, files: map[string]string{}, named: named, suggestOn: opts.Suggest}
	l.read = []string{d.share}
	if opts.Brain != "" {
		l.read = append(l.read, opts.Brain)
	} else if dir := harness.InstallDir(brain.Argv[0], os.Getenv("PATH")); dir != "" {
		l.read = append(l.read, dir)
	}
	userHome, _ := os.UserHomeDir()
	for _, file := range brain.Files {
		if rel, ok := strings.CutPrefix(file, "~/"); ok {
			if userHome != "" {
				l.files[rel] = filepath.Join(userHome, rel)
			}
		} else {
			l.read = append(l.read, file)
		}
	}
	// The brain must not reach what decides a verdict: the verilex home and ledger of this run
	// and the caller's, the real verilex binary, and this run's own records.
	l.hide = []string{home, opts.Ledger, verilex, os.Getenv("VERILEX_HOME"), os.Getenv("VERILEX_LEDGER"), d.log, d.home}
	var err error
	if l.verilexLog, err = os.Create(filepath.Join(d.log, "verilex.log")); err != nil {
		return nil, err
	}
	if l.egressLog, err = os.Create(filepath.Join(d.log, "egress.log")); err != nil {
		l.verilexLog.Close()
		return nil, err
	}
	return l, nil
}

func (l *launch) close() {
	l.verilexLog.Close()
	l.egressLog.Close()
}

// phase is one run of the brain: the suggest phase or the verify phase.
type phase struct {
	name, prompt string
	// bin holds the brain's verilex command.
	bin     string
	sockets []string
}

// brain runs one phase in the sandbox until the brain exits or ctx ends. Its output goes to the
// run's log, so a child the brain left running holds no pipe of the launcher's.
func (l *launch) brain(ctx context.Context, ph phase) error {
	if ctx.Err() != nil {
		return &budgetError{}
	}
	stdout, err := os.Create(filepath.Join(l.d.log, ph.name+".out"))
	if err != nil {
		return err
	}
	defer stdout.Close()
	stderr, err := os.Create(filepath.Join(l.d.log, ph.name+".err"))
	if err != nil {
		return err
	}
	defer stderr.Close()
	// The prompt goes in as a file, not a pipe: no copy can wait on a child the brain left running.
	stdin, err := os.Open(ph.prompt)
	if err != nil {
		return err
	}
	defer stdin.Close()
	proc, err := sandbox.Start(ctx, sandbox.Spec{
		Argv: l.argv, Env: brainEnv(ph), Project: l.project,
		Brain: l.d.brain, State: filepath.Join(l.d.sandbox, ph.name),
		Read: l.read, HomeFiles: l.files, Hide: l.hide, Sockets: ph.sockets,
		Stdin: stdin, Stdout: stdout, Stderr: stderr, EgressLog: l.egressLog,
	})
	if errors.Is(err, context.DeadlineExceeded) {
		return &budgetError{}
	}
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- proc.Wait() }()
	select {
	case err = <-done:
	case <-ctx.Done():
		proc.Kill()
		<-done
		return &budgetError{}
	}
	if err != nil {
		if detail := tail(stderr.Name()); detail != "" {
			return fmt.Errorf("brain exited: %v: %s", err, detail)
		}
		return fmt.Errorf("brain exited: %v", err)
	}
	return nil
}

func (l *launch) suggest(ctx context.Context, skill []byte, intent, diff string, changed []string) ([]string, error) {
	if !l.suggestOn {
		return nil, nil
	}
	ask := prompt.Suggest(prompt.Input{Skill: skill, Intent: intent, Diff: diff, Named: l.named, Changed: changed})
	path := filepath.Join(l.d.share, "suggest.txt")
	if err := os.WriteFile(path, []byte(ask), 0o600); err != nil {
		return nil, err
	}
	bin := filepath.Join(l.d.share, "suggest-bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		return nil, err
	}
	body := "#!/bin/sh\necho 'verilex-agent: refused: verilex is not available during intent formulation' >&2\nexit 2\n"
	if err := os.WriteFile(filepath.Join(bin, "verilex"), []byte(body), 0o700); err != nil {
		return nil, err
	}
	if err := l.brain(ctx, phase{name: "suggest", prompt: path, bin: bin}); err != nil {
		var unavailable *sandbox.Unavailable
		if isBudget(err) || errors.As(err, &unavailable) {
			return nil, err
		}
		return nil, fmt.Errorf("intent formulation: %v", err)
	}
	out, err := os.ReadFile(filepath.Join(l.d.log, "suggest.out"))
	if err != nil {
		return nil, err
	}
	return suggestedClaims(out)
}

func suggestedClaims(raw []byte) ([]string, error) {
	raw = bytes.TrimSpace(raw)
	start, end := bytes.IndexByte(raw, '{'), bytes.LastIndexByte(raw, '}')
	if start < 0 || end < start {
		return nil, errors.New("intent formulation returned no JSON claims")
	}
	var doc struct {
		Claims []string `json:"claims"`
	}
	if err := json.Unmarshal(raw[start:end+1], &doc); err != nil {
		return nil, fmt.Errorf("intent formulation returned malformed JSON: %v", err)
	}
	var out []string
	for _, claim := range doc.Claims {
		if strings.TrimSpace(claim) != "" {
			out = append(out, claim)
		}
	}
	return out, nil
}

// brainEnv is the launcher's environment without verilex's own variables, with the brain's
// verilex command first on PATH. The sandbox sets HOME, TMPDIR and the proxy.
func brainEnv(ph phase) []string {
	var env []string
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "VERILEX_HOME=") || strings.HasPrefix(entry, "VERILEX_LEDGER=") || strings.HasPrefix(entry, "PATH=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env,
		"PATH="+ph.bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"VERILEX_AGENT_PROMPT="+ph.prompt,
		"VERILEX_AGENT_PHASE="+ph.name,
	)
}

// tail is the end of a brain's stderr on one line, enough to say why it failed.
func tail(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := oneLine(string(data))
	if len(text) > 400 {
		text = "..." + text[len(text)-400:]
	}
	return text
}

func resolve(verilex, project, work string, opts Options) (ticket, string, error) {
	path := opts.Ticket
	if path == "" {
		path = filepath.Join(work, "ticket.yaml")
		if err := os.WriteFile(path, []byte(ticketYAML(opts)), 0o600); err != nil {
			return ticket{}, "", err
		}
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return ticket{}, "", err
	}
	var spec ticket
	err = verilexJSON(context.Background(), verilex, project, &spec, "ticket", "--json", path)
	if errors.Is(err, context.DeadlineExceeded) {
		return ticket{}, "", inconclusive("verilex ticket did not answer within %s", lookupTimeout)
	}
	if err != nil {
		return ticket{}, "", err
	}
	if spec.Harness == "" || spec.Model == "" {
		return ticket{}, "", errors.New("resolved ticket names no harness or model")
	}
	return spec, path, nil
}

// floor is the claims a green must prove: each --claim, and each claim verilex index finds for
// the intent, found by verilex, not by the brain. An intent that names no claim has no floor, so
// only --claim can stand for it. The lookup runs within ctx, the run's time budget.
func floor(ctx context.Context, verilex, project, intent string, claims []string) ([]string, error) {
	named := slices.Clone(claims)
	if intent == "" {
		return named, nil
	}
	var rows []struct {
		Claim string `json:"claim"`
	}
	err := verilexJSON(ctx, verilex, project, &rows, "index", "--intent", intent, "--json")
	switch {
	case ctx.Err() != nil:
		return nil, inconclusive("the time budget ended before verilex index --intent answered")
	case errors.Is(err, context.DeadlineExceeded):
		return nil, inconclusive("verilex index --intent did not answer within %s", lookupTimeout)
	case err != nil:
		// The launcher made this call itself, so a failure is the environment's, not the caller's.
		return nil, inconclusive("verilex index --intent failed, so the intent's claims are unknown: %v", err)
	}
	found := 0
	for _, row := range rows {
		if row.Claim != "" {
			found++
			if !slices.Contains(named, row.Claim) {
				named = append(named, row.Claim)
			}
		}
	}
	if found == 0 && len(claims) == 0 {
		return nil, inconclusive("intent %q names no claim (verilex index --intent found none), so no verilex run can prove it; name the claims with --claim", intent)
	}
	return named, nil
}

// lookupTimeout bounds each verilex command the launcher runs for itself, the ticket and the
// intent's claims. Both read local records, so one that takes longer is stuck.
const lookupTimeout = 30 * time.Second

// verilexJSON runs one read-only verilex command for the launcher itself and decodes its JSON
// into out. A refusal comes back as verilex worded it, and a deadline as context.DeadlineExceeded.
func verilexJSON(ctx context.Context, verilex, project string, out any, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, verilex, append([]string{"--project", project}, args...)...)
	cmd.Env = os.Environ()
	// A process group of its own, so the deadline ends every process it started.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		detail := oneLine(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return errors.New(strings.TrimPrefix(detail, "verilex: refused: "))
	}
	if err := json.Unmarshal(stdout.Bytes(), out); err != nil {
		return fmt.Errorf("verilex %s JSON: %v", args[0], err)
	}
	return nil
}

func ticketYAML(opts Options) string {
	var b strings.Builder
	write := func(key, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%s: %s\n", key, yamlString(value))
		}
	}
	write("intent", opts.Intent)
	write("diff", opts.Diff)
	write("profile", opts.Profile)
	write("harness", opts.Harness)
	write("model", opts.Model)
	write("effort", opts.Effort)
	return b.String()
}

func yamlString(value string) string {
	if value == "" || strings.ContainsAny(value, ":#{}[]&*!|>'\"%@`\n") || strings.HasPrefix(value, " ") {
		return fmt.Sprintf("%q", value)
	}
	return value
}

// changedFiles lists the paths diff changes under project, relative to it, unquoted, with both
// sides of a rename. A diff git cannot read is refused: an empty list would let a green skip
// the change.
func changedFiles(project, diff string) ([]string, error) {
	if diff == "" {
		return nil, nil
	}
	cmd := exec.Command("git", "diff", "--name-only", "-z", "--no-renames", "--relative", diff, "--")
	cmd.Dir = project
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("diff %s: %s", diff, or(oneLine(stderr.String()), err.Error()))
	}
	var files []string
	for _, path := range strings.Split(string(out), "\x00") {
		if path != "" {
			files = append(files, path)
		}
	}
	return files, nil
}

func loadSkill(path, project string) ([]byte, error) {
	if path == "" {
		for _, candidate := range []string{
			filepath.Join(project, "skills", "verilex", "SKILL.md"),
			"skills/verilex/SKILL.md",
		} {
			if _, err := os.Stat(candidate); err == nil {
				path = candidate
				break
			}
		}
	}
	if path == "" {
		return nil, errors.New("skill file not found; pass --skill or add skills/verilex/SKILL.md")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("skill %s: %v", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("skill %s is empty", path)
	}
	return data, nil
}

func writeWrapper(dir, socket string) error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(bin); err == nil {
		bin = real
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	body := "#!/bin/sh\nexec " + shellQuote(bin) + " relay --socket " + shellQuote(socket) + " -- \"$@\"\n"
	return os.WriteFile(filepath.Join(dir, "verilex"), []byte(body), 0o700)
}

func lockHome(home string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(home, "launcher.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("%s is in use by another verilex-agent run; each run needs its own home", home)
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// homeDir is the run's verilex home: the given path, or the run directory's own home.
func homeDir(path, fallback string) (string, error) {
	if path == "" {
		path = fallback
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", err
	}
	return abs(path)
}

func verilexPath(path string) (string, error) {
	if path == "" {
		found, err := exec.LookPath("verilex")
		if err != nil {
			return "", errors.New("verilex is not on PATH; pass --verilex")
		}
		return abs(found)
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("%s is not an executable file", path)
	}
	return abs(path)
}

func abs(path string) (string, error) {
	if path == "" {
		path = "."
	}
	full, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(full); err == nil {
		full = resolved
	}
	return full, nil
}

// shortTemp is where run directories go: a unix socket path must stay under about 100 bytes.
func shortTemp() string {
	dir := os.TempDir()
	if len(dir) > 40 {
		return "/tmp"
	}
	return dir
}

func or(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
