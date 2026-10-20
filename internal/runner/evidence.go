package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"unicode/utf8"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/verdict"
	"github.com/dlclark/regexp2"
)

// Each pattern matches the whole secret, because redact replaces what it matches: a private key
// block runs to its END line, or to the end of the file when that line is missing. A token starts
// where an identifier cannot go on: `\b` would miss one after `_` or a non-ASCII letter, as in
// MY_ghp_... or éghp_..., and an encoded byte such as %3D or \n ends in a letter or digit.
const tokenStart = `(?:(?<![A-Za-z0-9])|(?<=%[0-9A-Fa-f]{2})|(?<=\\[nrt]))`

var secretPatterns = []string{
	`-----BEGIN [A-Z ]*PRIVATE KEY-----(?:[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----|[\s\S]*)`,
	tokenStart + `gh[pousr]_[A-Za-z0-9]{36,}`,
	tokenStart + `github_pat_[A-Za-z0-9_]{22,}`,
	tokenStart + `AKIA[0-9A-Z]{16}(?![A-Za-z0-9])`,
	tokenStart + `xox[abprs]-[A-Za-z0-9-]{10,}`,
	tokenStart + `sk-ant-[A-Za-z0-9_-]{20,}`,
}

// redacted stands in for every secret verilex finds in evidence.
const redacted = "[redacted]"

func patterns(project dictionary.Project) ([]*regexp2.Regexp, error) {
	result := []*regexp2.Regexp{}
	for _, pattern := range secretPatterns {
		r, err := dictionary.CompilePattern(pattern)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return append(result, project.SecretPatterns...), nil
}

// scan redacts every secret pattern match in the names and files under dir and names, redacted,
// the first entry that held one. It runs before verilex reads anything from the evidence, so no
// secret it finds reaches a run record, the usage ledger or a command's output, and the evidence
// itself keeps none. A directory the word left closed is opened for the scan and closed again.
func scan(dir string, patterns []*regexp2.Regexp) (leak string, err error) {
	// An error names a path, and a path not yet renamed can hold a secret.
	defer func() {
		if err != nil {
			err = redactError(err, patterns)
		}
	}()
	renames := []string{}
	type opened struct {
		path string
		mode fs.FileMode
	}
	closed := []opened{}
	defer func() {
		for _, d := range slices.Backward(closed) {
			if restoreErr := os.Chmod(d.path, d.mode); err == nil {
				err = restoreErr
			}
		}
	}()
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != dir {
			name, err := redact([]byte(entry.Name()), patterns)
			if err != nil {
				return err
			}
			if name != nil {
				renames = append(renames, path)
				if leak == "" {
					leak = string(name)
				}
				if len(leak) > nameMax {
					leak = redacted
				}
			}
		}
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil || info.Mode().Perm()&0o500 == 0o500 {
				return err
			}
			closed = append(closed, opened{path, info.Mode()})
			return os.Chmod(path, info.Mode()|0o500)
		}
		found, err := scanFile(path, entry, patterns)
		if found && leak == "" {
			leak = entry.Name()
		}
		if found && errors.Is(err, fs.ErrPermission) {
			// Evidence another user owns, such as a container's root, cannot be rewritten: the
			// leak is still named, so the run stays inconclusive.
			return nil
		}
		return err
	})
	if err != nil {
		return leak, err
	}
	// The walk lists a directory before what it holds, so renaming in reverse never moves a path
	// still to be renamed.
	for _, path := range slices.Backward(renames) {
		name, err := redact([]byte(filepath.Base(path)), patterns)
		if err != nil {
			return leak, err
		}
		target, err := rename(path, string(name))
		if errors.Is(err, fs.ErrPermission) {
			// A name in a directory another user owns cannot change: the leak is still named.
			continue
		}
		if err != nil {
			return leak, err
		}
		for i, d := range closed {
			if rel, err := filepath.Rel(path, d.path); err == nil && filepath.IsLocal(rel) {
				closed[i].path = filepath.Join(target, rel)
			}
		}
	}
	return leak, nil
}

// scanFile redacts the secrets in one file the walk found and reports whether it held any. A
// link stays a link unless its target path or content holds a secret: then it becomes a file
// holding only the marker, because the target lies outside what the word wrote and none of it
// is copied into the evidence. A link verilex cannot follow holds nothing it could read.
func scanFile(path string, entry fs.DirEntry, patterns []*regexp2.Regexp) (bool, error) {
	link := entry.Type()&fs.ModeSymlink != 0
	if link {
		target, err := os.Readlink(path)
		if err != nil {
			return false, err
		}
		named, err := redact([]byte(target), patterns)
		if err != nil || named != nil {
			return named != nil, replaceLink(path, err)
		}
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		if link {
			return false, nil
		}
		return false, err
	}
	var data []byte
	read := func() (err error) {
		data, err = os.ReadFile(path)
		return err
	}
	if link {
		if read() != nil {
			return false, nil
		}
	} else if err = withBits(path, 0o400, read); err != nil {
		return false, err
	}
	clean, err := redact(data, patterns)
	if err != nil || clean == nil {
		return false, err
	}
	if link {
		return true, replaceLink(path, nil)
	}
	return true, replace(path, clean, info.Mode().Perm())
}

// replaceLink turns a link into a file that holds only the marker, unless err is already set.
func replaceLink(path string, err error) error {
	if err != nil {
		return err
	}
	return replace(path, []byte(redacted), 0o600)
}

// nameMax is the longest redacted name kept: NAME_MAX, less room for a numbered suffix.
const nameMax = 255 - len("-9999")

