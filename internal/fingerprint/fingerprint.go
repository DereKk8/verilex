// Package fingerprint owns how verilex fingerprints files, and the binding: what every word of a
// project runs with besides its own directory. Proof stamps and admission drift both read the
// binding from here, so a file a word runs with is never outside what admission judged.
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/DereKk8/verilex/internal/dictionary"
)

// tickGuard is the bounded wait used before running an external process when a witnessed file was
// modified within the last 20ms, so an in-place edit and restore while the process runs cannot
// land in the same coarse filesystem timestamp tick.
const tickGuard = 20 * time.Millisecond

// FileWitness is the kernel identity of one file, directory or symlink at the moment verilex read
// it. No unprivileged process can roll back ctime or restore a replaced inode with its earlier
// identity, so comparing witnesses before and after an executed process closes the edit-and-restore
// window even when bytes and mtime match.
type FileWitness struct {
	Mode   fs.FileMode
	Size   int64
	Mtime  int64
	Ctime  int64
	Ino    uint64
	Dev    uint64
	HasSys bool
	Target string
	Absent bool
}

// Witness maps each witnessed path to its FileWitness at read time, and tracks the newest ctime
// observed so WaitTick waits only when a file was just touched.
type Witness struct {
	entries  map[string]FileWitness
	maxCtime int64
}

// NewWitness returns an empty Witness.
func NewWitness() Witness {
	return Witness{entries: map[string]FileWitness{}}
}

// Add records the identity of one opened file or directory.
func (w *Witness) Add(key string, info fs.FileInfo) {
	if w.entries == nil {
		w.entries = map[string]FileWitness{}
	}
	ctime, ino, dev, ok := statIdentity(info)
	fw := FileWitness{
		Mode:   info.Mode(),
		Size:   info.Size(),
		Mtime:  info.ModTime().UnixNano(),
		Ctime:  ctime,
		Ino:    ino,
		Dev:    dev,
		HasSys: ok,
	}
	w.entries[key] = fw
	if ok && ctime > w.maxCtime {
		w.maxCtime = ctime
	}
}

// AddSymlink records a symlink's own lstat identity and link target under a distinct key.
func (w *Witness) AddSymlink(key string, info fs.FileInfo, target string) {
	if w.entries == nil {
		w.entries = map[string]FileWitness{}
	}
	ctime, ino, dev, ok := statIdentity(info)
	fw := FileWitness{
		Mode:   info.Mode(),
		Size:   info.Size(),
		Mtime:  info.ModTime().UnixNano(),
		Ctime:  ctime,
		Ino:    ino,
		Dev:    dev,
		HasSys: ok,
		Target: target,
	}
	w.entries[key+"\x00link"] = fw
	if ok && ctime > w.maxCtime {
		w.maxCtime = ctime
	}
}

// AddAbsent records that a path did not exist when read.
func (w *Witness) AddAbsent(key string) {
	if w.entries == nil {
		w.entries = map[string]FileWitness{}
	}
	w.entries[key] = FileWitness{Absent: true}
}

// Merge adds every entry of other into w.
func (w *Witness) Merge(other Witness) {
	if len(other.entries) == 0 {
		return
	}
	if w.entries == nil {
		w.entries = make(map[string]FileWitness, len(other.entries))
	}
	for k, v := range other.entries {
		w.entries[k] = v
	}
	if other.maxCtime > w.maxCtime {
		w.maxCtime = other.maxCtime
	}
}

// Equal reports whether w and other witnessed the exact same paths with identical kernel state.
func (w Witness) Equal(other Witness) bool {
	if len(w.entries) != len(other.entries) {
		return false
	}
	for k, v := range w.entries {
		if ov, ok := other.entries[k]; !ok || ov != v {
			return false
		}
	}
	return true
}

// MaxCtime returns the newest ctime (in Unix nanoseconds) among witnessed entries, or 0 if none.
func (w Witness) MaxCtime() int64 { return w.maxCtime }

// WaitTick waits out the filesystem timestamp tick window when maxCtime is within tickGuard of
// now, so any in-place write while an external process runs lands on a strictly newer tick.
func WaitTick(maxCtime int64) {
	if maxCtime <= 0 {
		return
	}
	age := time.Duration(time.Now().UnixNano() - maxCtime)
	if age >= tickGuard {
		return
	}
	wait := tickGuard - age
	if wait > tickGuard {
		wait = tickGuard
	}
	time.Sleep(wait)
}

