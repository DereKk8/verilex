// Package onboarding owns how a word that proves a claim joins the shared vocabulary. It runs once
// per proposal, never when an agent reads the vocabulary, and it is the only writer of the
// grouping file. Cheap checks come first:
//
//  1. Mechanical match: the proposal's claim against every grouped claim, by preconditions, entry
//     point, pinned states, evidence literals and sentence. A match makes the word a variant.
//  2. Behavioral check and correctness gate: the word is tried on the states of its latest real
//     use, healthy and under every planted defect, beside the matched claim's words. The word
//     must be green on the healthy product and red under each of its claim's planted defects (no
//     false pass, ever), and it must behave like those words everywhere; a difference is a finding.
//  3. A proposal still ambiguous after both joins as a claim of its own and goes back to the
//     calling agent as a decision request; verilex itself never calls a model.
//
// Usefulness stays per product: a word needs counted uses in two runs of the product it joins for.
package onboarding

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/DereKk8/verilex/internal/curation"
	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/featuremap"
	"github.com/DereKk8/verilex/internal/grouping"
	"github.com/DereKk8/verilex/internal/lifecycle"
	"github.com/DereKk8/verilex/internal/runner"
	"github.com/DereKk8/verilex/internal/verdict"
)

// Outcomes of one onboarding.
const (
	// Onboarded: the word joined the vocabulary, or became useful for one more product.
	Onboarded = "onboarded"
	// Already: the word is onboarded for this product and nothing it was judged on changed.
	Already = "already"
	// Rejected: the evidence shows the word or its claim is wrong.
	Rejected = "rejected"
	// Inconclusive: a trial could not be judged, so nothing was decided.
	Inconclusive = "inconclusive"
	// Undecided: the word joined, but only the calling agent can decide whether the claim it is
	// grouped under says the same as another grouped claim (see Request); until then they stay apart.
	Undecided = "undecided"
)

// compared bounds how many of a candidate claim's words the behavioral check tries beside the
// proposal; each was gated when it joined, so a few stand for the claim.
const compared = 3

// Result is one onboarding, as `verilex onboard --json` prints it and its record keeps it.
type Result struct {
	Word    string `json:"word"`
	Product string `json:"product"`
	Outcome string `json:"outcome"`
	// Reason says why the word was rejected or nothing was decided.
	Reason string `json:"reason,omitempty"`
	// Proves is the claim version the word pins; Claim the version it is grouped under.
	Proves string `json:"proves"`
	Claim  string `json:"claim,omitempty"`
	// Match is how the word was grouped (see package grouping).
	Match      string      `json:"match,omitempty"`
	Candidates []Candidate `json:"candidates,omitempty"`
	Trials     []Trial     `json:"trials,omitempty"`
	// Findings are the states on which the word and a candidate claim's words disagree.
	Findings []Finding `json:"findings,omitempty"`
	// Request is the decision the calling agent owes an undecided word.
	Request *Request `json:"request,omitempty"`
	// Moved lists the other words an agent's answer regrouped with this one: every word grouped
	// under the same claim moves with it.
	Moved []string `json:"moved,omitempty"`
	// Decision is what onboarding recorded in the grouping file.
	Decision *grouping.Decision `json:"decision,omitempty"`
	// Record is the directory that keeps this result and every trial's evidence.
	Record string `json:"record,omitempty"`
	Date   string `json:"date"`
}

// Finding is one state on which two words that should agree do not.
type Finding struct {
	State    string   `json:"state"`
	Text     string   `json:"text"`
	Evidence []string `json:"evidence"`
}

// Request asks the calling agent whether an undecided word's claim says the same as one of the
// grouped claims it matched ambiguously and behaved like on every trial state.
type Request struct {
	Question   string      `json:"question"`
	Proposal   ClaimText   `json:"proposal"`
	Candidates []ClaimText `json:"candidates"`
	// Commands are the answers: group the word under one candidate, or keep its claim apart.
	Commands []string `json:"commands"`
}

// ClaimText is one claim version as the agent reads it: its pin, its claim file and its words.
type ClaimText struct {
	Claim string   `json:"claim"`
	Text  string   `json:"text"`
	Words []string `json:"words,omitempty"`
}

