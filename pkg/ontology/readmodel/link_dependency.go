package readmodel

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// LinkDependencyKey identifies the authored target before link resolution.
// Metadata and ownership invalidation use the same key as catalog publication.
func LinkDependencyKey(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "[[") && strings.HasSuffix(value, "]]") {
		value = strings.TrimPrefix(strings.TrimSuffix(value, "]]"), "[[")
		if alias := strings.IndexByte(value, '|'); alias >= 0 {
			value = value[:alias]
		}
	}
	if fragment := strings.IndexByte(value, '#'); fragment >= 0 {
		value = value[:fragment]
	}
	return strings.TrimSuffix(paths.LinkKey(strings.TrimSpace(value)), ".md")
}
