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
	if args.from != "" {
		return refuse(errors.New("--continue is not used with a claim plan; run without --continue"))
	}
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
		} else {
			printClaimPlan(plan, out)
		}
		if plan.Inconclusive != "" {
			return verdict.Inconclusive.ExitCode()
		}
		return 0
	}
	if plan.Inconclusive != "" {
		return nothingCovered(plan, args, out, refuse)
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
	record.Format = runner.ClaimRunFormat
	record.Requested = requested(args)
	record.Touched = plan.Touched
	record.Gaps = plan.Gaps
	if err = attachCoverage(&record, ix, plan.Selected, plan.Unpicked, args.changed); err != nil {
		return refuse(err)
	}
	if record.Dir != "" {
		if err = runner.Save(record.Dir, &record); err != nil {
			return refuse(err)
		}
	}
	return writeRun(record, runErr, args.json, out, stderr, refuse)
}

// coverChain attaches a missed-claim warning to a run of the caller's chain that was given a diff.
func coverChain(project dictionary.Project, chain string, steps []dictionary.Step, record *runner.Record, changed []string) error {
	if len(changed) == 0 {
		return nil
	}
	ix, err := index.Build(project)
	if err != nil {
		return err
	}
	record.Format = runner.ClaimRunFormat
	record.Requested = &runner.Request{Claims: []string{}, Named: []string{}, Changed: listed(changed), Chain: &chain}
	record.Touched = ix.TouchedClaims(project, changed)
	record.Gaps = ix.Gaps(project, changed, steps)
	if err = attachCoverage(record, ix, nil, record.Touched, changed); err != nil {
		return err
	}
	if record.Dir != "" {
		return runner.Save(record.Dir, record)
	}
	return nil
}

func attachCoverage(record *runner.Record, ix index.Index, selected, missed []string, changed []string) error {
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
	for _, name := range selected {
		if seen[name] {
			continue
		}
		seen[name] = true
		record.Claims = append(record.Claims, runner.ClaimReport{
			Claim: name, Verdict: verdict.Inconclusive, Got: "not run", Next: nextCommand(name, changed, true),
		})
	}
	record.Uncovered = nil
	for _, name := range missed {
		if seen[name] {
			continue
		}
		record.Uncovered = append(record.Uncovered, runner.Uncovered{Claim: name, Next: nextCommand(name, changed, false)})
	}
	var phrases []string
	if n := len(record.Uncovered); n == 1 {
		phrases = append(phrases, "1 touched claim not covered")
	} else if n > 1 {
		phrases = append(phrases, fmt.Sprintf("%d touched claims not covered", n))
	}
	record.Warning = strings.Join(append(phrases, record.Gaps.Phrases()...), ", ")
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

// nothingCovered answers a run whose diff no claim covers. Green would claim the product works
// with no evidence, so the verdict is inconclusive and nothing launches.
func nothingCovered(plan index.ClaimPlan, args options, out io.Writer, refuse func(error) int) int {
	if args.json {
		value := struct {
			Format    string          `json:"format"`
			Verdict   verdict.Verdict `json:"verdict"`
			Reason    string          `json:"reason"`
			Requested *runner.Request `json:"requested"`
			Touched   []string        `json:"touched"`
			runner.Gaps
			Claims    []runner.ClaimReport `json:"claims"`
			Uncovered []runner.Uncovered   `json:"uncovered"`
		}{runner.ClaimRunFormat, verdict.Inconclusive, plan.Inconclusive, requested(args), plan.Touched, plan.Gaps, []runner.ClaimReport{}, []runner.Uncovered{}}
		if err := encode(out, value); err != nil {
			return refuse(err)
		}
	} else {
		fmt.Fprintf(out, "inconclusive: %s\n", plan.Inconclusive)
	}
	return verdict.Inconclusive.ExitCode()
}

func requested(args options) *runner.Request {
	return &runner.Request{Claims: listed(args.claims), Named: listed(args.named), Changed: listed(args.changed)}
}

func listed(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func printClaimPlan(plan index.ClaimPlan, out io.Writer) {
	if plan.Inconclusive != "" {
		fmt.Fprintf(out, "plan: inconclusive: %s\n", plan.Inconclusive)
		return
	}
	// When the chain runs live, a standing pass no longer means a skipped step: say proven.
	skipped := "skip"
	if plan.Rerun == "" {
		fmt.Fprintf(out, "plan: skip %d, run %d", len(plan.Skip), len(plan.Run))
	} else {
		skipped = "proven"
		fmt.Fprintf(out, "plan: whole chain runs live; run %d, proven %d", len(plan.Run), len(plan.Skip))
	}
	if plan.Intent == index.NoIntent {
		fmt.Fprintf(out, "; %s", plan.Intent)
	}
	if plan.Warning != "" {
		fmt.Fprintf(out, "; %s", plan.Warning)
	}
	if plan.Rerun != "" {
		fmt.Fprintf(out, "; %s", report.OneLine(plan.Rerun))
	}
	fmt.Fprintln(out)
	for _, skip := range plan.Skip {
		fmt.Fprintf(out, "  %s  %s  %s  relies on run %s\n", skipped, skip.Claim, skip.Step, skip.ReliesOn)
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
	report.Gaps(plan.Gaps, out)
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
