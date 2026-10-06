package cli

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/index"
	"github.com/DereKk8/verilex/internal/report"
	"github.com/DereKk8/verilex/internal/runner"
	"github.com/DereKk8/verilex/internal/ticket"
	"github.com/DereKk8/verilex/internal/verdict"
)

// claimSurface plans claims and a diff, and runs that plan. plan and run share PlanClaims.
func claimSurface(project dictionary.Project, words []dictionary.Word, args options, resolved *ticket.Ticket, out, stderr io.Writer, refuse func(error) int) int {
	ix, err := index.Build(project)
	if err != nil {
		return refuse(err)
	}
	plan, err := ix.PlanClaims(project, args.claims, args.named, args.changed)
	if err != nil {
		return refuse(err)
	}
	if args.command == "plan" {
		if args.json {
			if err = encode(out, plan); err != nil {
				return refuse(err)
			}
			return 0
		}
		printClaimPlan(plan, out)
		return 0
	}
	if plan.Chain == "" {
		return printNothingTouched(args.json, out, refuse)
	}
	steps, err := dictionary.ParseChain(plan.Chain, words)
	if err != nil {
		return refuse(err)
	}
	if err = dictionary.CheckOrder(steps); err != nil {
		return refuse(err)
	}
	opts := runner.Options{Keep: args.keep, Fresh: args.fresh, Continue: args.from, Ticket: resolved, ForceLive: plan.ForceLive()}
	decided, err := runner.Decide(project, steps, opts)
	if err != nil {
		return refuse(err)
	}
	run, err := runner.New(project, plan.Chain, steps)
	if err != nil {
		return refuse(err)
	}
	record, runErr := run.Execute(decided, opts)
	if refused := (runner.Refused{}); errors.As(runErr, &refused) {
		return refuse(refused.Err)
	}
	if err = attachCoverage(&record, ix, plan.Unpicked, args.changed); err != nil {
		return refuse(err)
	}
	if record.Dir != "" {
		if err = runner.Save(record.Dir, &record); err != nil {
			return refuse(err)
		}
	}
	return writeRun(record, runErr, args.json, out, stderr, refuse)
}

// coverChain attaches a missed-claim warning to a chain run that was given a diff.
func coverChain(project dictionary.Project, record *runner.Record, changed []string) error {
	if len(changed) == 0 {
		return nil
	}
	ix, err := index.Build(project)
	if err != nil {
		return err
	}
	if err = attachCoverage(record, ix, ix.TouchedClaims(project, changed), changed); err != nil {
		return err
	}
	if record.Dir != "" {
		return runner.Save(record.Dir, record)
	}
	return nil
}

func attachCoverage(record *runner.Record, ix index.Index, missed []string, changed []string) error {
	proved := map[string]bool{}
	words := map[string]dictionary.Word{}
	for _, word := range ix.Words() {
		words[word.Name] = word
	}
	record.Claims = []runner.ClaimReport{}
	seen := map[string]bool{}
	for _, step := range record.Words {
		word, ok := words[step.Word]
		if !ok || word.Claim == nil {
			continue
		}
		name := ix.GroupOf(word.Claim.Name)
		if seen[name] {
			continue
		}
		seen[name] = true
		if step.Verdict == verdict.Green {
			proved[name] = true
		}
		item := runner.ClaimReport{
			Claim: name, Proves: step.Proves, Word: step.Word,
			Step: strings.Join(append([]string{step.Word}, step.Args...), " "), Verdict: step.Verdict, Evidence: step.Evidence,
		}
		if step.Verdict != verdict.Green {
			item.Expected = expectedOf(word)
			item.Got = gotOf(step)
			item.Next = nextCommand(name, changed, true)
		}
		record.Claims = append(record.Claims, item)
	}
	record.Uncovered = nil
	for _, name := range missed {
		if proved[name] {
			continue
		}
		record.Uncovered = append(record.Uncovered, runner.Uncovered{Claim: name, Next: nextCommand(name, changed, false)})
	}
	record.Warning = ""
	if n := len(record.Uncovered); n == 1 {
		record.Warning = "1 touched claim not covered"
	} else if n > 1 {
		record.Warning = fmt.Sprintf("%d touched claims not covered", n)
	}
	return nil
}

