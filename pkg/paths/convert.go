package paths

import (
	stdpath "path"
	"path/filepath"
	"runtime"
	"strings"
)

// ToAbs converts a RelPath to AbsPath given a root directory.
// Resolves symlinks on non-Windows; uses Clean on Windows for POSIX-style test paths.
func ToAbs(rel RelPath, root string) (AbsPath, error) {
	if rel == "" {
		return AbsPath(""), nil
	}

	// Join root with relative path
	joined := filepath.Join(root, string(rel))

	// On Windows, filepath.Abs("/repo/x") rewrites to "<drive>:\\repo\\x" which
	// breaks deterministic, POSIX-style paths used in tests and some tooling.
	// Treat leading-slash paths as already-canonical POSIX paths.
	if runtime.GOOS == "windows" && strings.HasPrefix(string(rel), "/") {
		// If the relative path starts with /, treat it as POSIX-style
		// and just clean it without resolving to a Windows drive path
		return AbsPath(stdpath.Clean(string(rel))), nil
	}

	abs, err := filepath.Abs(joined)
	if err != nil {
		return AbsPath(filepath.Clean(joined)), err
	}

	// Resolve symlinks
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// Return cleaned absolute path if symlink resolution fails
		return AbsPath(abs), nil
	}
	return AbsPath(resolved), nil
}

// ToRel converts an AbsPath to RelPath given a root directory.
// The root should be symlink-resolved if the abs path was resolved.
func ToRel(abs AbsPath, root string) (RelPath, error) {
	if abs == "" {
		return RelPath(""), nil
	}

	// Resolve root symlinks to match the abs path resolution
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		// If resolution fails, use the original root
		resolvedRoot = root
	}

	rel, err := filepath.Rel(resolvedRoot, string(abs))
	if err != nil {
		return RelPath(""), err
	}

	return Normalize(rel), nil
}

// ResolveSymlinks resolves symlinks for an absolute path.
// Returns cleaned absolute path on error.
// Always returns forward slashes for consistent cross-platform database lookups.
func ResolveSymlinks(p string) AbsPath {
	if p == "" {
		return AbsPath("")
	}

	// On Windows, preserve POSIX-style paths starting with /
	if runtime.GOOS == "windows" && strings.HasPrefix(p, "/") {
		return AbsPath(stdpath.Clean(p))
	}

	abs, err := filepath.Abs(p)
	if err != nil {
		return AbsPath(filepath.ToSlash(filepath.Clean(p)))
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return AbsPath(filepath.ToSlash(abs))
	}
	return AbsPath(filepath.ToSlash(resolved))
}
