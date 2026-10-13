package index

import (
	"fmt"
	"slices"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/ledger"
	"github.com/DereKk8/verilex/internal/runner"
	"github.com/DereKk8/verilex/internal/stamp"
)

// PlanFormat is the claim-plan JSON contract. A different value is a breaking change.
const PlanFormat = "verilex-claim-plan-1"

// NoIntent is the plan's intent when the caller derived no claims: prove nothing the diff touched broke.
const NoIntent = "prove nothing this change touched broke"

// ClaimPlan is the model-free plan for claims and a diff. Execute Chain through `verilex run`
// with the same claim and diff flags; do not run only the Run list. Skip means a standing pass
// proves that claim at this chain's stamps. Run means it does not, or a dependency the stamp
// does not fingerprint changed. Chain may hold steps that only provide states the selected
// claims require (see Proves). Order is every claim that chain proves, dependency order.
type ClaimPlan struct {
	Format   string         `json:"format"`
	Intent   string         `json:"intent"`
	Selected []string       `json:"selected"`
	Skip     []SkippedClaim `json:"skip"`
	Run      []LiveClaim    `json:"run"`
	Order    []string       `json:"order"`
	Chain    string         `json:"chain"`
	Touched  []string       `json:"touched"`
	Unpicked []string       `json:"unpicked"`
	Warning  string         `json:"warning,omitempty"`
	// Rerun is why `verilex run` executes every word in Chain, or "" when it skips them all.
	// A claim in Skip still has a standing pass, yet the run drives its step again.
	Rerun string `json:"rerun,omitempty"`
	// Inconclusive is set when no claim covers the diff and none was given or named: nothing
	// can prove the change, so the run is inconclusive.
	Inconclusive string `json:"inconclusive,omitempty"`
	// Gaps is what the diff touched that Chain's claim verdicts do not prove; Warning counts it.
	runner.Gaps

	// force is why the chain must run live even when every stamp matches. It is not part of
	// the JSON contract; `verilex run` reads it so a matching stamp cannot hide the diff.
	force string
	// binding is where the plan took each word's arguments from; Proves reads it.
	binding binding
}

// SkippedClaim is a claim a standing pass still proves, with the fingerprints of that pass.
type SkippedClaim struct {
	Claim        string            `json:"claim"`
	Word         string            `json:"word"`
	Step         string            `json:"step"`
	Fingerprints map[string]string `json:"fingerprints"`
	Stamp        string            `json:"stamp"`
	ReliesOn     string            `json:"relies_on"`
}

// LiveClaim is a claim the plan must run, and why the last pass does not prove it.
type LiveClaim struct {
	Claim  string `json:"claim"`
	Word   string `json:"word"`
	Step   string `json:"step"`
	Reason string `json:"reason"`
}

// Gaps is what a diff touched that a chain's claim verdicts do not prove.
func (ix Index) Gaps(p dictionary.Project, changes []string, steps []dictionary.Step) runner.Gaps {
	return ix.gapsOf(ix.touchOf(p, changes), steps)
}

// TouchedClaims names the claims a diff hits, in index order. It runs nothing and binds no arguments.
func (ix Index) TouchedClaims(p dictionary.Project, changes []string) []string {
	hit := ix.touchOf(p, changes)
	names := []string{}
	for _, c := range ix.Claims {
		if hit.claims[c.Claim] {
			names = append(names, c.Claim)
		}
	}
	return names
}

