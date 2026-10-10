package e2e_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestProof_AcceptedIntent verifies that a setsid child is terminated at budget end
// and on brain exit on the current platform.
func TestProof_AcceptedIntent(t *testing.T) {
	t.Logf("Testing accepted intent on %s/%s", runtime.GOOS, runtime.GOARCH)
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
			start := time.Now()
			run := launchKept(t, dir, fake, brain, "--intent", "prove the store opens", "--claim", "store-opened", "--harness", "stub", "--model", "stub")
			elapsed := time.Since(start)
			t.Logf("[%s] launchKept returned in %v with exit code %d", tc.name, elapsed, run.code)
			if run.code != tc.exit {
				t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", run.code, tc.exit, run.stdout, run.stderr)
			}
			home := filepath.Join(run.root, "brain", "home")
			if _, err := os.Stat(filepath.Join(home, "locked")); err != nil {
				t.Fatalf("the setsid child never took its lock: %v", err)
			}
			if !lockFrees(t, filepath.Join(home, "held"), 5*time.Second) {
				t.Fatal("the setsid child still holds its lock after launcher returned")
			}
			t.Logf("[%s] PASSED: setsid child stopped promptly, lock freed", tc.name)
		})
	}
}

// TestProof_R1_SyscallDuration benchmarks the duration of two passes of 99,998 kill
// syscalls on the current platform to evaluate suspicion R1.
func TestProof_R1_SyscallDuration(t *testing.T) {
	t.Logf("Platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	const pidMax = 99999
	passes := 2
	start := time.Now()
	for p := 0; p < passes; p++ {
		for pid := 2; pid <= pidMax; pid++ {
			syscall.Kill(pid, 0)
		}
	}
	duration := time.Since(start)
	t.Logf("R1 benchmark: %d passes of 99,998 kill syscalls took %v (average %v per pass)",
		passes, duration, duration/time.Duration(passes))
	if duration > 2*time.Second {
		t.Logf("R1 WARNING: Syscall duration exceeds 2.0s stopWait!")
	} else {
		t.Logf("R1 RESULT: Syscall duration is well under 2.0s stopWait bound (%v < 2.0s)", duration)
	}
}

// TestProof_R2_EndSandboxLoop evaluates suspicion R2 regarding loop termination and bounds.
func TestProof_R2_EndSandboxLoop(t *testing.T) {
	t.Logf("Platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	content, err := os.ReadFile("internal/sandbox/init_darwin.go")
	if err != nil {
		t.Skip("init_darwin.go not accessible")
	}
	s := string(content)
	hasLoop := strings.Contains(s, "for ended := true; ended; {")
	hasMaxIter := strings.Contains(s, "iter") || strings.Contains(s, "timeout") || strings.Contains(s, "deadline")
	t.Logf("endSandbox() has outer loop 'for ended := true; ended;': %v", hasLoop)
	t.Logf("endSandbox() has iteration limit or timeout: %v", hasMaxIter)
	if hasLoop && !hasMaxIter {
		t.Logf("R2 CODE PROOF: endSandbox() lacks iteration limit or timeout, looping as long as syscall.Kill returns nil.")
	}
}

// TestProof_R3_LinuxSigterm verifies suspicion R3: delivering SIGTERM to sandbox-init
// on Linux causes sandbox-init to hang until the child brain exits on its own.
func TestProof_R3_LinuxSigterm(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("R3 is specific to non-Darwin (Linux) platforms")
	}
	r3, w3, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	r4, w4, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w3.WriteString("{}\n")
	w3.Close()

	cmd := exec.Command(agentBin, "sandbox-init", "--", "sleep", "3")
	cmd.ExtraFiles = []*os.File{r3, w4}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	r3.Close()
	w4.Close()

	buf := make([]byte, 64)
	n, _ := r4.Read(buf)
	r4.Close()
	t.Logf("sandbox-init ready output: %q", string(buf[:n]))

	time.Sleep(200 * time.Millisecond)
	t.Log("Sending SIGTERM to sandbox-init...")
	start := time.Now()
	cmd.Process.Signal(syscall.SIGTERM)

	waitErr := cmd.Wait()
	elapsed := time.Since(start)
	t.Logf("sandbox-init exit error: %v, elapsed: %v", waitErr, elapsed)
	if elapsed >= 2500*time.Millisecond {
		t.Logf("R3 REPRODUCED: sandbox-init intercepted SIGTERM, did not kill brain, and hung for %v until sleep 3 completed!", elapsed)
	} else {
		t.Logf("sandbox-init exited in %v", elapsed)
	}
}

// TestProof_R4_BrainKillsSandboxInit tests whether an untrusted brain killing its parent
// ($PPID) bypasses sandbox cleanup of setsid children.
func TestProof_R4_BrainKillsSandboxInit(t *testing.T) {
	t.Logf("Platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	escape := `python3 -c '
import fcntl, os, time
os.setsid()
held = open(os.path.join(os.environ["HOME"], "held"), "w")
fcntl.flock(held, fcntl.LOCK_EX)
open(os.path.join(os.environ["HOME"], "locked"), "w").close()
open(os.path.join(os.environ["HOME"], "child_pid"), "w").write(str(os.getpid()))
time.sleep(60)' &
while [ ! -e "$HOME/locked" ]; do sleep 0.05; done
`
	dir := t.TempDir()
	ticket := `{"intent":"prove the store opens","harness":"stub","model":"stub","time_budget":"5s"}`
	fake := writeFake(t, dir, fakeFiles{ticket: []byte(ticket + "\n"), run: claimRun("green", "r1", []string{"store-opened"}, nil)})
	brain := writeBrain(t, dir, "#!/bin/sh\n"+escape+"kill -9 $PPID\nsleep 60\n")
	start := time.Now()
	run := launchKept(t, dir, fake, brain, "--intent", "prove the store opens", "--claim", "store-opened", "--harness", "stub", "--model", "stub")
	elapsed := time.Since(start)
	t.Logf("launchKept finished in %v with exit code %d", elapsed, run.code)
	home := filepath.Join(run.root, "brain", "home")
	freed := lockFrees(t, filepath.Join(home, "held"), 3*time.Second)
	pidBytes, _ := os.ReadFile(filepath.Join(home, "child_pid"))
	pidStr := strings.TrimSpace(string(pidBytes))
	t.Logf("R4 setsid child PID: %s", pidStr)
	t.Logf("R4 result on %s: lock freed = %v", runtime.GOOS, freed)
	if !freed {
		t.Logf("R4 REPRODUCED on %s: setsid child outlived the run because brain killed sandbox-init parent!", runtime.GOOS)
		if pidStr != "" {
			var pid int
			if _, err := fmt.Sscanf(pidStr, "%d", &pid); err == nil && pid > 0 {
				syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	} else {
		t.Logf("R4 NOT REPRODUCED on %s: setsid child was terminated despite brain killing $PPID.", runtime.GOOS)
	}
}

// TestProof_R5_DarwinProfileSignal verifies whether darwin.go profile explicitly allows same-sandbox signals.
func TestProof_R5_DarwinProfileSignal(t *testing.T) {
	content, err := os.ReadFile("internal/sandbox/darwin.go")
	if err != nil {
		t.Fatalf("could not read darwin.go: %v", err)
	}
	s := string(content)
	hasSignal := strings.Contains(s, "(allow signal (target same-sandbox))")
	hasProcessInfo := strings.Contains(s, "(allow process-info* (target same-sandbox))")
	t.Logf("darwin.go contains (allow signal (target same-sandbox)): %v", hasSignal)
	t.Logf("darwin.go contains (allow process-info* (target same-sandbox)): %v", hasProcessInfo)
	if hasSignal {
		t.Log("R5 DISPROVED: sandbox profile explicitly permits same-sandbox signals.")
	} else {
		t.Fatal("R5 REPRODUCED: sandbox profile missing same-sandbox signal permission!")
	}
}