// rename gives the entry at path a new name in its directory, numbered when the name is taken,
// and returns its new path. A redacted name can outgrow the name it replaces; past nameMax the
// marker alone stands in for it.
func rename(path, name string) (string, error) {
	if len(name) > nameMax {
		name = redacted
	}
	target := filepath.Join(filepath.Dir(path), name)
	for n := 2; ; n++ {
		_, err := os.Lstat(target)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return path, err
		}
		target = filepath.Join(filepath.Dir(path), fmt.Sprintf("%s-%d", name, n))
	}
	return target, withBits(filepath.Dir(path), 0o300, func() error { return os.Rename(path, target) })
}

// redactJudgment redacts the values a word reported, judged from stdout as written, and replaces
// the whole of stdout, redacted by the scan, when decoding it still shows a secret, as with a
// JSON-escaped token whose place in the file is unknown. It reports whether that happened.
func redactJudgment(j *verdict.Judgment, stdout []byte, evidence string, patterns []*regexp2.Regexp) (bool, error) {
	// Every field counts, not only those verilex records: a secret in any of them stays in stdout.
	var report any
	_ = json.Unmarshal(stdout, &report)
	_, found, err := redactValue(report, patterns)
	if err != nil {
		return false, err
	}
	for _, value := range []*any{&j.Reason, &j.Observation, &j.Detail} {
		clean, _, err := redactValue(*value, patterns)
		if err != nil {
			return false, err
		}
		*value = clean
	}
	if !found {
		return false, nil
	}
	return true, replaceWhole(filepath.Join(evidence, "stdout"))
}

// redactError returns err with every secret in its text replaced.
func redactError(err error, patterns []*regexp2.Regexp) error {
	if clean, redactErr := redact([]byte(err.Error()), patterns); redactErr == nil && clean != nil {
		return errors.New(string(clean))
	}
	return err
}

// replaceWhole replaces the content of a file whose secret verilex cannot locate.
func replaceWhole(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return replace(path, []byte(redacted), info.Mode().Perm())
}

// redactValue returns a decoded JSON value with every pattern match in its strings and keys
// replaced, and whether anything matched.
func redactValue(v any, patterns []*regexp2.Regexp) (any, bool, error) {
	switch v := v.(type) {
	case string:
		clean, err := redact([]byte(v), patterns)
		if err != nil || clean == nil {
			return v, false, err
		}
		return string(clean), true, nil
	case []any:
		result, found := make([]any, len(v)), false
		for i, item := range v {
			clean, hit, err := redactValue(item, patterns)
			if err != nil {
				return v, false, err
			}
			result[i], found = clean, found || hit
		}
		return result, found, nil
	case map[string]any:
		result, found := make(map[string]any, len(v)), false
		// Sorted keys number alike keys the same way on every run.
		for _, key := range slices.Sorted(maps.Keys(v)) {
			item := v[key]
			cleanKey, keyHit, err := redactValue(key, patterns)
			if err != nil {
				return v, false, err
			}
			clean, hit, err := redactValue(item, patterns)
			if err != nil {
				return v, false, err
			}
			// Keys that redact alike keep every value under a numbered name.
			name := cleanKey.(string)
			for n := 2; ; n++ {
				if _, taken := result[name]; !taken {
					break
				}
				name = fmt.Sprintf("%s-%d", cleanKey, n)
			}
			result[name], found = clean, found || keyHit || hit
		}
		return result, found, nil
	}
	return v, false, nil
}

// redact returns data with every pattern match replaced, or nil when nothing matched. Bytes
// that are not UTF-8 match as U+FFFD and are kept outside a match. A match of no text marks a
// secret whose extent is unknown, so the whole file is replaced.
func redact(data []byte, patterns []*regexp2.Regexp) ([]byte, error) {
	runes := make([]rune, 0, len(data))
	offsets := make([]int, 0, len(data)+1)
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		runes, offsets = append(runes, r), append(offsets, i)
		i += size
	}
	offsets = append(offsets, len(data))
	spans := [][2]int{}
	for _, pattern := range patterns {
		m, err := pattern.FindRunesMatch(runes)
		for ; m != nil && err == nil; m, err = pattern.FindNextMatch(m) {
			if m.Length == 0 {
				return []byte(redacted), nil
			}
			spans = append(spans, [2]int{offsets[m.Index], offsets[m.Index+m.Length]})
		}
		if err != nil {
			return nil, err
		}
	}
	if len(spans) == 0 {
		return nil, nil
	}
	slices.SortFunc(spans, func(a, b [2]int) int { return a[0] - b[0] })
	clean, at := []byte{}, 0
	for _, span := range spans {
		if span[1] <= at {
			continue
		}
		if span[0] >= at {
			clean = append(append(clean, data[at:span[0]]...), redacted...)
		}
		at = span[1]
	}
	return append(clean, data[at:]...), nil
}

// replace swaps the file at path for one holding data. The rename replaces a symlink itself, so
// a word that links outside its evidence never gets verilex to write there.
func replace(path string, data []byte, mode fs.FileMode) error {
	return withBits(filepath.Dir(path), 0o300, func() error { return swap(path, data, mode) })
}

// withBits runs change, and when path refuses it, adds the owner bits for the retry and then
// restores its mode. A word may leave a read-only directory or an unreadable file in its
// evidence, and verilex runs as the same user as the word.
func withBits(path string, bits fs.FileMode, change func() error) error {
	err := change()
	if !errors.Is(err, fs.ErrPermission) {
		return err
	}
	info, statErr := os.Stat(path)
	if statErr != nil || info.Mode().Perm()&bits == bits {
		return err
	}
	if err = os.Chmod(path, info.Mode()|bits); err != nil {
		return err
	}
	err = change()
	if restoreErr := os.Chmod(path, info.Mode()); err == nil {
		err = restoreErr
	}
	return err
}

func swap(path string, data []byte, mode fs.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".redacted-*")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(f.Name(), mode)
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}
