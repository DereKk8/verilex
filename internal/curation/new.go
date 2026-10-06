package curation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/featuremap"
)

// ScaffoldSteps are the frame stubs `verilex new` writes into a project without .verilex/.
var ScaffoldSteps = []string{"launch", "doctor", "refresh", "cleanup"}

var stubPurpose = map[string]string{
	"launch":  `start an instance owned by this run (label it with $VERILEX_RUN) and print {"instance": ...} on stdout`,
	"doctor":  "exit 0 only when the instance is this run's and healthy enough to drive",
	"refresh": "bring a kept instance up to the current checkout, keeping every state it holds",
	"cleanup": "tear down only what this run launched",
}

// Scaffold is what `verilex new` created.
type Scaffold struct {
	Project  dictionary.Project
	Word     string
	Created  []string // paths relative to the project root
	Frame    bool     // .verilex/ was created with a config and frame stubs
	Sections []featuremap.Section
}

// New writes a provisional word whose contract implements refs. Every ref must resolve to a
// feature-map section. When start lies in no project, .verilex/ is created at start with a
// config and frame stubs; the stubs exit 2 and the word's run reports blocked until written, so
// a scaffold is always inconclusive, never green.
func New(start, name string, refs []string) (Scaffold, error) {
	if !dictionary.WordName.MatchString(name) {
		return Scaffold{}, fmt.Errorf("%q is not a word name: letters or digits, then '.', '_' or '-'", name)
	}
	if len(refs) == 0 {
		return Scaffold{}, fmt.Errorf("a word must implement a verify-skill feature-map section: --implements <skill>/<file>#<section>")
	}
	s := Scaffold{Word: name}
	p, err := dictionary.FindProject(start)
	if errors.Is(err, dictionary.ErrNoProject) {
		p, s.Frame, err = dictionary.Project{Root: start, Name: projectName(start), SkillDirs: dictionary.DefaultSkillDirs}, true, nil
	}
	if err != nil {
		return s, err
	}
	s.Project = p
	for _, ref := range refs {
		section, err := featuremap.Resolve(p.Root, p.SkillDirs, ref)
		if err != nil {
			return s, fmt.Errorf("%v; record a product moment the feature map lacks with `verilex gap`", err)
		}
		s.Sections = append(s.Sections, section)
	}
	dir := filepath.Join(p.Dir(), "words", name)
	if _, err = os.Lstat(dir); err == nil {
		return s, fmt.Errorf("%s already exists", s.rel(dir))
	}
	files := map[string]string{}
	if s.Frame {
		files[filepath.Join(p.Dir(), "config.yaml")] = "project: " + p.Name + "\n"
		for _, step := range ScaffoldSteps {
			files[p.Frame(step)] = fmt.Sprintf("#!/bin/sh\n# verilex frame %s: %s.\n# Write it from the verify skill's %s section; until then every run is inconclusive.\necho 'frame/%s is a stub' >&2\nexit 2\n", step, stubPurpose[step], titleCase(step), step)
		}
	}
	quoted := make([]string, len(refs))
	for i, ref := range refs {
		quoted[i] = "  - " + strconv.Quote(ref)
	}
	files[filepath.Join(dir, "word.md")] = fmt.Sprintf("---\nword: %s\npromise: \"TODO: the one product state this word promises, as a user of the product would say it.\"\nargs: []\nrequires: []\nprovides: []\nimplements:\n%s\n---\n\nProvisional. Describe the product moment this word drives, then write `run`.\n", strconv.Quote(name), strings.Join(quoted, "\n"))
	files[filepath.Join(dir, "run")] = fmt.Sprintf("#!/bin/sh\n# Provisional word %s: drive the product through a surface its users have, keep the action\n# and a second observation in $VERILEX_EVIDENCE, and print one result object.\necho '{\"verdict\": \"blocked\", \"detail\": \"%s is a stub; write its run\"}'\nexit 2\n", name, name)
	for _, path := range sortedKeys(files) {
		mode := os.FileMode(0644)
		if filepath.Base(path) == "run" || filepath.Dir(path) == filepath.Join(p.Dir(), "frame") {
			mode = 0755
		}
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return s, err
		}
		if err = os.WriteFile(path, []byte(files[path]), mode); err != nil {
			return s, err
		}
		s.Created = append(s.Created, s.rel(path))
	}
	return s, nil
}

func (s Scaffold) rel(path string) string {
	if rel, err := filepath.Rel(s.Project.Root, path); err == nil {
		return rel
	}
	return path
}

func projectName(root string) string {
	name := regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(filepath.Base(root), "-")
	if strings.Trim(name, "-.") == "" {
		return "project"
	}
	return name
}

func titleCase(s string) string {
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// Gap records a product moment the feature map has no section for, as a note for the
// verify skill's owner in .verilex/gaps/. verilex never edits a verify skill.
func Gap(p dictionary.Project, description string) (string, error) {
	description = strings.Join(strings.Fields(description), " ")
	if description == "" {
		return "", fmt.Errorf("describe the product moment the feature map lacks")
	}
	dir := filepath.Join(p.Dir(), "gaps")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	stamp := time.Now().UTC()
	base := stamp.Format("20060102-150405") + "-" + slug(description)
	note := fmt.Sprintf("# Gap: %s\n\nRecorded %s by `verilex gap`. The verify skill's feature map has no section for this product moment.\n\nFor the verify skill's owner: add a section for it, then a word can implement it with\n`verilex new <word> --implements <skill>/<file>#<section>`. verilex never edits a verify skill.\n", description, stamp.Format(time.RFC3339))
	for i := 1; ; i++ {
		path := filepath.Join(dir, base+".md")
		if i > 1 {
			path = filepath.Join(dir, fmt.Sprintf("%s-%d.md", base, i))
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = file.WriteString(note)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		return path, err
	}
}

func slug(text string) string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	result := ""
	for _, w := range words {
		if len(result)+len(w) > 48 {
			break
		}
		if result != "" {
			result += "-"
		}
		result += w
	}
	if result == "" {
		return "gap"
	}
	return result
}
