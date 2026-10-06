//go:build unix

package grouping

import (
	"os"
	"syscall"
)

// lock holds an exclusive advisory lock on a directory until the returned function runs. The
// grouping file is replaced on every save, so the lock sits on the directory that holds it.
func lock(dir string) (func(), error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
