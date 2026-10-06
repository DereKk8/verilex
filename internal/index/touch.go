package index

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/featuremap"
	"github.com/DereKk8/verilex/internal/grouping"
	"github.com/DereKk8/verilex/internal/lifecycle"
)

// diffKind is one entry of a diff: a path, a config key, an image pin or a runbook section.
type diffKind struct {
	kind, value string
}

// touch is which claims and words a diff hits, and why a hit the proof stamp does not
// fingerprint must run live. files is how the diff is reported back.
type touch struct {
	files   []string
	covered map[string]bool
	claims  map[string]bool
	forced  map[string]string
}

// touchOf intersects a diff with word dependencies. A path hits a word's directory, its inputs,
// its claim file, the feature files its sources point at, and the files every word shares.
// config:<key>, image:<pin> and runbook:<ref> hit the dependencies a word declares, plus the
// runbook refs and section hashes its claim sources pin. A config key or image pin is not in
// the proof stamp, so a hit forces the claim to run.
func (ix Index) touchOf(p dictionary.Project, raw []string) touch {
	hit := touch{files: []string{}, covered: map[string]bool{}, claims: map[string]bool{}, forced: map[string]string{}}
	var paths []string
	var typed []diffKind
	for _, item := range raw {
		kind, value := classifyChange(item)
		if kind == "path" {
			path := resolve(value)
			paths = append(paths, path)
			shown := path
			if rel, err := filepath.Rel(p.Root, path); err == nil && !strings.HasPrefix(rel, "..") {
				shown = rel
			}
			hit.files = append(hit.files, filepath.ToSlash(shown))
			continue
		}
		typed = append(typed, diffKind{kind, value})
		hit.files = append(hit.files, item)
	}
	covers := func(deps []string) bool {
		return slices.ContainsFunc(paths, func(path string) bool {
			return slices.ContainsFunc(deps, func(dep string) bool {
				return path == dep || strings.HasPrefix(path, dep+string(filepath.Separator))
			})
		})
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
		if covers(deps) || sectionHit(typed, runbook[name], hashes[name]) {
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
		reason := declaredHit(w, typed)
		if covers(deps) || reason != "" || sectionHit(typed, refs, digests) {
			hit.covered[w.Name] = true
			if name := ix.claimName(w); name != "" {
				hit.claims[name] = true
				if reason != "" {
					hit.forced[name] = reason
				}
			}
		}
	}
	return hit
}

// claimName is the grouped claim a word proves, or "" when it proves none.
func (ix Index) claimName(w dictionary.Word) string {
	if w.Claim == nil {
		return ""
	}
	return ix.groupOf(w.Claim.Name)
}

func sharedDeps(p dictionary.Project) []string {
	wordsDir := filepath.Join(p.Dir(), "words")
	shared := []string{filepath.Join(p.Dir(), "config.yaml"), filepath.Join(p.Dir(), "frame")}
	entries, err := os.ReadDir(wordsDir)
	if err != nil {
		return shared
	}
	for _, entry := range entries {
		if _, err := os.Stat(filepath.Join(wordsDir, entry.Name(), "word.md")); err != nil {
			shared = append(shared, filepath.Join(wordsDir, entry.Name()))
		}
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
func declaredHit(w dictionary.Word, typed []diffKind) string {
	var reasons []string
	for _, change := range typed {
		switch change.kind {
		case "config":
			if slices.Contains(w.Depends.ConfigKeys, change.value) {
				reasons = append(reasons, "config key "+change.value+" changed")
			}
		case "image":
			if slices.Contains(w.Depends.Images, change.value) {
				reasons = append(reasons, "image pin "+change.value+" changed")
			}
		}
	}
	slices.Sort(reasons)
	return strings.Join(reasons, "; ")
}

func sectionHit(typed []diffKind, refs, hashes []string) bool {
	for _, change := range typed {
		if change.kind != "runbook" {
			continue
		}
		if slices.Contains(refs, runbookRef(change.value)) || slices.Contains(hashes, change.value) {
			return true
		}
	}
	return false
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
