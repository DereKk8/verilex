// Package featuremap owns reading the verify skill's feature map: pinning a claim's anchors
// (a sub-feature id plus normalized requirement sentences) and resolving an older word's
// `implements` reference to a whole section. It only reads; verilex never edits a verify skill.
package featuremap

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// Section is the resolved text a reference points at.
type Section struct {
	Ref  string `json:"ref"`
	File string `json:"file"`
	Text string `json:"text"`
	Hash string `json:"hash"`
}

// MissingError reports a reference that names no section in any skill directory.
type MissingError struct{ Ref, Why string }

func (e *MissingError) Error() string { return fmt.Sprintf("%s: %s", e.Ref, e.Why) }

// Resolve finds ref (`<skill>/<file>#<anchor>`) under the first skill directory that holds
// the file. The anchor selects a section:
//   - a heading whose slug equals the anchor: that heading through the next heading of the
//     same or a higher level;
//   - otherwise a sub-feature id written as inline code (`anchor`): the whole feature file,
//     since a sub-feature's meaning spreads across its file's sections;
//   - otherwise the section is missing.
//
// Without an anchor the section is the whole file.
func Resolve(root string, skillDirs []string, ref string) (Section, error) {
	path, anchor, _ := strings.Cut(ref, "#")
	clean, ok := relative(path)
	if !ok {
		return Section{}, &MissingError{ref, "not a <skill>/<file>#<section> reference"}
	}
	for _, dir := range skillDirs {
		file := filepath.Join(root, dir, clean)
		data, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return Section{}, err
		}
		text, ok := section(strings.ReplaceAll(string(data), "\r\n", "\n"), anchor)
		if !ok {
			return Section{}, &MissingError{ref, fmt.Sprintf("%s has no section %q", filepath.Join(dir, clean), anchor)}
		}
		text = strings.Trim(text, "\n")
		sum := sha256.Sum256([]byte(text))
		return Section{Ref: ref, File: filepath.Join(dir, clean), Text: text, Hash: hex.EncodeToString(sum[:])}, nil
	}
	return Section{}, &MissingError{ref, fmt.Sprintf("no feature file %s under %s", path, strings.Join(skillDirs, ", "))}
}

var heading = regexp.MustCompile(`^(#{1,6})[ \t]+(.*?)[ \t#]*$`)

// fenceRun matches a line that opens or closes a fenced block: three or more backticks or tildes.
var fenceRun = regexp.MustCompile("^\\s*(`{3,}|~{3,})")

// fence tracks fenced blocks line by line: the run that opened the current block, or "" outside
// one. A block opens with three or more backticks or tildes and closes with a bare run of the
// same character at least as long, so a shorter or other fence inside it is content.
type fence string

// step moves f past line and reports whether line opens or closes a block.
func (f *fence) step(line string) bool {
	m := fenceRun.FindStringSubmatch(line)
	switch {
	case m == nil:
		return false
	case *f == "":
		*f = fence(m[1])
	case m[1][0] == (*f)[0] && len(m[1]) >= len(*f) && strings.TrimSpace(line) == m[1]:
		*f = ""
	default:
		return false
	}
	return true
}

// fenceEnd returns the index just past the fenced block that opens at lines[i].
func fenceEnd(lines []string, i int) int {
	var f fence
	f.step(lines[i])
	for i++; i < len(lines); i++ {
		if f.step(lines[i]) {
			return i + 1
		}
	}
	return i
}

func section(text, anchor string) (string, bool) {
	if anchor == "" {
		return text, true
	}
	if text, ok := headingSection(text, anchor); ok {
		return text, true
	}
	if strings.Contains(text, "`"+anchor+"`") {
		return text, true
	}
	return "", false
}

// headingSection is the section under the first heading whose slug equals anchor: that heading
// through the next heading of the same or a higher level.
func headingSection(text, anchor string) (string, bool) {
	lines := strings.Split(text, "\n")
	start, level := -1, 0
	var f fence
	for i, line := range lines {
		if f.step(line) {
			continue
		}
		m := heading.FindStringSubmatch(line)
		if f != "" || m == nil {
			continue
		}
		if start >= 0 && len(m[1]) <= level {
			return strings.Join(lines[start:i], "\n"), true
		}
		if start < 0 && Slug(m[2]) == anchor {
			start, level = i, len(m[1])
		}
	}
	if start >= 0 {
		return strings.Join(lines[start:], "\n"), true
	}
	return "", false
}

// Slug turns a heading into its anchor the way Markdown renderers do: lower case, letters,
// digits, hyphens and underscores kept, spaces turned into hyphens.
func Slug(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return b.String()
}
