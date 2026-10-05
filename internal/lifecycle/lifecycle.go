// Package lifecycle owns a word's lifecycle state: provisional until an outside curator's
// verdict admits it, drift-suspect once an admitted word's files, claim or feature-map sections
// change. It also owns a claim's review state: a claim needs review once the verify skill no
// longer holds what its sources pin. It only reads the project, so the skip rule can consult
// it; package curation moves words through the lifecycle.
package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/featuremap"
)

type State string

const (
	Provisional  State = "provisional"
	Admitted     State = "admitted"
	DriftSuspect State = "drift-suspect"
)

// Status is a word's place in its lifecycle. Only an admitted word may be skipped: a
// provisional or drift-suspect word must always run, so no skip rule may trust an earlier
// result for it (see Hold).
type Status struct {
	Word  string
	State State
	Drift []string // why the word is drift-suspect, one line per changed or missing section
}

// Admission is the record `verilex admit` writes beside an admitted word.
type Admission struct {
	Word       string   `json:"word"`
	Date       string   `json:"date"`
	Curator    string   `json:"curator"`
	Reason     string   `json:"reason,omitempty"`
	Packet     string   `json:"packet"`
	Runs       []string `json:"runs"`
	WordDigest string   `json:"word_digest"`
	// Claim is the claim version the word was admitted to prove, and ClaimSources the claim's
	// sources as the curator saw them (dictionary.Claim.SourcesDigest); Sections, for a word
	// without a claim, hold the hash of each feature-map section it implements.
	Claim        string            `json:"claim,omitempty"`
	ClaimSources string            `json:"claim_sources,omitempty"`
	Sections     map[string]string `json:"sections,omitempty"`
}

const admissionFile = "admission.json"

// AdmissionPath is where an admitted word's Admission lives.
func AdmissionPath(w dictionary.Word) string { return filepath.Join(w.Path, admissionFile) }

func readAdmission(w dictionary.Word) (*Admission, error) {
	data, err := os.ReadFile(AdmissionPath(w))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var a Admission
	if err = json.Unmarshal(data, &a); err != nil || a.Word != w.Name || a.Curator == "" || a.WordDigest == "" || (len(a.Sections) == 0 && a.Claim == "") {
		return nil, fmt.Errorf("%s: not an admission record written by `verilex admit`", AdmissionPath(w))
	}
	return &a, nil
}

// Load reads a project's claims and words, then fills each claim source's Covered: what the
// other claims map in the same sub-feature. A claim covers a sentence only when one of its words
// is curated for the claim as it is now (see Curated). A stub, a never-run or merely used word,
// or a claim that only lists a sub-feature its words were never judged against, covers nothing,
// so it can never clear another claim's review.
func Load(p dictionary.Project) (map[string]dictionary.Claim, []dictionary.Word, error) {
	claims, words, err := dictionary.Load(p)
	if err != nil {
		return nil, nil, err
	}
	covered := map[string][]string{}
	counted := map[string]bool{}
	for _, w := range words {
		if w.Claim == nil || counted[w.Claim.Name] {
			continue
		}
		if Curated(w) {
			counted[w.Claim.Name] = true
			for _, source := range w.Claim.Sources {
				key := dictionary.SubFeature(source.Ref)
				covered[key] = append(covered[key], source.Requirements...)
			}
		}
	}
	for name, c := range claims {
		c.Sources = slices.Clone(c.Sources)
		for i := range c.Sources {
			c.Sources[i].Covered = covered[dictionary.SubFeature(c.Sources[i].Ref)]
		}
		claims[name] = c
	}
	for i := range words {
		if words[i].Claim != nil {
			c := claims[words[i].Claim.Name]
			words[i].Claim = &c
		}
	}
	return claims, words, nil
}

// LoadWords is Load without the claims.
func LoadWords(p dictionary.Project) ([]dictionary.Word, error) {
	_, words, err := Load(p)
	return words, err
}

// Curated reports whether a curator admitted w for its claim as it is now: the admission names
// the claim version w pins, records the claim's current sources, and w's files are unchanged.
// Only such a word shows that its claim's sentences are exercised: a judged live use shows that
// the word runs, not that it exercises a sub-feature its claim was mapped to afterwards. An
// admission record or word file that cannot be read makes w uncurated; StatusOf reports why.
func Curated(w dictionary.Word) bool {
	if w.Claim == nil || w.Stale != "" {
		return false
	}
	a, err := readAdmission(w)
	if err != nil || a == nil || a.Claim != w.Claim.Pin() || a.ClaimSources != w.Claim.SourcesDigest() {
		return false
	}
	digest, err := WordDigest(w)
	return err == nil && digest == a.WordDigest
}

