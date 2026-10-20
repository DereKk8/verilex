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

// A launcher ended by SIGTERM, SIGINT or SIGHUP while its brain runs leaves no run directory
// behind and prints no verdict: the run is inconclusive.
func TestSignalLeavesNoRunDirectory(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP} {
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

// A signal while verilex answers a lookup before the brain starts ends the launcher at once and
// says it was interrupted, never that the time budget ended.
func TestSignalDuringLookupIsInterrupted(t *testing.T) {
	for _, sub := range []string{"ticket", "index"} {
		t.Run(sub, func(t *testing.T) {
			dir := t.TempDir()
			fake := writeFake(t, dir, fakeFiles{ticket: ticketJSON("stub", "stub")})
			slow := filepath.Join(dir, "slow-verilex")
			body := "#!/bin/sh\ntouch \"" + dir + "/asked\"\nfor a in \"$@\"; do [ \"$a\" = " + sub + " ] && sleep 30; done\nexec " + fake + " \"$@\"\n"
			if err := os.WriteFile(slow, []byte(body), 0o700); err != nil {
				t.Fatal(err)
			}
			args := []string{"--intent", "prove the store opens", "--harness", "stub", "--model", "stub"}
			if sub == "ticket" {
				ticket := filepath.Join(dir, "ticket.yaml")
				if err := os.WriteFile(ticket, []byte("intent: prove the store opens\nharness: stub\nmodel: stub\neffort: low\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				args = []string{"--ticket", ticket}
			}
			argv, err := launcherArgs(dir, slow, writeBrain(t, dir, "#!/bin/sh\nexit 0\n"), args...)
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
			for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
				if _, err = os.Stat(filepath.Join(dir, "asked")); err == nil {
					break
				}
			}
			time.Sleep(300 * time.Millisecond)
			cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-exited:
			case <-time.After(5 * time.Second):
				t.Fatal("the launcher kept waiting for verilex after the signal")
			}
			if code := cmd.ProcessState.ExitCode(); code != 2 || stdout.String() != "" {
				t.Fatalf("exit %d stdout %q stderr %s", code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), "interrupted") || strings.Contains(stderr.String(), "time budget") {
				t.Fatalf("stderr %q", stderr.String())
			}
			if left, _ := filepath.Glob(filepath.Join(tmp, "verilex-agent-*")); len(left) != 0 {
				t.Fatalf("run directories left behind: %v", left)
			}
		})
	}
}
