// Package harness turns a resolved run spec into the command that is the brain.
// A real harness runs only when the caller opts in. The stub command is the test brain.
package harness

import (
	"encoding/json"
	"fmt"
	"os"
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

// Argv is the command line to exec. A Brain path is a single executable.
// A template's {model} and {effort} are replaced. The prompt is written by the caller and passed on stdin.
func Argv(spec Spec) ([]string, error) {
	if spec.Brain != "" {
		info, err := os.Stat(spec.Brain)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			return nil, fmt.Errorf("--brain %s is not an executable file", spec.Brain)
		}
		return []string{spec.Brain}, nil
	}
	if spec.Name == "stub" || spec.Name == "" {
		name := spec.Name
		if name == "" {
			name = "stub"
		}
		return nil, fmt.Errorf("harness %s needs --brain, an executable the launcher starts", name)
	}
	if !spec.AllowHarness {
		return nil, fmt.Errorf("harness %s is not started unless --allow-harness is set; pass --brain to run a named executable instead", spec.Name)
	}
	argv, ok, err := template(spec)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("harness %s has no driver; add it to the harnesses file or pass --brain", spec.Name)
	}
	return fill(argv, spec.Model, spec.Effort), nil
}

func template(spec Spec) ([]string, bool, error) {
	if spec.TemplatesPath != "" {
		argv, ok, err := fromFile(spec.TemplatesPath, spec.Name)
		if err != nil || ok {
			return argv, ok, err
		}
	}
	argv, ok := builtins[spec.Name]
	return append([]string(nil), argv...), ok, nil
}

func fromFile(path, name string) ([]string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("harnesses %s: %v", path, err)
	}
	var file map[string][]string
	if err = json.Unmarshal(data, &file); err != nil {
		return nil, false, fmt.Errorf("harnesses %s: malformed JSON: %v", path, err)
	}
	argv, ok := file[name]
	if !ok {
		return nil, false, nil
	}
	if len(argv) == 0 || argv[0] == "" {
		return nil, false, fmt.Errorf("harnesses %s: %s has an empty command", path, name)
	}
	return argv, true, nil
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

// builtins match the flags no-mistakes uses for these harnesses. They run only with --allow-harness.
var builtins = map[string][]string{
	"claude":      {"claude", "-p", "--model", "{model}", "--effort", "{effort}"},
	"claude-code": {"claude", "-p", "--model", "{model}", "--effort", "{effort}"},
	"codex":       {"codex", "exec", "-m", "{model}", "-c", "model_reasoning_effort={effort}"},
	"pi":          {"pi", "--print", "--no-session", "--model", "{model}", "--thinking", "{effort}"},
}
