//go:build darwin

package sandbox

import (
	"os"
	"syscall"
)

// pidMax is the largest pid macOS hands out (PID_MAX in xnu).
const pidMax = 99999

// endSandbox ends every other process in this sandbox. macOS has no call that lists a sandbox's
// processes, but the profile lets a process signal only processes in its own sandbox, so a kill
// sent to every pid reaches exactly those, whatever session they moved to. A pass repeats while
// it still ended a process, so a child forked during a pass ends in the next.
func endSandbox() {
	self := os.Getpid()
	for ended := true; ended; {
		ended = false
		for pid := 2; pid <= pidMax; pid++ {
			if pid != self && syscall.Kill(pid, syscall.SIGKILL) == nil {
				ended = true
			}
		}
	}
}

// contained checks that the brain cannot signal a process outside its sandbox: endSandbox relies
// on that, and the launcher, this process's parent, is outside it.
func contained() string {
	if err := syscall.Kill(os.Getppid(), 0); err != syscall.EPERM {
		return "the brain could signal a process outside its sandbox"
	}
	return ""
}
