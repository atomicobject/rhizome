package paths

import (
	"path/filepath"
	"strings"
)

// NormalizeDocPath converts a note or code path into a stable doc-graph key.
// Note paths preserve their authored extension; this boundary does not infer
// Markdown's .md suffix.
// If the vault root is known, absolute paths are converted to vault-relative
// identifiers when possible; otherwise absolute paths are preserved.
func (v VaultPaths) NormalizeDocPath(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return ""
	}
	clean = filepath.ToSlash(clean)

	if v.Root() != "" {
		if filepath.IsAbs(clean) {
			resolved := ResolveSymlinks(clean)
			if rel, err := v.RelStrict(resolved.String()); err == nil && rel.String() != "" {
				clean = rel.String()
			} else if resolved != "" {
				clean = resolved.String()
			}
		} else {
			if rel, err := v.RelStrict(clean); err == nil && rel.String() != "" {
				clean = rel.String()
			}
		}
	}

	return string(Normalize(clean))
}

// NormalizeRelPathAuto normalizes a vault-relative path for watch/event keys.
func NormalizeRelPathAuto(rel string) string {
	clean := filepath.ToSlash(strings.TrimSpace(rel))
	if clean == "" {
		return ""
	}
	clean = strings.TrimPrefix(clean, "./")
	if strings.HasSuffix(clean, "/") {
		clean = strings.TrimSuffix(clean, "/")
	}
	if strings.HasSuffix(strings.ToLower(clean), ".md") {
		return string(NormalizeNote(clean))
	}
	return string(Normalize(clean))
}

// NormalizeAbsPathForCompare returns a stable absolute path key for comparisons.
// It is not intended for filesystem I/O.
func NormalizeAbsPathForCompare(p string) string {
	clean := strings.TrimSpace(p)
	if clean == "" {
		return ""
	}
	resolved := ResolveSymlinks(clean)
	if resolved != "" {
		return filepath.ToSlash(filepath.Clean(resolved.String()))
	}
	return filepath.ToSlash(filepath.Clean(clean))
}
