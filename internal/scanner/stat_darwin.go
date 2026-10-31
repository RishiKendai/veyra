package scanner

import (
	"io/fs"
	"syscall"
)

func devIno(_ string, fi fs.FileInfo) (dev, ino, nlink uint64) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(uint32(st.Dev)), st.Ino, uint64(st.Nlink)
	}
	return 0, 0, 0
}
