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
// does not fingerprint changed. Chain may start with words that only provide states the
// selected claims require. Order is every claim that chain proves, dependency order.
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
	chain, order, err := ix.order(p, plan.Selected)
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
		if name == "" || !selected[name] || seen[name] {
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
// proves, in dependency order.
func (ix Index) order(p dictionary.Project, selected []string) (string, []string, error) {
	bindings := ix.bindings(p)
	chosen := map[string]dictionary.Word{}
	for _, name := range selected {
		word, err := ix.wordFor(name)
		if err != nil {
			return "", nil, err
		}
		chosen[name] = word
	}
	bound := map[string][]string{}
	for _, word := range chosen {
		argv, ok := bindings[word.Name]
		if len(word.Args) > 0 && !ok {
			return "", nil, fmt.Errorf("%s needs arguments %s; run a chain that binds them, then plan again", word.Name, strings.Join(word.Args, ", "))
		}
		bound[word.Name] = argv
	}
	included := map[string]bool{}
	var words []dictionary.Word
	var queue []dictionary.Word
	for _, name := range slices.Sorted(slices.Values(selectedKeys(chosen))) {
		queue = append(queue, chosen[name])
	}
	for len(queue) > 0 {
		word := queue[0]
		queue = queue[1:]
		if included[word.Name] {
			continue
		}
		included[word.Name] = true
		words = append(words, word)
		for _, state := range realRequires(word, bound[word.Name]) {
			if stateProvided(words, bound, state) {
				continue
			}
			provider, argv, ok := ix.provider(state, bound, bindings)
			if !ok {
				return "", nil, fmt.Errorf("%s requires %s; no word provides it", word.Name, state)
			}
			if _, seen := bound[provider.Name]; !seen {
				bound[provider.Name] = argv
			}
			queue = append(queue, provider)
		}
	}
	ordered, err := topo(words, bound)
	if err != nil {
		return "", nil, err
	}
	parts := make([]string, 0, len(ordered))
	claims := []string{}
	seen := map[string]bool{}
	for _, word := range ordered {
		parts = append(parts, strings.Join(append([]string{word.Name}, bound[word.Name]...), " "))
		if name := ix.claimName(word); name != "" && !seen[name] {
			seen[name] = true
			claims = append(claims, name)
		}
	}
	return strings.Join(parts, " | "), claims, nil
}

func selectedKeys(words map[string]dictionary.Word) []string {
	names := make([]string, 0, len(words))
	for name := range words {
		names = append(names, name)
	}
	return names
}

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

func (ix Index) bindings(p dictionary.Project) map[string][]string {
	found := map[string][]string{}
	records, err := runner.LoadRuns(p)
	if err == nil {
		slices.SortFunc(records, func(a, b runner.Record) int { return strings.Compare(b.Started, a.Started) })
		for _, record := range records {
			ix.takeBindings(found, record.Chain)
		}
	}
	for _, word := range ix.words {
		if _, ok := found[word.Name]; ok {
			continue
		}
		if d, ok := ix.g.Sound(word.Name); ok && d.Chain != "" {
			ix.takeBindings(found, d.Chain)
		}
	}
	return found
}

func (ix Index) takeBindings(found map[string][]string, chain string) {
	steps, err := dictionary.ParseChain(chain, ix.words)
	if err != nil {
		return
	}
	for _, step := range steps {
		if _, ok := found[step.Word.Name]; !ok {
			found[step.Word.Name] = step.Argv
		}
	}
}

func (ix Index) provider(state string, bound, recorded map[string][]string) (dictionary.Word, []string, bool) {
	var found *dictionary.Word
	var argv []string
	for i := range ix.words {
		w := &ix.words[i]
		args, ok := matchProvide(*w, state, bound[w.Name], recorded[w.Name])
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

func stateProvided(words []dictionary.Word, bound map[string][]string, state string) bool {
	for _, word := range words {
		if slices.Contains(realProvides(word, bound[word.Name]), state) {
			return true
		}
	}
	return false
}

func topo(words []dictionary.Word, bound map[string][]string) ([]dictionary.Word, error) {
	names := map[string]dictionary.Word{}
	indegree := map[string]int{}
	after := map[string][]string{}
	for _, word := range words {
		names[word.Name] = word
		indegree[word.Name] = 0
	}
	provides := map[string][]string{}
	for _, word := range words {
		for _, state := range realProvides(word, bound[word.Name]) {
			provides[state] = append(provides[state], word.Name)
		}
	}
	for _, word := range words {
		for _, state := range realRequires(word, bound[word.Name]) {
			for _, provider := range provides[state] {
				if provider == word.Name {
					continue
				}
				after[provider] = append(after[provider], word.Name)
				indegree[word.Name]++
			}
		}
	}
	ready := []string{}
	for name, n := range indegree {
		if n == 0 {
			ready = append(ready, name)
		}
	}
	slices.Sort(ready)
	ordered := []dictionary.Word{}
	for len(ready) > 0 {
		name := ready[0]
		ready = ready[1:]
		ordered = append(ordered, names[name])
		next := append([]string{}, ready...)
		for _, consumer := range after[name] {
			indegree[consumer]--
			if indegree[consumer] == 0 {
				next = append(next, consumer)
			}
		}
		slices.Sort(next)
		ready = next
	}
	if len(ordered) != len(words) {
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
