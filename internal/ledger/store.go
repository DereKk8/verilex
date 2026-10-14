package ledger

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DereKk8/verilex/internal/stamp"
	"github.com/DereKk8/verilex/internal/verdict"
)

// Store is the ledger of recorded passes: a directory that every verilex instance pointed at it
// reads and writes, whether it sits on one machine or on storage many stateless instances share.
// Each pass is one immutable file, named by the stamp it proved and the digest of its content,
// beside its own copy of the step's evidence:
//
//	passes/<slot>/<stamp>.<digest>.json
//	evidence/<id>/
//
// Writers only create files, each under a name no other writer uses, and never rewrite or lock
// one: concurrent instances lose no pass and never mix two into one. Readers trust no file: a
// pass stands only when it matches its digest, proved the step's claim version on an instance its
// run launched, and its evidence still meets the evidence contract.
type Store struct {
	dir string
	// probed, when set, runs on the clock probe file before its time is read; tests move the
	// storage clock away from the writer's with it.
	probed func(path string)
}

// At opens the ledger kept in dir.
func At(dir string) Store { return Store{dir: dir} }

// passFormat changes whenever the meaning of a pass file changes, so older passes stop standing.
const passFormat = "verilex-pass-1"

// pass is one recorded pass as the ledger keeps it.
type pass struct {
	Format     string            `json:"format"`
	Slot       string            `json:"slot"`
	Label      string            `json:"label"`
	Stamp      string            `json:"stamp"`
	Components map[string]string `json:"components"`
	// Claim is the claim version the pass proved; empty for a word without a claim.
	Claim string `json:"claim,omitempty"`
	// Run recorded the pass on the instance Owner launched; a pass stands only when they agree.
	Run         string `json:"run"`
	Owner       string `json:"owner"`
	Recorded    string `json:"recorded"`
	Observation any    `json:"observation"`
	// Evidence is the pass's copy of its step's evidence, relative to the ledger, and Result the
	// sha256 of the word's result object (its stdout) there.
	Evidence string `json:"evidence"`
	Result   string `json:"result"`
}

var (
	passName    = regexp.MustCompile(`^([0-9a-f]{64})\.([0-9a-f]{64})\.json$`)
	evidenceRef = regexp.MustCompile(`^evidence/[0-9a-f]{32}$`)
)

// damaged is why a pass whose file does not match its digest or format never stands. Nothing in
// such a file can be trusted, not even the run it names.
const damaged = "a pass on record is damaged: its content does not match its digest"

// Reuse returns a pass for every step when a pass stands for each step's stamp and claim version
// (claims[i] is the one step i's word proves) and no step is held (its word provisional or
// drift-suspect). Otherwise it returns why the chain has to run.
// Reuse is all or nothing: every run launches a fresh instance, so a step that runs needs the
// effects of every step before it, and every step after it depends on its new result.
// An unreadable ledger also returns an error: a run can fall back to live execution, while a
// read-only caller can distinguish missing proof from an inability to read the proof.
func (s Store) Reuse(labels, claims []string, stamps []stamp.Stamp) ([]Entry, string, error) {
	now := time.Now()
	entries := make([]Entry, 0, len(stamps))
	for i, st := range stamps {
		if st.Unclear != "" {
			return nil, labels[i] + ": " + st.Unclear, nil
		}
		entry, why, err := s.find(st, claims[i], now)
		if err != nil {
			err = fmt.Errorf("the ledger is unreadable: %w", err)
			return nil, err.Error(), err
		}
		if why == "" {
			why = st.Hold
		}
		if why != "" {
			return nil, labels[i] + ": " + why, nil
		}
		entries = append(entries, entry)
	}
	return entries, "", nil
}

// Proof returns the newest pass that stands for a step stamped st which proved claim, or why
// none does. It is the same check Reuse makes for one step, including age-out. A step whose
// word is provisional or drift-suspect still needs its caller to honor st.Hold: Proof does not.
func (s Store) Proof(st stamp.Stamp, claim string) (Entry, string, error) {
	if st.Unclear != "" {
		return Entry{}, st.Unclear, nil
	}
	return s.find(st, claim, time.Now())
}

