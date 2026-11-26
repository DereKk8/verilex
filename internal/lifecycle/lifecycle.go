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
	"io"
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
	return parseAdmission(w, data)
}

func parseAdmission(w dictionary.Word, data []byte) (*Admission, error) {
	var a Admission
	if err := json.Unmarshal(data, &a); err != nil || a.Word != w.Name || a.Curator == "" || a.WordDigest == "" || len(a.Sections) == 0 {
		return nil, fmt.Errorf("%s: not an admission record written by `verilex admit`", AdmissionPath(w))
	}
	return &a, nil
}

// Files is one word directory's non-admission files read once from disk, carrying its canonical
// digest, every accepted permission-rule digest, its legacy content-only digest, its file bytes
// and its stat witness. Both matching against a stored digest and recording a new digest come
// from this single read.
type Files struct {
	entries      []wordFile
	captured     []fingerprint.CapturedEntry
	digest       string
	accepted     []string
	contentOnly  string
	wordMDDigest string
	witness      fingerprint.Witness
}

type wordFile struct {
	rel  string
	perm fs.FileMode
	data []byte
}

// Digest returns the canonical (RecordedPerm) digest of the files read into f.
func (f *Files) Digest() string { return f.digest }

// Matches reports whether stored matches the files read into f under any accepted Perm.
func (f *Files) Matches(stored string) bool { return slices.Contains(f.accepted, stored) }

// MatchesContentOnly reports whether stored matches the legacy permission-less digest of f.
func (f *Files) MatchesContentOnly(stored string) bool { return f.contentOnly == stored }

// MatchesContract reports whether the word.md bytes read into f match the word.md parsed into w.
func (f *Files) MatchesContract(w dictionary.Word) bool {
	return w.ContractDigest == "" || f.wordMDDigest == w.ContractDigest
}

// Each visits the files read into f in lexical order without reading disk again.
func (f *Files) Each(visit func(rel string, perm fs.FileMode, data []byte)) {
	for _, e := range f.entries {
		visit(e.rel, e.perm, e.data)
	}
}

// Witness returns the stat witness of the word directory and every file read into f.
func (f *Files) Witness() fingerprint.Witness { return f.witness }

// Materialize writes the captured word files into dest with private permissions.
func (f *Files) Materialize(dest string) error {
	return fingerprint.MaterializeEntries(dest, f.captured)
}

// ReadFiles reads a word's files once in lexical order, skipping its admission record and
// interpreter caches.
func ReadFiles(w dictionary.Word) (*Files, error) {
	wd, err := readWordDir(w, false)
	if err != nil {
		return nil, err
	}
	return wd.Files, nil
}

// WordDir is a whole word directory read once from disk, providing both the proof-stamp tree
// digest (including admission.json, identical to fingerprint.Tree) and the word's lifecycle Files
// and Admission bytes from the exact same read.
type WordDir struct {
	TreeDigest     string
	Files          *Files
	admissionBytes []byte
	hasAdmission   bool
	witness        fingerprint.Witness
}

// ReadWordDir reads w.Path once in a single walk and one open per file.
func ReadWordDir(w dictionary.Word) (*WordDir, error) {
	return readWordDir(w, true)
}

// Admission parses the admission.json bytes read during ReadWordDir, or returns (nil, nil) if none
// existed in w.Path.
func (wd *WordDir) Admission(w dictionary.Word) (*Admission, error) {
	if !wd.hasAdmission {
		return nil, nil
	}
	return parseAdmission(w, wd.admissionBytes)
}

// Witness returns the stat witness covering the entire word directory read by ReadWordDir.
func (wd *WordDir) Witness() fingerprint.Witness { return wd.witness }

// Materialize writes the captured word files into dest with private permissions.
func (wd *WordDir) Materialize(dest string) error { return wd.Files.Materialize(dest) }

