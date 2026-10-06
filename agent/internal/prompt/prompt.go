// Package prompt builds the text the launcher hands the brain. The brain's reply is not a verdict.
package prompt

import (
	"strings"
)

// Input is everything the brain is given before it drives verilex.
type Input struct {
	Skill       []byte
	Intent      string
	Diff        string
	Named       []string
	Changed     []string
	Suggestions []string
}

// Build returns the prompt. Skill bytes are copied as written.
func Build(in Input) string {
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
	b.WriteString("Derive claims from the intent. Named claims are a floor, never a ceiling.\n")
	b.WriteString("When the intent is the default, it means: prove nothing this change touched broke.\n")
	b.WriteString("Call verilex plan --json with --claim, --named and --changed, then verilex run --json with the same flags.\n")
	b.WriteString("If the plan lists unpicked, or the verdict says a touched claim is not covered, add that claim with --claim and run again.\n")
	b.WriteString("Do not decide the verdict. The launcher ignores your message and returns verilex's own JSON verdict.\n")
	b.WriteString("Do not share an instance with another run. Do not set VERILEX_HOME or VERILEX_LEDGER.\n")
	return b.String()
}

func writeList(b *strings.Builder, label string, items []string) {
	if len(items) == 0 {
		b.WriteString(label + ": none\n")
		return
	}
	for _, item := range items {
		b.WriteString(label + ": " + item + "\n")
	}
}