// Choice is the calling agent's answer to a Request: SameAs names the grouped claim the word's
// claim says the same as, or Distinct keeps it a claim of its own. The zero Choice onboards.
type Choice struct {
	SameAs   string
	Distinct bool
}

// Onboard runs the onboarding pipeline for one word, or records the agent's choice for an
// undecided one. An error refuses the proposal before anything is judged.
func Onboard(p dictionary.Project, name string, choice Choice) (Result, error) {
	words, err := lifecycle.LoadWords(p)
	if err != nil {
		return Result{}, err
	}
	i := slices.IndexFunc(words, func(w dictionary.Word) bool { return w.Name == name })
	if i < 0 {
		return Result{}, fmt.Errorf("unknown word %q; `verilex words` lists the dictionary", name)
	}
	w := words[i]
	if w.Claim == nil {
		return Result{}, fmt.Errorf("%s names feature-map sections, not a claim; it joins through `verilex propose` and `verilex admit`", name)
	}
	if w.Stale != "" {
		return Result{}, errors.New(w.Stale)
	}
	g, err := grouping.Load(p)
	if err != nil {
		return Result{}, err
	}
	r := Result{Word: name, Product: p.Name, Proves: w.Proves(), Date: time.Now().UTC().Format(time.RFC3339)}
	status, err := lifecycle.StatusOf(p, w)
	if err != nil {
		return r, err
	}
	if choice != (Choice{}) {
		if status.State != lifecycle.Admitted {
			return r, fmt.Errorf("%s is %s, so it has no grouping question to answer; onboard it first", name, status.State)
		}
		return r, settle(p, w, words, choice, &r)
	}
	if status.State == lifecycle.Admitted {
		return r, useful(p, w, g, &r)
	}
	c := *w.Claim
	anchors, review, err := lifecycle.Review(p, c)
	if err != nil {
		return r, err
	}
	if len(review) > 0 {
		return r, fmt.Errorf("claim %s needs review: %s; bring its sources in line with the verify skill first", c.Name, strings.Join(review, "; "))
	}
	if len(c.Defects) == 0 {
		return r, fmt.Errorf("claim %s declares no planted defect, so onboarding cannot show that %s catches one: add 'defects' to %s", c.Name, name, c.Path)
	}
	sources, err := crossRepo(p, c, anchors)
	if err != nil {
		return r, err
	}
	uses, err := counted(p, w)
	if err != nil {
		return r, err
	}
	claims, err := dictionary.LoadClaims(p)
	if err != nil {
		return r, err
	}
	o := &onboarding{p: p, w: w, words: words, claims: claims, now: grouping.PinsOf(claims, words), g: g, r: &r, opens: map[string]map[string][]string{}}
	if r.Record, err = newRecord(p, name); err != nil {
		return r, err
	}
	o.decide(anchors, uses)
	if r.Outcome == Onboarded || r.Outcome == Undecided {
		if err = grouping.Update(p, func(g *grouping.Grouping) error { return o.record(g, uses, sources) }); err != nil {
			return r, err
		}
	}
	return r, save(r)
}

type onboarding struct {
	p      dictionary.Project
	w      dictionary.Word
	words  []dictionary.Word
	claims map[string]dictionary.Claim
	now    grouping.Pins
	g      grouping.Grouping
	r      *Result
	// groups and opens are what the grouping said when the candidates were chosen; the decision
	// is recorded only while the grouping file still says the same.
	groups map[string]string
	opens  map[string]map[string][]string
	// chain is the chain of the word's trials; agree the candidates whose words behaved like it.
	chain string
	agree []Candidate
	// group is the claim the word joins under.
	group dictionary.Claim
}

func (o *onboarding) reject(why string) { o.r.Outcome, o.r.Reason = Rejected, why }

