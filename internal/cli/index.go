package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/index"
	"github.com/DereKk8/verilex/internal/lifecycle"
	"github.com/DereKk8/verilex/internal/report"
)

// intentLimit bounds how many claims an intent lookup returns.
const intentLimit = 5

// lookup prints one tier of the index, or the claims an intent or a change touches.
func lookup(project dictionary.Project, args options, out io.Writer, refuse func(error) int) int {
	if (args.intent != "" || len(args.changed) > 0) && (len(args.operands) > 0 || args.intent != "" && len(args.changed) > 0) {
		return refuse(errors.New("look up one thing at a time: a tier (`verilex index [claim [word]]`), --intent, or --changed"))
	}
	ix, err := index.Build(project)
	if err != nil {
		return refuse(err)
	}
	var value any
	var text strings.Builder
	switch {
	case args.intent != "":
		found := ix.Intent(args.intent, intentLimit)
		value = found
		fmt.Fprintf(&text, "intent: %d claim(s) for %s\n", len(found), quoteText(args.intent))
		for _, c := range found {
			line(&text, c)
		}
	case len(args.changed) > 0:
		change, err := ix.Changed(project, args.changed)
		if err != nil {
			return refuse(err)
		}
		value = change
		changes(&text, change)
	case len(args.operands) == 2:
		w, entry, err := ix.Details(args.operands[0], args.operands[1])
		if err != nil {
			return refuse(err)
		}
		status, err := lifecycle.StatusOf(project, w)
		if err != nil {
			return refuse(err)
		}
		value = struct {
			index.Word
			Proves   string   `json:"proves"`
			Status   string   `json:"status"`
			Requires []string `json:"requires"`
			Provides []string `json:"provides"`
			Inputs   []string `json:"inputs"`
			Env      []string `json:"env"`
			Timeout  int      `json:"timeout"`
			ReadOnly bool     `json:"read_only"`
		}{entry, w.Proves(), string(status.State), w.Requires, w.Provides, w.Inputs, w.Env, w.Timeout, w.ReadOnly}
		fmt.Fprintf(&text, "%s  proves %s through %s; %s\n", signature(w.Name, w.Args), w.Proves(), w.Entry, status.State)
		fmt.Fprintf(&text, "  requires: %s  provides: %s\n", states(w.Requires), states(w.Provides))
		fmt.Fprintf(&text, "  inputs: %s  env: %s  timeout: %ds", states(w.Inputs), states(w.Env), w.Timeout)
		if w.ReadOnly {
			text.WriteString("  read-only")
		}
		text.WriteString("\n")
		if entry.Chain != "" {
			fmt.Fprintf(&text, "  proven on: %s\n", entry.Chain)
		}
	case len(args.operands) == 1:
		c, err := ix.Find(args.operands[0])
		if err != nil {
			return refuse(err)
		}
		value = c
		fmt.Fprintf(&text, "%s@%s  %s\n  entry: %s  requires: %s  provides: %s\n", c.Claim, c.Version, c.Sentence, strings.Join(c.Entry, ", "), states(c.Requires), states(c.Provides))
		for _, w := range c.Words {
			notes := []string{}
			if w.Via != "" {
				notes = append(notes, "via "+w.Via)
			}
			if len(c.Entry) > 1 {
				notes = append(notes, w.Entry)
			}
			if w.State != index.Active {
				notes = append(notes, w.State)
			}
			fmt.Fprintf(&text, "  %s", signature(w.Word, w.Args))
			if len(notes) > 0 {
				fmt.Fprintf(&text, "  (%s)", strings.Join(notes, ", "))
			}
			text.WriteString("\n")
		}
	default:
		active := ix.Active()
		type tier1 struct {
			Claim    string `json:"claim"`
			Version  string `json:"version"`
			Sentence string `json:"sentence"`
		}
		claims := []tier1{}
		for _, c := range active {
			claims = append(claims, tier1{c.Claim, c.Version, c.Sentence})
		}
		value = claims
		if len(active) == 0 {
			fmt.Fprintf(&text, "index %s: no active claims; find one with `verilex index --intent '<what to prove>'`\n", ix.Product)
		} else {
			fmt.Fprintf(&text, "index %s: %d active claim(s); `verilex index <claim>` lists a claim's words\n", ix.Product, len(active))
		}
		for _, c := range active {
			line(&text, c)
		}
	}
	if args.json {
		if err = encode(out, value); err != nil {
			return refuse(err)
		}
		return 0
	}
	fmt.Fprint(out, text.String())
	return 0
}

// line prints a claim as tier 1 does: its name and sentence, and whether the product leaves it dormant.
func line(text *strings.Builder, c index.Claim) {
	fmt.Fprintf(text, "%s  %s", c.Claim, c.Sentence)
	if !c.Active {
		text.WriteString("  (dormant)")
	}
	text.WriteString("\n")
}

func changes(text *strings.Builder, change index.Change) {
	run, skipped := 0, 0
	for _, chain := range change.Chains {
		switch {
		case chain.Run:
			run++
		case !chain.Refused:
			skipped++
		}
	}
	fmt.Fprintf(text, "changed: %d claim(s); %d of %d known chain(s) must re-run\n", len(change.Claims), run, len(change.Chains))
	for _, c := range change.Claims {
		line(text, c)
	}
	for _, chain := range change.Chains {
		switch {
		case chain.Refused:
			fmt.Fprintf(text, "  refused  %s: %s\n", chain.Chain, report.OneLine(chain.Reason))
		case chain.Run:
			fmt.Fprintf(text, "  run  %s: %s\n", chain.Chain, report.OneLine(chain.Reason))
		}
	}
	if skipped > 0 {
		fmt.Fprintf(text, "  %d chain(s) still skip: `verilex plan '<chain>'` cites the run each relies on\n", skipped)
	}
}

// signature writes a word with its args as placeholders: item-stored <name>.
func signature(word string, args []string) string {
	parts := []string{word}
	for _, arg := range args {
		parts = append(parts, "<"+arg+">")
	}
	return strings.Join(parts, " ")
}

func quoteText(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
