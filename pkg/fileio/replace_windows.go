package fileio

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileRenameInfo mirrors FILE_RENAME_INFO; Go's field alignment matches C's
// on 386, amd64, and arm64. Flags occupies the ReplaceIfExists/Flags union.
type fileRenameInfo struct {
	Flags          uint32
	RootDirectory  windows.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

// Replace renames src over dst with POSIX semantics, so the replacement
// succeeds while readers hold dst open with FILE_SHARE_DELETE (OpenRead).
// os.Rename uses MoveFileEx, which fails with access denied in that case.
func Replace(src, dst string) error {
	err := renamePOSIX(src, dst)
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) ||
		errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
		// Pre-1607 Windows, or a file system or redirector without POSIX rename.
		return os.Rename(src, dst)
	}
	if err != nil {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: err}
	}
	return nil
}

func renamePOSIX(src, dst string) error {
	full, err := windows.FullPath(dst)
	if err != nil {
		return err
	}
	name, err := windows.UTF16FromString(readPath(full))
	if err != nil {
		return err
	}
	srcName, err := windows.UTF16PtrFromString(readPath(src))
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(srcName, windows.DELETE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)

	// FileNameLength counts bytes without the NUL; the buffer keeps the NUL.
	offset := unsafe.Offsetof(fileRenameInfo{}.FileName)
	size := max(offset+uintptr(len(name))*2, unsafe.Sizeof(fileRenameInfo{}))
	// Allocate as uint64s so the buffer satisfies the struct's alignment.
	buf := make([]uint64, (size+7)/8)
	info := (*fileRenameInfo)(unsafe.Pointer(&buf[0]))
	info.Flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS
	info.FileNameLength = uint32((len(name) - 1) * 2)
	copy(unsafe.Slice(&info.FileName[0], len(name)), name)
	return windows.SetFileInformationByHandle(handle, windows.FileRenameInfoEx,
		(*byte)(unsafe.Pointer(info)), uint32(size))
}
