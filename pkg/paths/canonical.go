package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// CanonicalExistingPath resolves an existing path for filesystem identity.
// Windows DOS namespace aliases use ordinary spelling only when both name the
// same file and Win32 parsing cannot change a component's name.
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
		if needsVerbatimDOSPath(ordinary) {
			// Go's long-path handling can make an ordinary spelling name the
			// same file even when another Win32 caller parses it differently.
			canonical = verbatimDOSPath(ordinary)
		} else if ordinary != canonical {
			original, originalErr := os.Stat(canonical)
			alias, aliasErr := os.Stat(ordinary)
			if originalErr == nil && aliasErr == nil && os.SameFile(original, alias) {
				canonical = ordinary
			}
		}
	}
	return AbsPath(canonical), nil
}

func needsVerbatimDOSPath(path string) bool {
	for component := range strings.SplitSeq(path, `\`) {
		if strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return true
		}
	}
	return false
}

func verbatimDOSPath(path string) string {
	if len(path) >= 3 && path[1] == ':' && path[2] == '\\' &&
		(path[0] >= 'A' && path[0] <= 'Z' || path[0] >= 'a' && path[0] <= 'z') {
		return `\\?\` + path
	}
	if strings.HasPrefix(path, `\\`) && !strings.HasPrefix(path, `\\?\`) && !strings.HasPrefix(path, `\\.\`) {
		return `\\?\UNC\` + path[2:]
	}
	return path
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
