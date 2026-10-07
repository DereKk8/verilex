// Package lifecycle owns a word's lifecycle state: provisional until it is admitted (a word that
// proves a claim by its onboarding decision, a word without a claim by an outside curator's
// verdict), drift-suspect once what that admission judged changes. It also owns a claim's review
// state: a claim needs review once the verify skill no longer holds what its sources pin. It only
// reads the project, so the skip rule can consult it; packages onboarding and curation move
// words through the lifecycle.
package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/DereKk8/verilex/internal/featuremap"
	"github.com/DereKk8/verilex/internal/fingerprint"
	"github.com/DereKk8/verilex/internal/grouping"
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

// Admission is the record `verilex admit` writes beside an admitted word without a claim.
type Admission struct {
	Word       string            `json:"word"`
	Date       string            `json:"date"`
	Curator    string            `json:"curator"`
	Reason     string            `json:"reason,omitempty"`
	Packet     string            `json:"packet"`
	Runs       []string          `json:"runs"`
	WordDigest string            `json:"word_digest"`
	Sections   map[string]string `json:"sections"`
	// Binding fingerprints the frame and the shared word files as the packet held them.
	Binding map[string]string `json:"binding,omitempty"`
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

// Load reads a project's claims and words, then fills each claim source's Covered: what the
// other claims map in the same sub-feature of the same repository. A claim covers a sentence
// only when one of its words is onboarded for the claim as it is now (see Curated). A stub, a
// never-run or merely used word, or a claim that only lists a sub-feature its words were never
// judged against, covers nothing, so it can never clear another claim's review.
func Load(p dictionary.Project) (map[string]dictionary.Claim, []dictionary.Word, error) {
	claims, words, err := dictionary.Load(p)
	if err != nil {
		return nil, nil, err
	}
	// An unreadable grouping file curates nothing; StatusOf reports why.
	g, _ := grouping.Load(p)
	covered := map[string][]string{}
	counted := map[string]bool{}
	for _, w := range words {
		if w.Claim == nil || counted[w.Claim.Name] {
			continue
		}
		if Curated(g, w) {
			counted[w.Claim.Name] = true
			for _, source := range w.Claim.Sources {
				key := dictionary.SourceKey(source)
				covered[key] = append(covered[key], source.Requirements...)
			}
		}
	}
	for name, c := range claims {
		c.Sources = slices.Clone(c.Sources)
		for i := range c.Sources {
			c.Sources[i].Covered = covered[dictionary.SourceKey(c.Sources[i])]
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

// Curated reports whether onboarding admitted w for its claim as it is now: w's decision in g
// is sound, pins the claim version w pins, records the claim's current sources, and w's files
// are unchanged. Only such a word shows that its claim's sentences are exercised: a judged live
// use shows that the word runs, not that it exercises a sub-feature its claim was mapped to
// afterwards.
func Curated(g grouping.Grouping, w dictionary.Word) bool {
	if w.Claim == nil || w.Stale != "" {
		return false
	}
	d, ok := g.Sound(w.Name)
	if !ok || d.Pin() != w.Proves() || d.ClaimSources != w.Claim.SourcesDigest() {
		return false
	}
	digest, err := WordDigest(w)
	return err == nil && digest == d.Digest
}

// StatusOf reports a word's lifecycle state. A word that proves a claim is admitted by its
// onboarding decision in the grouping file, and drifts when its files, the frame or the shared
// word files, its claim version, the claim it is grouped under, its claim's planted defects or
// the claim's review state no longer match that decision. A word without a claim is admitted by
// its admission record, and drifts when its files, the frame, the shared word files or stored
// section hashes no longer match the project.
func StatusOf(p dictionary.Project, w dictionary.Word) (Status, error) {
	if w.Claim != nil {
		return onboarded(p, w)
	}
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
	if err = s.bindingDrift(p, a.Binding, "admission", "propose it again"); err != nil {
		return s, err
	}
	err = s.sectionDrift(p, w, a)
	if len(s.Drift) > 0 {
		s.State = DriftSuspect
	}
	return s, err
}

func onboarded(p dictionary.Project, w dictionary.Word) (Status, error) {
	g, err := grouping.Load(p)
	if err != nil {
		return Status{Word: w.Name, State: Provisional}, err
	}
	d, ok := g.Words[w.Name]
	if !ok {
		return Status{Word: w.Name, State: Provisional}, nil
	}
	s := Status{Word: w.Name, State: DriftSuspect}
	if why := grouping.Broken(w.Name, d); why != "" {
		s.Drift = append(s.Drift, why+"; onboard it again")
		return s, nil
	}
	digest, err := WordDigest(w)
	if err != nil {
		return s, err
	}
	if digest != d.Digest {
		s.Drift = append(s.Drift, "the word's files changed since onboarding")
	}
	if err = s.bindingDrift(p, d.Binding, "onboarding", "onboard it again"); err != nil {
		return s, err
	}
	switch {
	case w.Stale != "":
		s.Drift = append(s.Drift, w.Stale)
	case d.Pin() != w.Proves():
		s.Drift = append(s.Drift, "onboarded for "+d.Pin()+", not claim "+w.Proves())
	case d.ClaimSources != w.Claim.SourcesDigest():
		// A re-map keeps the claim version, but onboarding checked the word against the old
		// sources: nothing yet says the word exercises what the claim now maps.
		s.Drift = append(s.Drift, "onboarded before claim "+w.Claim.Name+"'s sources changed; onboard it again")
	}
	if d.Proves != "" {
		group, err := dictionary.ReadClaim(filepath.Join(p.ClaimsDir(), grouping.Name(d.Claim)+".yaml"))
		switch {
		case err != nil:
			s.Drift = append(s.Drift, "grouped under claim "+grouping.Name(d.Claim)+", which no longer loads")
		case group.Pin() != d.Claim:
			s.Drift = append(s.Drift, "grouped under "+d.Claim+", which is now "+group.Pin())
		default:
			for _, name := range slices.Sorted(maps.Keys(group.Defects)) {
				if d.GroupDefects[name] != group.Defects[name].Digest() {
					s.Drift = append(s.Drift, "grouped claim "+group.Name+" declares planted defect "+name+", which "+w.Name+" was never compared under")
				}
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(w.Claim.Defects)) {
		if d.Defects[name] != w.Claim.Defects[name].Digest() {
			s.Drift = append(s.Drift, "claim "+w.Claim.Name+" declares planted defect "+name+", which "+w.Name+" was never gated against")
		}
	}
	_, review, err := Review(p, *w.Claim)
	if len(review) > 0 {
		s.Drift = append(s.Drift, "claim "+w.Claim.Name+" needs review: "+strings.Join(review, "; "))
	}
	if len(s.Drift) == 0 {
		s.State = Admitted
	}
	return s, err
}

// bindingDrift names each part of the binding, what every word runs with besides its own
// directory, whose fingerprint differs from the one recorded when the word was judged at since.
// A judgment recorded none, so it cannot say the word ran with the binding as it is now.
func (s *Status) bindingDrift(p dictionary.Project, recorded map[string]string, since, again string) error {
	parts, err := fingerprint.Binding(p)
	if err != nil {
		return err
	}
	if recorded == nil {
		s.Drift = append(s.Drift, "the frame and the shared word files were not recorded at "+since+"; "+again)
		return nil
	}
	for _, part := range parts {
		digest, err := part.Digest()
		if err != nil {
			return fmt.Errorf("%s: %w", part.Label, err)
		}
		if digest == recorded[part.Name] {
			continue
		}
		paths := make([]string, len(part.Names))
		for i, path := range part.Paths() {
			paths[i], _ = filepath.Rel(p.Root, path)
		}
		why := part.Label + " changed since " + since
		if len(paths) > 0 {
			why += " (" + strings.Join(paths, ", ") + ")"
		}
		s.Drift = append(s.Drift, why)
	}
	return nil
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
// other prose changed. Inline commands never ask for one. Review is computed on
// every call, never cached.
func Review(p dictionary.Project, c dictionary.Claim) ([]featuremap.Anchor, []string, error) {
	anchors := make([]featuremap.Anchor, 0, len(c.Sources))
	review := []string{}
	for _, source := range c.Sources {
		root, dirs := SourceRoot(p, source)
		anchor, err := featuremap.Pin(root, dirs, featuremap.Source{Ref: source.Ref, Requirements: source.Requirements, Prose: source.Prose, Covered: source.Covered})
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

// SourceRoot is where a claim source's verify skill lives: the product's own skill directories,
// or the default skill directories of the repository checkout the source names.
func SourceRoot(p dictionary.Project, source dictionary.ClaimSource) (string, []string) {
	if source.Repo == "" {
		return p.Root, p.SkillDirs
	}
	if filepath.IsAbs(source.Repo) {
		return filepath.Clean(source.Repo), dictionary.DefaultSkillDirs
	}
	return filepath.Join(p.Root, source.Repo), dictionary.DefaultSkillDirs
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
			if fingerprint.Cache(d.Name()) {
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