// decide runs every check that needs no model, leaving the result Onboarded, Rejected,
// Inconclusive or, for a proposal only the calling agent can settle, Undecided.
func (o *onboarding) decide(anchors []featuremap.Anchor, uses []curation.Use) {
	c, w := *o.w.Claim, o.w
	if why := mapping(c, anchors); why != "" {
		o.reject(why)
		return
	}
	for _, use := range uses {
		if use.Verdict == verdict.Green {
			if nonProof := onlyNonProof(text(use.Observation), c.Evidence.NonProofs, c.Evidence.Observation); nonProof != "" {
				o.reject(fmt.Sprintf("%s's evidence in run %s (%q) shows only a declared non-proof of claim %s: %s", w.Name, use.Run, text(use.Observation), c.Name, nonProof))
				return
			}
		}
	}
	kind, candidates := o.candidates(c)
	o.r.Candidates = candidates
	others := []dictionary.Claim{c}
	for _, candidate := range candidates {
		others = append(others, candidate.claim)
	}
	for _, claim := range others {
		if nonProof := onlyNonProof(c.Evidence.Observation, claim.Evidence.NonProofs, claim.Evidence.Observation); nonProof != "" {
			o.reject(fmt.Sprintf("claim %s's observation (%q) shows only a declared non-proof of claim %s: %s", c.Name, c.Evidence.Observation, claim.Name, nonProof))
			return
		}
	}
	chains, args := make([]string, len(uses)), make([][]string, len(uses))
	for i, use := range uses {
		chains[i], args[i] = use.Chain, use.Args
	}
	chain, k, err := sameStates(chains, args, w.Name, o.words)
	if err != nil {
		o.r.Outcome, o.r.Reason = Inconclusive, err.Error()
		return
	}
	o.chain = chain
	all := states(others...)
	steps, _ := dictionary.ParseChain(chain, o.words)
	values := map[int]string{}
	for i, arg := range c.Args {
		if j := slices.Index(w.Args, arg); j >= 0 {
			values[i] = steps[k].Argv[j]
		}
	}
	own := []Trial{}
	for _, s := range all {
		t := Trial{State: s.name, Word: w.Name, Chain: chain, env: s.env(all), steps: steps}
		switch {
		case s.defect == nil:
			t.Expect = string(verdict.Green)
		case s.owner == c.Name:
			t.Expect = string(verdict.Red)
		}
		own = append(own, t)
	}
	own = trials(o.p, filepath.Join(o.r.Record, "trials"), own, 0)
	o.r.Trials = own
	if o.gate(own) {
		return
	}
	// The correctness gate holds; the behavioral check compares the word with each candidate's words.
	for ci := range candidates {
		candidate := &candidates[ci]
		specs := []Trial{}
		for _, other := range o.variants(grouping.Name(candidate.Claim)) {
			built, steps, err := substitute(chain, k, other, values, candidate.claim, o.words)
			if err != nil {
				continue
			}
			candidate.Words = append(candidate.Words, other.Name)
			for _, s := range all {
				specs = append(specs, Trial{State: s.name, Word: other.Name, Chain: built, env: s.env(all), steps: steps})
			}
		}
		results := trials(o.p, filepath.Join(o.r.Record, "trials"), specs, len(o.r.Trials))
		o.r.Trials = append(o.r.Trials, results...)
		for _, t := range results {
			if t.Verdict == string(verdict.Inconclusive) || t.Verdict == notRun {
				o.r.Outcome, o.r.Reason = Inconclusive, fmt.Sprintf("trial %s %s was %s: %s", t.State, t.Chain, t.Verdict, t.Reason)
				return
			}
			mine := own[slices.IndexFunc(own, func(m Trial) bool { return m.State == t.State })]
			if mine.Verdict != t.Verdict {
				candidate.Differs = true
				o.r.Findings = append(o.r.Findings, Finding{State: t.State, Evidence: []string{mine.Evidence, t.Evidence},
					Text: fmt.Sprintf("on %s, %s is %s and %s (claim %s) is %s", stateText(t.State), mine.Chain, mine.Verdict, t.Chain, grouping.Name(candidate.Claim), t.Verdict)})
			}
		}
		if len(candidate.Words) == 0 {
			o.expected(candidate, own, all)
		}
		if !candidate.Differs && candidate.Kind == KindAmbiguous {
			o.agree = append(o.agree, *candidate)
		}
	}
	o.r.Candidates = candidates
	differs := slices.IndexFunc(candidates, func(candidate Candidate) bool { return candidate.Differs && candidate.Kind != KindAmbiguous })
	switch {
	case kind == KindNew || kind == KindAmbiguous && len(o.agree) == 0:
		o.r.Outcome, o.r.Match, o.group = Onboarded, grouping.New, c
	case differs >= 0:
		o.reject(fmt.Sprintf("%s behaves differently from the words of claim %s: one of them is wrong, or the claims differ", w.Name, grouping.Name(candidates[differs].Claim)))
	case kind == KindAmbiguous:
		o.r.Outcome, o.r.Match, o.group = Undecided, grouping.New, c
		o.r.Reason = fmt.Sprintf("claim %s may say the same as %s, and neither the mechanical match nor the behavioral check can tell", c.Name, claimNames(o.agree))
	default:
		o.r.Outcome, o.group = Onboarded, candidates[0].claim
		o.r.Match = grouping.Mechanical
		if kind == KindSame {
			o.r.Match = grouping.Same
		}
		if len(o.agree) > 0 {
			o.r.Outcome = Undecided
			o.r.Reason = fmt.Sprintf("claim %s is grouped under %s, which may say the same as %s; only the calling agent can tell", c.Name, o.group.Name, claimNames(o.agree))
		}
	}
}

