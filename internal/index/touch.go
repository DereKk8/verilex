package index

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/featuremap"
	"github.com/DereKk8/verilex/internal/fingerprint"
	"github.com/DereKk8/verilex/internal/grouping"
	"github.com/DereKk8/verilex/internal/lifecycle"
	"github.com/DereKk8/verilex/internal/runner"
)

// diffKind is one entry of a diff: a path, a config key, an image pin or a runbook section.
type diffKind struct {
	kind, value string
	at          int
}

// touch is which claims and words a diff hits, and why a hit the proof stamp does not
// fingerprint must run live, by word (forcedWords). files is how the diff is reported back.
type touch struct {
	files       []string
	covered     map[string]bool
	claims      map[string]bool
	forcedWords map[string]string
	// unmapped are the diff entries that hit no word and no claim, as given.
	unmapped []string
}

// touchOf intersects a diff with word dependencies. A path hits a word's directory, its inputs,
// its claim file, the feature files its sources point at, and the files every word shares.
// config:<key>, image:<pin> and runbook:<ref> hit the dependencies a word declares, plus the
// runbook refs and section hashes its claim sources pin. A config key or image pin is not in
// the proof stamp, so a hit forces the claim to run.
func (ix Index) touchOf(p dictionary.Project, raw []string) touch {
	hit := touch{files: []string{}, covered: map[string]bool{}, claims: map[string]bool{}, forcedWords: map[string]string{}}
	var paths []string
	var pathAt []int
	var typed []diffKind
	mapped := make([]bool, len(raw))
	for i, item := range raw {
		kind, value := classifyChange(item)
		if kind == "path" {
			path := resolve(value)
			paths = append(paths, path)
			pathAt = append(pathAt, i)
			shown := path
			if rel, err := filepath.Rel(p.Root, path); err == nil && !strings.HasPrefix(rel, "..") {
				shown = rel
			}
			hit.files = append(hit.files, filepath.ToSlash(shown))
			continue
		}
		typed = append(typed, diffKind{kind, value, i})
		hit.files = append(hit.files, item)
	}
	// covers marks every path entry that hits deps, so an entry that hits nothing is reported.
	covers := func(deps []string) bool {
		found := false
		for i, path := range paths {
			if slices.ContainsFunc(deps, func(dep string) bool {
				return path == dep || strings.HasPrefix(path, dep+string(filepath.Separator))
			}) {
				mapped[pathAt[i]], found = true, true
			}
		}
		return found
	}
	shared := sharedDeps(p)
	claimDeps := map[string][]string{}
	runbook := map[string][]string{}
	hashes := map[string][]string{}
	for name, c := range ix.claims {
		deps := []string{c.Path}
		refs, digests := ix.sections(p, c)
		runbook[name] = refs
		hashes[name] = digests
		for _, source := range c.Sources {
			root, dirs := lifecycle.SourceRoot(p, source)
			if anchor, err := featuremap.Pin(root, dirs, featuremap.Source{Ref: source.Ref, Requirements: source.Requirements, Prose: source.Prose, Covered: source.Covered}); err == nil && anchor.File != "" {
				deps = append(deps, filepath.Join(root, anchor.File))
			}
		}
		claimDeps[name] = deps
	}
	for name, deps := range claimDeps {
		pathHit := covers(deps)
		refHit := sectionHit(typed, runbook[name], hashes[name], mapped)
		if pathHit || refHit {
			hit.claims[ix.groupOf(name)] = true
		}
	}
	for _, w := range ix.words {
		deps := append(slices.Clone(shared), w.Path)
		for _, input := range append(slices.Clone(w.Inputs), w.Depends.Paths...) {
			if !filepath.IsAbs(input) {
				input = filepath.Join(p.Root, input)
			}
			deps = append(deps, filepath.Clean(input))
		}
		refs, digests := []string{}, []string{}
		if w.Claim != nil {
			deps = append(deps, filepath.Join(p.Dir(), "grouping.yaml"))
			deps = append(deps, claimDeps[w.Claim.Name]...)
			refs = append(refs, runbook[w.Claim.Name]...)
			digests = append(digests, hashes[w.Claim.Name]...)
			if d, ok := ix.g.Sound(w.Name); ok && d.Pin() == w.Proves() && grouping.Name(d.Claim) != w.Claim.Name {
				group := grouping.Name(d.Claim)
				deps = append(deps, claimDeps[group]...)
				refs = append(refs, runbook[group]...)
				digests = append(digests, hashes[group]...)
			}
		} else {
			for _, ref := range w.Implements {
				if section, err := featuremap.Resolve(p.Root, p.SkillDirs, ref); err == nil {
					deps = append(deps, filepath.Join(p.Root, section.File))
				}
			}
		}
		refs = append(refs, w.Depends.Runbook...)
		reason := declaredHit(w, typed, mapped)
		pathHit := covers(deps)
		refHit := sectionHit(typed, refs, digests, mapped)
		if pathHit || reason != "" || refHit {
			hit.covered[w.Name] = true
			if reason != "" {
				hit.forcedWords[w.Name] = reason
			}
			if name := ix.claimName(w); name != "" {
				hit.claims[name] = true
			}
		}
	}
	for i, item := range raw {
		if !mapped[i] {
			hit.unmapped = append(hit.unmapped, item)
		}
	}
	return hit
}