// StatusOf reports a word's lifecycle state, comparing an admitted word's file digest, claim
// version and the claim's review state (or, for a word without a claim, its stored section
// hashes) with the project as it is now.
func StatusOf(p dictionary.Project, w dictionary.Word) (Status, error) {
	a, err := readAdmission(w)
	if err != nil || a == nil {
		return Status{Word: w.Name, State: Provisional}, err
	}
	s := Status{Word: w.Name, State: Admitted}
	digest, err := WordDigest(w)
	if err != nil {
		return s, err
	}
	if digest != a.WordDigest {
		s.Drift = append(s.Drift, "the word's files changed since admission")
	}
	if w.Claim != nil {
		err = s.claimDrift(p, w, a)
	} else {
		err = s.sectionDrift(p, w, a)
	}
	if len(s.Drift) > 0 {
		s.State = DriftSuspect
	}
	return s, err
}

func (s *Status) claimDrift(p dictionary.Project, w dictionary.Word, a *Admission) error {
	switch {
	case w.Stale != "":
		s.Drift = append(s.Drift, w.Stale)
	case a.Claim != w.Claim.Pin():
		admitted := a.Claim
		if admitted == "" {
			admitted = "its feature-map sections"
		}
		s.Drift = append(s.Drift, "admitted for "+admitted+", not claim "+w.Claim.Pin())
	case a.ClaimSources != w.Claim.SourcesDigest():
		// A re-map keeps the claim version, but the curator judged the word against the old
		// sources: nothing yet says the word exercises what the claim now maps.
		s.Drift = append(s.Drift, "admitted before claim "+w.Claim.Name+"'s sources changed; propose it again")
	}
	_, review, err := Review(p, *w.Claim)
	if len(review) > 0 {
		s.Drift = append(s.Drift, "claim "+w.Claim.Name+" needs review: "+strings.Join(review, "; "))
	}
	return err
}

func (s *Status) sectionDrift(p dictionary.Project, w dictionary.Word, a *Admission) error {
	refs := map[string]bool{}
	for _, ref := range w.Implements {
		refs[ref] = true
		if _, ok := a.Sections[ref]; !ok {
			s.Drift = append(s.Drift, ref+": not part of the admission")
		}
	}
	for _, ref := range sortedKeys(a.Sections) {
		section, err := featuremap.Resolve(p.Root, p.SkillDirs, ref)
		var missing *featuremap.MissingError
		switch {
		case errors.As(err, &missing):
			s.Drift = append(s.Drift, ref+": section missing")
		case err != nil:
			return err
		case section.Hash != a.Sections[ref]:
			s.Drift = append(s.Drift, ref+": section changed")
		case !refs[ref]:
			s.Drift = append(s.Drift, ref+": no longer implemented by the word")
		}
	}
	return nil
}

// Review pins each of a claim's sources against the verify skill as it is now, and lists why the
// claim needs review: its sub-feature is gone, a requirement sentence it maps to is gone or has
// left the sub-feature, the sub-feature states a requirement that no proven claim maps, or its
// other prose changed. Commands and dated run history never ask for one. Review is computed on
// every call, never cached.
func Review(p dictionary.Project, c dictionary.Claim) ([]featuremap.Anchor, []string, error) {
	anchors := make([]featuremap.Anchor, 0, len(c.Sources))
	review := []string{}
	for _, source := range c.Sources {
		anchor, err := featuremap.Pin(p.Root, p.SkillDirs, featuremap.Source{Ref: source.Ref, Requirements: source.Requirements, Prose: source.Prose, Covered: source.Covered})
		if err != nil {
			return nil, nil, err
		}
		anchors = append(anchors, anchor)
		for _, why := range anchor.Review {
			review = append(review, source.Ref+": "+why)
		}
	}
	return anchors, review, nil
}

// WordDigest hashes every file of a word's directory except its admission record.
func WordDigest(w dictionary.Word) (string, error) {
	h := sha256.New()
	err := EachFile(w, func(rel string, data []byte) {
		fmt.Fprintf(h, "%s\x00%d\x00", rel, len(data))
		h.Write(data)
	})
	return hex.EncodeToString(h.Sum(nil)), err
}

// EachFile visits a word's files in lexical order, skipping its admission record and
// interpreter caches, which are not the word.
func EachFile(w dictionary.Word, visit func(rel string, data []byte)) error {
	return filepath.WalkDir(w.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(w.Path, path)
		if err != nil || rel == admissionFile || rel == admissionFile+".tmp" {
			return err
		}
		data, err := os.ReadFile(path)
		if err == nil {
			visit(filepath.ToSlash(rel), data)
		}
		return err
	})
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Hold says why a chain holding w must run live although every proof stamp may match: w is
// provisional, drift-suspect, or its status cannot be read. It is empty only for an admitted
// word whose sections and files are unchanged.
func Hold(p dictionary.Project, w dictionary.Word) string {
	s, err := StatusOf(p, w)
	switch {
	case err != nil:
		return "lifecycle status unreadable: " + err.Error()
	case s.State == Provisional:
		return "provisional; only admitted words are skipped"
	case s.State == DriftSuspect:
		return "drift-suspect: " + strings.Join(s.Drift, "; ")
	case s.State != Admitted:
		return "unknown lifecycle state " + string(s.State)
	}
	return ""
}