// gate applies the correctness gate to the word's own trials and reports whether it decided the
// onboarding: green on the healthy product, red under every planted defect of its claim, and an
// observation that shows more than a declared non-proof.
func (o *onboarding) gate(own []Trial) bool {
	c := *o.w.Claim
	for _, t := range own {
		switch {
		case t.Expect == string(verdict.Red) && t.Verdict == string(verdict.Green):
			o.reject(fmt.Sprintf("%s gives a false pass under planted defect %s: %s is green", o.w.Name, t.State, t.Chain))
		case t.Expect == string(verdict.Green) && t.Verdict == string(verdict.Red):
			o.reject(fmt.Sprintf("%s is red on the healthy product (%s), so it cannot be gated: %s", o.w.Name, t.Chain, t.Reason))
		case t.Expect == string(verdict.Green) && t.Verdict == string(verdict.Green):
			if nonProof := onlyNonProof(text(t.Observation), c.Evidence.NonProofs, c.Evidence.Observation); nonProof != "" {
				o.reject(fmt.Sprintf("%s's evidence on the healthy product (%q) shows only a declared non-proof of claim %s: %s", o.w.Name, text(t.Observation), c.Name, nonProof))
			}
		}
		if o.r.Outcome != "" {
			return true
		}
	}
	for _, t := range own {
		switch {
		case t.Verdict == notRun && t.State != Healthy:
			o.r.Outcome, o.r.Reason = Inconclusive, fmt.Sprintf("planted defect %s stops %s before %s runs (%s): a planted defect must leave the states the claim requires intact", t.State, t.Chain, o.w.Name, t.Reason)
		case t.Verdict == notRun || t.Verdict == string(verdict.Inconclusive):
			o.r.Outcome, o.r.Reason = Inconclusive, fmt.Sprintf("trial %s %s was %s: %s", t.State, t.Chain, t.Verdict, t.Reason)
		}
		if o.r.Outcome != "" {
			return true
		}
	}
	return false
}

