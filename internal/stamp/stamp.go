// Package stamp owns proof stamps: fingerprints of everything a word's result depended on.
package stamp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/fingerprint"
	"github.com/DereKk8/verilex/internal/grouping"
	"github.com/DereKk8/verilex/internal/lifecycle"
)

// format changes whenever the meaning of a stamp changes, so old stamps stop matching.
const format = "verilex-stamp-1"

// afterStamp is a test seam. When a test of this package sets it, it runs once Chain has computed
// a step's stamp digest, before Hold is checked for that step. No flag, environment variable or
// file sets it.
var afterStamp func(word string)

// Stamp fingerprints one step of a chain.
type Stamp struct {
	// Slot names the step by the chain prefix that ends in it; two steps share a slot only when
	// the same words with the same arguments ran before them in the same order.
	Slot string
	// Digest covers every component; it is empty when the step cannot be stamped.
	Digest string
	// Components maps each thing the result depended on to its fingerprint:
	// verilex, config, frame, shared, word, "word admission", claim, "claim sources", "input <path>",
	// "env <NAME>" and upstream.
	Components map[string]string
	// Unclear says why the step has no stamp.
	Unclear string
	// Hold says why the step must run live even when its stamp matches a recorded result: its
	// word is provisional or drift-suspect. A held step keeps its digest, because what it
	// depended on is still fingerprinted; only reusing a result for it is forbidden.
	Hold string

	// witness records the kernel identity of every file and directory read to compute this stamp
	// (including upstream steps). It is kept in memory only, never written to JSON or digests.
	witness fingerprint.Witness
	// binding and wordDir retain the exact bytes read for the frame, shared helpers and word
	// directory so execution can materialize a private snapshot without reading disk again.
	binding *fingerprint.BindingRead
	wordDir *lifecycle.WordDir
}

// Unchanged reports whether s and other are both valid stamps with identical digests and
// identical kernel stat witnesses across every file and directory they read.
func (s Stamp) Unchanged(other Stamp) bool {
	return s.Digest != "" && s.Digest == other.Digest && s.witness.Equal(other.witness)
}

// Binding returns the binding read captured when computing s, or nil if reading the binding failed.
func (s Stamp) Binding() *fingerprint.BindingRead { return s.binding }

// WordDir returns the word directory read captured when computing s, or nil if reading it failed.
func (s Stamp) WordDir() *lifecycle.WordDir { return s.wordDir }

// WaitTick waits out the filesystem timestamp granularity window for the newest ctime observed
// across stamps, so an in-place edit and restore during execution cannot share the same tick.
func WaitTick(stamps []Stamp) {
	var newest int64
	for _, s := range stamps {
		if c := s.witness.MaxCtime(); c > newest {
			newest = c
		}
	}
	fingerprint.WaitTick(newest)
}

