package retrieval

import "github.com/atomicobject/rhizome/pkg/paths"

// cleanTypedNotePath preserves the authored path carried by a typed note
// handle. Retrieval consumes persisted identifiers, so it must not infer a
// Markdown extension at this boundary.
func cleanTypedNotePath(raw string) (string, bool) {
	path, err := paths.CleanNotePath(raw)
	if err != nil || path == "" {
		return "", false
	}
	return path.String(), true
}