func readWordDir(w dictionary.Word, includeAll bool) (*WordDir, error) {
	linfo, err := os.Lstat(w.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s is missing", w.Path)
		}
		return nil, err
	}
	if linfo.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s links to a directory", w.Path)
	}
	if !linfo.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", w.Path)
	}

	perms := fingerprint.Accepted()
	permHashes := make([]interface {
		Write([]byte) (int, error)
		Sum([]byte) []byte
	}, len(perms))
	for i := range perms {
		permHashes[i] = sha256.New()
	}
	contentHash := sha256.New()

	var treeLines strings.Builder
	files := &Files{witness: fingerprint.NewWitness()}
	wd := &WordDir{Files: files, witness: fingerprint.NewWitness()}

	err = filepath.WalkDir(w.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(w.Path, path)
		if err != nil {
			return err
		}
		slashRel := filepath.ToSlash(rel)
		inCache := pathInCache(slashRel)

		if d.Type()&fs.ModeSymlink != 0 {
			li, err := d.Info()
			if err != nil {
				return err
			}
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			si, err := os.Stat(path)
			if err != nil {
				return err
			}
			if si.IsDir() {
				if !includeAll && fingerprint.Cache(d.Name()) {
					return filepath.SkipDir
				}
				return fmt.Errorf("%s links to a directory", path)
			}
			if !si.Mode().IsRegular() {
				return fmt.Errorf("%s is not a regular file", path)
			}
			wd.witness.AddSymlink(path, li, target)
			if !inCache && slashRel != admissionFile && slashRel != admissionFile+".tmp" {
				files.witness.AddSymlink(path, li, target)
			}
		} else if d.IsDir() {
			if !includeAll && fingerprint.Cache(d.Name()) {
				return filepath.SkipDir
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			wd.witness.Add(path, info)
			if includeAll {
				fmt.Fprintf(&treeLines, "%q dir %o\n", rel, fingerprint.RecordedPerm(info))
			}
			if !inCache {
				files.witness.Add(path, info)
				if rel != "." {
					files.captured = append(files.captured, fingerprint.CapturedEntry{
						Rel:   slashRel,
						IsDir: true,
						Mode:  info.Mode().Perm(),
					})
				}
			}
			return nil
		} else if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}

		isAdmission := slashRel == admissionFile || slashRel == admissionFile+".tmp"
		if !includeAll && (isAdmission || inCache) {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		info, statErr := f.Stat()
		if statErr != nil {
			f.Close()
			return statErr
		}
		if !info.Mode().IsRegular() {
			f.Close()
			return fmt.Errorf("%s is not a regular file", path)
		}
		data, readErr := io.ReadAll(f)
		f.Close()
		if readErr != nil {
			return readErr
		}

		wd.witness.Add(path, info)
		if includeAll {
			fmt.Fprintf(&treeLines, "%q %s\n", rel, fingerprint.FileDigest(fingerprint.RecordedPerm(info), data))
			if slashRel == admissionFile {
				wd.admissionBytes = data
				wd.hasAdmission = true
			}
		}
		if isAdmission || inCache {
			return nil
		}

		files.witness.Add(path, info)
		files.entries = append(files.entries, wordFile{
			rel:  slashRel,
			perm: fingerprint.RecordedPerm(info),
			data: data,
		})
		files.captured = append(files.captured, fingerprint.CapturedEntry{
			Rel:  slashRel,
			Mode: info.Mode().Perm(),
			Data: data,
		})
		if slashRel == "word.md" {
			sum := sha256.Sum256(data)
			files.wordMDDigest = hex.EncodeToString(sum[:])
		}
		for i, perm := range perms {
			fmt.Fprintf(permHashes[i], "%s\x00%o\x00%d\x00", slashRel, perm(info), len(data))
			permHashes[i].Write(data)
		}
		fmt.Fprintf(contentHash, "%s\x00%d\x00", slashRel, len(data))
		contentHash.Write(data)
		return nil
	})
	if err != nil {
		return nil, err
	}

	files.accepted = make([]string, len(perms))
	for i := range perms {
		files.accepted[i] = hex.EncodeToString(permHashes[i].Sum(nil))
	}
	files.digest = files.accepted[0]
	files.contentOnly = hex.EncodeToString(contentHash.Sum(nil))
	if includeAll {
		wd.TreeDigest = fingerprint.Of(treeLines.String())
	}
	return wd, nil
}

func pathInCache(slashRel string) bool {
	for _, seg := range strings.Split(slashRel, "/") {
		if fingerprint.Cache(seg) {
			return true
		}
	}
	return false
}

