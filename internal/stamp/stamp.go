// Package stamp owns proof stamps: fingerprints of everything a word's result depended on.
package stamp

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/fingerprint"
	"github.com/DereKk8/verilex/internal/grouping"
	"github.com/DereKk8/verilex/internal/lifecycle"
)

// format changes whenever the meaning of a stamp changes, so old stamps stop matching.
const format = "verilex-stamp-1"

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
}

// Chain stamps every step. A step's upstream component is the previous step's digest, so a
// change anywhere before a step changes its stamp too: every earlier word drives the same instance.
func Chain(project dictionary.Project, steps []dictionary.Step) []Stamp {
	h := hasher{cache: map[string]result{}}
	h.grouping, h.groupingErr = grouping.Load(project)
	common := map[string]string{"verilex": format}
	unclear := ""
	if exe, err := os.Executable(); err != nil {
		unclear = "verilex cannot locate its own executable"
	} else if common["verilex"], err = h.path(exe); err != nil {
		unclear = "verilex executable: " + err.Error()
	}
	if digest, err := h.path(filepath.Join(project.Dir(), "config.yaml")); err != nil {
		unclear = "config.yaml: " + err.Error()
	} else {
		common["config"] = digest
	}
	if binding, err := fingerprint.Digests(project); err != nil {
		unclear = err.Error()
	} else {
		maps.Copy(common, binding)
	}
	stamps := make([]Stamp, len(steps))
	holds := map[string]string{}
	prefix := [][]string{}
	upstream := ""
	for i, step := range steps {
		prefix = append(prefix, append([]string{step.Word.Name}, step.Argv...))
		slot, _ := json.Marshal(prefix)
		s := Stamp{Slot: fingerprint.Of(string(slot)), Components: map[string]string{"upstream": upstream}}
		for key, value := range common {
			s.Components[key] = value
		}
		s.Unclear = unclear
		if s.Unclear == "" {
			s.Unclear = h.word(step.Word, project.Root, s.Components)
		}
		if s.Unclear == "" && i > 0 && upstream == "" {
			s.Unclear = "upstream " + steps[i-1].Label() + " has no stamp"
		}
		if s.Unclear == "" {
			s.Digest = fingerprint.Of(s.Slot + "\n" + lines(s.Components))
		}
		hold, seen := holds[step.Word.Name]
		if !seen {
			hold = lifecycle.Hold(project, step.Word)
			holds[step.Word.Name] = hold
		}
		s.Hold = hold
		upstream = s.Digest
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

type result struct {
	digest string
	err    error
}

type hasher struct {
	cache       map[string]result
	grouping    grouping.Grouping
	groupingErr error
}

func (h hasher) word(word dictionary.Word, root string, components map[string]string) string {
	if !word.InputsDeclared {
		return "declares no inputs"
	}
	var err error
	if components["word"], err = h.path(word.Path); err != nil {
		return "word files: " + err.Error()
	}
	// A pass is evidence only for the claim version it ran against. The claim is read again,
	// so a claim edited while a run goes changes the stamp taken after it.
	if word.Claim != nil {
		c, err := dictionary.ReadClaim(word.Claim.Path)
		if err != nil {
			return "claim " + word.Claim.Name + ": " + err.Error()
		}
		components["claim"] = c.Fingerprint
		// Sources are not identity, yet a pass recorded before they were re-mapped was judged
		// against other requirement sentences, so the first run after a re-map goes live.
		components["claim sources"] = c.SourcesDigest()
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
		if components["input "+input], err = h.path(path); err != nil {
			return "input " + input + ": " + err.Error()
		}
	}
	// depends.paths is a declared footprint. A pass recorded before that file changed must not skip.
	for _, input := range word.Depends.Paths {
		path := input
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if components["depends "+input], err = h.path(path); err != nil {
			return "depends " + input + ": " + err.Error()
		}
	}
	for _, name := range word.Env {
		value, set := os.LookupEnv(name)
		components["env "+name] = fingerprint.Of(fmt.Sprintf("%t\x00%s", set, value))
	}
	return ""
}

// path fingerprints a file or a whole directory tree: names, permissions and contents.
func (h hasher) path(path string) (string, error) {
	if cached, ok := h.cache[path]; ok {
		return cached.digest, cached.err
	}
	digest, err := fingerprint.Tree(path)
	h.cache[path] = result{digest, err}
	return digest, err
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
