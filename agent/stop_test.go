package e2e_test

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A child that leaves the brain's session with setsid still ends with the run, whether the time
// budget stops the brain or the brain exits on its own. On Linux it ends even when the brain kills
// the sandbox's first process; on macOS that case is a documented limit (docs/sandbox.md). The
// child holds a lock on a file in the brain's home for as long as it lives, so the lock comes free
// exactly when it is gone.
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
		name, body, budget, stderr string
		exit                       int
		linuxOnly                  bool
	}{
		{"budget ends", escape + "sleep 60\n", "5s", "inconclusive: the time budget ended", 2, false},
		{"brain exits", escape + "verilex run --named store-opened > /dev/null\n", "", "", 0, false},
		{"brain kills the sandbox's first process", escape + "kill -9 $PPID\nsleep 60\n", "5s", "inconclusive: brain exited: exit status 137", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.linuxOnly && runtime.GOOS != "linux" {
				t.Skip("on macOS a brain that kills the sandbox's first process can leave a setsid child behind: docs/sandbox.md lists the limit and its workarounds")
			}
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
			if !strings.Contains(run.stderr, tc.stderr) {
				t.Fatalf("stderr: %s", run.stderr)
			}
		})
	}
}

// The launcher's SIGTERM asks the sandbox's first process to end the sandbox: it ends the brain
// at once instead of waiting for it. On macOS the same process refuses to start a brain where it
// can signal a process outside its sandbox, as it can when no sandbox holds it, because there
// ending the sandbox signals every pid.
func TestSandboxInitEndsTheBrainWhenAskedToStop(t *testing.T) {
	cfgRead, cfgWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(agentBin, "sandbox-init", "--", "sleep", "30")
	cmd.ExtraFiles = []*os.File{cfgRead, readyWrite}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill() })
	cfgRead.Close()
	readyWrite.Close()
	cfgWrite.WriteString("{}\n")
	cfgWrite.Close()
	line, _ := bufio.NewReader(readyRead).ReadString('\n')
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	if runtime.GOOS == "darwin" {
		if want := "fail: the brain could signal a process outside its sandbox\n"; line != want {
			t.Fatalf("sandbox-init outside a sandbox reported %q, want %q", line, want)
		}
		return
	}
	if line != "ok\n" {
		t.Fatalf("sandbox-init reported %q, want ok", line)
	}
	cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("sandbox-init still waits for the brain 5s after SIGTERM")
	}
	if code := cmd.ProcessState.ExitCode(); code != 128+int(syscall.SIGKILL) {
		t.Fatalf("sandbox-init exited %d, want %d for a brain ended by SIGKILL", code, 128+int(syscall.SIGKILL))
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
