//go:build unix

package ledger

import (
	"os"
	"syscall"
)

const (
	lockShared    = syscall.LOCK_SH
	lockExclusive = syscall.LOCK_EX
)

// lock holds an advisory lock so concurrent runs never interleave ledger writes.
func lock(path string, mode int) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), mode); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
