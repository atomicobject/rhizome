package paths

import (
	"errors"
	"path"
	"strings"
)

var ErrEmptyNotePath = errors.New("note path is required")

// Normalize is idempotent lexical path normalization. It preserves absolute
// roots and parent escapes so a later strict boundary can reject them.
// Use CleanRelPath for storage keys and filesystem inputs.
func Normalize(p string) RelPath {
	if p == "" {
		return ""
	}
	return RelPath(path.Clean(strings.ReplaceAll(p, "\\", "/")))
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

// EnsureNoteExtension applies an explicit note-authoring suffix. It preserves
// the authored suffix casing when it already matches the requested extension.
func EnsureNoteExtension(p, extension string) NotePath {
	normalized := NormalizeNotePath(p)
	if normalized == "" || extension == "" {
		return normalized
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if !strings.EqualFold(path.Ext(normalized.String()), extension) {
		return NotePath(normalized.String() + extension)
	}
	return normalized
}

// NormalizeCode returns a CodePath (no suffix manipulation).
func NormalizeCode(p string) CodePath {
	return CodePath(Normalize(p))
}