// find returns the newest pass that stands for a step stamped st, or why none does: why the
// newest pass with that stamp cannot stand, or else what changed since the newest pass in the
// step's slot.
func (s Store) find(st stamp.Stamp, claim string, now time.Time) (Entry, string, error) {
	dir := filepath.Join(s.dir, "passes", st.Slot)
	files, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return Entry{}, "no green result on record", nil
	}
	if err != nil {
		return Entry{}, "", err
	}
	// A pass file names its stamp, so only passes for this step's stamp are read, unless none of
	// them stands and the newest other pass has to say what changed.
	matching, others := [][]string{}, [][]string{}
	for _, file := range files {
		if m := passName.FindStringSubmatch(file.Name()); m != nil && m[1] == st.Digest {
			matching = append(matching, m)
		} else if m != nil {
			others = append(others, m)
		}
	}
	var stands, refused, other *pass
	why := ""
	for _, m := range matching {
		p, reason := s.open(dir, m)
		if reason == "" {
			reason = s.stands(p, st, claim, now)
		}
		switch {
		case reason == "" && newer(p, stands):
			stands = &p
		case reason != "" && (refused == nil || newer(p, refused)):
			refused, why = &p, reason
		}
	}
	if stands == nil && refused == nil {
		for _, m := range others {
			if p, damage := s.open(dir, m); damage == "" && newer(p, other) {
				other = &p
			}
		}
	}
	switch {
	case stands != nil:
		return stands.entry(s.dir), "", nil
	case refused != nil:
		return Entry{}, why, nil
	case other != nil:
		if diff := stamp.Diff(other.Components, st.Components); diff != "" {
			return Entry{}, diff, nil
		}
		return Entry{}, "stamp changed", nil
	}
	return Entry{}, "no green result on record", nil
}

// open reads the pass file whose name matched passName as m, and says why it is damaged: its
// content does not hash to the digest in its name, is no pass, or names another stamp or slot.
func (s Store) open(dir string, m []string) (pass, string) {
	data, err := os.ReadFile(filepath.Join(dir, m[0]))
	if errors.Is(err, fs.ErrNotExist) {
		// Recording dropped an expired pass after the directory was listed.
		return pass{}, "no green result on record"
	}
	if err != nil {
		return pass{}, damaged
	}
	return parse(data, filepath.Base(dir), m)
}

// parse reads the content of a pass file in slot whose name matched passName as m, and says why
// it is damaged.
func parse(data []byte, slot string, m []string) (pass, string) {
	var p pass
	if digest(data) != m[2] || json.Unmarshal(data, &p) != nil || p.Format != passFormat || p.Stamp != m[1] || p.Slot != slot || !evidenceRef.MatchString(p.Evidence) {
		return pass{}, damaged
	}
	return p, ""
}

// stands says why an intact pass with the step's stamp cannot stand for it, or "" when it can:
// it proved the step's claim version, its run launched the instance it drove, its evidence still
// exists unchanged and meets the evidence contract, and it is younger than MaxAge.
func (s Store) stands(p pass, st stamp.Stamp, claim string, now time.Time) string {
	from := "the pass from run " + p.Run
	switch {
	case p.Claim != claim:
		return fmt.Sprintf("%s proved %s, not %s; a pass proves only the claim version it ran against", from, orNone(p.Claim), orNone(claim))
	case p.Owner != p.Run:
		return fmt.Sprintf("%s was recorded on an instance run %s launched; only a run that launched its own instance records a pass", from, orNone(p.Owner))
	}
	evidence := filepath.Join(s.dir, filepath.FromSlash(p.Evidence))
	stdout, err := os.ReadFile(filepath.Join(evidence, "stdout"))
	if err != nil {
		return "evidence from run " + p.Run + " is gone"
	}
	if digest(stdout) != p.Result {
		return "the evidence of " + from + " changed after it was recorded"
	}
	if why := backed(evidence, stdout); why != "" {
		return from + " misses the evidence contract: " + why
	}
	return fresh(p.Run, p.Recorded, now)
}

// backed says why a step's evidence does not show a pass the honesty rules accept: an exit code
// of 0 and a result object reporting pass with a second observation. It is "" when it does.
func backed(evidence string, stdout []byte) string {
	text, err := os.ReadFile(filepath.Join(evidence, "exit"))
	code, convErr := strconv.Atoi(strings.TrimSpace(string(text)))
	if err != nil || convErr != nil {
		return "no exit code on record"
	}
	if j := verdict.Judge(&code, stdout, 0); j.Verdict != verdict.Green {
		if j.Reason == nil {
			return "the result is " + string(j.Verdict)
		}
		return fmt.Sprint(j.Reason)
	}
	return ""
}

func (p pass) entry(dir string) Entry {
	return Entry{
		Label: p.Label, Verdict: verdict.Green, Stamp: p.Stamp, Components: p.Components, Claim: p.Claim,
		Run: p.Run, Owner: p.Owner, Evidence: filepath.Join(dir, filepath.FromSlash(p.Evidence)), Observation: p.Observation, Recorded: p.Recorded,
	}
}

