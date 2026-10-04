package runner

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/dlclark/regexp2"
)

var secretPatterns = []string{
	`-----BEGIN [A-Z ]*PRIVATE KEY-----`,
	`\bgh[pousr]_[A-Za-z0-9]{36,}`,
	`\bgithub_pat_[A-Za-z0-9_]{22,}`,
	`\bAKIA[0-9A-Z]{16}\b`,
	`\bxox[abprs]-[A-Za-z0-9-]{10,}`,
	`\bsk-ant-[A-Za-z0-9_-]{20,}`,
}

func patterns(project dictionary.Project) ([]*regexp2.Regexp, error) {
	result := []*regexp2.Regexp{}
	for _, pattern := range secretPatterns {
		r, err := dictionary.CompilePattern(pattern)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return append(result, project.SecretPatterns...), nil
}

func scan(dir string, patterns []*regexp2.Regexp) (string, error) {
	var leak string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := strings.ToValidUTF8(string(data), "\uFFFD")
		for _, pattern := range patterns {
			matched, err := pattern.MatchString(text)
			if err != nil {
				return err
			}
			if matched {
				leak = entry.Name()
				return fs.SkipAll
			}
		}
		return nil
	})
	return leak, err
}

type result struct {
	Verdict             string
	Observation, Detail any
	PreconditionsHeld   bool
}

func judge(code *int, evidence string, timeout int) (string, any, result, error) {
	var r result
	if code == nil {
		return "unverified", fmt.Sprintf("timed out after %ds", timeout), r, nil
	}
	data, err := os.ReadFile(filepath.Join(evidence, "stdout"))
	if err != nil {
		return "unverified", nil, r, err
	}
	var raw any
	if json.Unmarshal(data, &raw) != nil {
		return "unverified", "stdout is not one result JSON object", r, nil
	}
	m, ok := raw.(map[string]any)
	if ok {
		r.Verdict, _ = m["verdict"].(string)
		r.Observation = m["observation"]
		r.Detail = m["detail"]
		r.PreconditionsHeld, _ = m["preconditions_held"].(bool)
	}
	if !ok || (r.Verdict != "pass" && r.Verdict != "fail" && r.Verdict != "blocked") {
		return "unverified", "result has no verdict of pass, fail or blocked", result{}, nil
	}
	if ExitCode(r.Verdict) != *code {
		return "unverified", fmt.Sprintf("exit %d disagrees with verdict %s", *code, r.Verdict), r, nil
	}
	if r.Verdict == "pass" && (!dictionary.Truthy(r.Observation) || strings.TrimSpace(fmt.Sprint(r.Observation)) == "") {
		return "unverified", "pass without a second observation", r, nil
	}
	if r.Verdict == "fail" && !r.PreconditionsHeld {
		return "unverified", "fail without stating that its preconditions held", r, nil
	}
	return r.Verdict, r.Detail, r, nil
}
