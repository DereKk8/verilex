// Package verdict owns the three verdicts and the honesty rules that turn a word's report into one.
package verdict

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
)

// Verdict is verilex's judgment of a step or a run.
type Verdict string

const (
	// Green means the product works, backed by evidence.
	Green Verdict = "green"
	// Red means the product is broken, backed by evidence.
	Red Verdict = "red"
	// Inconclusive covers environment or harness trouble and anything unbacked; it is never a product failure.
	Inconclusive Verdict = "inconclusive"
)

// ExitCode is the process exit code that reports v.
func (v Verdict) ExitCode() int {
	switch v {
	case Green:
		return 0
	case Red:
		return 1
	default:
		return 2
	}
}

// Overrides reports whether next replaces current as a run's verdict: the first verdict stands,
// except that inconclusive trumps red, because a red found in an untrustworthy instance is unbacked.
func Overrides(current *Verdict, next Verdict) bool {
	return current == nil || (*current == Red && next == Inconclusive)
}

// Legacy maps a label written by earlier versions (pass, fail, blocked, unverified) onto a verdict.
func Legacy(label string) Verdict {
	switch label {
	case "pass", string(Green):
		return Green
	case "fail", string(Red):
		return Red
	default:
		return Inconclusive
	}
}

// Reports a word may make in its result object, and the exit code each must come with.
var reports = map[string]struct {
	verdict Verdict
	exit    int
}{"pass": {Green, 0}, "fail": {Red, 1}, "blocked": {Inconclusive, 2}}

// ReportExit is the exit code that matches a word's report.
func ReportExit(report string) int {
	if c, ok := reports[report]; ok {
		return c.exit
	}
	return 2
}

// Judgment is a word's verdict together with what the word said.
type Judgment struct {
	Verdict             Verdict
	Reported            string
	Reason              any
	Observation, Detail any
}

// Judge applies the honesty rules to a word's exit code (nil on timeout) and its stdout.
func Judge(code *int, stdout []byte, timeout int) Judgment {
	if code == nil {
		return Judgment{Verdict: Inconclusive, Reason: fmt.Sprintf("timed out after %ds", timeout)}
	}
	var raw any
	if json.Unmarshal(stdout, &raw) != nil {
		return Judgment{Verdict: Inconclusive, Reason: "stdout is not one result JSON object"}
	}
	m, ok := raw.(map[string]any)
	var j Judgment
	if ok {
		j.Reported, _ = m["verdict"].(string)
		j.Observation = m["observation"]
		j.Detail = m["detail"]
	}
	report, known := reports[j.Reported]
	if !ok || !known {
		return Judgment{Verdict: Inconclusive, Reason: "result has no verdict of pass, fail or blocked"}
	}
	j.Verdict, j.Reason = Inconclusive, j.Detail
	held, _ := m["preconditions_held"].(bool)
	switch {
	case report.exit != *code:
		j.Reason = fmt.Sprintf("exit %d disagrees with verdict %s", *code, j.Reported)
	case report.verdict == Green && (!dictionary.Truthy(j.Observation) || strings.TrimSpace(fmt.Sprint(j.Observation)) == ""):
		j.Reason = "pass without a second observation"
	case report.verdict == Red && !held:
		j.Reason = "fail without stating that its preconditions held"
	default:
		j.Verdict = report.verdict
	}
	return j
}
