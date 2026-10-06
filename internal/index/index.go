// Package index owns the word index: three tiers of progressive disclosure over the vocabulary,
// and the lookups by intent and by change. It is generated on every call from the claim files,
// the word contracts and the grouping file, never maintained by hand and never cached, so it
// cannot fall out of date, and the same records always give the same index. An agent pays for
// the product's active claims, not for the whole vocabulary:
//
//   - tier 1, always loaded: the claims active for this product, one line each;
//   - tier 2, on demand: one claim's words;
//   - tier 3, only when a word is about to run: its run details.
//
// A word is active for a product once onboarding recorded two uses of it there; every other word,
// and every alias whose words a product never used, stays dormant for it and out of tiers 1 and 2.
package index

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/grouping"
	"github.com/DereKk8/verilex/internal/lifecycle"
	"github.com/DereKk8/verilex/internal/terms"
)

// A word's state for one product.
const (
	// Active: onboarded, and useful for this product.
	Active = "active"
	// Dormant: onboarded, but never used by this product.
	Dormant = "dormant"
	// Provisional: not onboarded.
	Provisional = "provisional"
)

// Index is the vocabulary as one product sees it.
type Index struct {
	Product string  `json:"product"`
	Claims  []Claim `json:"claims"`

	words  []dictionary.Word
	claims map[string]dictionary.Claim
	g      grouping.Grouping
	group  map[string]string
}

// Claim is a grouped claim, or a claim no onboarding has grouped yet, with the words that prove it.
type Claim struct {
	Claim    string   `json:"claim"`
	Version  string   `json:"version"`
	Sentence string   `json:"sentence"`
	Entry    []string `json:"entry"`
	Requires []string `json:"requires"`
	Provides []string `json:"provides"`
	Active   bool     `json:"active"`
	// Aliases are the claims grouped under this one whose words the product uses.
	Aliases []string `json:"aliases,omitempty"`
	Words   []Word   `json:"words"`

	aliases []string
}

// Word is one word of a claim, as tier 2 lists it.
type Word struct {
	Word  string   `json:"word"`
	Args  []string `json:"args"`
	Entry string   `json:"entry"`
	// Via is the claim the word pins when that claim is an alias of this one.
	Via   string `json:"via,omitempty"`
	State string `json:"state"`
	// Chain is the chain onboarding proved the word on.
	Chain string `json:"chain,omitempty"`
}

// Build generates a product's index from its records.
func Build(p dictionary.Project) (Index, error) {
	ix := Index{Product: p.Name, Claims: []Claim{}}
	var err error
	if ix.claims, ix.words, err = lifecycle.Load(p); err != nil {
		return ix, err
	}
	if ix.g, err = grouping.Load(p); err != nil {
		return ix, err
	}
	ix.group = ix.g.Groups("")
	entries := map[string]*Claim{}
	for _, name := range slices.Sorted(maps.Keys(ix.claims)) {
		if ix.groupOf(name) == name {
			c := ix.claims[name]
			entries[name] = &Claim{Claim: name, Version: c.Version(), Sentence: c.Sentence, Entry: c.Entry, Requires: c.Requires, Provides: c.Provides, Words: []Word{}}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(ix.claims)) {
		if group := ix.groupOf(name); group != name && entries[group] != nil {
			entries[group].aliases = append(entries[group].aliases, name)
		}
	}
	for _, w := range ix.words {
		if w.Claim == nil {
			continue
		}
		entry := entries[ix.groupOf(w.Claim.Name)]
		if entry == nil {
			continue
		}
		word := Word{Word: w.Name, Args: w.Args, Entry: w.Entry, State: Provisional}
		if w.Claim.Name != entry.Claim {
			word.Via = w.Claim.Name
		}
		// A decision counts only for the claim it judged: a word re-pinned since stays provisional.
		if d, ok := ix.g.Sound(w.Name); ok && d.Pin() == w.Proves() {
			word.State, word.Chain = Dormant, d.Chain
			if _, used := d.Uses[p.Name]; used {
				word.State = Active
			}
		}
		if word.State == Active {
			entry.Active = true
			if word.Via != "" && !slices.Contains(entry.Aliases, word.Via) {
				entry.Aliases = append(entry.Aliases, word.Via)
				slices.Sort(entry.Aliases)
			}
		}
		entry.Words = append(entry.Words, word)
	}
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		slices.SortFunc(entries[name].Words, func(a, b Word) int { return strings.Compare(a.Word, b.Word) })
		ix.Claims = append(ix.Claims, *entries[name])
	}
	return ix, nil
}

