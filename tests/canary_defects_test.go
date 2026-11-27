package tests

import (
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/DereKk8/verilex/internal/dictionary"
)

// The canary's launch plants each defect a canary claim declares by replacing one literal line of
// the source. Once a change rewrites that line, the defect no longer applies: the trial that plants
// it is inconclusive, so an edit that leaves the canary word drift-suspect cannot be onboarded again
// (#101). Every anchor must match its source exactly once, planting it must change the source and
// leave Go that still parses, and every defect a canary claim declares must be one the launch plants.
func TestCanaryDefectAnchorsMatchTheirSource(t *testing.T) {
	launch := read(t, filepath.Join("..", ".verilex", "frame", "launch"))
	arms := regexp.MustCompile(`(?m)^\s+([a-z-]+)\)\s+(f=.*?)\s*;;\s*$`).FindAllStringSubmatch(launch, -1)
	if len(arms) == 0 {
		t.Fatal(".verilex/frame/launch plants no defect")
	}
	planted := []string{}
	for _, arm := range arms {
		name := arm[1]
		planted = append(planted, name)
		// bash reads the arm's quoting, as the launch does.
		out, err := exec.Command("bash", "-c", arm[2]+`; printf '%s\0%s\0%s' "$f" "$from" "$to"`).Output()
		if err != nil {
			t.Fatalf("planted defect %s: %v", name, err)
		}
		fields := strings.Split(string(out), "\x00")
		if len(fields) != 3 || fields[0] == "" || fields[1] == "" {
			t.Fatalf("planted defect %s names no file and line: %q", name, arm[2])
		}
		file, from, to := fields[0], fields[1], fields[2]
		source := read(t, filepath.Join("..", filepath.FromSlash(file)))
		switch count := strings.Count(source, from); {
		case count == 0:
			t.Errorf("planted defect %s no longer applies: %s does not hold %q", name, file, from)
			continue
		case count > 1:
			t.Errorf("planted defect %s is ambiguous: %s holds %q %d times", name, file, from, count)
		}
		if from == to {
			t.Errorf("planted defect %s changes nothing in %s", name, file)
		}
		if strings.HasSuffix(file, ".go") {
			if _, err := parser.ParseFile(token.NewFileSet(), file, strings.Replace(source, from, to, 1), parser.SkipObjectResolution); err != nil {
				t.Errorf("planted defect %s leaves %s that does not parse: %v", name, file, err)
			}
		}
	}
	paths, err := filepath.Glob(filepath.Join("..", ".verilex", "claims", "*.yaml"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no canary claims: %v", err)
	}
	for _, path := range paths {
		c, err := dictionary.ReadClaim(path)
		if err != nil {
			t.Fatal(err)
		}
		for name, defect := range c.Defects {
			if value := defect.Env["CANARY_DEFECT"]; !slices.Contains(planted, value) {
				t.Errorf("claim %s declares planted defect %s as CANARY_DEFECT=%q, which the launch does not plant", c.Name, name, value)
			}
		}
	}
}