// Snapshot carries project files already read by a caller (such as stamp.Chain) so lifecycle
// status is judged from that exact read instead of re-reading disk.
type Snapshot struct {
	WordDir     *WordDir
	Binding     *fingerprint.BindingRead
	HasGrouping bool
	Grouping    grouping.Grouping
	GroupingErr error
	Claim       *dictionary.Claim
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
	files, err := ReadFiles(w)
	return err == nil && files.MatchesContract(w) && files.Matches(d.Digest)
}

// StatusOf reports a word's lifecycle state. A word that proves a claim is admitted by its
// onboarding decision in the grouping file, and drifts when its files, the frame or the shared
// word files, its claim version, the claim it is grouped under, its claim's planted defects or
// the claim's review state no longer match that decision. A word without a claim is admitted by
// its admission record, and drifts when its files, the frame, the shared word files or stored
// section hashes no longer match the project.
func StatusOf(p dictionary.Project, w dictionary.Word) (Status, error) {
	return statusOf(p, w, Snapshot{})
}

// StatusOfSnapshot reports a word's lifecycle state using any pre-read inputs in snap.
func StatusOfSnapshot(p dictionary.Project, w dictionary.Word, snap Snapshot) (Status, error) {
	return statusOf(p, w, snap)
}

func statusOf(p dictionary.Project, w dictionary.Word, snap Snapshot) (Status, error) {
	if w.Claim != nil {
		return onboarded(p, w, snap)
	}
	var (
		a   *Admission
		err error
	)
	if snap.WordDir != nil {
		a, err = snap.WordDir.Admission(w)
	} else {
		a, err = readAdmission(w)
	}
	if err != nil || a == nil {
		return Status{Word: w.Name, State: Provisional}, err
	}
	files, err := snapshotFiles(w, snap)
	if err != nil {
		return Status{Word: w.Name, State: Admitted}, err
	}
	binding, err := snapshotBinding(p, snap)
	if err != nil {
		return Status{Word: w.Name, State: Admitted}, err
	}
	s := Status{Word: w.Name, State: Admitted}
	if !files.MatchesContract(w) {
		s.Drift = append(s.Drift, "the word's files changed while reading; propose it again")
	}
	s.filesDrift(files, a.WordDigest, "the word's files changed since admission", "admitted before verilex hashed word permissions; propose it again")
	s.bindingDrift(p, binding, a.Binding, "admission", "propose it again")
	err = s.sectionDrift(p, w, a)
	if len(s.Drift) > 0 {
		s.State = DriftSuspect
	}
	return s, err
}

