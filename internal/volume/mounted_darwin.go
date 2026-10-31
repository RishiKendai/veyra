package volume

import (
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// mounted reports whether root is still on a mounted volume. A dead mount can
// make system calls block, so the check runs with a timeout and counts a
// timeout as "gone".
func mounted(root string) bool {
	res := make(chan bool, 1)
	go func() {
		var st unix.Statfs_t
		if err := unix.Statfs(root, &st); err != nil {
			res <- false
			return
		}
		// If the volume was unmounted, the path resolves to the boot volume.
		if !strings.HasPrefix(unix.ByteSliceToString(st.Mntonname[:]), "/Volumes/") {
			res <- false
			return
		}
		_, err := os.Stat(root)
		res <- err == nil
	}()
	select {
	case ok := <-res:
		return ok
	case <-time.After(3 * time.Second):
		return false
	}
}
