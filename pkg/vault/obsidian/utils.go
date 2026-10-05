package obsidian

import (
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

func RemoveMdSuffix(str string) string {
	if strings.HasSuffix(str, ".md") {
		return strings.TrimSuffix(str, ".md")
	}
	return str
}

// NormalizeWithDefaultExt normalizes path separators and appends defaultExt when no extension is present.
// defaultExt should include the leading dot (e.g., ".md"); if empty, no extension is added.
func NormalizeWithDefaultExt(path string, defaultExt string) string {
	path = NormalizePath(path)
	if filepath.Ext(path) == "" && defaultExt != "" {
		if !strings.HasPrefix(defaultExt, ".") {
			defaultExt = "." + defaultExt
		}
		path += defaultExt
	}
	return path
}

// DeduplicateResults removes duplicate entries from a slice of strings
func DeduplicateResults(results []string) []string {
	seen := make(map[string]bool)
	var unique []string
	for _, result := range results {
		if !seen[result] {
			seen[result] = true
			unique = append(unique, result)
		}
	}
	return unique
}

// NormalizePath normalizes a path for comparison.
//
// Deprecated: Use paths.Normalize directly for new code.
// This function is maintained for backward compatibility only.
func NormalizePath(path string) string {
	return string(paths.Normalize(path))
}
