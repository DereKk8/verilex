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

	"github.com/DereKk8/verilex/internal/dictionary"
)

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
	digests, err := part.digests([]Perm{RecordedPerm})
	if err != nil {
		return "", err
	}
	return digests[0], nil
}

// Matches reports whether stored fingerprints the part's entries as they are now (see Read).
func (part Part) Matches(stored string) (bool, error) {
	read, err := part.Read()
	return err == nil && read.Matches(stored), err
}

// Read fingerprints the part's entries in one read.
func (part Part) Read() (Read, error) {
	digests, err := part.digests(accepted)
	return Read{digests}, err
}

// digests fingerprints the part's entries in one read, once with each of perms.
func (part Part) digests(perms []Perm) ([]string, error) {
	lines := make([][]string, len(perms))
	for _, name := range part.Names {
		digests, err := tree(filepath.Join(part.Dir, name), true, perms)
		if err != nil {
			return nil, err
		}
		for i, digest := range digests {
			lines[i] = append(lines[i], name+"="+digest)
		}
	}
	digests := make([]string, len(perms))
	for i, entries := range lines {
		digests[i] = Of(strings.Join(entries, "\n"))
	}
	return digests, nil
}

// BindingRead is one read of a project's binding: each part's Read, by part name.
type BindingRead map[string]Read

// ReadBinding reads each part of a project's binding once.
func ReadBinding(p dictionary.Project) (BindingRead, error) {
	parts, err := Binding(p)
	if err != nil {
		return nil, err
	}
	read := BindingRead{}
	for _, part := range parts {
		if read[part.Name], err = part.Read(); err != nil {
			return nil, fmt.Errorf("%s: %w", part.Label, err)
		}
	}
	return read, nil
}

// Digests is the fingerprint of each part that verilex records now, by part name.
func (b BindingRead) Digests() map[string]string {
	digests := map[string]string{}
	for name, read := range b {
		digests[name] = read.Digest()
	}
	return digests
}

// Matches reports whether stored fingerprints each part of the read, and no other part (see Read).
func (b BindingRead) Matches(stored map[string]string) bool {
	if len(stored) != len(b) {
		return false
	}
	for name, read := range b {
		if !read.Matches(stored[name]) {
			return false
		}
	}
	return true
}

// Digests fingerprints each part of a project's binding, by part name.
func Digests(p dictionary.Project) (map[string]string, error) {
	parts, err := Binding(p)
	if err != nil {
		return nil, err
	}
	digests := map[string]string{}
	for _, part := range parts {
		if digests[part.Name], err = part.Digest(); err != nil {
			return nil, fmt.Errorf("%s: %w", part.Label, err)
		}
	}
	return digests, nil
}

// Cache reports whether a directory name holds an interpreter's cache, such as Python's
// __pycache__: files an interpreter derives from the code it runs, which differ between checkouts.
func Cache(name string) bool { return name == "__pycache__" }

// Tree fingerprints a file or a whole directory tree: names, permissions and contents.
func Tree(root string) (string, error) {
	digests, err := tree(root, false, []Perm{RecordedPerm})
	if err != nil {
		return "", err
	}
	return digests[0], nil
}

// tree fingerprints a file or a whole directory tree in one read, once with each of perms.
func tree(root string, skipCaches bool, perms []Perm) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s is missing", root)
		}
		return nil, err
	}
	if !info.IsDir() {
		return file(root, info, perms)
	}
	lines := make([]strings.Builder, len(perms))
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skipCaches && Cache(entry.Name()) {
				return filepath.SkipDir
			}
			if entry.Type()&fs.ModeSymlink != 0 {
				return fmt.Errorf("%s links to a directory", path)
			}
			for i, perm := range perms {
				fmt.Fprintf(&lines[i], "%q dir %o\n", rel, perm(info))
			}
			return nil
		}
		digests, err := file(path, info, perms)
		if err != nil {
			return err
		}
		for i, digest := range digests {
			fmt.Fprintf(&lines[i], "%q %s\n", rel, digest)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	digests := make([]string, len(perms))
	for i := range lines {
		digests[i] = Of(lines[i].String())
	}
	return digests, nil
}

// file fingerprints a regular file once with each of perms, from one open of it: every digest
// hashes the same contents, and the mode of the file those contents came from.
func file(path string, info fs.FileInfo, perms []Perm) ([]string, error) {
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if info, err = f.Stat(); err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	sums := make([]hash.Hash, len(perms))
	writers := make([]io.Writer, len(perms))
	for i, perm := range perms {
		sums[i] = sha256.New()
		fmt.Fprintf(sums[i], "file %o\n", perm(info))
		writers[i] = sums[i]
	}
	if _, err = io.Copy(io.MultiWriter(writers...), f); err != nil {
		return nil, err
	}
	digests := make([]string, len(perms))
	for i, sum := range sums {
		digests[i] = hex.EncodeToString(sum.Sum(nil))
	}
	return digests, nil
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

// fullPerm is every permission bit, as verilex recorded them before RecordedPerm.
func fullPerm(info fs.FileInfo) fs.FileMode { return info.Mode().Perm() }

// Read is one read of some files, fingerprinted with each accepted Perm. A record is matched
// against a Read and made from the same Read, never from two reads, so what verilex records is
// exactly what it matched: the same bytes and modes, or nothing.
type Read struct{ digests []string }

// Reading fingerprints one read of some files with each accepted Perm. digest hashes those files
// with one Perm; it must hash what was read, never read the files again.
func Reading(digest func(Perm) string) Read {
	read := Read{}
	for _, perm := range accepted {
		read.digests = append(read.digests, digest(perm))
	}
	return read
}

// Digest is the fingerprint verilex records now: the read under RecordedPerm.
func (r Read) Digest() string {
	if len(r.digests) == 0 {
		return ""
	}
	return r.digests[0]
}

// Matches reports whether stored fingerprints the read with any accepted Perm.
func (r Read) Matches(stored string) bool { return stored != "" && slices.Contains(r.digests, stored) }

// Of is the sha256 of a text, in hex.
func Of(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
