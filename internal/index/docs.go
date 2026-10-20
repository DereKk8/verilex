package index

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/featuremap"
)

// docsDir is where a project's docs live: every regular markdown file under it is read for anchors.
const docsDir = "docs"

// An anchor is a line of its own, outside code blocks, that ties the passage it sits in to
// claim ids: <!-- claims: chain-skipped chain-runs-live -->. The passage is the file's section
// under the nearest heading above the anchor, up to the next heading. An HTML comment renders as
// nothing, and a claim id survives a refactor where a line number would not. A comment that opens
// with claim or claims in any case (<!-- claim: chain-red -->) is meant as an anchor, so it is
// read as one and refused unless written exactly; it is never silently ignored.
var (
	anchorStart = regexp.MustCompile(`(?i)^<!--\s*claims?(?:[\s:=]|-->)`)
	anchorLine  = regexp.MustCompile(`^<!--\s*claims:(.*)-->$`)
)

// Passage is a doc section and the claims its anchors name.
type Passage struct {
	File    string   `json:"file"`
	Heading string   `json:"heading"`
	Claims  []string `json:"claims"`
}

// passages reads every anchor in the project's docs, file by file and heading by heading in order.
// An anchor that names a claim that does not exist, or that sits above the first heading, is
// refused, so a doc never points at a claim nobody proves.
func (ix Index) passages(p dictionary.Project) ([]Passage, error) {
	found := []Passage{}
	dir := filepath.Join(p.Root, docsDir)
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return found, nil
	}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		// Only regular files: a symlink may dangle or lead outside the product, and a FIFO blocks.
		if err != nil || !d.Type().IsRegular() || filepath.Ext(path) != ".md" {
			return err
		}
		rel, err := filepath.Rel(p.Root, path)
		if err != nil {
			return err
		}
		file := filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("cannot read %s, so its anchors are unknown: %w", file, err)
		}
		text := strings.TrimPrefix(strings.ReplaceAll(string(data), "\r\n", "\n"), "\ufeff")
		lines := strings.Split(text, "\n")
		fenced := featuremap.Fenced(lines)
		heading, at := "", -1
		for i, line := range lines {
			if fenced[i] || indented(line) {
				continue
			}
			if level, text := featuremap.Heading(line); level > 0 {
				heading, at = text, -1
				continue
			}
			line = strings.TrimSpace(line)
			if !anchorStart.MatchString(line) {
				continue
			}
			m := anchorLine.FindStringSubmatch(line)
			if m == nil || len(strings.Fields(m[1])) == 0 {
				return fmt.Errorf("%s:%d: write an anchor as `<!-- claims: <claim> ... -->`, naming at least one claim", file, i+1)
			}
			if heading == "" {
				return fmt.Errorf("%s:%d: the anchor sits above the first heading; put it under the heading of the passage it describes", file, i+1)
			}
			if at < 0 {
				found, at = append(found, Passage{File: file, Heading: heading, Claims: []string{}}), len(found)
			}
			for _, claim := range strings.Fields(m[1]) {
				if _, ok := ix.claims[claim]; !ok {
					return fmt.Errorf("%s:%d: the anchor names claim %s, which does not exist; `verilex claims` lists the claims", file, i+1, claim)
				}
				if !slices.Contains(found[at].Claims, claim) {
					found[at].Claims = append(found[at].Claims, claim)
				}
			}
		}
		return nil
	})
	return found, err
}

// indented reports a line of an indented code block, where an anchor is an example: the same rule
// as the site's isIndented (site/docs.go, a separate module), so the index reads exactly the
// anchors the site hides.
func indented(line string) bool {
	return strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")
}

// tied lists the passages whose anchors name one of claims (or a claim grouped under one), each
// with only those claims.
func (ix Index) tied(passages []Passage, claims []Claim) []Passage {
	reported := map[string]bool{}
	for _, c := range claims {
		reported[c.Claim] = true
	}
	out := []Passage{}
	for _, passage := range passages {
		names := slices.DeleteFunc(slices.Clone(passage.Claims), func(name string) bool { return !reported[ix.groupOf(name)] })
		if len(names) > 0 {
			out = append(out, Passage{File: passage.File, Heading: passage.Heading, Claims: names})
		}
	}
	return out
}

// within lists the passages of the docs a diff touches: a file, or a directory that holds it.
func within(p dictionary.Project, passages []Passage, touched []string) []Passage {
	var paths []string
	for _, item := range touched {
		if kind, value := classifyChange(item); kind == "path" {
			paths = append(paths, resolve(value))
		}
	}
	out := []Passage{}
	for _, passage := range passages {
		file := filepath.Join(p.Root, filepath.FromSlash(passage.File))
		if slices.ContainsFunc(paths, func(path string) bool {
			return file == path || strings.HasPrefix(file, path+string(filepath.Separator))
		}) {
			out = append(out, passage)
		}
	}
	return out
}
