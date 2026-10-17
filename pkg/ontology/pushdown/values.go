package pushdown

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// PathPredicateVariants returns the authored canonical path form used when
// matching a notePath/path predicate. The caller must not invent an extension:
// provider ownership is resolved before this SQL boundary.
func PathPredicateVariants(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return []string{string(paths.NormalizeNotePath(value))}
}