// candidates finds the grouped claims the proposal may belong to: the one it already belongs to
// when its claim is grouped (KindSame), else whatever the mechanical match finds. A grouped claim
// whose question is still open brings the claims it may say the same as along, as ambiguous
// candidates, so every word that joins it is compared with them before the agent answers.
func (o *onboarding) candidates(c dictionary.Claim) (string, []Candidate) {
	group, claims := o.g.Groups(o.w.Name, o.now), o.claims
	o.groups = group
	kind, candidates := KindNew, []Candidate{}
	if name, ok := group[c.Name]; ok {
		if grouped, ok := claims[name]; ok {
			kind, candidates = KindSame, []Candidate{{Claim: grouped.Pin(), Kind: KindSame, Sentence: 1, Literals: true, claim: grouped}}
		}
	}
	if kind == KindNew {
		grouped := []dictionary.Claim{}
		for name, to := range group {
			if claim, ok := claims[name]; ok && name == to && name != c.Name {
				grouped = append(grouped, claim)
			}
		}
		slices.SortFunc(grouped, func(a, b dictionary.Claim) int { return strings.Compare(a.Name, b.Name) })
		kind, candidates = match(c, o.w.Entry, grouped)
	}
	if kind != KindSame && kind != KindMatch {
		return kind, candidates
	}
	for _, primary := range slices.Clone(candidates) {
		open := o.g.Open(grouping.Name(primary.Claim), o.w.Name, o.now)
		o.opens[grouping.Name(primary.Claim)] = open
		for _, pin := range slices.Sorted(maps.Keys(open)) {
			open, ok := claims[grouping.Name(pin)]
			if !ok || open.Pin() != pin || open.Name == c.Name || slices.ContainsFunc(candidates, func(other Candidate) bool { return other.Claim == pin }) {
				continue
			}
			candidates = append(candidates, Candidate{Claim: pin, Kind: KindAmbiguous, claim: open})
		}
	}
	return kind, candidates
}

// expected compares the word with what a candidate claim's gate demands of every word grouped
// under it, when none of them can be compared: green on the healthy product and red under each
// of the claim's planted defects.
func (o *onboarding) expected(candidate *Candidate, own []Trial, all []state) {
	for i, s := range all {
		expect := verdict.Green
		if s.defect != nil {
			expect = verdict.Red
			if !slices.ContainsFunc(slices.Collect(maps.Values(candidate.claim.Defects)), func(d dictionary.Defect) bool { return d.Digest() == s.defect.Digest() }) {
				continue
			}
		}
		if mine := own[i]; mine.Verdict != string(expect) {
			candidate.Differs = true
			o.r.Findings = append(o.r.Findings, Finding{State: s.name, Evidence: []string{mine.Evidence},
				Text: fmt.Sprintf("on %s, %s is %s, and claim %s requires %s there; none of its words could be compared", stateText(s.name), mine.Chain, mine.Verdict, grouping.Name(candidate.Claim), expect)})
		}
	}
}

// variants lists up to compared admitted words grouped under a claim, other than the proposal.
func (o *onboarding) variants(claim string) []dictionary.Word {
	result := []dictionary.Word{}
	for _, other := range o.words {
		d, ok := o.g.Sound(other.Name)
		if other.Name == o.w.Name || !ok || grouping.Name(d.Claim) != claim || other.Stale != "" || !d.Holds(other.Name, o.now) {
			continue
		}
		if s, err := lifecycle.StatusOf(o.p, other); err == nil && s.State == lifecycle.Admitted {
			result = append(result, other)
		}
		if len(result) == compared {
			break
		}
	}
	return result
}

// record turns an onboarded result into the word's sealed decision in g, the grouping file as it
// is under the lock. When another onboarding or answer changed how claims are grouped since the
// candidates were chosen, the decision would rest on a grouping that no longer exists, so nothing
// is recorded.
func (o *onboarding) record(g *grouping.Grouping, uses []curation.Use, sources []grouping.Source) error {
	changed := !maps.Equal(g.Groups(o.w.Name, o.now), o.groups)
	for claim, open := range o.opens {
		changed = changed || !reflect.DeepEqual(g.Open(claim, o.w.Name, o.now), open)
	}
	if changed {
		return fmt.Errorf("the grouping changed while %s was onboarded, by another onboarding or answer; onboard it again", o.w.Name)
	}
	c := *o.w.Claim
	digest, _ := lifecycle.WordDigest(o.w)
	d := grouping.Decision{Claim: o.group.Pin(), Entry: o.w.Entry, Digest: digest, Match: o.r.Match,
		Defects: map[string]string{}, Uses: map[string][]string{o.p.Name: runs(uses)}, Chain: o.chain, ClaimSources: c.SourcesDigest(), Sources: sources,
		Date: o.r.Date, Record: filepath.Base(o.r.Record)}
	for name, defect := range c.Defects {
		d.Defects[name] = defect.Digest()
	}
	if o.group.Name != c.Name {
		d.Proves, d.GroupDefects = o.w.Proves(), map[string]string{}
		for name, defect := range o.group.Defects {
			d.GroupDefects[name] = defect.Digest()
		}
	}
	if o.r.Outcome == Undecided {
		for _, candidate := range o.agree {
			pending := grouping.Pending{Claim: candidate.Claim, Words: candidate.Words, Defects: map[string]string{}}
			for name, defect := range candidate.claim.Defects {
				pending.Defects[name] = defect.Digest()
			}
			d.Pending = append(d.Pending, pending)
		}
	}
	g.Set(o.w.Name, d)
	sealed := g.Words[o.w.Name]
	o.r.Claim, o.r.Decision = d.Claim, &sealed
	o.r.Request = request(o.p, o.w, sealed)
	return nil
}