// CapturedEntry is one directory or regular file captured during a single-read walk so callers can
// materialize an exact private snapshot without reading disk again.
type CapturedEntry struct {
	Rel   string
	IsDir bool
	Mode  fs.FileMode
	Data  []byte
}

// MaterializeEntries writes captured entries under dest with private owner permissions (0700 for
// directories and executable files, 0600 for regular files).
func MaterializeEntries(dest string, entries []CapturedEntry) error {
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	for _, e := range entries {
		target := dest
		if e.Rel != "" && e.Rel != "." {
			target = filepath.Join(dest, filepath.FromSlash(e.Rel))
		}
		if e.IsDir {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		mode := fs.FileMode(0o600)
		if e.Mode&0o100 != 0 {
			mode = 0o700
		}
		if err := os.WriteFile(target, e.Data, mode); err != nil {
			return err
		}
	}
	return nil
}

// Part is one part of a project's binding: named entries of one directory.
type Part struct {
	// Name is the part's proof-stamp component: "frame" or "shared".
	Name string
	// Label is what a person calls the part.
	Label string
	Dir   string
	Names []string
}

// Binding lists the parts every word of a project runs with besides its own directory: the
// frame steps and the shared word files (every entry of the words directory that is no word's
// directory, such as a helper the words import). config.yaml is not part of it: it names the
// product, and one admitted vocabulary serves several products. Interpreter caches are not part
// of it either.
func Binding(p dictionary.Project) ([]Part, error) {
	frame := Part{Name: "frame", Label: "the frame", Dir: p.Dir(), Names: []string{"frame"}}
	shared := Part{Name: "shared", Label: "the shared word files", Dir: filepath.Join(p.Dir(), "words")}
	entries, err := os.ReadDir(shared.Dir)
	for _, entry := range entries {
		if _, err := os.Stat(filepath.Join(shared.Dir, entry.Name(), "word.md")); err != nil && !Cache(entry.Name()) {
			shared.Names = append(shared.Names, entry.Name())
		}
	}
	if err != nil {
		err = fmt.Errorf("%s: %w", shared.Label, err)
	}
	return []Part{frame, shared}, err
}

// Paths lists the part's entries as absolute paths.
func (part Part) Paths() []string {
	paths := make([]string, len(part.Names))
	for i, name := range part.Names {
		paths[i] = filepath.Join(part.Dir, name)
	}
	return paths
}

// Digest fingerprints the part's entries together, each like Tree but without interpreter caches.
func (part Part) Digest() (string, error) {
	read, err := readPart(part, false)
	if err != nil {
		return "", err
	}
	return read.digest, nil
}

// Matches reports whether stored fingerprints the part's entries as they are now (see Match).
func (part Part) Matches(stored string) (bool, error) {
	read, err := readPart(part, false)
	if err != nil {
		return false, err
	}
	return read.Matches(stored), nil
}

// PartRead is one binding part read once from disk, carrying its canonical digest, every accepted
// permission-rule digest, its captured entries and its stat witness.
type PartRead struct {
	Part     Part
	digest   string
	accepted []string
	roots    []partRoot
	witness  Witness
}

type partRoot struct {
	name    string
	isDir   bool
	entries []CapturedEntry
}

// Digest returns the part's canonical (RecordedPerm) digest.
func (r PartRead) Digest() string { return r.digest }

// Matches reports whether stored matches the bytes read into r under any accepted Perm.
func (r PartRead) Matches(stored string) bool { return slices.Contains(r.accepted, stored) }

// Witness returns the stat witness of every entry read into r.
func (r PartRead) Witness() Witness { return r.witness }

// BindingRead is a project's complete binding read once from disk. Both matching against a
// recorded binding and recording a new binding come from the same read.
type BindingRead struct {
	parts   []PartRead
	witness Witness
}

// ReadBinding reads every part of p's binding once from disk, capturing bytes, digests under
// every accepted Perm, and stat witnesses.
func ReadBinding(p dictionary.Project) (*BindingRead, error) {
	parts, err := Binding(p)
	if err != nil {
		return nil, err
	}
	out := &BindingRead{parts: make([]PartRead, len(parts)), witness: NewWitness()}
	for i, part := range parts {
		pr, err := readPart(part, true)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", part.Label, err)
		}
		out.parts[i] = pr
		out.witness.Merge(pr.witness)
	}
	return out, nil
}

