package scanner

import (
	"io/fs"
	"syscall"
)

// hiddenAttr reports the Windows Hidden or System attribute.
func hiddenAttr(fi fs.FileInfo) bool {
	d, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	return ok && d.FileAttributes&(syscall.FILE_ATTRIBUTE_HIDDEN|syscall.FILE_ATTRIBUTE_SYSTEM) != 0
}
