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
	now    grouping.Pins
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
	// Drift is set when the word's files or claim changed since onboarding: it always runs.
	Drift bool `json:"drift_suspect,omitempty"`
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
	ix.now = grouping.PinsOf(ix.claims, ix.words)
	ix.group = ix.g.Groups("", ix.now)
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
		// A decision counts only while it holds: a word re-pinned since, a word whose claim has a
		// new version, or a word grouped under an old version stays provisional.
		if d, ok := ix.g.Sound(w.Name); ok && d.Holds(w.Name, ix.now) {
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

// Words returns the product's words. GroupOf names the claim a claim is grouped under.
func (ix Index) Words() []dictionary.Word { return slices.Clone(ix.words) }

// GroupOf names the claim a claim is grouped under: itself unless onboarding made it an alias.
func (ix Index) GroupOf(name string) string { return ix.groupOf(name) }

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
// sentence, and a rarer term counts more. A claim must also cover at least half of what the
// intent says, share a term that sets it apart, and, for a two-term intent, match both terms or
// be named by it (see covers). When the claims found leave an intent term uncovered, the intent
// must name one of them, or nothing is found.
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
		at    int
		score float64
	}
	// A term every claim holds sets no claim apart; with one claim, every term does. A term at most
	// half the claims hold is rare.
	common := func(term string) bool { return len(ix.Claims) > 1 && frequency[term] >= len(ix.Claims) }
	rare := func(term string) bool { return 2*frequency[term] <= len(ix.Claims) }
	never := func(string) bool { return false }
	found := []scored{}
	for i, c := range ix.Claims {
		if !covers(want, primary[i], secondary[i], names(c, want, common, never), common) {
			continue
		}
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
			found = append(found, scored{c, i, score})
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
	result, held := []Claim{}, map[string]int{}
	for i := 0; i < len(found) && i < limit && 2*found[i].score >= found[0].score; i++ {
		result = append(result, found[i].claim)
		for _, term := range append(slices.Clone(primary[found[i].at]), secondary[found[i].at]...) {
			held[term]++
		}
	}
	// A term no claim found holds is either a filler word (works in `list works`) or the subject of
	// a weak intent (refunds in `store item refunds`). Only a found claim the intent names, by all of
	// a name's terms that are not common or by one rare name term no other found claim holds, tells
	// them apart: proving claims that miss the subject would be a false green.
	alone := func(term string) bool { return rare(term) && held[term] == 1 }
	anchored := slices.ContainsFunc(result, func(c Claim) bool { return names(c, want, common, alone) })
	if !anchored && slices.ContainsFunc(want, func(term string) bool { return held[term] == 0 }) {
		return []Claim{}
	}
	return result
}

// covers is the relevance floor: a claim covers an intent when it matches at least half of the
// intent's terms. A term in the claim's name or sentence counts whole, one only in the rest of
// the claim counts half. A term every claim holds (common) says nothing about which claim is
// meant, so at least one shared term must be rarer: `store items` shares only store and item,
// and finds nothing. A two-term intent that shares only one term with the claim passes only when
// it holds all of one of the claim's names that is not common (named), so `list works` finds
// item-listed but `store refunds` and `refused payment` find nothing. Longer intents need two
// shared terms anyway.
func covers(want, primary, secondary []string, named bool, common func(string) bool) bool {
	matched, shared, telling := 0.0, 0, false
	for _, term := range want {
		switch {
		case slices.Contains(primary, term):
			matched, shared = matched+1, shared+1
		case slices.Contains(secondary, term):
			matched, shared = matched+0.5, shared+1
		default:
			continue
		}
		telling = telling || !common(term)
	}
	return telling && 2*matched >= float64(len(want)) && (len(want) == 1 || shared > 1 || named)
}

// names reports whether an intent names a claim by one of the names it goes by (its own and its
// aliases'): the intent holds every term of the name that is not common, or one term of the name
// that rare accepts. So `list works` names item-listed, and `store item refunds` names no tally
// claim.
func names(c Claim, want []string, common, rare func(string) bool) bool {
	for _, name := range append([]string{c.Claim}, c.aliases...) {
		parts := terms.Of(name)
		apart := slices.DeleteFunc(slices.Clone(parts), common)
		if len(apart) > 0 && !slices.ContainsFunc(apart, func(term string) bool { return !slices.Contains(want, term) }) {
			return true
		}
		if slices.ContainsFunc(parts, func(term string) bool { return rare(term) && slices.Contains(want, term) }) {
			return true
		}
	}
	return false
}

// doc gathers the terms a claim is found by: its name and sentence and those of its aliases
// first, then the rest.
func (ix Index) doc(c Claim) ([]string, []string) {
	rest, named := []string{}, []string{c.Claim, c.Sentence}
	for _, name := range c.aliases {
		named = append(named, name, ix.claims[name].Sentence)
	}
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
	primary := terms.Of(strings.Join(named, "\n"))
	secondary := slices.DeleteFunc(terms.Of(strings.Join(rest, "\n")), func(term string) bool { return slices.Contains(primary, term) })
	return primary, secondary
}
