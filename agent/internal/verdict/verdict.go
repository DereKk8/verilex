// Package verdict reads the JSON every verilex run printed and decides which one the launcher
// returns. It never invents a verdict. A red from any run stands, a green must cover the run
// spec and every claim an earlier inconclusive run left open, and a document whose process exit
// disagrees with it is refused.
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
	// Claims is every claim a claim run selected, with the verdict verilex gave it.
	Claims []Claim `json:"claims"`
	// Words is every word the run ran or reused.
	Words []Step `json:"words"`
}

// Claim is one selected claim of a claim run.
type Claim struct {
	Claim   string `json:"claim"`
	Verdict string `json:"verdict"`
}

// Step is one word of a run: the claim version it proves (<claim>@<version>) and its verdict.
type Step struct {
	Proves  string `json:"proves"`
	Verdict string `json:"verdict"`
}

// Run is what one verilex run printed and its exit code.
type Run struct {
	Stdout []byte
	Exit   int
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

// Want is what the run spec requires a green to cover: the named claims (the launcher's --claim
// and the claims its intent names) and every changed path, relative to Project.
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

// Decide picks the verdict the launcher returns from every run the brain made, in order, and its
// exit code. A red from any run is the verdict: the project is read-only, so a later green on
// other claims cannot undo a failure verilex found in the code under test. Otherwise the last run
// is the verdict. A last run that is green must cover the spec and must have proved every claim
// that an earlier inconclusive run left open. An error is a reason to return no verdict.
func Decide(runs []Run, want Want) (Doc, int, error) {
	if len(runs) == 0 {
		return Doc{}, 2, fmt.Errorf("the brain ran no verilex run, so there is no verdict")
	}
	docs := make([]Doc, len(runs))
	for i, run := range runs {
		doc, err := Parse(run.Stdout)
		if err != nil {
			return Doc{}, 2, err
		}
		if _, err = Exit(doc, run.Exit); err != nil {
			return Doc{}, 2, fmt.Errorf("run %s: %v", doc.Run, err)
		}
		docs[i] = doc
	}
	for i := len(docs) - 1; i >= 0; i-- {
		if docs[i].Verdict == "red" {
			return docs[i], 1, nil
		}
	}
	last := docs[len(docs)-1]
	if last.Verdict != "green" {
		return last, code(last.Verdict), nil
	}
	if err := Check(last, want); err != nil {
		return Doc{}, 2, err
	}
	proven := provenBy(last)
	for _, doc := range docs[:len(docs)-1] {
		if doc.Verdict != "inconclusive" {
			continue
		}
		var open []string
		for _, claim := range selected(doc) {
			if !proven[claim] {
				open = append(open, claim)
			}
		}
		if len(open) > 0 {
			return Doc{}, 2, fmt.Errorf("run %s is green but did not prove %s, which run %s left inconclusive", last.Run, strings.Join(open, ", "), doc.Run)
		}
	}
	return last, 0, nil
}

// provenBy is every claim a green run proved: the claims it was asked for, and each selected
// claim and word it gave a green. A run that is not green proved none.
func provenBy(doc Doc) map[string]bool {
	out := map[string]bool{}
	if doc.Verdict != "green" {
		return out
	}
	if doc.Requested != nil {
		for _, name := range append(slices.Clone(doc.Requested.Claims), doc.Requested.Named...) {
			out[name] = true
		}
	}
	for _, claim := range doc.Claims {
		if claim.Verdict == "green" {
			out[claim.Claim] = true
		}
	}
	for _, step := range doc.Words {
		if step.Verdict == "green" && claimOf(step) != "" {
			out[claimOf(step)] = true
		}
	}
	return out
}

// selected is every claim a run set out to prove: the claims it was asked for, the claims a claim
// run selected, and the claim each of its words proves.
func selected(doc Doc) []string {
	var out []string
	add := func(name string) {
		if name != "" && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	if doc.Requested != nil {
		for _, name := range append(slices.Clone(doc.Requested.Claims), doc.Requested.Named...) {
			add(name)
		}
	}
	for _, claim := range doc.Claims {
		add(claim.Claim)
	}
	for _, step := range doc.Words {
		add(claimOf(step))
	}
	return out
}

// claimOf is the claim a word proves, without its version; empty for a word with no claim.
func claimOf(step Step) string {
	name, _, _ := strings.Cut(step.Proves, "@")
	return name
}

// Check refuses a green that does not prove the spec: a run that was not a claim run, one that
// neither was asked for nor proved a named claim, or one whose request left out a changed path. A
// red or inconclusive verdict stands as verilex printed it.
func Check(doc Doc, want Want) error {
	if doc.Verdict != "green" {
		return nil
	}
	if doc.Format != RunFormat || doc.Requested == nil {
		return fmt.Errorf("run %s is green but is not a claim run, so it does not show what it was asked", doc.Run)
	}
	proven := provenBy(doc)
	var missing []string
	for _, name := range want.Named {
		if !proven[name] {
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
