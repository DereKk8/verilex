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
	// Claim is the claim version the word was admitted to prove; Sections, for a word without
	// a claim, hold the hash of each feature-map section it implements.
	Claim    string            `json:"claim,omitempty"`
	Sections map[string]string `json:"sections,omitempty"`
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
	}
	_, review, err := Review(p, *w.Claim)
	for _, line := range review {
		s.Drift = append(s.Drift, "claim "+w.Claim.Name+" needs review: "+line)
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
// claim needs review: a sub-feature or a requirement sentence it maps to is gone. Prose, commands
// and run history around them never ask for one. Review is computed on every call, never cached.
func Review(p dictionary.Project, c dictionary.Claim) ([]featuremap.Anchor, []string, error) {
	anchors := make([]featuremap.Anchor, 0, len(c.Sources))
	review := []string{}
	for _, source := range c.Sources {
		anchor, err := featuremap.Pin(p.Root, p.SkillDirs, source.Ref, source.Requirements)
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
