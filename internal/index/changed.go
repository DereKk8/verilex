package index

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/featuremap"
	"github.com/DereKk8/verilex/internal/lifecycle"
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
	change := Change{Files: []string{}, Claims: []Claim{}, Chains: []Rerun{}}
	paths := make([]string, 0, len(touched))
	for _, file := range touched {
		path := resolve(file)
		paths = append(paths, path)
		if rel, err := filepath.Rel(p.Root, path); err == nil && !strings.HasPrefix(rel, "..") {
			path = rel
		}
		change.Files = append(change.Files, filepath.ToSlash(path))
	}
	covers := func(deps []string) bool {
		return slices.ContainsFunc(paths, func(path string) bool {
			return slices.ContainsFunc(deps, func(dep string) bool { return path == dep || strings.HasPrefix(path, dep+string(filepath.Separator)) })
		})
	}
	wordsDir := filepath.Join(p.Dir(), "words")
	shared := []string{filepath.Join(p.Dir(), "config.yaml"), filepath.Join(p.Dir(), "frame")}
	if entries, err := os.ReadDir(wordsDir); err == nil {
		for _, entry := range entries {
			if _, err := os.Stat(filepath.Join(wordsDir, entry.Name(), "word.md")); err != nil {
				shared = append(shared, filepath.Join(wordsDir, entry.Name()))
			}
		}
	}
	claimDeps := map[string][]string{}
	for name, c := range ix.claims {
		deps := []string{c.Path}
		for _, source := range c.Sources {
			root, dirs := lifecycle.SourceRoot(p, source)
			if anchor, err := featuremap.Pin(root, dirs, featuremap.Source{Ref: source.Ref, Requirements: source.Requirements, Prose: source.Prose, Covered: source.Covered}); err == nil && anchor.File != "" {
				deps = append(deps, filepath.Join(root, anchor.File))
			}
		}
		claimDeps[name] = deps
	}
	covered := map[string]bool{}
	claims := map[string]bool{}
	for name, deps := range claimDeps {
		if covers(deps) {
			claims[ix.groupOf(name)] = true
		}
	}
	for _, w := range ix.words {
		deps := append(slices.Clone(shared), w.Path)
		for _, input := range w.Inputs {
			if !filepath.IsAbs(input) {
				input = filepath.Join(p.Root, input)
			}
			deps = append(deps, filepath.Clean(input))
		}
		if w.Claim != nil {
			deps = append(deps, filepath.Join(p.Dir(), "grouping.yaml"))
			deps = append(deps, claimDeps[w.Claim.Name]...)
		} else {
			for _, ref := range w.Implements {
				if section, err := featuremap.Resolve(p.Root, p.SkillDirs, ref); err == nil {
					deps = append(deps, filepath.Join(p.Root, section.File))
				}
			}
		}
		if covers(deps) {
			covered[w.Name] = true
			if w.Claim != nil {
				claims[ix.groupOf(w.Claim.Name)] = true
			}
		}
	}
	for _, c := range ix.Claims {
		if claims[c.Claim] {
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
