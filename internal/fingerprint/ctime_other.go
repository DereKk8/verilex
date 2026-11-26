//go:build !linux && !darwin

package fingerprint

import "io/fs"

func statIdentity(fs.FileInfo) (int64, uint64, uint64, bool) {
	return 0, 0, 0, false
}
