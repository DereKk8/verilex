package cli

import (
	"io"
	"testing"

	"github.com/DereKk8/verilex/internal/runner"
	"github.com/DereKk8/verilex/internal/usage"
)

func TestUsageRemovesAStartRefusedDuringExecution(t *testing.T) {
	store := usage.Store{Dir: t.TempDir()}
	other := &usageCapture{store: store}
	other.begin(runner.Record{Run: "other"}, io.Discard)
	capture := &usageCapture{store: store}
	capture.begin(runner.Record{Run: "refused-continue"}, io.Discard)
	// Execute returns an empty record when another run took the kept instance
	// after Decide. The start has already been published at that point.
	capture.finish(runner.Record{}, io.Discard)
	report, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if report.Totals.Runs != 1 || report.Entries[0].Record.Run != "other" {
		t.Fatalf("refusal retained or another run removed: %+v", report)
	}
}
