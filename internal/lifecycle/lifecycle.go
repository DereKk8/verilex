// Package lifecycle owns a word's lifecycle state: provisional until an outside curator's
// verdict admits it, drift-suspect once an admitted word's feature-map sections or files
// change. It only reads the project, so the skip rule can consult it; package curation
// moves words through the lifecycle.
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
	Word       string            `json:"word"`
	Date       string            `json:"date"`
	Curator    string            `json:"curator"`
	Reason     string            `json:"reason,omitempty"`
	Packet     string            `json:"packet"`
	Runs       []string          `json:"runs"`
	WordDigest string            `json:"word_digest"`
	Sections   map[string]string `json:"sections"`
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
	if err = json.Unmarshal(data, &a); err != nil || a.Word != w.Name || a.Curator == "" || a.WordDigest == "" || len(a.Sections) == 0 {
		return nil, fmt.Errorf("%s: not an admission record written by `verilex admit`", AdmissionPath(w))
	}
	return &a, nil
}

// StatusOf reports a word's lifecycle state, comparing an admitted word's stored section
// hashes and file digest with the project as it is now.
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
			return s, err
		case section.Hash != a.Sections[ref]:
			s.Drift = append(s.Drift, ref+": section changed")
		case !refs[ref]:
			s.Drift = append(s.Drift, ref+": no longer implemented by the word")
		}
	}
	if len(s.Drift) > 0 {
		s.State = DriftSuspect
	}
	return s, nil
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
