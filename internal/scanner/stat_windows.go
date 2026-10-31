package scanner

import (
	"io/fs"
	"syscall"
)

// devIno identifies a file by volume serial number and file index, the Windows
// equivalent of device and inode. FileInfo does not carry them, so the file is
// opened with no access rights, which is enough to ask for its attributes.
func devIno(path string, _ fs.FileInfo) (dev, ino, nlink uint64) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, 0
	}
	h, err := syscall.CreateFile(p, 0,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, 0, 0
	}
	defer syscall.CloseHandle(h)
	var info syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &info) != nil {
		return 0, 0, 0
	}
	return uint64(info.VolumeSerialNumber), uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow), uint64(info.NumberOfLinks)
}
