package paths

import (
	"path/filepath"
	"strings"
)

// NoteTitle derives a display title from either path separator and removes
// only the basename's final extension. It does not change note identity.
func NoteTitle(path string) string {
	base := filepath.Base(strings.ReplaceAll(path, `\`, "/"))
	return strings.TrimSuffix(base, filepath.Ext(base))
}