func expectedOf(word dictionary.Word) string {
	text := ""
	if word.Claim != nil {
		text = word.Claim.Evidence.Observation
		if strings.TrimSpace(text) == "" {
			text = word.Claim.Evidence.Action
		}
	}
	return report.OneLine(text)
}

func gotOf(step runner.WordRecord) string {
	switch {
	case dictionary.Truthy(step.Detail):
		return report.OneLine(fmt.Sprint(step.Detail))
	case dictionary.Truthy(step.Reason):
		return report.OneLine(fmt.Sprint(step.Reason))
	case dictionary.Truthy(step.Observation):
		return report.OneLine(fmt.Sprint(step.Observation))
	default:
		return step.Reported
	}
}

func nextCommand(claim string, changed []string, fresh bool) string {
	parts := []string{"verilex run"}
	if fresh {
		parts = append(parts, "--fresh")
	}
	parts = append(parts, "--claim", quoteText(claim))
	for _, change := range changed {
		parts = append(parts, "--changed", quoteText(change))
	}
	return strings.Join(parts, " ")
}

func printNothingTouched(asJSON bool, out io.Writer, refuse func(error) int) int {
	if asJSON {
		value := struct {
			Format    string               `json:"format"`
			Verdict   verdict.Verdict      `json:"verdict"`
			Reason    string               `json:"reason"`
			Claims    []runner.ClaimReport `json:"claims"`
			Uncovered []runner.Uncovered   `json:"uncovered"`
			Chain     string               `json:"chain"`
		}{index.PlanFormat, verdict.Green, index.NoIntent, []runner.ClaimReport{}, []runner.Uncovered{}, ""}
		if err := encode(out, value); err != nil {
			return refuse(err)
		}
		return 0
	}
	fmt.Fprintln(out, "green: nothing this change touched broke")
	return 0
}

func printClaimPlan(plan index.ClaimPlan, out io.Writer) {
	fmt.Fprintf(out, "plan: skip %d, run %d", len(plan.Skip), len(plan.Run))
	if plan.Intent == index.NoIntent {
		fmt.Fprintf(out, "; %s", plan.Intent)
	}
	if plan.Warning != "" {
		fmt.Fprintf(out, "; %s", plan.Warning)
	}
	fmt.Fprintln(out)
	for _, skip := range plan.Skip {
		fmt.Fprintf(out, "  skip  %s  %s  relies on run %s\n", skip.Claim, skip.Step, skip.ReliesOn)
		keys := make([]string, 0, len(skip.Fingerprints))
		for key := range skip.Fingerprints {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			fmt.Fprintf(out, "    %s  %s\n", key, skip.Fingerprints[key])
		}
	}
	for _, live := range plan.Run {
		fmt.Fprintf(out, "  run  %s  %s: %s\n", live.Claim, live.Step, report.OneLine(live.Reason))
	}
	if len(plan.Order) > 0 {
		fmt.Fprintf(out, "  order: %s\n", strings.Join(plan.Order, ", "))
	}
	if plan.Chain != "" {
		fmt.Fprintf(out, "  chain: %s\n", plan.Chain)
	}
	for _, name := range plan.Unpicked {
		fmt.Fprintf(out, "  unpicked  %s\n", name)
	}
}

func writeRun(record runner.Record, runErr error, asJSON bool, out, stderr io.Writer, refuse func(error) int) int {
	if asJSON {
		if err := encode(out, record); err != nil {
			return refuse(err)
		}
	} else {
		report.Human(record, out)
	}
	if runErr != nil {
		fmt.Fprintf(stderr, "verilex: %v\n", runErr)
		return 2
	}
	if record.Verdict == nil {
		return verdict.Inconclusive.ExitCode()
	}
	return record.Verdict.ExitCode()
}