// Chain stamps every step. A step's upstream component is the previous step's digest, so a
// change anywhere before a step changes its stamp too: every earlier word drives the same instance.
// Each step's Hold is judged from the exact same reads of the word directory, binding, grouping
// file and claim that produced its stamp components.
func Chain(project dictionary.Project, steps []dictionary.Step) []Stamp {
	h := hasher{
		cache:    map[string]pathResult{},
		wordDirs: map[string]wordDirResult{},
		claims:   map[string]claimResult{},
	}
	h.loadGrouping(project)

	common := map[string]string{"verilex": format}
	commonWitness := fingerprint.NewWitness()
	unclear := ""
	if exe, err := os.Executable(); err != nil {
		unclear = "verilex cannot locate its own executable"
	} else if d, w, err := h.path(exe, ""); err != nil {
		unclear = "verilex executable: " + err.Error()
	} else {
		common["verilex"] = d
		commonWitness.Merge(w)
	}
	if d, w, err := h.path(filepath.Join(project.Dir(), "config.yaml"), project.Dir()); err != nil {
		unclear = "config.yaml: " + err.Error()
	} else {
		common["config"] = d
		commonWitness.Merge(w)
	}
	bindingRead, bindingErr := fingerprint.ReadBinding(project)
	if bindingErr != nil {
		unclear = bindingErr.Error()
	} else {
		maps.Copy(common, bindingRead.Digests())
		commonWitness.Merge(bindingRead.Witness())
	}

	stamps := make([]Stamp, len(steps))
	holds := map[string]string{}
	prefix := [][]string{}
	upstream := ""
	upstreamWitness := fingerprint.NewWitness()
	for i, step := range steps {
		prefix = append(prefix, append([]string{step.Word.Name}, step.Argv...))
		slot, _ := json.Marshal(prefix)
		s := Stamp{
			Slot:       fingerprint.Of(string(slot)),
			Components: map[string]string{"upstream": upstream},
			witness:    fingerprint.NewWitness(),
			binding:    bindingRead,
		}
		s.witness.Merge(upstreamWitness)
		s.witness.Merge(commonWitness)
		for key, value := range common {
			s.Components[key] = value
		}
		wd, wdErr := h.wordDir(step.Word)
		s.wordDir = wd
		if wd != nil {
			s.witness.Merge(wd.Witness())
		}
		var (
			freshClaim *dictionary.Claim
			claimErr   error
		)
		if step.Word.Claim != nil {
			var cw fingerprint.Witness
			freshClaim, cw, claimErr = h.claim(project, step.Word.Claim)
			s.witness.Merge(cw)
			s.witness.Merge(h.groupingWitness)
		}

		s.Unclear = unclear
		if s.Unclear == "" {
			s.Unclear = h.word(step.Word, project.Root, wd, wdErr, freshClaim, claimErr, s.Components, &s.witness)
		}
		if s.Unclear == "" && i > 0 && upstream == "" {
			s.Unclear = "upstream " + steps[i-1].Label() + " has no stamp"
		}
		if s.Unclear == "" {
			s.Digest = fingerprint.Of(s.Slot + "\n" + lines(s.Components))
		}
		if afterStamp != nil {
			afterStamp(step.Word.Name)
		}
		hold, seen := holds[step.Word.Name]
		if !seen {
			switch {
			case wdErr != nil:
				hold = "lifecycle status unreadable: " + wdErr.Error()
			case bindingErr != nil:
				hold = "lifecycle status unreadable: " + bindingErr.Error()
			case step.Word.Claim != nil && claimErr != nil:
				hold = "lifecycle status unreadable: " + claimErr.Error()
			default:
				hold = lifecycle.HoldSnapshot(project, step.Word, lifecycle.Snapshot{
					WordDir:     wd,
					Binding:     bindingRead,
					HasGrouping: true,
					Grouping:    h.grouping,
					GroupingErr: h.groupingErr,
					Claim:       freshClaim,
				})
			}
			holds[step.Word.Name] = hold
		}
		s.Hold = hold
		upstream = s.Digest
		upstreamWitness.Merge(s.witness)
		stamps[i] = s
	}
	return stamps
}

// Diff names the first component that differs between two stamps' components.
func Diff(old, current map[string]string) string {
	keys := map[string]bool{}
	for key := range old {
		keys[key] = true
	}
	for key := range current {
		keys[key] = true
	}
	names := make([]string, 0, len(keys))
	for key := range keys {
		names = append(names, key)
	}
	// Report the step's own components before the upstream, which only echoes an earlier change.
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == "upstream") != (names[j] == "upstream") {
			return names[j] == "upstream"
		}
		return names[i] < names[j]
	})
	for _, key := range names {
		if old[key] != current[key] {
			return key + " changed"
		}
	}
	return ""
}

type pathResult struct {
	digest  string
	witness fingerprint.Witness
	err     error
}

type wordDirResult struct {
	wd  *lifecycle.WordDir
	err error
}

type claimResult struct {
	claim   *dictionary.Claim
	witness fingerprint.Witness
	err     error
}

type hasher struct {
	cache           map[string]pathResult
	wordDirs        map[string]wordDirResult
	claims          map[string]claimResult
	grouping        grouping.Grouping
	groupingErr     error
	groupingWitness fingerprint.Witness
}

func (h *hasher) loadGrouping(project dictionary.Project) {
	path := grouping.Path(project)
	data, w, err := fingerprint.ReadFile(path, project.Dir())
	if errors.Is(err, fs.ErrNotExist) {
		h.grouping = grouping.Grouping{Words: map[string]grouping.Decision{}}
		h.groupingWitness = fingerprint.NewWitness()
		h.groupingWitness.AddAbsent(path)
		return
	}
	if err != nil {
		h.groupingErr = err
		return
	}
	h.groupingWitness = w
	h.grouping, h.groupingErr = grouping.Parse(path, data)
}