// newer reports whether p was recorded after q, or q is nil.
func newer(p pass, q *pass) bool {
	if q == nil {
		return true
	}
	a, _ := time.Parse(time.RFC3339, p.Recorded)
	b, _ := time.Parse(time.RFC3339, q.Recorded)
	return a.After(b)
}

// Record adds a pass for each entry, keyed by the stamp slot it proved, and drops the expired
// passes. It refuses an entry that may not become a pass: one that is not
// green, has no stamp, was recorded on an instance its run did not launch, names a claim
// version its stamp does not hold, or whose evidence misses the evidence contract. Passes
// already recorded stay as they are: concurrent runs that prove one step record one pass each.
func (s Store) Record(entries map[string]Entry) error {
	slots := make([]string, 0, len(entries))
	for slot := range entries {
		slots = append(slots, slot)
	}
	slices.Sort(slots)
	errs := []error{}
	now := time.Now()
	for _, slot := range slots {
		if err := s.add(slot, entries[slot], now); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", entries[slot].Label, err))
		}
	}
	s.prune()
	return errors.Join(errs...)
}

func (s Store) add(slot string, e Entry, now time.Time) error {
	switch {
	case e.Verdict != verdict.Green:
		return errors.New("only a green result is a pass")
	case e.Stamp == "" || slot == "":
		return errors.New("a result without a stamp proves nothing")
	case e.Run == "" || e.Owner != e.Run:
		return fmt.Errorf("run %s drove an instance run %s launched; only a run that launched its own instance records a pass", orNone(e.Run), orNone(e.Owner))
	case !holds(e.Claim, e.Components["claim"]):
		return fmt.Errorf("claim %s is not the claim version its stamp holds", orNone(e.Claim))
	}
	stdout, err := os.ReadFile(filepath.Join(e.Evidence, "stdout"))
	if err != nil {
		return err
	}
	if why := backed(e.Evidence, stdout); why != "" {
		return errors.New("its evidence misses the evidence contract: " + why)
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return err
	}
	ref := "evidence/" + hex.EncodeToString(id[:])
	if err = copyTree(e.Evidence, filepath.Join(s.dir, filepath.FromSlash(ref))); err != nil {
		return err
	}
	p := pass{
		Format: passFormat, Slot: slot, Label: e.Label, Stamp: e.Stamp, Components: e.Components, Claim: e.Claim,
		Run: e.Run, Owner: e.Owner, Recorded: now.UTC().Format(time.RFC3339Nano), Observation: e.Observation, Evidence: ref, Result: digest(stdout),
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Join(s.dir, "passes", slot)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return publish(filepath.Join(dir, e.Stamp+"."+digest(data)+".json"), data)
}

// holds reports whether claim, <claim>@<version>, names the claim fingerprint a stamp holds;
// both are empty for a word without a claim.
func holds(claim, fingerprint string) bool {
	if claim == "" || fingerprint == "" {
		return claim == fingerprint
	}
	_, version, ok := strings.Cut(claim, "@")
	return ok && version != "" && strings.HasPrefix(fingerprint, version)
}

// strandedAge is how old a temporary entry must be before prune treats its writer as crashed.
const strandedAge = 24 * time.Hour

// stranded matches the temporary names verilex itself creates (os.CreateTemp and os.MkdirTemp
// with the prefixes below), so prune never removes another tool's .tmp- entries.
var stranded = regexp.MustCompile(`^\.tmp-(clock-)?[0-9]+$`)

// maxPass is the largest pass file prune reads, far above the 1.4 KB a pass takes: a damaged or
// planted larger file under a pass name is left unread and in place, so it costs no recording run.
const maxPass = 1 << 20

// prune drops every pass that is MaxAge old or older, in every slot, then its evidence, and the
// temporary entries a crashed writer left. A reader that listed a pass first finds it gone and
// runs the step live. It leaves damaged files in place.
//
// Age is judged on the storage's own clock, read from a file just created there, not on the
// writer's: a writer whose clock runs ahead must not drop passes other instances wrote that are
// still young. The age comes from each pass's record, never its file time, which a git checkout
// or a copy resets. A call costs a directory listing per slot and a read of every pass. When the
// storage clock cannot be read, nothing is pruned.
//
// Deletion stays inside the ledger's real directories: prune follows no symlink, not even when
// the ledger itself, evidence/ or passes/ is one. Passes are dropped only when both evidence/ and
// passes/ are real, since dropping a pass removes its evidence. Every listing, read and removal
// goes through a directory prune opened and checked once, so a co-writer that swaps one for a
// symlink mid-way cannot steer it outside.
func (s Store) prune() {
	root := openReal(os.Lstat, os.OpenRoot, s.dir)
	if root == nil {
		return
	}
	defer root.Close()
	now, ok := s.storageNow(root)
	if !ok {
		return
	}
	sweep(root, now)
	evidence := openReal(root.Lstat, root.OpenRoot, "evidence")
	if evidence == nil {
		return
	}
	defer evidence.Close()
	sweep(evidence, now)
	passes := openReal(root.Lstat, root.OpenRoot, "passes")
	if passes == nil {
		return
	}
	defer passes.Close()
	for _, slot := range list(passes) {
		if !slot.IsDir() {
			continue
		}
		if dir := openReal(passes.Lstat, passes.OpenRoot, slot.Name()); dir != nil {
			s.pruneSlot(dir, evidence, slot.Name(), now)
			dir.Close()
		}
	}
}

func (s Store) pruneSlot(dir, evidence *os.Root, slot string, now time.Time) {
	sweep(dir, now)
	for _, file := range list(dir) {
		m := passName.FindStringSubmatch(file.Name())
		if m == nil {
			continue
		}
		info, err := dir.Lstat(m[0])
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxPass {
			continue
		}
		p, damage := parse(readCapped(dir, m[0]), slot, m)
		at, err := time.Parse(time.RFC3339, p.Recorded)
		if damage != "" || err != nil || now.Sub(at) < MaxAge {
			continue
		}
		if dir.Remove(m[0]) == nil {
			evidence.RemoveAll(strings.TrimPrefix(p.Evidence, "evidence/"))
		}
	}
}

// sweep removes the temporary entries verilex created in dir that are strandedAge old or older.
func sweep(dir *os.Root, now time.Time) {
	for _, file := range list(dir) {
		if !stranded.MatchString(file.Name()) {
			continue
		}
		if info, err := dir.Lstat(file.Name()); err == nil && now.Sub(info.ModTime()) >= strandedAge {
			dir.RemoveAll(file.Name())
		}
	}
}

// openReal opens name as a root when it is a directory itself, not a symlink to one, and is still
// the directory it checked once open, even if a co-writer swapped the path in between.
func openReal(lstat func(string) (fs.FileInfo, error), open func(string) (*os.Root, error), name string) *os.Root {
	checked, err := lstat(name)
	if err != nil || !checked.IsDir() {
		return nil
	}
	r, err := open(name)
	if err != nil {
		return nil
	}
	if opened, err := r.Stat("."); err != nil || !os.SameFile(checked, opened) {
		r.Close()
		return nil
	}
	return r
}

// list returns the entries of dir, or none when it cannot be read.
func list(dir *os.Root) []fs.DirEntry {
	f, err := dir.Open(".")
	if err != nil {
		return nil
	}
	defer f.Close()
	files, _ := f.ReadDir(-1)
	return files
}

// readCapped reads name in dir, or returns nil when it cannot be read or holds more than maxPass.
func readCapped(dir *os.Root, name string) []byte {
	f, err := dir.Open(name)
	if err != nil {
		return nil
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxPass+1))
	if err != nil || len(data) > maxPass {
		return nil
	}
	return data
}

// storageNow reads the clock of the storage that holds the ledger, from the time of a file it
// creates in root. The probe has a name of stranded's shape, so a later sweep removes one a
// killed run stranded.
func (s Store) storageNow(root *os.Root) (time.Time, bool) {
	var n [4]byte
	if _, err := rand.Read(n[:]); err != nil {
		return time.Time{}, false
	}
	name := ".tmp-clock-" + strconv.FormatUint(uint64(binary.BigEndian.Uint32(n[:])), 10)
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return time.Time{}, false
	}
	defer root.Remove(name)
	defer f.Close()
	if s.probed != nil {
		s.probed(filepath.Join(s.dir, name))
	}
	info, err := f.Stat()
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

// copyTree gives dest its own copy of the regular files and directories under src, linking
// files where the file system allows, so a local ledger costs no extra space. dest appears whole
// or not at all.
func copyTree(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dest), ".tmp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		target := filepath.Join(tmp, rel)
		switch {
		case d.IsDir():
			return os.Mkdir(target, 0700)
		case !d.Type().IsRegular():
			return nil
		case os.Link(path, target) == nil:
			return nil
		}
		return copyFile(path, target)
	})
	if err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// publish writes data to path through a temporary file, so readers see all of it or nothing.
func publish(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
