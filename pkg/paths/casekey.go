package paths

import (
	"golang.org/x/text/unicode/norm"
	"runtime"
	"strings"
)

// caseInsensitiveFS reports whether the current OS typically has case-insensitive filesystems.
var caseInsensitiveFS = runtime.GOOS == "windows" || runtime.GOOS == "darwin"

// CaseKey returns a path suitable for use as a map key on the current platform.
// On case-insensitive filesystems (Windows, macOS), it lowercases the path.
// On case-sensitive filesystems (Linux), it returns the path unchanged.
//
// This is an operating-system guess for keys that have no vault root, such as
// watcher dedupe and absolute root comparisons. It is wrong for a
// case-sensitive volume on macOS and a case-folding directory on Linux.
// Anything that compares vault-relative spellings against configured patterns
// or stored paths uses the probed per-root policy, VaultPaths.Case or
// CasePolicyFor, instead.
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

// LinkKey is the platform-independent identity of an authored link name.
// Filesystem identity still uses CaseKey; graph resolution folds NFC and case.
func LinkKey(name string) string { return strings.ToLower(norm.NFC.String(name)) }
