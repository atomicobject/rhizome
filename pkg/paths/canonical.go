package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// CanonicalExistingPath resolves an existing path for filesystem identity.
// Windows DOS namespace aliases use ordinary spelling only when both name the same file.
func CanonicalExistingPath(path string) (AbsPath, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	canonical = filepath.Clean(canonical)
	if runtime.GOOS == "windows" {
		ordinary := ordinaryDOSPath(canonical)
		if ordinary != canonical {
			original, originalErr := os.Stat(canonical)
			alias, aliasErr := os.Stat(ordinary)
			if originalErr == nil && aliasErr == nil && os.SameFile(original, alias) {
				canonical = ordinary
			}
		}
	}
	return AbsPath(canonical), nil
}

func ordinaryDOSPath(path string) string {
	if !strings.HasPrefix(path, `\\?\`) {
		return path
	}
	tail := path[4:]
	if len(tail) >= 4 && strings.EqualFold(tail[:4], `UNC\`) {
		return `\\` + tail[4:]
	}
	if len(tail) >= 3 && tail[1] == ':' && tail[2] == '\\' &&
		(tail[0] >= 'A' && tail[0] <= 'Z' || tail[0] >= 'a' && tail[0] <= 'z') {
		return tail
	}
	return path
}
