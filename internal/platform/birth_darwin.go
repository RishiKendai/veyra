package platform

import (
	"os"
	"syscall"
	"time"
)

// BirthTime returns the filesystem creation time, or the zero time when the
// filesystem does not report one.
func BirthTime(fi os.FileInfo) time.Time {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Birthtimespec.Sec == 0 {
		return time.Time{}
	}
	return time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec)
}
