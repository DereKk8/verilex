package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A closed terminal sends SIGHUP twice; the launcher still leaves no run directory behind.
func TestHangupTwiceLeavesNoRunDirectory(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			dir := t.TempDir()
			fake := writeFake(t, dir, fakeFiles{run: claimRun("green", "r1", []string{"store-opened"}, nil), ticket: ticketJSON("stub", "stub")})
			brain := writeBrain(t, dir, holdBrain+"verilex run --json --named store-opened\n")
			argv, err := launcherArgs(dir, fake, brain, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub")
			if err != nil {
				t.Fatal(err)
			}
			tmp := runTemp(t)
			cmd := exec.Command(agentBin, argv...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "TMPDIR="+tmp)
			var stdout, stderr strings.Builder
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			exited := make(chan error, 1)
			go func() { exited <- cmd.Wait() }()
			defer cmd.Process.Kill()
			held := false
			for deadline := time.Now().Add(10 * time.Second); !held && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
				matches, _ := filepath.Glob(filepath.Join(tmp, "verilex-agent-*", "brain", "home", "hold"))
				held = len(matches) == 1
			}
			if !held {
				t.Fatalf("the brain never held\nstderr: %s", stderr.String())
			}
			// A closed terminal: the kernel and then the shell each send SIGHUP.
			cmd.Process.Signal(sig)
			time.Sleep(time.Millisecond)
			cmd.Process.Signal(sig)
			select {
			case <-exited:
			case <-time.After(10 * time.Second):
				t.Fatal("the launcher did not end after the signal")
			}
			if code := cmd.ProcessState.ExitCode(); code != 2 {
				t.Fatalf("exit %d, want 2\nstderr: %s", code, stderr.String())
			}
			if stdout.String() != "" || !strings.Contains(stderr.String(), "interrupted") {
				t.Fatalf("stdout %q stderr %q", stdout.String(), stderr.String())
			}
			if left, _ := filepath.Glob(filepath.Join(tmp, "verilex-agent-*")); len(left) != 0 {
				t.Fatalf("run directories left behind: %v", left)
			}
		})
	}
}