func (h *hasher) wordDir(w dictionary.Word) (*lifecycle.WordDir, error) {
	if cached, ok := h.wordDirs[w.Name]; ok {
		return cached.wd, cached.err
	}
	wd, err := lifecycle.ReadWordDir(w)
	h.wordDirs[w.Name] = wordDirResult{wd: wd, err: err}
	return wd, err
}

func (h *hasher) claim(project dictionary.Project, orig *dictionary.Claim) (*dictionary.Claim, fingerprint.Witness, error) {
	if cached, ok := h.claims[orig.Path]; ok {
		return cached.claim, cached.witness, cached.err
	}
	data, w, err := fingerprint.ReadFile(orig.Path, project.Dir())
	if err != nil {
		h.claims[orig.Path] = claimResult{err: err}
		return nil, fingerprint.Witness{}, err
	}
	c, err := dictionary.ParseClaim(orig.Path, data)
	if err != nil {
		h.claims[orig.Path] = claimResult{witness: w, err: err}
		return nil, w, err
	}
	// Preserve Covered computed by lifecycle.Load for matching sub-features.
	covered := map[string][]string{}
	for _, src := range orig.Sources {
		if len(src.Covered) > 0 {
			covered[dictionary.SourceKey(src)] = slices.Clone(src.Covered)
		}
	}
	for i := range c.Sources {
		c.Sources[i].Covered = covered[dictionary.SourceKey(c.Sources[i])]
	}
	h.claims[orig.Path] = claimResult{claim: &c, witness: w}
	return &c, w, nil
}

func (h *hasher) word(word dictionary.Word, root string, wd *lifecycle.WordDir, wdErr error, freshClaim *dictionary.Claim, claimErr error, components map[string]string, witness *fingerprint.Witness) string {
	if !word.InputsDeclared {
		return "declares no inputs"
	}
	if wdErr != nil {
		return "word files: " + wdErr.Error()
	}
	if !wd.Files.MatchesContract(word) {
		return "word files: word.md changed while loading"
	}
	components["word"] = wd.TreeDigest
	// A pass is evidence only for the claim version it ran against. The claim is read once here
	// and reused by HoldSnapshot, so stamp and Hold judge the exact same bytes.
	if word.Claim != nil {
		if claimErr != nil {
			return "claim " + word.Claim.Name + ": " + claimErr.Error()
		}
		components["claim"] = freshClaim.Fingerprint
		// Sources are not identity, yet a pass recorded before they were re-mapped was judged
		// against other requirement sentences, so the first run after a re-map goes live.
		components["claim sources"] = freshClaim.SourcesDigest()
		// The onboarding decision admits the word the way an admission record admits a word
		// without a claim, so onboarding runs the word's chains live once more.
		if h.groupingErr != nil {
			return "grouping: " + h.groupingErr.Error()
		}
		components["word admission"] = "none"
		if d, ok := h.grouping.Words[word.Name]; ok {
			components["word admission"] = d.Seal
		}
	}
	for _, input := range word.Inputs {
		path := input
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		d, w, err := h.path(path, root)
		if err != nil {
			return "input " + input + ": " + err.Error()
		}
		components["input "+input] = d
		witness.Merge(w)
	}
	// depends.paths is a declared footprint. A pass recorded before that file changed must not skip.
	for _, input := range word.Depends.Paths {
		path := input
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		d, w, err := h.path(path, root)
		if err != nil {
			return "depends " + input + ": " + err.Error()
		}
		components["depends "+input] = d
		witness.Merge(w)
	}
	for _, name := range word.Env {
		value, set := os.LookupEnv(name)
		components["env "+name] = fingerprint.Of(fmt.Sprintf("%t\x00%s", set, value))
	}
	return ""
}

// path fingerprints a file or a whole directory tree: names, permissions and contents.
func (h *hasher) path(path, stopAt string) (string, fingerprint.Witness, error) {
	key := stopAt + "\x00" + path
	if cached, ok := h.cache[key]; ok {
		return cached.digest, cached.witness, cached.err
	}
	digest, w, err := fingerprint.TreeWithWitness(path, stopAt)
	h.cache[key] = pathResult{digest, w, err}
	return digest, w, err
}

func lines(components map[string]string) string {
	keys := make([]string, 0, len(components))
	for key := range components {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&b, "%q=%s\n", key, components[key])
	}
	return b.String()
}
