// Package curation moves a word through its lifecycle: a word starts provisional, earns uses
// in runs, is proposed to an outside curator, and is admitted only by that curator's recorded
// verdict. Package lifecycle reads the resulting state. verilex never calls a model and never
// edits a verify skill.
package curation

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/featuremap"
)

// sections resolves the feature-map sections a word without a claim implements; a word that
// proves a claim is anchored through its claim instead.
func sections(p dictionary.Project, w dictionary.Word) ([]featuremap.Section, error) {
	result := make([]featuremap.Section, 0, len(w.Implements))
	if w.Claim != nil {
		return result, nil
	}
	for _, ref := range w.Implements {
		section, err := featuremap.Resolve(p.Root, p.SkillDirs, ref)
		if err != nil {
			return nil, err
		}
		result = append(result, section)
	}
	return result, nil
}

func find(words []dictionary.Word, name string) (dictionary.Word, error) {
	for _, w := range words {
		if w.Name == name {
			return w, nil
		}
	}
	return dictionary.Word{}, fmt.Errorf("unknown word %q; `verilex words` lists the dictionary", name)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }
