//go:build !darwin

package scanner

import "io/fs"

func devIno(fs.FileInfo) (dev, ino, nlink uint64) { return 0, 0, 0 }
