package sqlite

import (
	"fmt"
	stdpath "path"
	"strings"
)

// externalStoragePath accepts only canonicalizable vault-root-relative keys.
// Store APIs have no vault root with which to safely reinterpret absolute paths.
func externalStoragePath(raw, field string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("external evidence %s is required", field)
	}
	normalized := strings.ReplaceAll(trimmed, "\\", "/")
	if strings.HasPrefix(normalized, "/") || (len(normalized) >= 2 && normalized[1] == ':') {
		return "", fmt.Errorf("external evidence %s must be vault-root-relative: %q", field, raw)
	}
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." {
			return "", fmt.Errorf("external evidence %s must be vault-root-relative: %q", field, raw)
		}
	}
	cleaned := stdpath.Clean(normalized)
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("external evidence %s is required", field)
	}
	return cleaned, nil
}
