package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A child that leaves the brain's session with setsid still ends with the run, whether the time
// budget stops the brain or the brain exits on its own. The child holds a lock on a file in the
// brain's home for as long as it lives, so the lock comes free exactly when it is gone.
func TestNoChildOfTheBrainOutlivesTheRun(t *testing.T) {
	escape := `python3 -c '
import fcntl, os, time
os.setsid()
held = open(os.path.join(os.environ["HOME"], "held"), "w")
fcntl.flock(held, fcntl.LOCK_EX)
open(os.path.join(os.environ["HOME"], "locked"), "w").close()
time.sleep(60)' &
while [ ! -e "$HOME/locked" ]; do sleep 0.05; done
`
	for _, tc := range []struct {
		name, body, budget string
		exit               int
	}{
		{"budget ends", escape + "sleep 60\n", "5s", 2},
		{"brain exits", escape + "verilex run --named store-opened > /dev/null\n", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ticket := `{"intent":"prove the store opens","harness":"stub","model":"stub","time_budget":"` + tc.budget + `"}`
			fake := writeFake(t, dir, fakeFiles{ticket: []byte(ticket + "\n"), run: claimRun("green", "r1", []string{"store-opened"}, nil)})
			brain := writeBrain(t, dir, "#!/bin/sh\n"+tc.body)
			run := launchKept(t, dir, fake, brain, "--intent", "prove the store opens", "--claim", "store-opened", "--harness", "stub", "--model", "stub")
			if run.code != tc.exit {
				t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", run.code, tc.exit, run.stdout, run.stderr)
			}
			home := filepath.Join(run.root, "brain", "home")
			if _, err := os.Stat(filepath.Join(home, "locked")); err != nil {
				t.Fatalf("the setsid child never took its lock, so the run proves nothing: %v\nstderr: %s", err, run.stderr)
			}
			if !lockFrees(t, filepath.Join(home, "held"), 5*time.Second) {
				t.Fatal("the setsid child still holds its lock after the launcher returned: it outlived the run")
			}
			if tc.exit == 2 && !strings.Contains(run.stderr, "inconclusive: the time budget ended") {
				t.Fatalf("stderr: %s", run.stderr)
			}
		})
	}
}

// lockFrees reports whether an exclusive lock on path can be taken within wait.
func lockFrees(t *testing.T, path string, wait time.Duration) bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for deadline := time.Now().Add(wait); ; {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return true
		}
		if err != syscall.EWOULDBLOCK {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}
