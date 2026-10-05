package featuremap

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Anchor is where a claim's meaning sits in a verify skill: a stable sub-feature id plus the
// normalized requirement sentences the claim maps to. Prose, commands, run history and layout
// around them can change freely; only a change to the id or to one of those sentences asks for
// a review.
type Anchor struct {
	Ref          string   `json:"ref"`
	File         string   `json:"file,omitempty"`
	Requirements []string `json:"requirements"`
	// Hash fingerprints the id and the normalized requirement sentences the anchor pins.
	Hash string `json:"hash"`
	// Review says why the verify skill no longer holds what the anchor pins; empty when it does.
	Review []string `json:"review,omitempty"`
}

// Pin checks that ref (`<skill>/<file>#<sub-feature-id>`) still names its sub-feature and that
// every sentence in requirements is still a requirement sentence of that feature file. A missing
// file, id or sentence goes into the anchor's Review; only an unreadable file is an error.
func Pin(root string, skillDirs []string, ref string, requirements []string) (Anchor, error) {
	path, id, _ := strings.Cut(ref, "#")
	pinned := make([]string, 0, len(requirements))
	for _, sentence := range requirements {
		pinned = append(pinned, Normalize(sentence))
	}
	sorted := slices.Clone(pinned)
	slices.Sort(sorted)
	sum := sha256.Sum256([]byte(id + "\n" + strings.Join(sorted, "\n")))
	a := Anchor{Ref: ref, Requirements: pinned, Hash: hex.EncodeToString(sum[:])}
	clean, ok := relative(path)
	if !ok || id == "" {
		a.Review = append(a.Review, "not a <skill>/<file>#<sub-feature> reference")
		return a, nil
	}
	for _, dir := range skillDirs {
		file := filepath.Join(root, dir, clean)
		data, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return a, err
		}
		a.File = filepath.Join(dir, clean)
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		if _, found := section(text, id); !found {
			a.Review = append(a.Review, fmt.Sprintf("sub-feature %s is gone", id))
		}
		current := Requirements(text)
		for _, sentence := range pinned {
			if !slices.Contains(current, sentence) {
				a.Review = append(a.Review, "requirement changed or gone: "+sentence)
			}
		}
		return a, nil
	}
	a.Review = append(a.Review, fmt.Sprintf("no feature file %s under %s", path, strings.Join(skillDirs, ", ")))
	return a, nil
}

var (
	// requirementWord marks a requirement sentence, matched with code spans masked.
	requirementWord = regexp.MustCompile(`\b(Require|require|must|Expect|Success is|exits?|returns?)\b`)
	// datedHistory marks a run-history sentence: it records when something was seen, not a rule.
	datedHistory = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
	codeSpan     = regexp.MustCompile("`[^`\n]*`")
	listMarker   = regexp.MustCompile(`^\s*(?:>\s*)*(?:[-*+]|\d+[.)])\s+`)
	marker       = regexp.MustCompile(`^\s*(?:>\s*)*(?:(?:[-*+]|\d+[.)])\s+)?`)
	emphasis     = regexp.MustCompile(`\*\*|__`)
)

// Requirements lists a feature file's requirement sentences, normalized: the sentences that
// state what must happen or what counts as success (`Expect`, `must`, `require`, `exits`,
// `returns`, `Success is`). A sentence that starts with `Run ` is an action, so its command text
// never counts; a sentence that carries a date is run history; headings and fenced blocks are
// not sentences. Literal values in code spans stay, because they are what is required.
func Requirements(text string) []string {
	result := []string{}
	for _, unit := range units(text) {
		masked, spans := mask(unit)
		for _, sentence := range split(masked) {
			if strings.HasPrefix(sentence, "Run ") || datedHistory.MatchString(sentence) || !requirementWord.MatchString(sentence) {
				continue
			}
			if normalized := collapse(unmask(sentence, spans)); !slices.Contains(result, normalized) {
				result = append(result, normalized)
			}
		}
	}
	return result
}

// Normalize brings one sentence into the form Requirements produces, so a sentence quoted in a
// claim matches the runbook however its list marker, emphasis or line breaks are written.
func Normalize(sentence string) string {
	masked, spans := mask(strings.TrimSpace(sentence))
	return collapse(unmask(masked, spans))
}

// units splits Markdown into paragraphs, list items and table cells, leaving out headings and
// fenced blocks.
func units(text string) []string {
	result := []string{}
	current := []string{}
	flush := func() {
		if len(current) > 0 {
			result = append(result, strings.Join(current, " "))
			current = nil
		}
	}
	fenced := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			fenced = !fenced
			flush()
			continue
		}
		switch {
		case fenced:
		case trimmed == "" || heading.MatchString(line):
			flush()
		case strings.HasPrefix(trimmed, "|"):
			flush()
			result = append(result, cells(trimmed)...)
		case listMarker.MatchString(line):
			flush()
			current = append(current, trimmed)
		default:
			current = append(current, trimmed)
		}
	}
	flush()
	return result
}

// cells splits a table row on the pipes outside code spans.
func cells(row string) []string {
	result := []string{}
	start, code := 0, false
	for i, r := range row {
		switch {
		case r == '`':
			code = !code
		case r == '|' && !code:
			if cell := strings.TrimSpace(row[start:i]); cell != "" {
				result = append(result, cell)
			}
			start = i + 1
		}
	}
	if cell := strings.TrimSpace(row[start:]); cell != "" {
		result = append(result, cell)
	}
	return result
}

// mask drops quote and list markers and emphasis, then replaces every code span with a
// placeholder so that neither its keywords nor its punctuation shape the sentence.
func mask(unit string) (string, []string) {
	unit = marker.ReplaceAllString(unit, "")
	spans := codeSpan.FindAllString(unit, -1)
	i := 0
	masked := codeSpan.ReplaceAllStringFunc(unit, func(string) string {
		i++
		return fmt.Sprintf("\x00%d\x00", i-1)
	})
	return emphasis.ReplaceAllString(masked, ""), spans
}

func unmask(masked string, spans []string) string {
	for i, span := range spans {
		masked = strings.Replace(masked, fmt.Sprintf("\x00%d\x00", i), span, 1)
	}
	return masked
}

// split cuts masked text into sentences after '.', '!' or '?' followed by a space.
func split(masked string) []string {
	result := []string{}
	start := 0
	runes := []rune(masked)
	for i, r := range runes {
		if (r == '.' || r == '!' || r == '?') && (i+1 == len(runes) || runes[i+1] == ' ' || runes[i+1] == '\t') {
			result = append(result, strings.TrimSpace(string(runes[start:i+1])))
			start = i + 1
		}
	}
	if rest := strings.TrimSpace(string(runes[start:])); rest != "" {
		result = append(result, rest)
	}
	return result
}

func collapse(text string) string { return strings.Join(strings.Fields(text), " ") }

// relative cleans a reference path and reports whether it stays inside a skill directory.
func relative(path string) (string, bool) {
	clean := filepath.Clean(filepath.FromSlash(path))
	if path == "" || filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	return clean, true
}
