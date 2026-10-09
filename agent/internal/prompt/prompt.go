// Package prompt builds the text the launcher hands the brain. The brain's reply is not a verdict.
package prompt

import (
	"strings"
)

// DefaultIntent is the intent of a run whose spec names a diff and no intent.
const DefaultIntent = "prove nothing this change touched broke"

// Input is everything the brain is given before it drives verilex.
type Input struct {
	Skill       []byte
	Intent      string
	Diff        string
	Named       []string
	Changed     []string
	Suggestions []string
}

// Verify returns the prompt of the run phase: the skill, the run spec, the rules and the task.
func Verify(in Input) string {
	return shared(in) + "\n# Task\nProve the intent now. Run verilex yourself, by the rules above, until your last verilex run prints its verdict. Do not ask for instructions: nobody reads your reply.\n"
}

// Suggest returns the prompt of the suggest phase, which asks for extra claims and must not run
// verilex.
func Suggest(in Input) string {
	return shared(in) + "\n# Suggest\nReply with one JSON object and nothing else: {\"claims\":[\"name\"]}.\nDo not call verilex. A suggestion is not a verdict and not a ceiling.\n"
}

// shared is what both phases are given. Skill bytes are copied as written.
func shared(in Input) string {
	var b strings.Builder
	b.WriteString("# verilex agent skill\n")
	b.Write(in.Skill)
	if len(in.Skill) == 0 || in.Skill[len(in.Skill)-1] != '\n' {
		b.WriteByte('\n')
	}
	b.WriteString("\n# Run\n")
	b.WriteString("intent: " + in.Intent + "\n")
	if in.Diff != "" {
		b.WriteString("diff: " + in.Diff + "\n")
	} else {
		b.WriteString("diff: none\n")
	}
	writeList(&b, "floor", in.Named)
	writeList(&b, "changed", in.Changed)
	writeList(&b, "suggestion", in.Suggestions)
	b.WriteString("\n# Rules\n")
	b.WriteString("You are the brain. verilex is a model-free tool. Drive it only through the verilex command on PATH.\n")
	b.WriteString("Derive claims from the intent. Named claims are a floor, never a ceiling: pass each floor claim with --named.\n")
	b.WriteString("When the intent is the default, it means: prove nothing this change touched broke.\n")
	b.WriteString("Pass every changed path with --changed. Call verilex plan with --claim, --named and --changed, then verilex run with the same flags and no chain.\n")
	b.WriteString("If the plan lists unpicked, or the verdict says a touched claim is not covered, add that claim with --claim and run again.\n")
	b.WriteString(Rule)
	b.WriteString("You may run index, plan, run, words, claims, runs, ticket and check, never with --keep or --continue.\n")
	b.WriteString("The project is read-only: verilex tests the code as it is. Write scratch files only in $HOME or $TMPDIR.\n")
	return b.String()
}

// Rule is docs/verdict-rule.md as the brain reads it: how the launcher turns the brain's verilex
// runs into one result. verdict.Decide implements it, so change the three together.
const Rule = "The launcher returns verilex's own JSON verdict and ignores your message. It adds --no-chain to every verilex run and plan, so verilex refuses any chain argument, an empty one too: run only planned commands, with --claim, --named and --changed. The first case that applies decides:\n" +
	"1. A run whose exit code disagrees with its JSON, or that verilex did not plan: inconclusive.\n" +
	"2. A red: that red, whatever runs follow.\n" +
	"3. The time budget ended before you finished: inconclusive.\n" +
	"4. No verilex run: inconclusive.\n" +
	"5. Your last run is inconclusive: that run.\n" +
	"6. Your last run is green: that green only if it proved every claim that an earlier inconclusive run selected, proved every floor claim, and passed every changed path with --changed. Otherwise inconclusive.\n"

func writeList(b *strings.Builder, label string, items []string) {
	if len(items) == 0 {
		b.WriteString(label + ": none\n")
		return
	}
	for _, item := range items {
		b.WriteString(label + ": " + item + "\n")
	}
}
