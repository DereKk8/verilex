package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrainCannotSupplyCallerOwnedUsageData(t *testing.T) {
	dir, product, ledger := changedProduct(t)
	usageLedger := filepath.Join(dir, "usage-ledger")
	brain := writeBrain(t, dir, `#!/bin/sh
log="$HOME/refusals"
verilex run --claim store-opened --tokens 999999 2>> "$log"; echo "exit $?" >> "$log"
verilex run --claim store-opened --tokens=999999 2>> "$log"; echo "exit $?" >> "$log"
verilex run --claim store-opened --estimated --tokens 999999 2>> "$log"; echo "exit $?" >> "$log"
verilex run --claim store-opened --change forged-by-brain 2>> "$log"; echo "exit $?" >> "$log"
verilex run --claim store-opened --change=forged-by-brain 2>> "$log"; echo "exit $?" >> "$log"
verilex run --claim store-opened > "$HOME/accepted"
`)
	run := launchKeptEnv(t, dir, coreBin, brain, []string{"VERILEX_USAGE_LEDGER=" + usageLedger},
		"--project", product, "--ledger", ledger, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub")
	var doc runDoc
	if run.code != 0 || json.Unmarshal([]byte(run.stdout), &doc) != nil || doc.Verdict != "green" {
		t.Fatalf("exit %d\n%s\n%s", run.code, run.stdout, run.stderr)
	}
	text := run.home(t, "refusals")
	for _, name := range []string{"--tokens", "--estimated", "--change"} {
		if !strings.Contains(text, name+" is not available to the brain") {
			t.Fatalf("caller-owned %s accepted: %s", name, text)
		}
	}
	if strings.Count(text, "exit 2") != 5 {
		t.Fatalf("expected five refusals: %s", text)
	}
	entries, err := os.ReadDir(usageLedger)
	if err != nil || len(entries) != 1 {
		t.Fatalf("usage entries %v: %v", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(usageLedger, doc.Run, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entry map[string]any
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	if entry["tokens"] != nil || entry["change"] != nil || entry["record"].(map[string]any)["verdict"] != "green" {
		t.Fatalf("brain supplied caller data: %s", data)
	}
}