// PlanClaims plans derived claims, named claims and a diff. Named claims are always included.
// With no derived claims the selected set is the claims the diff touches, plus any named claims.
// Unpicked are claims the diff touches that were not selected. It runs nothing.
func (ix Index) PlanClaims(p dictionary.Project, derived, named, changes []string) (ClaimPlan, error) {
	plan := ClaimPlan{
		Format: PlanFormat, Intent: "given", Selected: []string{}, Skip: []SkippedClaim{}, Run: []LiveClaim{},
		Order: []string{}, Touched: []string{}, Unpicked: []string{},
	}
	hit := ix.touchOf(p, changes)
	for _, c := range ix.Claims {
		if hit.claims[c.Claim] {
			plan.Touched = append(plan.Touched, c.Claim)
		}
	}
	selected := map[string]bool{}
	add := func(name string) error {
		group, err := ix.resolveClaim(name)
		if err != nil {
			return err
		}
		if !selected[group] {
			selected[group] = true
			plan.Selected = append(plan.Selected, group)
		}
		return nil
	}
	if len(derived) == 0 {
		plan.Intent = NoIntent
		plan.Selected = append([]string{}, plan.Touched...)
		for _, name := range plan.Selected {
			selected[name] = true
		}
	} else {
		for _, name := range derived {
			if err := add(name); err != nil {
				return plan, err
			}
		}
	}
	for _, name := range named {
		if err := add(name); err != nil {
			return plan, err
		}
	}
	slices.Sort(plan.Selected)
	for _, name := range plan.Touched {
		if !selected[name] {
			plan.Unpicked = append(plan.Unpicked, name)
		}
	}
	if len(plan.Selected) == 0 {
		plan.Gaps = ix.gapsOf(hit, nil)
		plan.Inconclusive = "no claim covers this change; fall back to the product verify skill"
		if len(plan.Unclaimed) > 0 {
			plan.Inconclusive = strings.Join(plan.Unclaimed, ", ") + " proves no claim and the diff touched it; fall back to the product verify skill"
		}
		return plan, nil
	}
	plan.binding = ix.bindings(p)
	chain, order, err := ix.order(plan.binding, plan.Selected)
	if err != nil {
		return plan, err
	}
	plan.Chain, plan.Order = chain, order
	steps, err := dictionary.ParseChain(chain, ix.words)
	if err != nil {
		return plan, err
	}
	if err = dictionary.CheckOrder(steps); err != nil {
		return plan, err
	}
	plan.force = hit.forcedChain(steps)
	plan.Gaps = ix.gapsOf(hit, steps)
	var phrases []string
	if n := len(plan.Unpicked); n > 0 {
		phrases = append(phrases, countPhrase(n, "touched claim not picked", "touched claims not picked"))
	}
	plan.Warning = strings.Join(append(phrases, plan.Gaps.Phrases()...), ", ")
	stamps := stamp.Chain(p, steps)
	store := ledger.At(runner.LedgerDir(p))
	seen := map[string]bool{}
	for i, step := range steps {
		name := ix.claimName(step.Word)
		if name == "" || !selected[name] || seen[name] || !plan.Proves(step.Word.Name, step.Argv) {
			continue
		}
		seen[name] = true
		entry, why, err := store.Proof(stamps[i], step.Word.Proves())
		if err != nil {
			return plan, fmt.Errorf("the ledger is unreadable: %w", err)
		}
		if stamps[i].Hold != "" {
			why = stamps[i].Hold
		}
		// The run forces a live chain from its own words only, so the plan reads the same rule.
		if why == "" {
			why = hit.forcedWords[step.Word.Name]
		}
		if why == "" {
			prints := entry.Components
			if prints == nil {
				prints = map[string]string{}
			}
			plan.Skip = append(plan.Skip, SkippedClaim{
				Claim: name, Word: step.Word.Name, Step: step.Label(), Fingerprints: prints, Stamp: entry.Stamp, ReliesOn: entry.Run,
			})
			continue
		}
		plan.Run = append(plan.Run, LiveClaim{Claim: name, Word: step.Word.Name, Step: step.Label(), Reason: why})
	}
	decided, err := runner.Decide(p, steps, runner.Options{ForceLive: plan.force})
	if err != nil {
		return plan, err
	}
	plan.Rerun = decided.Rerun
	return plan, nil
}

// ForceLive is why a claim-mode run must not skip, or "" when the stamps already decide.
func (p ClaimPlan) ForceLive() string { return p.force }

// Proves reports whether a step of Chain proves its word's claim. An admitted word proves it only
// on the arguments onboarding admitted it on; a step of it on other arguments only provides a state.
func (p ClaimPlan) Proves(word string, argv []string) bool { return p.binding.proves(word, argv) }

func (ix Index) resolveClaim(name string) (string, error) {
	pin := name
	if i := strings.LastIndex(name, "@"); i > 0 {
		pin = name[:i]
		group := ix.groupOf(pin)
		c, err := ix.Find(group)
		if err != nil {
			return "", err
		}
		if name[i+1:] != c.Version {
			return "", fmt.Errorf("claim %s is now %s@%s; a plan names the current version", name, c.Claim, c.Version)
		}
		return c.Claim, nil
	}
	c, err := ix.Find(ix.groupOf(name))
	if err != nil {
		return "", err
	}
	return c.Claim, nil
}