// request builds the decision request for a word whose decision leaves grouped claims pending.
// The question is about the claim the word is grouped under, so every word grouped under it is
// regrouped by the one answer.
func request(p dictionary.Project, w dictionary.Word, d grouping.Decision) *Request {
	if len(d.Pending) == 0 {
		return nil
	}
	claim := grouping.Name(d.Claim)
	proposal := ClaimText{Claim: d.Claim}
	if text, err := os.ReadFile(filepath.Join(p.ClaimsDir(), claim+".yaml")); err == nil {
		proposal.Text = string(text)
	}
	r := &Request{Proposal: proposal}
	names := []string{}
	for _, pending := range d.Pending {
		name := grouping.Name(pending.Claim)
		names = append(names, pending.Claim)
		text, _ := os.ReadFile(filepath.Join(p.ClaimsDir(), name+".yaml"))
		r.Candidates = append(r.Candidates, ClaimText{Claim: pending.Claim, Text: string(text), Words: pending.Words})
		r.Commands = append(r.Commands, "verilex onboard "+w.Name+" --same-as "+name)
	}
	r.Commands = append(r.Commands, "verilex onboard "+w.Name+" --distinct")
	r.Question = fmt.Sprintf("Does claim %s say the same as %s? %s behaved like the words compared with it on the healthy product and under every planted defect, but the mechanical match cannot tell; %s stays a claim of its own, with every word grouped under it, until you decide.", claim, strings.Join(names, " or "), w.Name, claim)
	return r
}

// settle records the calling agent's choice about the claim an undecided word is grouped under.
// The answer regroups every word grouped under that claim, so identical words never end up in
// different groups: --same-as moves them all under the pending claim, at the version they were
// compared with, and is refused while one of them was never compared with it; --distinct closes
// the question for all of them. A word moved under a claim whose own question is still open keeps
// the pending claims it was compared with that are part of that question. Every check runs on the
// grouping file under its lock, so two answers at once cannot both apply.
func settle(p dictionary.Project, w dictionary.Word, words []dictionary.Word, choice Choice, r *Result) error {
	if choice.SameAs != "" && choice.Distinct {
		return errors.New("choose one: --same-as <claim> or --distinct")
	}
	claims, err := dictionary.LoadClaims(p)
	if err != nil {
		return err
	}
	now := grouping.PinsOf(claims, words)
	return grouping.Update(p, func(g *grouping.Grouping) error {
		d, _ := g.Sound(w.Name)
		if len(d.Pending) == 0 {
			return fmt.Errorf("%s has no open grouping question: it is grouped under %s", w.Name, d.Claim)
		}
		claim := grouping.Name(d.Claim)
		grouped := g.Grouped(claim, now)
		var to grouping.Pending
		var keep map[string][]string
		if choice.SameAs != "" {
			i := slices.IndexFunc(d.Pending, func(pending grouping.Pending) bool { return grouping.Name(pending.Claim) == choice.SameAs })
			if i < 0 {
				names := []string{}
				for _, pending := range d.Pending {
					names = append(names, grouping.Name(pending.Claim))
				}
				return fmt.Errorf("%s's claim may say the same only as %s, not %s", w.Name, strings.Join(names, " or "), choice.SameAs)
			}
			to = d.Pending[i]
			if current := now.Claims[choice.SameAs]; current != to.Claim {
				return fmt.Errorf("claim %s is now %s, not the version %s was compared with (%s); keep it apart with --distinct, or onboard it again once its files change", choice.SameAs, current, w.Name, to.Claim)
			}
			if group := g.Groups("", now)[choice.SameAs]; group != choice.SameAs {
				return fmt.Errorf("claim %s is now grouped under %s, which %s was never compared with; onboard it again", choice.SameAs, group, w.Name)
			}
			missing := []string{}
			for _, word := range grouped {
				if !slices.ContainsFunc(g.Words[word].Pending, func(pending grouping.Pending) bool { return pending.Claim == to.Claim }) {
					missing = append(missing, word)
				}
			}
			if len(missing) > 0 {
				return fmt.Errorf("claim %s cannot join %s: its words %s were never compared with claim %s's words, or behaved differently from them; keep claim %s apart with --distinct", claim, choice.SameAs, strings.Join(missing, ", "), choice.SameAs, claim)
			}
			keep = g.Open(choice.SameAs, "", now)
		}
		for _, word := range grouped {
			other := g.Words[word]
			pending := other.Pending
			other.Pending = nil
			if choice.SameAs != "" {
				compared := pending[slices.IndexFunc(pending, func(p grouping.Pending) bool { return p.Claim == to.Claim })]
				other.Claim, other.Proves, other.GroupDefects, other.Match = to.Claim, other.Pin(), maps.Clone(compared.Defects), grouping.Agent
				for _, p := range pending {
					if _, open := keep[p.Claim]; open {
						other.Pending = append(other.Pending, p)
					}
				}
			}
			g.Set(word, other)
			if word != w.Name {
				r.Moved = append(r.Moved, word)
			}
		}
		sealed := g.Words[w.Name]
		r.Outcome, r.Claim, r.Match, r.Decision = Onboarded, sealed.Claim, sealed.Match, &sealed
		return nil
	})
}

