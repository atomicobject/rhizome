package store

import (
	"strings"
)

func splitFindDirectoryAndContent(pattern string) (string, string) {
	parts := strings.SplitN(pattern, "/", 2)
	dirPattern := parts[0]
	contentPattern := ""
	if len(parts) > 1 {
		contentPattern = parts[1]
	}
	return dirPattern, contentPattern
}

func unionSortedPaths(left, right []string) []string {
	if len(left) == 0 {
		return append([]string(nil), right...)
	}
	if len(right) == 0 {
		return append([]string(nil), left...)
	}
	out := make([]string, 0, len(left)+len(right))
	i, j := 0, 0
	for i < len(left) || j < len(right) {
		switch {
		case i >= len(left):
			out = append(out, right[j:]...)
			return dedupeSortedPaths(out)
		case j >= len(right):
			out = append(out, left[i:]...)
			return dedupeSortedPaths(out)
		case left[i] == right[j]:
			out = append(out, left[i])
			i++
			j++
		case left[i] < right[j]:
			out = append(out, left[i])
			i++
		default:
			out = append(out, right[j])
			j++
		}
	}
	return dedupeSortedPaths(out)
}

func intersectSortedPaths(left, right []string) []string {
	if len(left) == 0 || len(right) == 0 {
		return []string{}
	}
	out := make([]string, 0, min(len(left), len(right)))
	i, j := 0, 0
	for i < len(left) && j < len(right) {
		switch {
		case left[i] == right[j]:
			out = append(out, left[i])
			i++
			j++
		case left[i] < right[j]:
			i++
		default:
			j++
		}
	}
	return out
}

func dedupeSortedPaths(paths []string) []string {
	if len(paths) < 2 {
		return paths
	}
	out := paths[:1]
	for _, path := range paths[1:] {
		if path == out[len(out)-1] {
			continue
		}
		out = append(out, path)
	}
	return out
}

func trimNonEmptyStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}