func onboarded(p dictionary.Project, w dictionary.Word, snap Snapshot) (Status, error) {
	var (
		g   grouping.Grouping
		err error
	)
	if snap.HasGrouping {
		g, err = snap.Grouping, snap.GroupingErr
	} else {
		g, err = grouping.Load(p)
	}
	if err != nil {
		return Status{Word: w.Name, State: Provisional}, err
	}
	d, ok := g.Words[w.Name]
	if !ok {
		return Status{Word: w.Name, State: Provisional}, nil
	}
	if snap.Claim != nil {
		w.Claim = snap.Claim
		if w.Stale == "" && w.Claim.Pin() != w.Proves() {
			w.Stale = fmt.Sprintf("%s pins claim %s, which is now %s; a pass proves only the version it ran against: check that %s still proves the claim, then pin %s", w.Name, w.Proves(), w.Claim.Pin(), w.Name, w.Claim.Pin())
		}
	}
	s := Status{Word: w.Name, State: DriftSuspect}
	if why := grouping.Broken(w.Name, d); why != "" {
		s.Drift = append(s.Drift, why+"; onboard it again")
		return s, nil
	}
	files, err := snapshotFiles(w, snap)
	if err != nil {
		return s, err
	}
	binding, err := snapshotBinding(p, snap)
	if err != nil {
		return s, err
	}
	if !files.MatchesContract(w) {
		s.Drift = append(s.Drift, "the word's files changed while reading; onboard it again")
	}
	s.filesDrift(files, d.Digest, "the word's files changed since onboarding", "onboarded before verilex hashed word permissions; onboard it again")
	s.bindingDrift(p, binding, d.Binding, "onboarding", "onboard it again")
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
	// A claim plan binds the word's arguments from that chain only, so a chain that no longer
	// binds them leaves the word nothing it was admitted on.
	if d.Chain != "" && w.Stale == "" {
		if _, ok := dictionary.OwnStep(d.Chain, w); !ok {
			s.Drift = append(s.Drift, "its onboarded chain no longer binds its arguments, so claim plans refuse it; onboard it again")
		}
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
			for _, name := range slices.Sorted(maps.Keys(d.GroupDefects)) {
				if _, ok := group.Defects[name]; !ok {
					s.Drift = append(s.Drift, "grouped claim "+group.Name+" no longer declares planted defect "+name+", which "+w.Name+" was compared under; onboard it again")
				}
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(w.Claim.Defects)) {
		if d.Defects[name] != w.Claim.Defects[name].Digest() {
			s.Drift = append(s.Drift, "claim "+w.Claim.Name+" declares planted defect "+name+", which "+w.Name+" was never gated against")
		}
	}
	for _, name := range slices.Sorted(maps.Keys(d.Defects)) {
		if _, ok := w.Claim.Defects[name]; !ok {
			s.Drift = append(s.Drift, "claim "+w.Claim.Name+" no longer declares planted defect "+name+", which "+w.Name+" was gated against; onboard it again")
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

func snapshotFiles(w dictionary.Word, snap Snapshot) (*Files, error) {
	if snap.WordDir != nil {
		return snap.WordDir.Files, nil
	}
	return ReadFiles(w)
}

func snapshotBinding(p dictionary.Project, snap Snapshot) (*fingerprint.BindingRead, error) {
	if snap.Binding != nil {
		return snap.Binding, nil
	}
	return fingerprint.ReadBinding(p)
}

// bindingDrift names each part of the binding, what every word runs with besides its own
// directory, whose fingerprint differs from the one recorded when the word was judged at since.
// A judgment recorded none, so it cannot say the word ran with the binding as it is now.
func (s *Status) bindingDrift(p dictionary.Project, binding *fingerprint.BindingRead, recorded map[string]string, since, again string) {
	if recorded == nil {
		s.Drift = append(s.Drift, "the frame and the shared word files were not recorded at "+since+"; "+again)
		return
	}
	for _, part := range binding.Parts() {
		if part.Matches(recorded[part.Part.Name]) {
			continue
		}
		paths := make([]string, len(part.Part.Names))
		for i, path := range part.Part.Paths() {
			paths[i], _ = filepath.Rel(p.Root, path)
		}
		why := part.Part.Label + " changed since " + since
		if len(paths) > 0 {
			why += " (" + strings.Join(paths, ", ") + ")"
		}
		s.Drift = append(s.Drift, why)
	}
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

// WordDigest hashes every file of a word's directory except its admission record: its name,
// permissions and contents, as a proof stamp does.
func WordDigest(w dictionary.Word) (string, error) {
	files, err := ReadFiles(w)
	if err != nil {
		return "", err
	}
	return files.Digest(), nil
}

// filesDrift adds changed when files no longer match the digest a record stored. A digest an
// earlier verilex stored without permissions adds older instead: the files may be unchanged, but
// the record cannot show that their permissions are the ones judged.
func (s *Status) filesDrift(files *Files, stored, changed, older string) {
	if files.Matches(stored) {
		return
	}
	if files.MatchesContentOnly(stored) {
		changed = older
	}
	s.Drift = append(s.Drift, changed)
}

// FilesMatch reports whether stored is the digest of w's files as they are now (see
// fingerprint.Match).
func FilesMatch(w dictionary.Word, stored string) (bool, error) {
	files, err := ReadFiles(w)
	if err != nil {
		return false, err
	}
	return files.Matches(stored), nil
}

// EachFile visits a word's files in lexical order, skipping its admission record and
// interpreter caches, which are not the word. The permission it reports is the one
// fingerprint.RecordedPerm stores, so admission and the proof stamp agree.
func EachFile(w dictionary.Word, visit func(rel string, perm fs.FileMode, data []byte)) error {
	files, err := ReadFiles(w)
	if err != nil {
		return err
	}
	files.Each(visit)
	return nil
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
	return holdStatus(StatusOf(p, w))
}

// HoldSnapshot is Hold evaluated on pre-read project state from snap.
func HoldSnapshot(p dictionary.Project, w dictionary.Word, snap Snapshot) string {
	return holdStatus(StatusOfSnapshot(p, w, snap))
}

func holdStatus(s Status, err error) string {
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
