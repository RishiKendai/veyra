//go:build !unix && !windows

package scanner

import "io/fs"

// No device/inode on this platform: hard links are not detected.
func devIno(string, fs.FileInfo) (dev, ino, nlink uint64) { return 0, 0, 0 }
