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

// A launcher started under nohup keeps SIGHUP ignored: a closed terminal does not end its run.
func TestNohupRunSurvivesHangup(t *testing.T) {
	dir := t.TempDir()
	fake := writeFake(t, dir, fakeFiles{run: claimRun("green", "r1", []string{"store-opened"}, nil), ticket: ticketJSON("stub", "stub")})
	brain := writeBrain(t, dir, holdBrain+"verilex run --json --named store-opened\n")
	argv, err := launcherArgs(dir, fake, brain, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub")
	if err != nil {
		t.Fatal(err)
	}
	tmp := runTemp(t)
	// nohup starts the launcher with SIGHUP ignored and then runs it in its own process.
	cmd := exec.Command("nohup", append([]string{agentBin}, argv...)...)
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
	var hold []string
	for deadline := time.Now().Add(10 * time.Second); len(hold) != 1 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		hold, _ = filepath.Glob(filepath.Join(tmp, "verilex-agent-*", "brain", "home", "hold"))
	}
	if len(hold) != 1 {
		t.Fatalf("the brain never held\nstderr: %s", stderr.String())
	}
	cmd.Process.Signal(syscall.SIGHUP)
	time.Sleep(100 * time.Millisecond)
	if err = os.WriteFile(filepath.Join(filepath.Dir(hold[0]), "release"), nil, 0o600); err != nil {
		<-exited
		t.Fatalf("the hangup ended the run: exit %d\nstderr: %s", cmd.ProcessState.ExitCode(), stderr.String())
	}
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the launcher did not end after the release")
	}
	if code := cmd.ProcessState.ExitCode(); code != 0 || !strings.Contains(stdout.String(), `"verdict":"green"`) {
		t.Fatalf("exit %d, want 0 and a green verdict\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
}
