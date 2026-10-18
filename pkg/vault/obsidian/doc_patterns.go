package obsidian

import (
	"path/filepath"
	"strings"
)

// NormalizeDocPatterns trims and filters doc patterns, defaulting to FileContextConfigDefaults when empty.
func NormalizeDocPatterns(patterns []string) []string {
	if len(patterns) == 0 {
		return DefaultDocPatterns()
	}
	out := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		out = append(out, pattern)
	}
	if len(out) == 0 {
		return DefaultDocPatterns()
	}
	return out
}

// MatchDocPatternNormalized reports whether path matches any normalized doc pattern.
func MatchDocPatternNormalized(path string, patterns []string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	clean := filepath.ToSlash(path)
	base := filepath.Base(clean)
	lowerPath := strings.ToLower(clean)
	lowerBase := strings.ToLower(base)

	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		pattern = filepath.ToSlash(pattern)
		lowerPattern := strings.ToLower(pattern)
		if strings.Contains(lowerPattern, "/") {
			if lowerPath == lowerPattern {
				return true
			}
			if strings.HasSuffix(lowerPath, "/"+lowerPattern) {
				return true
			}
			continue
		}
		if lowerBase == lowerPattern {
			return true
		}
	}
	return false
}

// DefaultDocPatterns lists conventional repository context documents.
func DefaultDocPatterns() []string { return []string{"CONTEXT.md"} }
