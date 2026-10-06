package index

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/runner"
)

// Change is what the change lookup returns: the claims whose dependencies cover the touched
// files, and for every known chain that holds one of their words, the decision `verilex plan`
// makes for it.
type Change struct {
	Files  []string `json:"files"`
	Claims []Claim  `json:"claims"`
	Chains []Rerun  `json:"chains"`
}

// Rerun is one chain and its plan: run it (Run, with the first reason it runs live), skip it
// (relying on an earlier run), or refused, exactly as `verilex plan` decides.
type Rerun struct {
	Chain    string `json:"chain"`
	Run      bool   `json:"run"`
	Refused  bool   `json:"refused,omitempty"`
	Reason   string `json:"reason,omitempty"`
	ReliesOn string `json:"relies_on,omitempty"`
}

// Changed looks up what touched files affect. A word depends on its own directory, its claim
// file, the verify-skill files its claim's sources (or its implements) point at, its inputs, and
// everything every word shares: config.yaml, the frame, the files beside the word directories
// and the grouping file. The chains are those onboarding proved the covered words on and those
// this product ran with them; each gets the skip decision `verilex plan` makes, from the same code.
func (ix Index) Changed(p dictionary.Project, touched []string) (Change, error) {
	hit := ix.touchOf(p, touched)
	change := Change{Files: hit.files, Claims: []Claim{}, Chains: []Rerun{}}
	covered := hit.covered
	for _, c := range ix.Claims {
		if hit.claims[c.Claim] {
			change.Claims = append(change.Claims, c)
		}
	}
	// chains maps each chain, spaced one way, to how it was first written.
	chains := map[string]string{}
	add := func(chain string) {
		if _, ok := chains[normal(chain)]; !ok {
			chains[normal(chain)] = strings.TrimSpace(chain)
		}
	}
	for word := range covered {
		if d, ok := ix.g.Sound(word); ok && d.Chain != "" {
			add(d.Chain)
		}
	}
	records, err := runner.LoadRuns(p)
	if err != nil {
		return change, err
	}
	for _, record := range records {
		for _, segment := range strings.Split(record.Chain, "|") {
			if fields := strings.Fields(segment); len(fields) > 0 && covered[fields[0]] {
				add(record.Chain)
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(chains)) {
		change.Chains = append(change.Chains, decide(p, ix.words, chains[key]))
	}
	return change, nil
}

// decide makes the skip decision `verilex plan` makes for a chain, through the same calls.
func decide(p dictionary.Project, words []dictionary.Word, chain string) Rerun {
	r := Rerun{Chain: chain, Run: true}
	steps, err := dictionary.ParseChain(chain, words)
	if err == nil {
		err = dictionary.CheckOrder(steps)
	}
	var plan runner.Plan
	if err == nil {
		plan, err = runner.Decide(p, steps, runner.Options{})
	}
	switch {
	case err != nil:
		r.Run, r.Refused, r.Reason = false, true, err.Error()
	case plan.Skipped():
		r.Run, r.ReliesOn = false, plan.Skip[len(plan.Skip)-1].ReliesOn
	default:
		r.Reason = plan.Rerun
	}
	return r
}

// normal writes a chain with single spaces, so one chain written two ways is checked once.
func normal(chain string) string {
	segments := strings.Split(chain, "|")
	for i, segment := range segments {
		segments[i] = strings.Join(strings.Fields(segment), " ")
	}
	return strings.Join(segments, " | ")
}

// resolve makes a touched path absolute, with symbolic links resolved as the project root's are,
// even when the file itself is gone.
func resolve(file string) string {
	path, err := filepath.Abs(file)
	if err != nil {
		return file
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(dir, filepath.Base(path))
	}
	return path
}