// forcedChain is why a chain must run live for this diff: its first step whose word depends on
// a config key or image pin the diff changed. "" when the stamps decide.
func (hit touch) forcedChain(steps []dictionary.Step) string {
	for _, step := range steps {
		if why := hit.forcedWords[step.Word.Name]; why != "" {
			return step.Label() + ": " + why
		}
	}
	return ""
}

// gapsOf is what hit touched outside steps: entries that hit nothing, claimless words, and words
// whose claim steps prove through another word. Each list is sorted.
func (ix Index) gapsOf(hit touch, steps []dictionary.Step) runner.Gaps {
	gaps := runner.Gaps{Unmapped: hit.unmapped}
	inChain, proved := map[string]bool{}, map[string]bool{}
	for _, step := range steps {
		inChain[step.Word.Name] = true
		if name := ix.claimName(step.Word); name != "" {
			proved[name] = true
		}
	}
	for _, word := range ix.words {
		if !hit.covered[word.Name] || inChain[word.Name] {
			continue
		}
		if name := ix.claimName(word); name == "" {
			gaps.Unclaimed = append(gaps.Unclaimed, word.Name)
		} else if proved[name] {
			gaps.Unrun = append(gaps.Unrun, word.Name)
		}
	}
	slices.Sort(gaps.Unclaimed)
	slices.Sort(gaps.Unrun)
	return gaps
}

// ForcedLive is why a chain must run live for a diff, or "" when the stamps decide. A chain
// run given --changed passes it to the runner, so a matching stamp cannot hide the diff.
func (ix Index) ForcedLive(p dictionary.Project, steps []dictionary.Step, changes []string) string {
	return ix.touchOf(p, changes).forcedChain(steps)
}

// claimName is the grouped claim a word proves, or "" when it proves none.
func (ix Index) claimName(w dictionary.Word) string {
	if w.Claim == nil {
		return ""
	}
	return ix.groupOf(w.Claim.Name)
}

// sharedDeps lists config.yaml and the binding's paths: what every word runs with besides its own
// directory.
func sharedDeps(p dictionary.Project) []string {
	// An unreadable words directory lists no shared word files; the stamp reports why.
	parts, _ := fingerprint.Binding(p)
	shared := []string{filepath.Join(p.Dir(), "config.yaml")}
	for _, part := range parts {
		shared = append(shared, part.Paths()...)
	}
	return shared
}

func (ix Index) sections(p dictionary.Project, c dictionary.Claim) (refs, hashes []string) {
	for _, source := range c.Sources {
		refs = append(refs, source.Ref)
		root, dirs := lifecycle.SourceRoot(p, source)
		anchor, err := featuremap.Pin(root, dirs, featuremap.Source{Ref: source.Ref, Requirements: source.Requirements, Prose: source.Prose, Covered: source.Covered})
		if err != nil {
			continue
		}
		if anchor.Prose != "" {
			hashes = append(hashes, anchor.Prose)
		}
		if anchor.Hash != "" {
			hashes = append(hashes, anchor.Hash)
		}
	}
	return refs, hashes
}

// classifyChange splits a diff entry. A bare path stays a path. config:, image:, runbook: and
// path: name the other kinds; the value may itself contain colons.
func classifyChange(raw string) (kind, value string) {
	for _, kind := range []string{"config", "image", "runbook", "path"} {
		prefix := kind + ":"
		if strings.HasPrefix(raw, prefix) && len(raw) > len(prefix) {
			return kind, raw[len(prefix):]
		}
	}
	return "path", raw
}

// declaredHit names config keys and image pins the diff hits. Those are not in the proof
// stamp, so a hit must run live. A runbook section is fingerprinted with the claim sources, so
// it is a touch, not a forced run.
func declaredHit(w dictionary.Word, typed []diffKind, mapped []bool) string {
	var reasons []string
	for _, change := range typed {
		switch change.kind {
		case "config":
			if slices.Contains(w.Depends.ConfigKeys, change.value) {
				mapped[change.at] = true
				reasons = append(reasons, "config key "+change.value+" changed")
			}
		case "image":
			if slices.Contains(w.Depends.Images, change.value) {
				mapped[change.at] = true
				reasons = append(reasons, "image pin "+change.value+" changed")
			}
		}
	}
	slices.Sort(reasons)
	return strings.Join(reasons, "; ")
}

// sectionHit reports whether a runbook entry hits refs or hashes, and marks each entry that does.
func sectionHit(typed []diffKind, refs, hashes []string, mapped []bool) bool {
	found := false
	for _, change := range typed {
		if change.kind != "runbook" {
			continue
		}
		if slices.Contains(refs, runbookRef(change.value)) || slices.Contains(hashes, change.value) {
			mapped[change.at], found = true, true
		}
	}
	return found
}

func runbookRef(value string) string {
	if i := strings.LastIndex(value, "@"); i > 0 && hexOnly(value[i+1:]) {
		return value[:i]
	}
	return value
}

func hexOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
