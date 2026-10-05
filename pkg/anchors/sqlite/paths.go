package sqlite

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

func normalizeLookupPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	return string(paths.Normalize(p))
}

func normalizeNoteLookupPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	// Lookups receive stored authored note identities. Preserve an explicit
	// extension and its casing rather than treating this as Markdown input.
	return string(paths.NormalizeNotePath(p))
}

func normalizeCodeLookupPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	return string(paths.NormalizeCode(p))
}

func normalizeCodePathInputs(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = normalizeCodeLookupPath(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
