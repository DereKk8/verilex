package runner

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/dlclark/regexp2"
)

var secretPatterns = []string{
	`-----BEGIN [A-Z ]*PRIVATE KEY-----`,
	`\bgh[pousr]_[A-Za-z0-9]{36,}`,
	`\bgithub_pat_[A-Za-z0-9_]{22,}`,
	`\bAKIA[0-9A-Z]{16}\b`,
	`\bxox[abprs]-[A-Za-z0-9-]{10,}`,
	`\bsk-ant-[A-Za-z0-9_-]{20,}`,
}

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
		text := strings.ToValidUTF8(string(data), "\uFFFD")
		for _, pattern := range patterns {
			matched, err := pattern.MatchString(text)
			if err != nil {
				return err
			}
			if matched {
				leak = entry.Name()
				return fs.SkipAll
			}
		}
		return nil
	})
	return leak, err
}
