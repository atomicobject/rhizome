package fileio

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// OpenRead opens a file without blocking cooperative replacement or removal.
// os.Open on Windows shares reads and writes, but not deletion.
func OpenRead(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(readPath(path))
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}

// CreateFile needs an extended path when Windows long-path support is disabled.
func readPath(path string) string {
	normalized := strings.ReplaceAll(path, "/", `\`)
	if strings.HasPrefix(normalized, `\\?\`) || strings.HasPrefix(normalized, `\??\`) || strings.HasPrefix(normalized, `\\.\`) {
		return path
	}
	full, err := windows.FullPath(path)
	if err != nil || len(full) < 248 {
		return path
	}
	if strings.HasPrefix(full, `\\`) {
		return `\\?\UNC\` + full[2:]
	}
	return `\\?\` + full
}
