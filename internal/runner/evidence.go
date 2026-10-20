package runner

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"unicode/utf8"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/dlclark/regexp2"
)

// Each pattern matches the whole secret, because redact replaces what it matches: a private key
// block runs to its END line, or to the end of the file when that line is missing.
var secretPatterns = []string{
	`-----BEGIN [A-Z ]*PRIVATE KEY-----(?:[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----|[\s\S]*)`,
	`\bgh[pousr]_[A-Za-z0-9]{36,}`,
	`\bgithub_pat_[A-Za-z0-9_]{22,}`,
	`\bAKIA[0-9A-Z]{16}\b`,
	`\bxox[abprs]-[A-Za-z0-9-]{10,}`,
	`\bsk-ant-[A-Za-z0-9_-]{20,}`,
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

// scan redacts every secret pattern match in the files under dir and names the first file that
// held one. It runs before verilex reads anything from the evidence, so no secret it finds
// reaches a run record, the usage ledger or a command's output, and the evidence itself keeps
// none.
func scan(dir string, patterns []*regexp2.Regexp) (string, error) {
	var leak string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
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
	return leak, err
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
				return []byte(redacted + "\n"), nil
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
