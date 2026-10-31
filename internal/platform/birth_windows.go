package platform

import (
	"os"
	"syscall"
	"time"
)

// BirthTime returns the filesystem creation time, or the zero time when the
// filesystem does not report one.
func BirthTime(fi os.FileInfo) time.Time {
	d, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	if !ok || d.CreationTime.Nanoseconds() == 0 {
		return time.Time{}
	}
	return time.Unix(0, d.CreationTime.Nanoseconds())
}
