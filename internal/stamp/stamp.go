// Package stamp owns proof stamps: fingerprints of everything a word's result depended on.
package stamp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
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
	// verilex, frame, shared, word, "input <path>", "env <NAME>" and upstream.
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
	common := map[string]string{"verilex": format}
	unclear := ""
	if exe, err := os.Executable(); err != nil {
		unclear = "verilex cannot locate its own executable"
	} else if common["verilex"], err = h.path(exe); err != nil {
		unclear = "verilex executable: " + err.Error()
	}
	if digest, err := h.paths(project.Dir(), "config.yaml", "frame"); err != nil {
		unclear = "frame: " + err.Error()
	} else {
		common["frame"] = digest
	}
	if digest, err := h.shared(filepath.Join(project.Dir(), "words")); err != nil {
		unclear = "shared word files: " + err.Error()
	} else {
		common["shared"] = digest
	}
	stamps := make([]Stamp, len(steps))
	holds := map[string]string{}
	prefix := [][]string{}
	upstream := ""
	for i, step := range steps {
		prefix = append(prefix, append([]string{step.Word.Name}, step.Argv...))
		slot, _ := json.Marshal(prefix)
		s := Stamp{Slot: digestOf(string(slot)), Components: map[string]string{"upstream": upstream}}
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
			s.Digest = digestOf(s.Slot + "\n" + lines(s.Components))
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

type hasher struct{ cache map[string]result }

func (h hasher) word(word dictionary.Word, root string, components map[string]string) string {
	if !word.InputsDeclared {
		return "declares no inputs"
	}
	var err error
	if components["word"], err = h.path(word.Path); err != nil {
		return "word files: " + err.Error()
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
	for _, name := range word.Env {
		value, set := os.LookupEnv(name)
		components["env "+name] = digestOf(fmt.Sprintf("%t\x00%s", set, value))
	}
	return ""
}

// paths fingerprints named entries of a directory together.
func (h hasher) paths(dir string, names ...string) (string, error) {
	parts := []string{}
	for _, name := range names {
		digest, err := h.path(filepath.Join(dir, name))
		if err != nil {
			return "", err
		}
		parts = append(parts, name+"="+digest)
	}
	return digestOf(strings.Join(parts, "\n")), nil
}

// shared fingerprints everything in the words directory outside word directories, such as
// helpers that words import.
func (h hasher) shared(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	names := []string{}
	for _, entry := range entries {
		if _, err := os.Stat(filepath.Join(dir, entry.Name(), "word.md")); err == nil {
			continue
		}
		names = append(names, entry.Name())
	}
	return h.paths(dir, names...)
}

// path fingerprints a file or a whole directory tree: names, permissions and contents.
func (h hasher) path(path string) (string, error) {
	if cached, ok := h.cache[path]; ok {
		return cached.digest, cached.err
	}
	digest, err := tree(path)
	h.cache[path] = result{digest, err}
	return digest, err
}

func tree(root string) (string, error) {
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
	return digestOf(lines.String()), nil
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

func digestOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