// order builds a runnable chain for the selected claims and returns the claims that chain
// proves, in dependency order. A word may run twice: once on the arguments it proves its claim
// on, and once on other arguments only to provide a state another word requires.
func (ix Index) order(b binding, selected []string) (string, []string, error) {
	var queue []planned
	for _, name := range selected {
		word, err := ix.wordFor(name)
		if err != nil {
			return "", nil, err
		}
		argv, err := b.argsOf(word)
		if err != nil {
			return "", nil, err
		}
		queue = append(queue, planned{word, argv})
	}
	included := map[string]bool{}
	var steps []planned
	for i := 0; i < len(queue); i++ {
		step := queue[i]
		if included[step.label()] {
			continue
		}
		included[step.label()] = true
		steps = append(steps, step)
		for _, state := range realRequires(step.word, step.argv) {
			if stateProvided(queue, state) {
				continue
			}
			provider, argv, ok := ix.provider(state, b)
			if !ok {
				return "", nil, fmt.Errorf("%s requires %s; no word provides it", step.word.Name, state)
			}
			queue = append(queue, planned{provider, argv})
		}
	}
	ordered, err := topo(steps)
	if err != nil {
		return "", nil, err
	}
	parts := make([]string, 0, len(ordered))
	claims := []string{}
	seen := map[string]bool{}
	for _, step := range ordered {
		parts = append(parts, step.label())
		if name := ix.claimName(step.word); name != "" && !seen[name] && b.proves(step.word.Name, step.argv) {
			seen[name] = true
			claims = append(claims, name)
		}
	}
	return strings.Join(parts, " | "), claims, nil
}

// planned is one step of a claim plan's chain.
type planned struct {
	word dictionary.Word
	argv []string
}

func (s planned) label() string { return strings.Join(append([]string{s.word.Name}, s.argv...), " ") }

func (ix Index) wordFor(claim string) (dictionary.Word, error) {
	var found *dictionary.Word
	rank := 2
	for i := range ix.words {
		w := &ix.words[i]
		if ix.claimName(*w) != claim {
			continue
		}
		active := 1
		if d, ok := ix.g.Sound(w.Name); ok {
			if _, used := d.Uses[ix.Product]; used {
				active = 0
			}
		}
		if found == nil || active < rank || (active == rank && w.Name < found.Name) {
			found, rank = w, active
		}
	}
	if found == nil {
		return dictionary.Word{}, fmt.Errorf("claim %s has no word; `verilex index %s` lists its words", claim, claim)
	}
	return *found, nil
}

// binding is where a claim plan takes each word's arguments from.
type binding struct {
	// own is each admitted word's arguments on the chain onboarding admitted it on.
	own map[string][]string
	// lost names admitted words whose onboarded chain no longer binds them; no other chain may.
	lost map[string]bool
	// other binds a word with no onboarded chain: from an admitted chain, else the newest run record.
	other map[string][]string
}

func (ix Index) bindings(p dictionary.Project) binding {
	b := binding{own: map[string][]string{}, lost: map[string]bool{}, other: map[string][]string{}}
	var admitted []string
	for _, word := range ix.words {
		d, ok := ix.g.Sound(word.Name)
		if !ok || d.Chain == "" {
			continue
		}
		admitted = append(admitted, d.Chain)
		if step, ok := dictionary.OwnStep(d.Chain, word); ok {
			b.own[word.Name] = step.Argv
		} else {
			b.lost[word.Name] = true
		}
	}
	for _, chain := range admitted {
		ix.takeBindings(b, chain)
	}
	records, err := runner.LoadRuns(p)
	if err == nil {
		slices.SortFunc(records, func(a, b runner.Record) int { return strings.Compare(b.Started, a.Started) })
		for _, record := range records {
			ix.takeBindings(b, record.Chain)
		}
	}
	return b
}

// takeBindings binds each word of chain that has no onboarded chain and is not yet bound.
func (ix Index) takeBindings(b binding, chain string) {
	steps, err := dictionary.ParseChain(chain, ix.words)
	if err != nil {
		return
	}
	for _, step := range steps {
		name := step.Word.Name
		if _, own := b.own[name]; own || b.lost[name] {
			continue
		}
		if _, ok := b.other[name]; !ok {
			b.other[name] = step.Argv
		}
	}
}

// argsOf is the arguments a selected word proves its claim on.
func (b binding) argsOf(word dictionary.Word) ([]string, error) {
	if argv, ok := b.own[word.Name]; ok {
		return argv, nil
	}
	if b.lost[word.Name] {
		return nil, fmt.Errorf("%s: its onboarded chain no longer binds its arguments; onboard it again", word.Name)
	}
	argv, ok := b.other[word.Name]
	if len(word.Args) > 0 && !ok {
		return nil, fmt.Errorf("%s needs arguments %s; run a chain that binds them, then plan again", word.Name, strings.Join(word.Args, ", "))
	}
	return argv, nil
}

// proves reports whether a step proves its word's claim. An admitted word proves it only on the
// arguments onboarding admitted it on: on others it only provides a state.
func (b binding) proves(word string, argv []string) bool {
	if own, ok := b.own[word]; ok {
		return slices.Equal(own, argv)
	}
	return !b.lost[word]
}

