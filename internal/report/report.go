// Package report owns quiet human output: verdict first, failures only, evidence by reference.
package report

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/runner"
	"github.com/DereKk8/verilex/internal/ticket"
	"github.com/DereKk8/verilex/internal/verdict"
)

const maxCause = 300

// Human prints the run's verdict and counts, then only the steps that are not green, each with
// a one-line cause and a path to its evidence.
func Human(record runner.Record, out io.Writer) {
	v := verdict.Inconclusive
	if record.Verdict != nil {
		v = *record.Verdict
	}
	fmt.Fprintf(out, "%s: %s", v, counts(record))
	if record.Skipped {
		fmt.Fprintf(out, ", skipped: stamps match %s", reliedOn(record))
	} else if record.Continues != "" {
		fmt.Fprintf(out, ", continued %s", record.Continues)
		if n := len(record.Words) - live(record); n > 0 {
			fmt.Fprintf(out, ", %d skipped: proven on its instance by %s", n, reliedOn(record))
		}
	}
	fmt.Fprintf(out, "; run %s\n", record.Run)
	labels := []string{}
	step := func(v verdict.Verdict, label, cause, evidence string) {
		labels = append(labels, label)
		if cause = oneLine(cause); cause != "" {
			label += ": " + cause
		}
		fmt.Fprintf(out, "  %s  %s\n    evidence: %s\n", v, label, evidence)
	}
	frames := map[string]runner.Frame{}
	for _, f := range record.Frame {
		frames[f.Step] = f
	}
	frame := func(name string) {
		if f, ok := frames[name]; ok {
			if cause := frameCause(f); cause != "" {
				step(verdict.Inconclusive, name, cause, f.Evidence)
			}
		}
	}
	frame("launch")
	frame("refresh")
	frame("doctor")
	for _, word := range record.Words {
		if word.Verdict == verdict.Green {
			continue
		}
		cause := ""
		if dictionary.Truthy(word.Reason) {
			cause = fmt.Sprint(word.Reason)
		}
		step(word.Verdict, strings.Join(append([]string{word.Word}, word.Args...), " "), cause, word.Evidence)
		fmt.Fprintf(out, "    verify skill: %s\n", strings.Join(word.Implements, ", "))
	}
	frame("doctor-after-failure")
	frame("cleanup")
	if v != verdict.Green && record.Reason != nil && *record.Reason != "" && !explained(*record.Reason, labels) {
		fmt.Fprintf(out, "  cause: %s\n", oneLine(*record.Reason))
	}
	if record.Cleanup == "kept" {
		fmt.Fprintf(out, "kept: tear down with `verilex cleanup %s`\n", record.Run)
	}
}

// Plan prints a skip decision: how many steps are skipped and run, the first reason a step runs
// live, and the run each skipped step relies on.
func Plan(p runner.Plan, out io.Writer) {
	skipped := 0
	for _, s := range p.Skip {
		if s.Skipped {
			skipped++
		}
	}
	fmt.Fprintf(out, "plan: skip %d, run %d", skipped, len(p.Skip)-skipped)
	if p.Continues != "" {
		fmt.Fprintf(out, " on the instance kept by %s", p.Continues)
	}
	if p.Rerun != "" {
		fmt.Fprintf(out, "; %s", oneLine(p.Rerun))
	}
	fmt.Fprintln(out)
	for _, s := range p.Skip {
		if s.Skipped {
			fmt.Fprintf(out, "  skip  %s  relies on run %s\n", s.Step, s.ReliesOn)
		}
	}
}

func live(record runner.Record) int {
	n := 0
	for _, word := range record.Words {
		if word.ReliesOn == "" {
			n++
		}
	}
	return n
}

func counts(record runner.Record) string {
	tally := map[verdict.Verdict]int{}
	for _, word := range record.Words {
		tally[word.Verdict]++
	}
	parts := []string{}
	for _, v := range []verdict.Verdict{verdict.Green, verdict.Red, verdict.Inconclusive} {
		if tally[v] > 0 || (v == verdict.Green && len(record.Words) == 0) {
			parts = append(parts, fmt.Sprintf("%d %s", tally[v], v))
		}
	}
	if missing := record.Steps - len(record.Words); missing > 0 {
		parts = append(parts, fmt.Sprintf("%d not run", missing))
	}
	return strings.Join(parts, ", ")
}

func reliedOn(record runner.Record) string {
	runs := []string{}
	seen := map[string]bool{}
	for _, word := range record.Words {
		if word.ReliesOn != "" && !seen[word.ReliesOn] {
			seen[word.ReliesOn] = true
			runs = append(runs, word.ReliesOn)
		}
	}
	if len(runs) == 1 {
		return "run " + runs[0]
	}
	return "runs " + strings.Join(runs, ", ")
}

func frameCause(f runner.Frame) string {
	if f.Leak != "" {
		return "secret pattern in evidence " + f.Leak
	}
	if f.Exit != nil && *f.Exit == 0 {
		return ""
	}
	exit := "timed out"
	if f.Exit != nil {
		exit = fmt.Sprintf("exit %d", *f.Exit)
	}
	switch f.Step {
	case "doctor", "doctor-after-failure":
		return "refused the instance (" + exit + ")"
	case "cleanup":
		return exit + "; the instance may outlive the run"
	}
	return exit
}

// explained reports whether a run's reason only repeats a step line already printed.
func explained(reason string, labels []string) bool {
	for _, label := range labels {
		if reason == label || strings.HasPrefix(reason, label+": ") || strings.HasPrefix(reason, label+" ") {
			return true
		}
	}
	return false
}

func oneLine(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > maxCause {
		text = string(runes[:maxCause-1]) + "…"
	}
	return text
}

// Ticket prints a resolved run ticket, one field per line, each resolved field with the level
// that set it.
func Ticket(t ticket.Ticket, out io.Writer) {
	tokens := ""
	if t.TokenBudget > 0 {
		tokens = strconv.Itoa(t.TokenBudget)
	}
	for _, field := range []struct{ name, value string }{
		{"intent", oneLine(t.Intent)}, {"diff", t.Diff}, {"profile", t.Profile}, {"harness", t.Harness}, {"model", t.Model},
		{"effort", t.Effort}, {"token_budget", tokens}, {"time_budget", t.TimeBudget},
	} {
		if field.value == "" {
			continue
		}
		fmt.Fprintf(out, "%s: %s", field.name, field.value)
		if from := t.From[field.name]; from != "" {
			fmt.Fprintf(out, "  from %s", from)
		}
		fmt.Fprintln(out)
	}
}
