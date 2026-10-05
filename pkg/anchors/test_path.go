package codeanchor

import (
	"path/filepath"
	"strings"
)

// IsTestPath reports whether a code path follows a supported test-file or
// test-directory convention. Indexing and retrieval share this vocabulary.
func IsTestPath(path string) bool {
	path = strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	if path == "" {
		return false
	}
	if strings.HasPrefix(path, "test/") || strings.HasPrefix(path, "tests/") || strings.HasPrefix(path, "__tests__/") || strings.HasPrefix(path, "testdata/") ||
		strings.Contains(path, "/test/") || strings.Contains(path, "/tests/") || strings.Contains(path, "/__tests__/") || strings.Contains(path, "/testdata/") {
		return true
	}
	base := filepath.Base(path)
	if strings.HasPrefix(base, "test_") {
		return true
	}
	if strings.Contains(base, "_test.") {
		return true
	}
	return strings.Contains(base, ".test.") || strings.Contains(base, ".spec.")
}