func (ix Index) provider(state string, b binding) (dictionary.Word, []string, bool) {
	var found *dictionary.Word
	var argv []string
	for i := range ix.words {
		w := &ix.words[i]
		args, ok := matchProvide(*w, state, b.own[w.Name], b.other[w.Name])
		if !ok {
			continue
		}
		if found == nil || w.Name < found.Name {
			found, argv = w, args
		}
	}
	if found == nil {
		return dictionary.Word{}, nil, false
	}
	return *found, argv, true
}

func matchProvide(word dictionary.Word, state string, have, recorded []string) ([]string, bool) {
	if have != nil && slices.Contains(realProvides(word, have), state) {
		return have, true
	}
	if recorded != nil && slices.Contains(realProvides(word, recorded), state) {
		return recorded, true
	}
	for _, pattern := range word.Provides {
		if args, ok := matchPattern(pattern, state, word.Args); ok {
			return args, true
		}
	}
	return nil, false
}

func matchPattern(pattern, state string, args []string) ([]string, bool) {
	bound := map[string]string{}
	pi, si := 0, 0
	pr, sr := []rune(pattern), []rune(state)
	for pi < len(pr) && si <= len(sr) {
		if pr[pi] == '{' {
			end := strings.IndexRune(string(pr[pi:]), '}')
			if end < 0 {
				return nil, false
			}
			name := string(pr[pi+1 : pi+end])
			pi += end + 1
			stop := len(sr)
			if pi < len(pr) {
				next := string(pr[pi])
				if i := strings.Index(string(sr[si:]), next); i >= 0 {
					stop = si + i
				} else {
					return nil, false
				}
			}
			bound[name] = string(sr[si:stop])
			si = stop
			continue
		}
		if si == len(sr) || pr[pi] != sr[si] {
			return nil, false
		}
		pi++
		si++
	}
	if pi != len(pr) || si != len(sr) {
		return nil, false
	}
	argv := make([]string, len(args))
	for i, arg := range args {
		value, ok := bound[arg]
		if !ok {
			return nil, false
		}
		argv[i] = value
	}
	return argv, true
}

func stateProvided(steps []planned, state string) bool {
	for _, step := range steps {
		if slices.Contains(realProvides(step.word, step.argv), state) {
			return true
		}
	}
	return false
}

func topo(steps []planned) ([]planned, error) {
	byLabel := map[string]planned{}
	indegree := map[string]int{}
	after := map[string][]string{}
	for _, step := range steps {
		byLabel[step.label()] = step
		indegree[step.label()] = 0
	}
	provides := map[string][]string{}
	for _, step := range steps {
		for _, state := range realProvides(step.word, step.argv) {
			provides[state] = append(provides[state], step.label())
		}
	}
	for _, step := range steps {
		for _, state := range realRequires(step.word, step.argv) {
			for _, provider := range provides[state] {
				if provider == step.label() {
					continue
				}
				after[provider] = append(after[provider], step.label())
				indegree[step.label()]++
			}
		}
	}
	ready := []string{}
	for label, n := range indegree {
		if n == 0 {
			ready = append(ready, label)
		}
	}
	slices.Sort(ready)
	ordered := []planned{}
	for len(ready) > 0 {
		label := ready[0]
		ready = ready[1:]
		ordered = append(ordered, byLabel[label])
		next := append([]string{}, ready...)
		for _, consumer := range after[label] {
			indegree[consumer]--
			if indegree[consumer] == 0 {
				next = append(next, consumer)
			}
		}
		slices.Sort(next)
		ready = next
	}
	if len(ordered) != len(steps) {
		return nil, fmt.Errorf("word dependencies cycle; no chain order is safe")
	}
	return ordered, nil
}

func realProvides(word dictionary.Word, argv []string) []string {
	return bindList(word.Provides, word.Args, argv)
}

func realRequires(word dictionary.Word, argv []string) []string {
	return bindList(word.Requires, word.Args, argv)
}

func bindList(states, args, argv []string) []string {
	values := map[string]string{}
	for i, arg := range args {
		if i < len(argv) {
			values[arg] = argv[i]
		}
	}
	out := make([]string, len(states))
	for i, state := range states {
		out[i] = fill(state, values)
	}
	return out
}

func fill(state string, values map[string]string) string {
	var b strings.Builder
	for i := 0; i < len(state); {
		if state[i] == '{' {
			if end := strings.IndexByte(state[i:], '}'); end > 1 {
				name := state[i+1 : i+end]
				if value, ok := values[name]; ok {
					b.WriteString(value)
					i += end + 1
					continue
				}
			}
		}
		b.WriteByte(state[i])
		i++
	}
	return b.String()
}

func countPhrase(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
