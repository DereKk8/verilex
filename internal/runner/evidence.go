package runner

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"unicode/utf8"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/verdict"
	"github.com/dlclark/regexp2"
)

// Each pattern matches the whole secret, because redact replaces what it matches: a private key
// block runs to its END line, or to the end of the file when that line is missing. A token
// starts after any character but an ASCII letter or digit: `\b` would miss one after `_` or a
// non-ASCII letter, as in MY_ghp_... or éghp_....
var secretPatterns = []string{
	`-----BEGIN [A-Z ]*PRIVATE KEY-----(?:[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----|[\s\S]*)`,
	`(?<![A-Za-z0-9])gh[pousr]_[A-Za-z0-9]{36,}`,
	`(?<![A-Za-z0-9])github_pat_[A-Za-z0-9_]{22,}`,
	`(?<![A-Za-z0-9])AKIA[0-9A-Z]{16}(?![A-Za-z0-9])`,
	`(?<![A-Za-z0-9])xox[abprs]-[A-Za-z0-9-]{10,}`,
	`(?<![A-Za-z0-9])sk-ant-[A-Za-z0-9_-]{20,}`,
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
// itself keeps none.
func scan(dir string, patterns []*regexp2.Regexp) (string, error) {
	var leak string
	renames := []string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
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
			}
		}
		if entry.IsDir() {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		clean, err := redact(data, patterns)
		if err != nil || clean == nil {
			return err
		}
		if leak == "" {
			leak = entry.Name()
		}
		return replace(path, clean, info.Mode().Perm())
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
		if err = rename(path, string(name)); err != nil {
			return leak, err
		}
	}
	return leak, nil
}

// rename gives the entry at path a new name in its directory, numbered when the name is taken.
func rename(path, name string) error {
	target := filepath.Join(filepath.Dir(path), name)
	for n := 2; ; n++ {
		if _, err := os.Lstat(target); errors.Is(err, fs.ErrNotExist) {
			break
		}
		target = filepath.Join(filepath.Dir(path), fmt.Sprintf("%s-%d", name, n))
	}
	return writable(filepath.Dir(path), func() error { return os.Rename(path, target) })
}

// redactJudgment redacts the values a word reported once decoded, which the scan of the raw bytes
// can miss, as with a JSON-escaped token. Where such a secret sits in stdout is unknown, so the
// whole file is replaced. It reports whether anything matched.
func redactJudgment(j *verdict.Judgment, evidence string, patterns []*regexp2.Regexp) (bool, error) {
	found := false
	for _, value := range []*any{&j.Reason, &j.Observation, &j.Detail} {
		clean, hit, err := redactValue(*value, patterns)
		if err != nil {
			return false, err
		}
		*value, found = clean, found || hit
	}
	if !found {
		return false, nil
	}
	return true, replaceWhole(filepath.Join(evidence, "stdout"))
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
		for key, item := range v {
			cleanKey, keyHit, err := redactValue(key, patterns)
			if err != nil {
				return v, false, err
			}
			clean, hit, err := redactValue(item, patterns)
			if err != nil {
				return v, false, err
			}
			result[cleanKey.(string)], found = clean, found || keyHit || hit
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
	return writable(filepath.Dir(path), func() error { return swap(path, data, mode) })
}

// writable runs change, and when dir refuses it, makes dir writable for the retry and then
// restores its mode. A word may leave a read-only directory in its evidence, and verilex runs as
// the same user as the word.
func writable(dir string, change func() error) error {
	err := change()
	if !errors.Is(err, fs.ErrPermission) {
		return err
	}
	info, statErr := os.Stat(dir)
	if statErr != nil || info.Mode().Perm()&0o300 == 0o300 {
		return err
	}
	if err = os.Chmod(dir, info.Mode()|0o300); err != nil {
		return err
	}
	err = change()
	if restoreErr := os.Chmod(dir, info.Mode()); err == nil {
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
