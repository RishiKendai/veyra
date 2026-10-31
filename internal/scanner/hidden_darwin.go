package scanner

import (
	"io/fs"
	"syscall"
)

// ufHidden is the BSD "hidden" file flag (chflags hidden), which Finder also
// honours. It marks files that have no leading dot.
const ufHidden = 0x8000

func hiddenAttr(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Flags&ufHidden != 0
}
