package rhizomemdtemplates

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
)

//go:embed *.md
var templatesFS embed.FS

// Load returns the named RHIZOME.md template with surrounding whitespace trimmed.
func Load(name string) (string, error) {
	b, err := fs.ReadFile(templatesFS, name)
	if err != nil {
		return "", fmt.Errorf("read rhizome-md template %q: %w", name, err)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", fmt.Errorf("rhizome-md template %q is empty", name)
	}
	return s, nil
}
