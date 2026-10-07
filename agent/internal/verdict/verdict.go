// Package verdict reads the JSON a verilex run printed and decides whether the launcher may return
// it. It never invents a verdict. It refuses a green that does not cover the run spec, and any
// document whose process exit disagrees with it.
package verdict

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// RunFormat is the claim-run contract the launcher reads. Any other format is a breaking change.
const RunFormat = "verilex-claim-run-1"

// Doc is the document verilex printed for one run. Raw is returned unchanged.
type Doc struct {
	Raw       []byte      `json:"-"`
	Format    string      `json:"format"`
	Verdict   string      `json:"verdict"`
	Run       string      `json:"run"`
	Reason    *string     `json:"reason"`
	Skipped   bool        `json:"skipped"`
	Requested *Request    `json:"requested"`
	Warning   string      `json:"warning"`
	Uncovered []Uncovered `json:"uncovered"`
	Unmapped  []string    `json:"unmapped"`
	Unclaimed []string    `json:"unclaimed"`
	Unrun     []string    `json:"unrun"`
}

// Request is what verilex says it was asked, read from verilex rather than from the brain.
type Request struct {
	Claims  []string `json:"claims"`
	Named   []string `json:"named"`
	Changed []string `json:"changed"`
}

// Uncovered is a touched claim the run did not prove.
type Uncovered struct {
	Claim string `json:"claim"`
	Next  string `json:"next"`
}

// Want is what the run spec requires a green to cover: the named claims and every changed path,
// relative to Project.
type Want struct {
	Project string
	Named   []string
	Changed []string
}

// Parse reads one JSON object and keeps its bytes.
func Parse(raw []byte) (Doc, error) {
	trimmed := bytes.TrimSpace(raw)
	var doc Doc
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return Doc{}, fmt.Errorf("verilex printed no JSON verdict: %v", err)
	}
	if doc.Format != "" && doc.Format != RunFormat {
		return Doc{}, fmt.Errorf("verilex printed format %q; this launcher reads %s", doc.Format, RunFormat)
	}
	if code(doc.Verdict) < 0 {
		return Doc{}, fmt.Errorf("verilex JSON has no verdict")
	}
	doc.Raw = append(slices.Clone(trimmed), '\n')
	return doc, nil
}

// Exit is the process code for doc. verilex exits 0, 1 or 2 for green, red or inconclusive; any
// other pairing is an environment failure, never a product verdict.
func Exit(doc Doc, verilexExit int) (int, error) {
	want := code(doc.Verdict)
	if want != verilexExit {
		return 2, fmt.Errorf("verilex printed %s but exited %d", doc.Verdict, verilexExit)
	}
	return want, nil
}

// Check refuses a green that does not prove the spec: a run that was not a claim run, or whose
// request left out a named claim or a changed path. A red or inconclusive verdict stands as
// verilex printed it.
func Check(doc Doc, want Want) error {
	if doc.Verdict != "green" {
		return nil
	}
	if doc.Format != RunFormat || doc.Requested == nil {
		return fmt.Errorf("run %s is green but is not a claim run, so it does not show what it was asked", doc.Run)
	}
	asked := append(slices.Clone(doc.Requested.Claims), doc.Requested.Named...)
	var missing []string
	for _, name := range want.Named {
		if !slices.Contains(asked, name) {
			missing = append(missing, "claim "+name)
		}
	}
	changed := map[string]bool{}
	for _, entry := range doc.Requested.Changed {
		if path, ok := pathOf(entry); ok {
			changed[resolve(want.Project, path)] = true
		}
	}
	for _, path := range want.Changed {
		if !changed[resolve(want.Project, path)] {
			missing = append(missing, "change "+path)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("run %s is green but was not asked about %s", doc.Run, strings.Join(missing, ", "))
	}
	return nil
}

// Summary is the quiet human form, built only from doc: the verdict line, then each gap verilex
// reported with the command that would close it.
func Summary(doc Doc) string {
	line := doc.Verdict
	if doc.Run != "" {
		line += "; run " + doc.Run
	}
	if doc.Skipped {
		line += "; skipped"
	}
	if doc.Warning != "" {
		line += "; " + doc.Warning
	}
	if doc.Reason != nil && *doc.Reason != "" {
		line += "; " + *doc.Reason
	}
	lines := []string{line}
	for _, u := range doc.Uncovered {
		lines = append(lines, "  uncovered  "+u.Claim+"  "+u.Next)
	}
	for _, gap := range []struct {
		label string
		items []string
	}{{"unmapped", doc.Unmapped}, {"unclaimed", doc.Unclaimed}, {"unrun", doc.Unrun}} {
		for _, item := range gap.items {
			lines = append(lines, "  "+gap.label+"  "+item)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// pathOf returns the path of a diff entry, as verilex classifies it.
func pathOf(entry string) (string, bool) {
	for _, kind := range []string{"config:", "image:", "runbook:"} {
		if strings.HasPrefix(entry, kind) && len(entry) > len(kind) {
			return "", false
		}
	}
	if strings.HasPrefix(entry, "path:") && len(entry) > len("path:") {
		return entry[len("path:"):], true
	}
	return entry, true
}

// resolve matches verilex: a relative path is under the project, and symlinks are followed.
func resolve(project, path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(project, path)
	}
	path = filepath.Clean(path)
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(dir, filepath.Base(path))
	}
	return path
}

func code(verdict string) int {
	switch verdict {
	case "green":
		return 0
	case "red":
		return 1
	case "inconclusive":
		return 2
	}
	return -1
}
