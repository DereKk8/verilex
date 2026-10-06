// Package run launches one brain and returns the verdict verilex itself printed.
package run

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/DereKk8/verilex/agent/internal/harness"
	"github.com/DereKk8/verilex/agent/internal/prompt"
	"github.com/DereKk8/verilex/agent/internal/proxy"
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
type Result struct {
	Stdout []byte
	Exit   int
}

type budgetError struct{ budget time.Duration }

func (e *budgetError) Error() string {
	return fmt.Sprintf("time budget %s ended before the brain finished", e.budget)
}

func isBudget(err error) bool {
	var target *budgetError
	return errors.As(err, &target)
}

type ticket struct {
	Intent      string `json:"intent"`
	Diff        string `json:"diff"`
	Harness     string `json:"harness"`
	Model       string `json:"model"`
	Effort      string `json:"effort"`
	TokenBudget int    `json:"token_budget"`
	TimeBudget  string `json:"time_budget"`
}

// Run resolves the spec through the verilex CLI, starts the brain, and returns verilex's verdict.
func Run(opts Options) (Result, error) {
	project, err := abs(opts.Project)
	if err != nil {
		return Result{}, err
	}
	opts.Project = project
	verilex, err := verilexPath(opts.Verilex)
	if err != nil {
		return Result{}, err
	}
	skillPath, skill, err := loadSkill(opts.Skill, project)
	if err != nil {
		return Result{}, err
	}
	_ = skillPath
	work, err := os.MkdirTemp("", "verilex-agent-")
	if err != nil {
		return Result{}, err
	}
	if !opts.KeepWork {
		defer os.RemoveAll(work)
	}
	spec, err := resolve(verilex, project, work, opts)
	if err != nil {
		return Result{}, err
	}
	intent := spec.Intent
	if intent == "" {
		intent = verdict.DefaultIntent
	}
	changed, err := changedFiles(project, spec.Diff)
	if err != nil {
		return Result{}, err
	}
	suggestions, err := suggest(opts, spec, work, skill, intent, changed)
	if err != nil {
		return Result{}, err
	}
	text := prompt.Build(prompt.Input{
		Skill: skill, Intent: intent, Diff: spec.Diff,
		Named: opts.Claims, Changed: changed, Suggestions: suggestions,
	})
	promptPath := filepath.Join(work, "prompt.txt")
	if err = os.WriteFile(promptPath, []byte(text), 0o600); err != nil {
		return Result{}, err
	}
	home, created, err := homeDir(opts.Home)
	if err != nil {
		return Result{}, err
	}
	if created && !opts.KeepWork {
		defer os.RemoveAll(home)
	}
	release, err := lockHome(home)
	if err != nil {
		return Result{}, err
	}
	defer release()
	if err = os.MkdirAll(filepath.Join(home, "stores"), 0o700); err != nil {
		return Result{}, err
	}
	px, err := proxy.Listen(shortDir())
	if err != nil {
		return Result{}, err
	}
	defer px.Close()
	px.Verilex = verilex
	px.Project = project
	px.Home = home
	px.Ledger = opts.Ledger
	px.Env = os.Environ()
	go px.Serve()
	if err = writeWrapper(work, px.Socket()); err != nil {
		return Result{}, err
	}
	budget, err := time.ParseDuration(or(spec.TimeBudget, "0s"))
	if err != nil {
		return Result{}, fmt.Errorf("time_budget: %v", err)
	}
	brainErr := startBrain(opts, spec, work, promptPath, text, "verify", budget)
	if isBudget(brainErr) {
		return Result{}, brainErr
	}
	stdout, verilexExit, ok := px.LastRun()
	if !ok {
		if brainErr != nil {
			return Result{}, brainErr
		}
		return Result{}, errors.New("the brain exited without a verilex run, so there is no verdict")
	}
	doc, err := verdict.Parse(stdout)
	if err != nil {
		return Result{}, err
	}
	code, err := verdict.Exit(doc, verilexExit)
	out := verdict.Normalize(stdout)
	if err != nil {
		return Result{Stdout: out}, err
	}
	if missed := verdict.MissingWarning(px.LastPlan(), doc); len(missed) > 0 {
		return Result{Stdout: out}, fmt.Errorf("plan listed uncovered claim %s but the verdict carried no missed-claim warning", strings.Join(missed, ", "))
	}
	if !opts.JSON {
		out = []byte(verdict.Summary(doc) + "\n")
		for _, name := range doc.Missed {
			out = append(out, []byte("  "+name+"\n")...)
		}
	}
	return Result{Stdout: out, Exit: code}, nil
}

