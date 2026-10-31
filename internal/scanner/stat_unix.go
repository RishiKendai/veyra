//go:build unix && !darwin

package scanner

import (
	"io/fs"
	"syscall"
)

// devIno identifies a file by device and inode so hard links to one file can
// be recognised.
func devIno(_ string, fi fs.FileInfo) (dev, ino, nlink uint64) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Dev), uint64(st.Ino), uint64(st.Nlink)
	}
	return 0, 0, 0
}