// groupOf names the claim a claim is grouped under: itself unless onboarding made it an alias.
func (ix Index) groupOf(name string) string {
	if group, ok := ix.group[name]; ok {
		return group
	}
	return name
}

// Active lists the claims active for the product: tier 1.
func (ix Index) Active() []Claim {
	return slices.DeleteFunc(slices.Clone(ix.Claims), func(c Claim) bool { return !c.Active })
}

// Find returns a claim by its name or the name of a claim grouped under it, with only its active
// words, or every word when none is active: tier 2.
func (ix Index) Find(name string) (Claim, error) {
	group := ix.groupOf(name)
	i := slices.IndexFunc(ix.Claims, func(c Claim) bool { return c.Claim == group })
	if i < 0 {
		return Claim{}, fmt.Errorf("no claim %q; `verilex index --intent '<what to prove>'` finds claims", name)
	}
	c := ix.Claims[i]
	if c.Active {
		c.Words = slices.DeleteFunc(slices.Clone(c.Words), func(w Word) bool { return w.State != Active })
	}
	return c, nil
}

// Details returns one word of a claim, ready to run: tier 3.
func (ix Index) Details(claim, word string) (dictionary.Word, Word, error) {
	c, err := ix.Find(claim)
	if err != nil {
		return dictionary.Word{}, Word{}, err
	}
	full := ix.Claims[slices.IndexFunc(ix.Claims, func(x Claim) bool { return x.Claim == c.Claim })]
	i := slices.IndexFunc(full.Words, func(w Word) bool { return w.Word == word })
	if i < 0 {
		return dictionary.Word{}, Word{}, fmt.Errorf("claim %s has no word %q; `verilex index %s` lists its words", c.Claim, word, c.Claim)
	}
	j := slices.IndexFunc(ix.words, func(w dictionary.Word) bool { return w.Name == word })
	return ix.words[j], full.Words[i], nil
}

// Intent finds the claims an intent names, best first and at most limit. A claim is found by its
// name, sentence, evidence, aliases, sources and words; a term counts double in its name or
// sentence, and a rarer term counts more.
func (ix Index) Intent(text string, limit int) []Claim {
	want := terms.Of(text)
	primary, secondary := make([][]string, len(ix.Claims)), make([][]string, len(ix.Claims))
	frequency := map[string]int{}
	for i, c := range ix.Claims {
		primary[i], secondary[i] = ix.doc(c)
		for _, term := range append(slices.Clone(primary[i]), secondary[i]...) {
			frequency[term]++
		}
	}
	type scored struct {
		claim Claim
		score float64
	}
	found := []scored{}
	for i, c := range ix.Claims {
		score := 0.0
		for _, term := range want {
			switch {
			case slices.Contains(primary[i], term):
				score += 2 / float64(frequency[term])
			case slices.Contains(secondary[i], term):
				score += 1 / float64(frequency[term])
			}
		}
		if score > 0 {
			found = append(found, scored{c, score})
		}
	}
	slices.SortStableFunc(found, func(a, b scored) int {
		switch {
		case a.score != b.score:
			if a.score > b.score {
				return -1
			}
			return 1
		case a.claim.Active != b.claim.Active:
			if a.claim.Active {
				return -1
			}
			return 1
		}
		return strings.Compare(a.claim.Claim, b.claim.Claim)
	})
	// A claim that scores less than half the best one is noise next to it.
	result := []Claim{}
	for i := 0; i < len(found) && i < limit && 2*found[i].score >= found[0].score; i++ {
		result = append(result, found[i].claim)
	}
	return result
}

// doc gathers the terms a claim is found by: its name and sentence first, then the rest.
func (ix Index) doc(c Claim) ([]string, []string) {
	rest := []string{}
	for _, name := range append([]string{c.Claim}, c.aliases...) {
		claim := ix.claims[name]
		rest = append(rest, name, claim.Sentence, claim.Evidence.Action, claim.Evidence.Observation)
		for _, ref := range claim.Refs() {
			_, id, _ := strings.Cut(ref, "#")
			rest = append(rest, id)
		}
	}
	for _, w := range c.Words {
		j := slices.IndexFunc(ix.words, func(x dictionary.Word) bool { return x.Name == w.Word })
		rest = append(rest, w.Word, ix.words[j].Promise)
	}
	primary := terms.Of(c.Claim + "\n" + c.Sentence)
	secondary := slices.DeleteFunc(terms.Of(strings.Join(rest, "\n")), func(term string) bool { return slices.Contains(primary, term) })
	return primary, secondary
}