func suggest(opts Options, spec ticket, work string, skill []byte, intent string, changed []string) ([]string, error) {
	if !opts.Suggest {
		return nil, nil
	}
	ask := prompt.Build(prompt.Input{Skill: skill, Intent: intent, Diff: spec.Diff, Named: opts.Claims, Changed: changed})
	ask += "\n# Suggest\nReply with one JSON object and nothing else: {\"claims\":[\"name\"]}.\n"
	ask += "Do not call verilex. A suggestion is not a verdict and not a ceiling.\n"
	path := filepath.Join(work, "suggest.txt")
	if err := os.WriteFile(path, []byte(ask), 0o600); err != nil {
		return nil, err
	}
	block := filepath.Join(work, "suggest-bin")
	if err := os.MkdirAll(block, 0o700); err != nil {
		return nil, err
	}
	body := "#!/bin/sh\necho 'verilex-agent: refused: verilex is not available during intent formulation' >&2\nexit 2\n"
	if err := os.WriteFile(filepath.Join(block, "verilex"), []byte(body), 0o700); err != nil {
		return nil, err
	}
	var stdout bytes.Buffer
	if err := runBrain(opts, spec, block+string(os.PathListSeparator)+os.Getenv("PATH"), path, ask, "suggest", 0, &stdout); err != nil {
		return nil, fmt.Errorf("intent formulation: %v", err)
	}
	claims, err := suggestedClaims(stdout.Bytes())
	if err != nil {
		return nil, err
	}
	return claims, nil
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

func startBrain(opts Options, spec ticket, work, promptPath, text, phase string, budget time.Duration) error {
	path := filepath.Join(work, "bin") + string(os.PathListSeparator) + os.Getenv("PATH")
	return runBrain(opts, spec, path, promptPath, text, phase, budget, nil)
}

func runBrain(opts Options, spec ticket, path, promptPath, text, phase string, budget time.Duration, stdout *bytes.Buffer) error {
	argv, err := harness.Argv(harness.Spec{
		Name: spec.Harness, Model: spec.Model, Effort: spec.Effort,
		Brain: opts.Brain, AllowHarness: opts.AllowHarness, TemplatesPath: opts.Harnesses,
	})
	if err != nil {
		return err
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = opts.Project
	cmd.Env = brainEnv(path, promptPath, phase)
	cmd.Stdin = strings.NewReader(text)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if stdout != nil {
		cmd.Stdout = stdout
	} else {
		cmd.Stdout = ioDiscard{}
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	if budget > 0 {
		select {
		case err = <-done:
		case <-time.After(budget):
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
			return &budgetError{budget: budget}
		}
	} else {
		err = <-done
	}
	syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if err != nil {
		detail := oneLine(stderr.String())
		if detail == "" {
			return fmt.Errorf("brain exited: %v", err)
		}
		return fmt.Errorf("brain exited: %v: %s", err, detail)
	}
	return nil
}

func brainEnv(path, prompt, phase string) []string {
	var env []string
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "VERILEX_HOME=") || strings.HasPrefix(entry, "VERILEX_LEDGER=") || strings.HasPrefix(entry, "PATH=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env,
		"PATH="+path,
		"VERILEX_AGENT_PROMPT="+prompt,
		"VERILEX_AGENT_PHASE="+phase,
	)
}

func resolve(verilex, project, work string, opts Options) (ticket, error) {
	path := opts.Ticket
	if path == "" {
		path = filepath.Join(work, "ticket.yaml")
		if err := os.WriteFile(path, []byte(ticketYAML(opts)), 0o600); err != nil {
			return ticket{}, err
		}
	}
	cmd := exec.Command(verilex, "--project", project, "ticket", "--json", path)
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := oneLine(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return ticket{}, errors.New(strings.TrimPrefix(detail, "verilex: refused: "))
	}
	var spec ticket
	if err := json.Unmarshal(stdout.Bytes(), &spec); err != nil {
		return ticket{}, fmt.Errorf("verilex ticket JSON: %v", err)
	}
	if spec.Harness == "" || spec.Model == "" {
		return ticket{}, errors.New("resolved ticket names no harness or model")
	}
	return spec, nil
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
		return strconvQuote(value)
	}
	return value
}

func strconvQuote(value string) string {
	return fmt.Sprintf("%q", value)
}

func changedFiles(project, diff string) ([]string, error) {
	if diff == "" {
		return nil, nil
	}
	cmd := exec.Command("git", "-C", project, "diff", "--name-only", diff)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func loadSkill(path, project string) (string, []byte, error) {
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
		return "", nil, errors.New("skill file not found; pass --skill or add skills/verilex/SKILL.md")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("skill %s: %v", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return "", nil, fmt.Errorf("skill %s is empty", path)
	}
	return path, data, nil
}

func writeWrapper(work, socket string) error {
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Join(work, "bin")
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

func homeDir(path string) (string, bool, error) {
	if path == "" {
		dir, err := os.MkdirTemp("", "verilex-agent-home-")
		return dir, true, err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", false, err
	}
	return path, false, nil
}

func verilexPath(path string) (string, error) {
	if path == "" {
		found, err := exec.LookPath("verilex")
		if err != nil {
			return "", errors.New("verilex is not on PATH; pass --verilex")
		}
		return found, nil
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("%s is not an executable file", path)
	}
	return path, nil
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

func shortDir() string {
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

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