// useful makes an onboarded word useful for one more product: its correctness gate is universal
// and already passed for these files, so only this product's two uses are checked.
func useful(p dictionary.Project, w dictionary.Word, g grouping.Grouping, r *Result) error {
	d, _ := g.Sound(w.Name)
	r.Claim, r.Match, r.Decision = d.Claim, d.Match, &d
	if _, ok := d.Uses[p.Name]; ok {
		r.Outcome = Already
		r.Request = request(p, w, d)
		return nil
	}
	uses, err := counted(p, w)
	if err != nil {
		return err
	}
	d.Uses[p.Name] = runs(uses)
	r.Outcome = Onboarded
	return grouping.Update(p, func(g *grouping.Grouping) error {
		g.Set(w.Name, d)
		sealed := g.Words[w.Name]
		r.Decision = &sealed
		return nil
	})
}

// counted returns the word's counted uses in this product, which must come from two runs.
func counted(p dictionary.Project, w dictionary.Word) ([]curation.Use, error) {
	uses, err := curation.Uses(p, w)
	if err != nil {
		return nil, err
	}
	if len(uses) < curation.MinRuns {
		return nil, fmt.Errorf("%s has counted uses in %d run(s) of %s; onboarding needs at least %d different runs in which it was green or red", w.Name, len(uses), p.Name, curation.MinRuns)
	}
	return uses, nil
}

func runs(uses []curation.Use) []string {
	result := make([]string, len(uses))
	for i, use := range uses {
		result[i] = use.Run
	}
	return result
}

func claimNames(candidates []Candidate) string {
	names := make([]string, len(candidates))
	for i, c := range candidates {
		names[i] = grouping.Name(c.Claim)
	}
	return strings.Join(names, " or ")
}

func stateText(state string) string {
	if state == Healthy {
		return "the healthy product"
	}
	return "planted defect " + state
}

// newRecord creates the directory that keeps one onboarding's result and trial evidence.
func newRecord(p dictionary.Project, word string) (string, error) {
	var suffix [3]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	dir := filepath.Join(runner.StateHome(), p.Name, "onboarding", word, fmt.Sprintf("%d-%s", time.Now().Unix(), hex.EncodeToString(suffix[:])))
	return dir, os.MkdirAll(dir, 0700)
}

func save(r Result) error {
	if r.Record == "" {
		return nil
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.Record, "record.json"), append(data, '\n'), 0600)
}
