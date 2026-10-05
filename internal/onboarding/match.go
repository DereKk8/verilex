package onboarding

import (
	"fmt"
	"slices"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/terms"
)

// NearIdentical is how alike two claim sentences' significant terms must be to read the same.
const NearIdentical = 0.8

// How a proposal's claim relates to a claim the vocabulary already groups.
const (
	// KindSame: the word pins a grouped claim, or a claim already grouped under one.
	KindSame = "same"
	// KindMatch: same structure and evidence literals, and a near-identical sentence.
	KindMatch = "match"
	// KindAmbiguous: same structure, and either the same evidence literals or a near-identical
	// sentence, but not both.
	KindAmbiguous = "ambiguous"
	// KindNew: no grouped claim is alike.
	KindNew = "new"
)

// Candidate is a grouped claim the proposal may be a variant of.
type Candidate struct {
	// Claim is the grouped claim's current version, <claim>@<version>.
	Claim string `json:"claim"`
	Kind  string `json:"kind"`
	// Sentence is how alike the two sentences are (shared significant terms over all of them),
	// and Literals whether the evidence contracts name the same literal values.
	Sentence float64 `json:"sentence"`
	Literals bool    `json:"literals"`
	// Words are the candidate's onboarded words the behavioral check compared with the proposal.
	Words []string `json:"words,omitempty"`
	// Differs is set when those words behaved differently from the proposal on some trial state.
	Differs bool `json:"differs,omitempty"`

	claim dictionary.Claim
}

// match compares a proposal's claim with every grouped claim. Two claims are alike only when they
// hold under the same preconditions (R4), the grouped claim lists the entry point the word
// exercises (R5), and they take as many args and pin the same states, whatever the args are
// called. Among those, the evidence literals and the sentence decide between a match and an
// ambiguous candidate.
func match(proposal dictionary.Claim, entry string, grouped []dictionary.Claim) (string, []Candidate) {
	candidates := []Candidate{}
	for _, c := range grouped {
		if !slices.Equal(proposal.Preconditions, c.Preconditions) || !slices.Contains(c.Entry, entry) || len(proposal.Args) != len(c.Args) ||
			!slices.Equal(positional(proposal.Requires, proposal.Args), positional(c.Requires, c.Args)) ||
			!slices.Equal(positional(proposal.Provides, proposal.Args), positional(c.Provides, c.Args)) {
			continue
		}
		candidate := Candidate{Claim: c.Pin(), claim: c,
			Sentence: terms.Jaccard(terms.Of(proposal.Sentence), terms.Of(c.Sentence)),
			Literals: slices.Equal(evidenceLiterals(proposal), evidenceLiterals(c))}
		switch near := candidate.Sentence >= NearIdentical; {
		case candidate.Literals && near:
			candidate.Kind = KindMatch
		case candidate.Literals || near:
			candidate.Kind = KindAmbiguous
		default:
			continue
		}
		candidates = append(candidates, candidate)
	}
	matches := slices.DeleteFunc(slices.Clone(candidates), func(c Candidate) bool { return c.Kind != KindMatch })
	switch {
	case len(matches) == 1:
		return KindMatch, matches
	case len(matches) > 1:
		for i := range matches {
			matches[i].Kind = KindAmbiguous
		}
		return KindAmbiguous, matches
	case len(candidates) > 0:
		return KindAmbiguous, candidates
	}
	return KindNew, nil
}

// positional names every arg placeholder by its position, so {name} and {item} in two claims
// that each take one arg pin the same state.
func positional(states, args []string) []string {
	result := make([]string, 0, len(states))
	for _, state := range states {
		for i, arg := range args {
			state = strings.ReplaceAll(state, "{"+arg+"}", fmt.Sprintf("{#%d}", i))
		}
		result = append(result, state)
	}
	slices.Sort(result)
	return result
}

func evidenceLiterals(c dictionary.Claim) []string {
	return terms.Literals(c.Evidence.Action + "\n" + c.Evidence.Observation)
}
