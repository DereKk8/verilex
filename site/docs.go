package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Bundle is everything the web app shows: docs/README.md as the navigation, and every page it lists.
type Bundle struct {
	Name     string    `json:"name"`
	Repo     string    `json:"repo"`
	Branch   string    `json:"branch"`
	Intro    string    `json:"intro"`
	Sections []Section `json:"sections"`
	Pages    []Page    `json:"pages"`
	Static   bool      `json:"static,omitempty"`
	// AskRules are the instructions every answer is grounded with, so a page that asks a model
	// itself (a claude.ai artifact) asks exactly what the docs server asks.
	AskRules string `json:"askRules"`
}

// Section is one `##` heading of the index and the pages listed under it, in order.
type Section struct {
	Title string   `json:"title"`
	Pages []string `json:"pages"`
}

// Page is one docs/<slug>.md file.
type Page struct {
	Slug     string    `json:"slug"`
	File     string    `json:"file"`
	Title    string    `json:"title"`
	Summary  string    `json:"summary"`
	Section  string    `json:"section"`
	Markdown string    `json:"markdown"`
	Headings []Heading `json:"headings"`
}

// Heading is a heading outside fenced blocks, with the anchor GitHub gives it.
type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	ID    string `json:"id"`
}

var (
	headingLine = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	indexItem   = regexp.MustCompile(`^- \[([^\]]+)\]\(([a-z0-9-]+\.md)\):\s*(.+)$`)
	linkPattern = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)\)`)
	codeSpan    = regexp.MustCompile("(`+)[^`]*?(`+)")
	fenceOpen   = regexp.MustCompile("^\\s*(```+|~~~+)")
)

// Load reads docs/README.md under root and every page it lists.
func Load(root, repo, branch string) (*Bundle, error) {
	index, err := os.ReadFile(filepath.Join(root, "docs", "README.md"))
	if err != nil {
		return nil, err
	}
	b := &Bundle{Name: "verilex", Repo: repo, Branch: branch, AskRules: instructions}
	var intro []string
	var section *Section
	for _, line := range outsideFences(string(index)) {
		switch {
		case strings.HasPrefix(line, "# "):
		case strings.HasPrefix(line, "## "):
			b.Sections = append(b.Sections, Section{Title: strings.TrimSpace(line[3:])})
			section = &b.Sections[len(b.Sections)-1]
		case section == nil:
			intro = append(intro, line)
		default:
			m := indexItem.FindStringSubmatch(line)
			if m == nil {
				if strings.TrimSpace(line) != "" {
					return nil, fmt.Errorf("docs/README.md: under %q, %q is not a `- [Title](page.md): summary` item", section.Title, line)
				}
				continue
			}
			page, err := loadPage(root, m[2])
			if err != nil {
				return nil, err
			}
			page.Title, page.Summary, page.Section = m[1], m[3], section.Title
			section.Pages = append(section.Pages, page.Slug)
			b.Pages = append(b.Pages, page)
		}
	}
	b.Intro = strings.TrimSpace(strings.Join(intro, "\n"))
	return b, nil
}

func loadPage(root, file string) (Page, error) {
	data, err := os.ReadFile(filepath.Join(root, "docs", file))
	if err != nil {
		return Page{}, err
	}
	text := string(data)
	return Page{Slug: strings.TrimSuffix(file, ".md"), File: "docs/" + file, Markdown: text, Headings: Headings(text)}, nil
}

// Headings lists a markdown file's headings with GitHub's anchors: duplicates get -1, -2 and so on.
func Headings(markdown string) []Heading {
	var out []Heading
	seen := map[string]int{}
	for _, line := range outsideFences(markdown) {
		m := headingLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		text := plainText(m[2])
		id := Slug(text)
		if n := seen[id]; n > 0 {
			seen[id] = n + 1
			id = fmt.Sprintf("%s-%d", id, n)
		} else {
			seen[id] = 1
		}
		out = append(out, Heading{Level: len(m[1]), Text: text, ID: id})
	}
	return out
}

