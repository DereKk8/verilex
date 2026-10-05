//go:build unix

package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// execute isolates a process group so a timeout also stops the word's children.
func execute(argv []string, evidence string, env []string, timeout int, stdin string) (*int, error) {
	if err := os.MkdirAll(evidence, 0700); err != nil {
		return nil, err
	}
	out, err := os.Create(filepath.Join(evidence, "stdout"))
	if err != nil {
		return nil, err
	}
	defer out.Close()
	stderr, err := os.Create(filepath.Join(evidence, "stderr"))
	if err != nil {
		return nil, err
	}
	defer stderr.Close()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = out
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	timer := time.NewTimer(time.Duration(timeout) * time.Second)
	defer timer.Stop()
	var code *int
	select {
	case err = <-finished:
		if err != nil {
			if _, ok := err.(*exec.ExitError); !ok {
				return nil, err
			}
		}
		status := cmd.ProcessState.Sys().(syscall.WaitStatus)
		exit := status.ExitStatus()
		if status.Signaled() {
			exit = -int(status.Signal())
		}
		code = &exit
	case <-timer.C:
		err = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err != nil && err != syscall.ESRCH {
			return nil, err
		}
		<-finished
	}
	text := "timeout\n"
	if code != nil {
		text = fmt.Sprintf("%d\n", *code)
	}
	if err = os.WriteFile(filepath.Join(evidence, "exit"), []byte(text), 0600); err != nil {
		return nil, err
	}
	return code, nil
}

// lock holds an exclusive advisory lock on path until the returned function runs.
func lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
