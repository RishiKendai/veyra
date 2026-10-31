//go:build !darwin && !windows

package scanner

import "io/fs"

// Other systems hide files by name only.
func hiddenAttr(fs.FileInfo) bool { return false }
