package codeanchor

import (
	"runtime"
	"strings"
)

func isCaseInsensitiveFS() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}

func normalizePathCase(path string) string {
	if path == "" {
		return ""
	}
	if isCaseInsensitiveFS() {
		return strings.ToLower(path)
	}
	return path
}

func normalizePathCaseSlice(paths []string) []string {
	if len(paths) == 0 || !isCaseInsensitiveFS() {
		return paths
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		out = append(out, strings.ToLower(p))
	}
	return out
}
