// Package verdict reads the JSON a verilex process printed and decides what the launcher may report.
// It never invents a verdict. It only refuses to call a run green when verilex's own plan and
// verdict disagree, or when the process exit disagrees with the document.
package verdict

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Doc is the machine-readable document verilex printed for one run.
type Doc struct {
	Raw     []byte
	Verdict string
	Run     string
	Warning string
	Missed  []string
	Skipped bool
}

// DefaultIntent is the intent a run uses when the spec names a diff and no intent.
const DefaultIntent = "prove nothing this change touched broke"

// Parse reads one JSON object. It keeps Raw so the launcher can return those bytes unchanged.
func Parse(raw []byte) (Doc, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return Doc{}, fmt.Errorf("verilex returned no JSON verdict")
	}
	var fields map[string]any
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return Doc{}, fmt.Errorf("verilex verdict is not JSON: %v", err)
	}
	doc := Doc{Raw: append([]byte(nil), trimmed...)}
	doc.Verdict, _ = fields["verdict"].(string)
	doc.Run, _ = fields["run"].(string)
	doc.Warning, _ = fields["warning"].(string)
	doc.Skipped, _ = fields["skipped"].(bool)
	doc.Missed = names(fields, "missed_claims", "unpicked", "uncovered", "uncovered_claims", "touched_unpicked")
	if doc.Warning == "" {
		doc.Warning = warningText(fields["warnings"])
	}
	if doc.Verdict == "" {
		return Doc{}, fmt.Errorf("verilex JSON has no verdict")
	}
	return doc, nil
}

// Unpicked reads a plan document and returns claims verilex says the run did not pick.
// An empty list means the plan did not report any, not that coverage was checked here.
func Unpicked(raw []byte) []string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil
	}
	var fields map[string]any
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil
	}
	found := names(fields, "missed_claims", "unpicked", "uncovered_claims", "touched_unpicked")
	found = append(found, disposed(fields["claims"])...)
	return unique(found)
}

// Normalize keeps the document's bytes and ends them with one newline.
func Normalize(raw []byte) []byte {
	return append(bytes.TrimRight(raw, "\n"), '\n')
}

// Exit is the process code for doc. A green document whose verilex process did not exit 0 is
// not reported as green.
func Exit(doc Doc, verilexExit int) (int, error) {
	want := code(doc.Verdict)
	if want == 0 && verilexExit != 0 {
		return 2, fmt.Errorf("verilex printed %s but exited %d", doc.Verdict, verilexExit)
	}
	if want == 1 && verilexExit == 0 {
		return 2, fmt.Errorf("verilex printed red but exited 0")
	}
	return want, nil
}

// MissingWarning returns plan claims that the verdict document does not warn about.
// A verdict that carries any missed-claim warning is treated as having warned.
func MissingWarning(planRaw []byte, doc Doc) []string {
	unpicked := Unpicked(planRaw)
	if len(unpicked) == 0 || warned(doc) {
		return nil
	}
	return unpicked
}

// Summary is the quiet human line. It is built only from doc.
func Summary(doc Doc) string {
	line := doc.Verdict
	if doc.Run != "" {
		line += "; run " + doc.Run
	}
	if doc.Skipped {
		line += "; skipped"
	}
	switch {
	case len(doc.Missed) > 0:
		line += fmt.Sprintf("; missed claims: %d", len(doc.Missed))
	case doc.Warning != "":
		line += "; " + oneLine(doc.Warning)
	}
	return line
}

func warned(doc Doc) bool {
	if doc.Warning != "" || len(doc.Missed) > 0 {
		return true
	}
	text := strings.ToLower(string(doc.Raw))
	return strings.Contains(text, "not covered") || strings.Contains(text, "not picked")
}

func code(verdict string) int {
	switch verdict {
	case "green":
		return 0
	case "red":
		return 1
	default:
		return 2
	}
}

func names(fields map[string]any, keys ...string) []string {
	var found []string
	for _, key := range keys {
		found = append(found, listNames(fields[key])...)
	}
	return unique(found)
}

func listNames(value any) []string {
	switch v := value.(type) {
	case string:
		if v != "" {
			return []string{v}
		}
	case []any:
		var found []string
		for _, item := range v {
			switch n := item.(type) {
			case string:
				if n != "" {
					found = append(found, n)
				}
			case map[string]any:
				if name := claimName(n); name != "" {
					found = append(found, name)
				}
			}
		}
		return found
	}
	return nil
}

func disposed(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	var found []string
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok || !unpickedDisposition(fields) {
			continue
		}
		if name := claimName(fields); name != "" {
			found = append(found, name)
		}
	}
	return found
}

func unpickedDisposition(fields map[string]any) bool {
	for _, key := range []string{"disposition", "status", "decision"} {
		switch strings.ToLower(fmt.Sprint(fields[key])) {
		case "unpicked", "missed", "uncovered", "not_picked", "not picked":
			return true
		}
	}
	if v, ok := fields["picked"].(bool); ok && !v {
		return true
	}
	if v, ok := fields["unpicked"].(bool); ok && v {
		return true
	}
	return false
}

func claimName(fields map[string]any) string {
	for _, key := range []string{"claim", "name", "id"} {
		if s, ok := fields[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func warningText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			switch n := item.(type) {
			case string:
				parts = append(parts, n)
			case map[string]any:
				if text, ok := n["text"].(string); ok {
					parts = append(parts, text)
				} else if text, ok := n["warning"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "; ")
	default:
		return ""
	}
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range in {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
