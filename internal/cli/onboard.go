package cli

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/grouping"
	"github.com/DereKk8/verilex/internal/onboarding"
	"github.com/DereKk8/verilex/internal/report"
)

// onboard runs the onboarding pipeline for one word and prints its outcome first, then only what
// went wrong, with evidence by reference.
func onboard(project dictionary.Project, args options, out io.Writer, refuse func(error) int) int {
	r, err := onboarding.Onboard(project, args.operand, onboarding.Choice{SameAs: args.sameAs, Distinct: args.distinct})
	if err != nil {
		return refuse(err)
	}
	if args.json {
		if err = encode(out, r); err != nil {
			return refuse(err)
		}
	} else if args.distinct {
		fmt.Fprintf(out, "onboarded %s: claim %s stays a claim of its own, as the agent decided\n", r.Word, r.Claim)
	} else {
		onboarded(r, out)
	}
	switch r.Outcome {
	case onboarding.Onboarded, onboarding.Already, onboarding.Undecided:
		return 0
	case onboarding.Rejected:
		return 1
	}
	return 2
}

func onboarded(r onboarding.Result, out io.Writer) {
	switch r.Outcome {
	case onboarding.Already:
		fmt.Fprintf(out, "onboarded %s already: it proves %s for %s, and nothing it was judged on changed\n", r.Word, grouped(r), r.Product)
		decide(r, out)
		return
	case onboarding.Onboarded:
		how := ""
		switch r.Match {
		case grouping.New:
			how = "claim " + r.Claim + " joins the vocabulary"
		case grouping.Same:
			how = "variant of " + grouped(r)
		case grouping.Mechanical:
			how = "variant of " + grouped(r) + ", matched mechanically"
		case grouping.Agent:
			fmt.Fprintf(out, "onboarded %s: variant of %s, as the agent decided\n", r.Word, grouped(r))
			return
		}
		if r.Record == "" {
			fmt.Fprintf(out, "onboarded %s for %s: it proves %s, and its correctness gate already holds\n", r.Word, r.Product, grouped(r))
			return
		}
		fmt.Fprintf(out, "onboarded %s: %s; caught %s\n", r.Word, how, strings.Join(slices.Sorted(maps.Keys(r.Decision.Defects)), ", "))
	case onboarding.Undecided:
		fmt.Fprintf(out, "undecided %s: claim %s joins the vocabulary as a claim of its own for now; caught %s\n", r.Word, r.Claim, strings.Join(slices.Sorted(maps.Keys(r.Decision.Defects)), ", "))
		decide(r, out)
	default:
		fmt.Fprintf(out, "%s %s: %s\n", r.Outcome, r.Word, report.OneLine(r.Reason))
		for _, t := range r.Trials {
			if t.Expect != "" && t.Verdict != t.Expect && t.Verdict != "inconclusive" && t.Verdict != "not run" {
				fmt.Fprintf(out, "  trial  %s  %s: %s, expected %s\n    evidence: %s\n", t.State, t.Chain, t.Verdict, t.Expect, t.Evidence)
			}
		}
		for _, f := range r.Findings {
			fmt.Fprintf(out, "  finding  %s\n    evidence: %s\n", f.Text, strings.Join(f.Evidence, " "))
		}
	}
	fmt.Fprintf(out, "  record: %s\n", r.Record)
}

// decide prints the grouping question an undecided word leaves to the calling agent.
func decide(r onboarding.Result, out io.Writer) {
	if r.Request == nil {
		return
	}
	pins := []string{}
	for _, c := range r.Request.Candidates {
		pins = append(pins, c.Claim+" (words: "+strings.Join(c.Words, ", ")+")")
	}
	fmt.Fprintf(out, "  decide: does claim %s say the same as %s? It behaved like their words on every trial state.\n", grouping.Name(r.Proves), strings.Join(pins, " or "))
	for _, command := range r.Request.Commands {
		fmt.Fprintf(out, "    %s\n", command)
	}
}

// grouped names the claim version a word is grouped under, and the claim it pins when that is an
// alias of it.
func grouped(r onboarding.Result) string {
	if grouping.Name(r.Proves) != grouping.Name(r.Claim) {
		return r.Claim + " through claim " + grouping.Name(r.Proves)
	}
	return r.Claim
}
