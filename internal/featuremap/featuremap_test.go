package featuremap_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/DereKk8/verilex/internal/featuremap"
)

const feature = "# Store\n\nIntro.\n\n## Open a store\n\nRun `open`.\n\n```sh\n# not a heading\n```\n\n### Gotchas\n\nOpening twice exits 1.\n\n## Close a store\n\nRun `close`.\n"

func TestResolveSelectsHeadingSectionsAndFeatureIDs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skills", "verify-x", "features")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "store.md"), []byte(feature), 0644); err != nil {
		t.Fatal(err)
	}
	dirs := []string{".missing", "skills"}
	for ref, want := range map[string]string{
		"verify-x/features/store.md#open-a-store":  "## Open a store\n\nRun `open`.\n\n```sh\n# not a heading\n```\n\n### Gotchas\n\nOpening twice exits 1.",
		"verify-x/features/store.md#close-a-store": "## Close a store\n\nRun `close`.",
		"verify-x/features/store.md#close":         feature[:len(feature)-1],
		"verify-x/features/store.md":               feature[:len(feature)-1],
	} {
		section, err := featuremap.Resolve(root, dirs, ref)
		if err != nil || section.Text != want || len(section.Hash) != 64 || section.File != filepath.Join("skills", "verify-x", "features", "store.md") {
			t.Fatalf("%s: %#v %v", ref, section, err)
		}
	}
	for _, ref := range []string{"verify-x/features/store.md#delete", "verify-x/features/other.md#open", "../store.md#open", "#open"} {
		var missing *featuremap.MissingError
		if _, err := featuremap.Resolve(root, dirs, ref); !errors.As(err, &missing) {
			t.Fatalf("%s resolved: %v", ref, err)
		}
	}
}