func readPart(part Part, capture bool) (PartRead, error) {
	witness := NewWitness()
	if part.Name == "shared" {
		if info, err := os.Lstat(part.Dir); err == nil {
			witness.Add(part.Dir, info)
		}
	}
	perPermLines := make([][]string, len(accepted))
	for k := range accepted {
		perPermLines[k] = make([]string, len(part.Names))
	}
	var roots []partRoot
	for i, name := range part.Names {
		entryPath := filepath.Join(part.Dir, name)
		res, err := readTree(entryPath, true, capture, "", accepted)
		if err != nil {
			return PartRead{}, err
		}
		witness.Merge(res.witness)
		for k := range accepted {
			perPermLines[k][i] = name + "=" + res.digests[k]
		}
		if capture {
			roots = append(roots, partRoot{name: name, isDir: res.isDir, entries: res.entries})
		}
	}
	digests := make([]string, len(accepted))
	for k := range accepted {
		digests[k] = Of(strings.Join(perPermLines[k], "\n"))
	}
	return PartRead{
		Part:     part,
		digest:   digests[0],
		accepted: digests,
		roots:    roots,
		witness:  witness,
	}, nil
}

// Parts returns each read binding part.
func (b *BindingRead) Parts() []PartRead { return b.parts }

// Digests returns a fresh map from binding part name to its canonical digest.
func (b *BindingRead) Digests() map[string]string {
	out := make(map[string]string, len(b.parts))
	for _, part := range b.parts {
		out[part.Part.Name] = part.digest
	}
	return out
}

// Matches reports whether stored matches every part read into b under any accepted Perm, with no
// extra parts.
func (b *BindingRead) Matches(stored map[string]string) bool {
	if len(stored) != len(b.parts) {
		return false
	}
	for _, part := range b.parts {
		if !part.Matches(stored[part.Part.Name]) {
			return false
		}
	}
	return true
}

// Witness returns the combined stat witness of every binding part read into b.
func (b *BindingRead) Witness() Witness { return b.witness }

// MaterializeFrame writes the captured frame files into destDir.
func (b *BindingRead) MaterializeFrame(destDir string) error {
	for _, part := range b.parts {
		if part.Part.Name != "frame" {
			continue
		}
		for _, root := range part.roots {
			if err := MaterializeEntries(destDir, root.entries); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("binding has no frame part")
}

// MaterializeShared writes the captured shared word files into destDir.
func (b *BindingRead) MaterializeShared(destDir string) error {
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return err
	}
	for _, part := range b.parts {
		if part.Part.Name != "shared" {
			continue
		}
		for _, root := range part.roots {
			target := filepath.Join(destDir, root.name)
			if !root.isDir {
				if len(root.entries) == 1 {
					e := root.entries[0]
					mode := fs.FileMode(0o600)
					if e.Mode&0o100 != 0 {
						mode = 0o700
					}
					if err := os.WriteFile(target, e.Data, mode); err != nil {
						return err
					}
				}
				continue
			}
			if err := MaterializeEntries(target, root.entries); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}

// Digests fingerprints each part of a project's binding, by part name.
func Digests(p dictionary.Project) (map[string]string, error) {
	b, err := ReadBinding(p)
	if err != nil {
		return nil, err
	}
	return b.Digests(), nil
}

// DigestsMatch reports whether stored fingerprints each part of a project's binding as it is now,
// and no other part (see Match).
func DigestsMatch(p dictionary.Project, stored map[string]string) (bool, error) {
	b, err := ReadBinding(p)
	if err != nil {
		return false, err
	}
	return b.Matches(stored), nil
}

// Cache reports whether a directory name holds an interpreter's cache, such as Python's
// __pycache__: files an interpreter derives from the code it runs, which differ between checkouts.
func Cache(name string) bool { return name == "__pycache__" }

// Tree fingerprints a file or a whole directory tree: names, permissions and contents.
func Tree(root string) (string, error) {
	res, err := readTree(root, false, false, "", []Perm{RecordedPerm})
	if err != nil {
		return "", err
	}
	return res.digests[0], nil
}

// TreeWithWitness fingerprints a file or directory tree in a single open per file and returns both
// the digest and the stat witness of every file, directory, symlink and intermediate directory
// strictly between stopAt and root.
func TreeWithWitness(root, stopAt string) (string, Witness, error) {
	res, err := readTree(root, false, false, stopAt, []Perm{RecordedPerm})
	if err != nil {
		return "", Witness{}, err
	}
	return res.digests[0], res.witness, nil
}

// ReadFile reads one regular file (following symlinks) in a single open, returning its bytes and
// stat witness (including intermediate directories strictly between stopAt and path when stopAt
// is non-empty).
func ReadFile(path, stopAt string) ([]byte, Witness, error) {
	w := NewWitness()
	linfo, err := os.Lstat(path)
	if err != nil {
		return nil, Witness{}, err
	}
	if err := addIntermediateDirs(&w, path, stopAt); err != nil {
		return nil, Witness{}, err
	}
	if linfo.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return nil, Witness{}, err
		}
		w.AddSymlink(path, linfo, target)
		sinfo, err := os.Stat(path)
		if err != nil {
			return nil, Witness{}, err
		}
		if !sinfo.Mode().IsRegular() {
			return nil, Witness{}, fmt.Errorf("%s is not a regular file", path)
		}
	} else if !linfo.Mode().IsRegular() {
		return nil, Witness{}, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, Witness{}, err
	}
	defer f.Close()
	finfo, err := f.Stat()
	if err != nil {
		return nil, Witness{}, err
	}
	if !finfo.Mode().IsRegular() {
		return nil, Witness{}, fmt.Errorf("%s is not a regular file", path)
	}
	w.Add(path, finfo)
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, Witness{}, err
	}
	return data, w, nil
}

type treeResult struct {
	digests []string
	witness Witness
	isDir   bool
	entries []CapturedEntry
}

func readTree(root string, skipCaches, capture bool, stopAt string, perms []Perm) (treeResult, error) {
	linfo, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return treeResult{}, fmt.Errorf("%s is missing", root)
		}
		return treeResult{}, err
	}
	witness := NewWitness()
	if err := addIntermediateDirs(&witness, root, stopAt); err != nil {
		return treeResult{}, err
	}
	isDir := linfo.IsDir()
	if linfo.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(root)
		if err != nil {
			return treeResult{}, err
		}
		witness.AddSymlink(root, linfo, target)
		sinfo, err := os.Stat(root)
		if err != nil {
			if os.IsNotExist(err) {
				return treeResult{}, fmt.Errorf("%s is missing", root)
			}
			return treeResult{}, err
		}
		if sinfo.IsDir() {
			return treeResult{}, fmt.Errorf("%s links to a directory", root)
		}
		if !sinfo.Mode().IsRegular() {
			return treeResult{}, fmt.Errorf("%s is not a regular file", root)
		}
		isDir = false
	} else if !isDir && !linfo.Mode().IsRegular() {
		return treeResult{}, fmt.Errorf("%s is not a regular file", root)
	}
	if !isDir {
		digests, finfo, data, err := readSingleFile(root, capture, perms)
		if err != nil {
			return treeResult{}, err
		}
		witness.Add(root, finfo)
		var entries []CapturedEntry
		if capture {
			entries = []CapturedEntry{{Rel: ".", Mode: finfo.Mode().Perm(), Data: data}}
		}
		return treeResult{digests: digests, witness: witness, isDir: false, entries: entries}, nil
	}
	lines := make([]strings.Builder, len(perms))
	var entries []CapturedEntry
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			li, err := entry.Info()
			if err != nil {
				return err
			}
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			witness.AddSymlink(path, li, target)
			si, err := os.Stat(path)
			if err != nil {
				return err
			}
			if si.IsDir() {
				if skipCaches && Cache(entry.Name()) {
					return filepath.SkipDir
				}
				return fmt.Errorf("%s links to a directory", path)
			}
			if !si.Mode().IsRegular() {
				return fmt.Errorf("%s is not a regular file", path)
			}
		} else if entry.IsDir() {
			if skipCaches && Cache(entry.Name()) {
				return filepath.SkipDir
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			witness.Add(path, info)
			for k, perm := range perms {
				fmt.Fprintf(&lines[k], "%q dir %o\n", rel, perm(info))
			}
			if capture && rel != "." {
				entries = append(entries, CapturedEntry{Rel: filepath.ToSlash(rel), IsDir: true, Mode: info.Mode().Perm()})
			}
			return nil
		} else if !entry.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		digests, finfo, data, err := readSingleFile(path, capture, perms)
		if err != nil {
			return err
		}
		witness.Add(path, finfo)
		for k := range perms {
			fmt.Fprintf(&lines[k], "%q %s\n", rel, digests[k])
		}
		if capture {
			entries = append(entries, CapturedEntry{Rel: filepath.ToSlash(rel), Mode: finfo.Mode().Perm(), Data: data})
		}
		return nil
	})
	if err != nil {
		return treeResult{}, err
	}
	digests := make([]string, len(perms))
	for k := range perms {
		digests[k] = Of(lines[k].String())
	}
	return treeResult{digests: digests, witness: witness, isDir: true, entries: entries}, nil
}

