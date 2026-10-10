//go:build darwin

package sandbox

import (
	"os"
	"syscall"
	"time"
)

// pidMax is the largest pid macOS hands out (PID_MAX in xnu).
const pidMax = 99999

// endWait bounds endSandbox. A pass over every pid takes about 40ms; the bound is reached only
// when a process the sweep reached does not go. It stays under the launcher's stopWait.
const endWait = time.Second

// endSandbox ends every other process in this sandbox. macOS has no call that lists a sandbox's
// processes, but the profile lets a process signal only processes in its own sandbox, so a kill
// sent to every pid reaches exactly those, whatever session they moved to.
func endSandbox() {
	sweep(func(pid int) bool { return syscall.Kill(pid, syscall.SIGKILL) == nil }, os.Getpid(), pidMax, time.Now().Add(endWait))
}

// contained checks that the brain cannot signal a process outside its sandbox: endSandbox relies
// on that, and the launcher, this process's parent, is outside it.
func contained() string {
	if err := syscall.Kill(os.Getppid(), 0); err != syscall.EPERM {
		return "the brain could signal a process outside its sandbox"
	}
	return ""
}
