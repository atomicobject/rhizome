package paths

import (
	"errors"
	"strings"
)

var ErrEmptyNotePath = errors.New("note path is required")

// Normalize converts any path to a clean RelPath.
// - Converts separators to forward slashes
// - Removes ./ and ../ prefixes (single level only, like current behavior)
// - Does NOT resolve symlinks or make absolute
func Normalize(p string) RelPath {
	if p == "" {
		return RelPath("")
	}
	// Convert all path separators to forward slashes
	p = strings.ReplaceAll(p, "\\", "/")
	// Remove single-level ./ and ../ prefixes (matching current obsidian.NormalizePath behavior)
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "../")
	return RelPath(p)
}

// NormalizeNotePath returns a normalized authored note path without inferring
// a file extension. It preserves the authored extension and its casing.
func NormalizeNotePath(p string) NotePath {
	return NotePath(Normalize(p))
}

// CleanNotePath validates and returns a canonical vault-relative authored note
// path without inferring an extension. Use this strict constructor at storage,
// source, and filesystem-I/O boundaries; NormalizeNotePath is lexical-only.
func CleanNotePath(p string) (NotePath, error) {
	rel, err := CleanRelPath(p)
	if err != nil {
		return NotePath(""), err
	}
	if rel == "" {
		return NotePath(""), ErrEmptyNotePath
	}
	return NotePath(rel), nil
}

// NormalizeNote returns a Markdown NotePath and appends .md if missing.
//
// Deprecated: canonical format-neutral boundaries must use CleanNotePath;
// NormalizeNotePath is lexical-only. Keep this helper only at Markdown
// authoring and legacy-input boundaries.
func NormalizeNote(p string) NotePath {
	normalized := NormalizeNotePath(p)
	path := string(normalized)
	if !strings.HasSuffix(strings.ToLower(path), ".md") {
		path += ".md"
	}
	return NotePath(path)
}

// NormalizeCode returns a CodePath (no suffix manipulation).
func NormalizeCode(p string) CodePath {
	return CodePath(Normalize(p))
}
