package paths

import (
	"runtime"
	"strings"
)

// caseInsensitiveFS reports whether the current OS typically has case-insensitive filesystems.
var caseInsensitiveFS = runtime.GOOS == "windows" || runtime.GOOS == "darwin"

// CaseKey returns a path suitable for use as a map key on the current platform.
// On case-insensitive filesystems (Windows, macOS), it lowercases the path.
// On case-sensitive filesystems (Linux), it returns the path unchanged.
func CaseKey(path string) string {
	if caseInsensitiveFS {
		return strings.ToLower(path)
	}
	return path
}

// CaseEqual reports whether two paths are equal on the current platform.
// On case-insensitive filesystems, comparison is case-insensitive.
func CaseEqual(a, b string) bool {
	if caseInsensitiveFS {
		return strings.EqualFold(a, b)
	}
	return a == b
}
