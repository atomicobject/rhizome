package obsidian

import "path/filepath"

// UnifiedIndexPath normalizes the unified index path for a vault.
// It resolves relative paths against the vault root and enforces the
// unified db.sqlite location when configured inside .rhizome/.
func UnifiedIndexPath(vaultPath, configured string) string {
	return unifiedIndexPath(vaultPath, configured)
}

func unifiedIndexPath(vaultPath, configured string) string {
	defaultPath := filepath.Join(rhizomeDir(vaultPath), RhizomeDBFile)
	if configured == "" {
		return defaultPath
	}

	path := configured
	if !filepath.IsAbs(path) {
		path = filepath.Join(vaultPath, path)
	}
	path = filepath.Clean(path)

	// The unified index is always stored as db.sqlite inside the vault's .rhizome
	// directory. If a config points at a differently-named DB in that directory,
	// treat it as legacy and use the unified default.
	rhDir := filepath.Clean(rhizomeDir(vaultPath))
	if filepath.Clean(filepath.Dir(path)) == rhDir && filepath.Base(path) != RhizomeDBFile {
		return defaultPath
	}
	return path
}
