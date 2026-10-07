// Package harness turns a resolved run spec into the command that is the brain.
// A real harness runs only when the caller opts in. The stub command is the test brain.
package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Spec is the brain choice the core resolved, plus the launcher's own opt-in command.
type Spec struct {
	Name          string
	Model         string
	Effort        string
	Brain         string // executable path; wins over a template
	AllowHarness  bool
	TemplatesPath string
}

// Brain is the command to exec and the host files it reads. The prompt is written by the caller
// and passed on stdin.
type Brain struct {
	Argv []string
	// Files are the harness's credentials and the like. A "~/" path is relative to the user's
	// home, and the sandbox shows it read-only at the same place in the brain's own home; any
	// other path is shown read-only where it is.
	Files []string
}

// template is one harness driver: its argv, where {model} and {effort} are replaced, and the
// files it needs from the user's home.
type template struct {
	Argv  []string `json:"argv"`
	Files []string `json:"files"`
}

// Resolve returns the brain for spec. A Brain path is a single executable.
func Resolve(spec Spec) (Brain, error) {
	if spec.Brain != "" {
		info, err := os.Stat(spec.Brain)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			return Brain{}, fmt.Errorf("--brain %s is not an executable file", spec.Brain)
		}
		return Brain{Argv: []string{spec.Brain}}, nil
	}
	if spec.Name == "stub" || spec.Name == "" {
		name := spec.Name
		if name == "" {
			name = "stub"
		}
		return Brain{}, fmt.Errorf("harness %s needs --brain, an executable the launcher starts", name)
	}
	if !spec.AllowHarness {
		return Brain{}, fmt.Errorf("harness %s is not started unless --allow-harness is set; pass --brain to run a named executable instead", spec.Name)
	}
	tmpl, ok, err := lookup(spec)
	if err != nil {
		return Brain{}, err
	}
	if !ok {
		return Brain{}, fmt.Errorf("harness %s has no driver; add it to the harnesses file or pass --brain", spec.Name)
	}
	return Brain{Argv: fill(tmpl.Argv, spec.Model, spec.Effort), Files: tmpl.Files}, nil
}

// InstallDir is the directory a harness command's files live in: the directory of the file
// argv0 resolves to on path, or the nearest enclosing package directory for a node package.
func InstallDir(argv0, path string) string {
	exe := argv0
	if !strings.Contains(argv0, "/") {
		exe = ""
		for _, dir := range filepath.SplitList(path) {
			candidate := filepath.Join(dir, argv0)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				exe = candidate
				break
			}
		}
	}
	real, err := filepath.EvalSymlinks(exe)
	if exe == "" || err != nil {
		return ""
	}
	dir := filepath.Dir(real)
	for up, i := dir, 0; i < 4 && up != "/"; up, i = filepath.Dir(up), i+1 {
		if _, err := os.Stat(filepath.Join(up, "package.json")); err == nil {
			return up
		}
	}
	return dir
}

func lookup(spec Spec) (template, bool, error) {
	if spec.TemplatesPath != "" {
		tmpl, ok, err := fromFile(spec.TemplatesPath, spec.Name)
		if err != nil || ok {
			return tmpl, ok, err
		}
	}
	tmpl, ok := builtins[spec.Name]
	return tmpl, ok, nil
}

// fromFile reads a harnesses file: each name maps to an argv list, or to {"argv": [...],
// "files": [...]}.
func fromFile(path, name string) (template, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return template{}, false, fmt.Errorf("harnesses %s: %v", path, err)
	}
	var file map[string]json.RawMessage
	if err = json.Unmarshal(data, &file); err != nil {
		return template{}, false, fmt.Errorf("harnesses %s: malformed JSON: %v", path, err)
	}
	raw, ok := file[name]
	if !ok {
		return template{}, false, nil
	}
	var tmpl template
	if err = json.Unmarshal(raw, &tmpl.Argv); err != nil {
		if err = json.Unmarshal(raw, &tmpl); err != nil {
			return template{}, false, fmt.Errorf("harnesses %s: %s is neither an argv list nor {\"argv\", \"files\"}", path, name)
		}
	}
	if len(tmpl.Argv) == 0 || tmpl.Argv[0] == "" {
		return template{}, false, fmt.Errorf("harnesses %s: %s has an empty command", path, name)
	}
	return tmpl, true, nil
}

func fill(argv []string, model, effort string) []string {
	out := make([]string, len(argv))
	for i, arg := range argv {
		arg = strings.ReplaceAll(arg, "{model}", model)
		arg = strings.ReplaceAll(arg, "{effort}", effort)
		out[i] = arg
	}
	return out
}

// builtins run only with --allow-harness. Each harness's own permission prompts and sandbox are
// off: the brain already runs in the launcher's sandbox, and a headless brain that must ask
// before each command cannot run verilex at all. Files are the credentials each harness reads.
var builtins = map[string]template{
	"claude": {
		Argv:  []string{"claude", "-p", "--model", "{model}", "--effort", "{effort}", "--permission-mode", "bypassPermissions"},
		Files: []string{"~/.claude/.credentials.json"},
	},
	"claude-code": {
		Argv:  []string{"claude", "-p", "--model", "{model}", "--effort", "{effort}", "--permission-mode", "bypassPermissions"},
		Files: []string{"~/.claude/.credentials.json"},
	},
	"codex": {
		Argv:  []string{"codex", "exec", "-m", "{model}", "-c", "model_reasoning_effort={effort}", "--dangerously-bypass-approvals-and-sandbox"},
		Files: []string{"~/.codex/auth.json"},
	},
	"pi": {
		Argv:  []string{"pi", "--print", "--no-session", "--model", "{model}", "--thinking", "{effort}"},
		Files: []string{"~/.pi/agent/auth.json"},
	},
}
