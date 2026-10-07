// Package fingerprint owns how verilex fingerprints files, and the binding: what every word of a
// project runs with besides its own directory. Proof stamps and admission drift both read the
// binding from here, so a file a word runs with is never outside what admission judged.
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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
	lines := make([]string, len(part.Names))
	for i, name := range part.Names {
		digest, err := tree(filepath.Join(part.Dir, name), true)
		if err != nil {
			return "", err
		}
		lines[i] = name + "=" + digest
	}
	return Of(strings.Join(lines, "\n")), nil
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
func Tree(root string) (string, error) { return tree(root, false) }

func tree(root string, skipCaches bool) (string, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%s is missing", root)
		}
		return "", err
	}
	if !info.IsDir() {
		return file(root, info)
	}
	var lines strings.Builder
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
			fmt.Fprintf(&lines, "%q dir %o\n", rel, info.Mode().Perm())
			return nil
		}
		digest, err := file(path, info)
		if err != nil {
			return err
		}
		fmt.Fprintf(&lines, "%q %s\n", rel, digest)
		return nil
	})
	if err != nil {
		return "", err
	}
	return Of(lines.String()), nil
}

func file(path string, info fs.FileInfo) (string, error) {
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := sha256.New()
	fmt.Fprintf(sum, "file %o\n", info.Mode().Perm())
	if _, err = io.Copy(sum, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// Of is the sha256 of a text, in hex.
func Of(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
