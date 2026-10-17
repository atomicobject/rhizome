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
	path = string(paths.Normalize(path))
	if filepath.Ext(path) == "" && defaultExt != "" {
		if !strings.HasPrefix(defaultExt, ".") {
			defaultExt = "." + defaultExt
		}
		path += defaultExt
	}
	return path
}
