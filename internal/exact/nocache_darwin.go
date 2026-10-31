package exact

import (
	"os"

	"golang.org/x/sys/unix"
)

// setNoCache asks the kernel not to keep this file's pages in the unified
// buffer cache and to read ahead sequentially.
func setNoCache(f *os.File) {
	fd := int(f.Fd())
	_, _ = unix.FcntlInt(uintptr(fd), unix.F_NOCACHE, 1)
	_, _ = unix.FcntlInt(uintptr(fd), unix.F_RDAHEAD, 1)
}
