package cli

import (
	"strings"
	"testing"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/index"
	"github.com/DereKk8/verilex/internal/runner"
	"github.com/DereKk8/verilex/internal/verdict"
)

// Rule (issue #48): a claim run is green only when it proved every selected claim. No product
// route leaves a selected claim unrun since #47, so the test forces that state: a green run whose
// only step proves store-opened, while item-added was selected too. It must end inconclusive, exit
// 2, with the reason in the JSON the run record and the launcher read; a red found on the way stays
// red.
func TestAClaimRunWithAnUnrunSelectedClaimIsNotGreen(t *testing.T) {
	project, err := dictionary.FindProject("../../tests/fixtures/tally")
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.Build(project)
	if err != nil {
		t.Fatal(err)
	}
	refuse := func(err error) int { t.Fatal(err); return 2 }
	for _, c := range []struct {
		ran  verdict.Verdict
		exit int
		head string
		json string
	}{
		{verdict.Green, 2, "inconclusive: 1 green; run r1\n", "\"verdict\": \"inconclusive\",\n  \"reason\": \"selected claim item-added was not run\","},
		{verdict.Red, 1, "red: 1 red; run r1\n", "\"verdict\": \"red\","},
	} {
		ran := c.ran
		record := runner.Record{Run: "r1", Verdict: &ran, Words: []runner.WordRecord{{Word: "store-open", Verdict: ran}}}
		if err = attachCoverage(&record, ix, nil, []string{"item-added", "store-opened"}, nil, nil); err != nil {
			t.Fatal(err)
		}
		var out, stderr strings.Builder
		if code := writeRun(record, nil, false, &out, &stderr, refuse); code != c.exit || !strings.HasPrefix(out.String(), c.head) || stderr.Len() > 0 ||
			!strings.Contains(out.String(), "\n  inconclusive  item-added: not run\n    next: verilex run --fresh --claim 'item-added'\n") {
			t.Fatalf("%s run: exit %d, want %d\n%s%s", ran, code, c.exit, out.String(), stderr.String())
		}
		out.Reset()
		if code := writeRun(record, nil, true, &out, &stderr, refuse); code != c.exit || !strings.Contains(out.String(), c.json) {
			t.Fatalf("%s run --json: exit %d, want %d and %s\n%s", ran, code, c.exit, c.json, out.String())
		}
	}
}
