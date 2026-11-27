package judge

import (
	"io/fs"
	"syscall"
)

func statIdentity(info fs.FileInfo) (ctime int64, ino, dev uint64, ok bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, 0, false
	}
	return st.Ctimespec.Nano(), uint64(st.Ino), uint64(st.Dev), true
}
