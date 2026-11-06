package paths

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func needsVerbatimDOSPath(path string) bool {
	for component := range strings.SplitSeq(path, `\`) {
		if strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return true
		}
	}
	return false
}

func canonicalExistingPath(path string) (string, error) {
	// A verbatim input must reach CreateFile unchanged. In particular, do not
	// pass its significant final dots or spaces through lexical normalization.
	if !strings.HasPrefix(path, `\\?\`) && !strings.HasPrefix(path, `\\.\`) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		path = abs
	}
	name, err := windows.UTF16FromString(path)
	if err != nil {
		return "", &os.PathError{Op: "canonicalize", Path: path, Err: err}
	}
	// Abs has already applied Win32's ordinary path parsing. Use the extended
	// spelling for long paths, as Rust's Windows path preparation does.
	if len(name) >= 248 {
		name, err = windows.UTF16FromString(verbatimDOSPath(path))
		if err != nil {
			return "", &os.PathError{Op: "canonicalize", Path: path, Err: err}
		}
	}
	handle, err := windows.CreateFile(&name[0], 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", &os.PathError{Op: "canonicalize", Path: path, Err: err}
	}
	defer windows.CloseHandle(handle)

	// Do not turn a caller's non-DOS device namespace into a drive path.
	if strings.HasPrefix(path, `\\.\`) || (strings.HasPrefix(path, `\\?\`) && ordinaryDOSPath(path) == path) {
		return path, nil
	}

	// FindFirstFile, used by filepath.EvalSymlinks, can erase significant dots
	// and spaces. Resolve the opened object instead, matching Rust canonicalize.
	buffer := make([]uint16, 256)
	for {
		n, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
		if err != nil {
			return "", &os.PathError{Op: "canonicalize", Path: path, Err: err}
		}
		if n >= uint32(len(buffer)) {
			buffer = make([]uint16, n+1)
			continue
		}
		canonical := windows.UTF16ToString(buffer[:n])
		ordinary := ordinaryDOSPath(canonical)
		if ordinary != canonical && !needsVerbatimDOSPath(ordinary) {
			original, originalErr := os.Stat(canonical)
			alias, aliasErr := os.Stat(ordinary)
			if originalErr == nil && aliasErr == nil && os.SameFile(original, alias) {
				return ordinary, nil
			}
		}
		return canonical, nil
	}
}