// Slug is GitHub's heading anchor: lower case, letters, digits, '-' and '_' kept, spaces to '-'.
func Slug(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r), r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// plainText drops inline markdown from a heading: code ticks, emphasis and link targets.
func plainText(s string) string {
	s = linkPattern.ReplaceAllString(s, "$1")
	return strings.NewReplacer("`", "", "**", "", "*", "").Replace(s)
}

// mdLine is one line of a markdown file, and whether it sits inside a fenced block (fence lines do).
type mdLine struct {
	Text   string
	Fenced bool
}

// mdLines splits a markdown file into lines and marks the ones inside fenced blocks.
func mdLines(markdown string) []mdLine {
	var out []mdLine
	fence := ""
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if m := fenceOpen.FindStringSubmatch(line); m != nil && fence == "" {
			fence = m[1]
			out = append(out, mdLine{line, true})
			continue
		}
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]) == "" {
				fence = ""
			}
			out = append(out, mdLine{line, true})
			continue
		}
		out = append(out, mdLine{line, false})
	}
	return out
}

// outsideFences returns the lines that are not part of a fenced block.
func outsideFences(markdown string) []string {
	var out []string
	for _, l := range mdLines(markdown) {
		if !l.Fenced {
			out = append(out, l.Text)
		}
	}
	return out
}

// Validate reports every page left out of the index, every index title that differs from its page's
// title, and every relative link or anchor in the docs and the root README that leads nowhere.
func Validate(b *Bundle, root string) []error {
	var errs []error
	listed := map[string]bool{}
	pages := map[string]Page{}
	for _, p := range b.Pages {
		if listed[p.Slug] {
			errs = append(errs, fmt.Errorf("docs/README.md lists %s twice", p.File))
		}
		listed[p.Slug] = true
		pages[p.Slug] = p
		if hs := p.Headings; len(hs) == 0 || hs[0].Level != 1 || hs[0].Text != p.Title {
			errs = append(errs, fmt.Errorf("%s: the index calls it %q, but its first heading is not `# %s`", p.File, p.Title, p.Title))
		}
	}
	files, _ := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	for _, f := range files {
		slug := strings.TrimSuffix(filepath.Base(f), ".md")
		if slug != "README" && !listed[slug] {
			errs = append(errs, fmt.Errorf("docs/%s.md is not listed in docs/README.md, so the site never shows it", slug))
		}
	}
	anchors := func(file string) ([]Heading, bool) {
		if p, ok := pages[strings.TrimSuffix(strings.TrimPrefix(file, "docs/"), ".md")]; ok && strings.HasPrefix(file, "docs/") {
			return p.Headings, true
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			return nil, false
		}
		return Headings(string(data)), true
	}
	check := func(from, text string) {
		for _, line := range outsideFences(text) {
			line = codeSpan.ReplaceAllString(line, "")
			for _, m := range linkPattern.FindAllStringSubmatch(line, -1) {
				if err := checkLink(root, from, m[2], anchors); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	for _, p := range b.Pages {
		check(p.File, p.Markdown)
	}
	for _, f := range []string{"docs/README.md", "README.md"} {
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		check(f, string(data))
	}
	return errs
}

func checkLink(root, from, target string, anchors func(string) ([]Heading, bool)) error {
	if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
		return nil
	}
	file, anchor, _ := strings.Cut(target, "#")
	if file == "" {
		file = from
	} else {
		file = path.Clean(path.Join(path.Dir(from), file))
	}
	if strings.HasPrefix(file, "../") {
		return fmt.Errorf("%s: link %s leaves the repository", from, target)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(file))); err != nil {
		return fmt.Errorf("%s: link %s: %s does not exist", from, target, file)
	}
	if anchor == "" {
		return nil
	}
	hs, ok := anchors(file)
	if !ok || !slices.ContainsFunc(hs, func(h Heading) bool { return h.ID == anchor }) {
		return fmt.Errorf("%s: link %s: %s has no heading #%s", from, target, file, anchor)
	}
	return nil
}

// Script is the bundle as the web app loads it: one classic script, so the built site opens from disk.
func (b *Bundle) Script() ([]byte, error) {
	data, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	return append(append([]byte("window.VERILEX_DOCS = "), data...), ";\n"...), nil
}