func readSingleFile(path string, capture bool, perms []Perm) ([]string, fs.FileInfo, []byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, nil, fmt.Errorf("%s is not a regular file", path)
	}
	digests := make([]string, len(perms))
	if capture {
		data, err := io.ReadAll(f)
		if err != nil {
			return nil, nil, nil, err
		}
		for k, perm := range perms {
			digests[k] = FileDigest(perm(info), data)
		}
		return digests, info, data, nil
	}
	hashes := make([]hash.Hash, len(perms))
	writers := make([]io.Writer, len(perms))
	for k, perm := range perms {
		h := sha256.New()
		fmt.Fprintf(h, "file %o\n", perm(info))
		hashes[k] = h
		writers[k] = h
	}
	if _, err = io.Copy(io.MultiWriter(writers...), f); err != nil {
		return nil, nil, nil, err
	}
	for k, h := range hashes {
		digests[k] = hex.EncodeToString(h.Sum(nil))
	}
	return digests, info, nil, nil
}

// FileDigest hashes one file's mode and contents in the same format as Tree.
func FileDigest(mode fs.FileMode, data []byte) string {
	sum := sha256.New()
	fmt.Fprintf(sum, "file %o\n", mode)
	sum.Write(data)
	return hex.EncodeToString(sum.Sum(nil))
}

func addIntermediateDirs(w *Witness, root, stopAt string) error {
	if stopAt == "" {
		return nil
	}
	stopClean := filepath.Clean(stopAt)
	verilexDir := filepath.Join(stopClean, ".verilex")
	cur := filepath.Dir(filepath.Clean(root))
	for {
		if cur == stopClean || cur == "." || cur == string(filepath.Separator) {
			break
		}
		rel, err := filepath.Rel(stopClean, cur)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			break
		}
		// Skip .verilex itself because onboarding locks .verilex/grouping.yaml.lock there.
		if cur != verilexDir {
			info, err := os.Lstat(cur)
			if err != nil {
				return err
			}
			if info.Mode()&fs.ModeSymlink != 0 {
				target, err := os.Readlink(cur)
				if err != nil {
					return err
				}
				w.AddSymlink(cur, info, target)
				sinfo, err := os.Stat(cur)
				if err != nil {
					return err
				}
				w.Add(cur, sinfo)
			} else {
				w.Add(cur, info)
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return nil
}

// RecordedPerm is the permission a fingerprint stores.
// Git stores a regular file as 0755 when the owner execute bit is set, and as
// 0644 otherwise. Group and other execute bits, and every write bit, are not
// stored. Git stores no directory mode, so a directory is always 0755, the
// mode already written for a canonical tree.
func RecordedPerm(info fs.FileInfo) fs.FileMode {
	if info.IsDir() {
		return 0o755
	}
	if info.Mode().Perm()&0o100 != 0 {
		return 0o755
	}
	return 0o644
}

// Perm is the permission a fingerprint records for a file or a directory.
type Perm func(fs.FileInfo) fs.FileMode

// accepted lists each Perm a stored fingerprint may have been made with, the current one first.
// fullPerm is how verilex recorded permissions before RecordedPerm, so a record it wrote on a
// checkout with bits git does not track, such as a group-writable one, still matches. A digest
// equal under fullPerm was made of the same contents and the same permission bits, so accepting
// it never hides a change.
var accepted = []Perm{RecordedPerm, fullPerm}

// Accepted returns the permission rules a stored fingerprint may have been made with, the
// canonical rule first.
func Accepted() []Perm { return slices.Clone(accepted) }

// fullPerm is every permission bit, as verilex recorded them before RecordedPerm.
func fullPerm(info fs.FileInfo) fs.FileMode { return info.Mode().Perm() }

// Match reports whether stored is the digest of the files as they are now, made with any
// accepted Perm. digest computes that digest with one Perm.
func Match(stored string, digest func(Perm) (string, error)) (bool, error) {
	for _, perm := range accepted {
		current, err := digest(perm)
		if err != nil || current == stored {
			return err == nil, err
		}
	}
	return false, nil
}

// Of is the sha256 of a text, in hex.
func Of(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
