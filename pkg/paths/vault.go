package paths

import (
	"errors"
	stdpath "path"
	"path/filepath"
	"strings"
)

// VaultPaths encapsulates path normalization relative to a vault root.
// It centralizes conversion between absolute filesystem paths and the
// vault-root-relative paths stored in Rhizome indexes.
type VaultPaths struct {
	root    AbsPath
	rootAbs AbsPath
}

var (
	ErrOutsideVault                 = errors.New("path is outside vault root")
	ErrWorkingDirectoryNotVaultRoot = errors.New("working directory is not the vault root")
)

// NewVaultPaths constructs a VaultPaths helper from a vault root directory.
func NewVaultPaths(root string) (VaultPaths, error) {
	if strings.TrimSpace(root) == "" {
		return VaultPaths{}, nil
	}
	absRoot, err := filepath.Abs(root)
	if err != nil || absRoot == "" {
		absRoot = root
	}
	resolved := ResolveSymlinks(absRoot)
	if resolved == "" {
		return VaultPaths{}, nil
	}
	return VaultPaths{
		root:    resolved,
		rootAbs: AbsPath(filepath.ToSlash(absRoot)),
	}, nil
}

// Root returns the absolute, symlink-resolved vault root.
func (v VaultPaths) Root() string {
	return v.root.String()
}

// RequireWorkingDirectoryAtRoot accepts symlink aliases of the canonical root.
func (v VaultPaths) RequireWorkingDirectoryAtRoot(cwd string) error {
	if !filepath.IsAbs(cwd) {
		return ErrWorkingDirectoryNotVaultRoot
	}
	rel, err := v.RelStrict(cwd)
	if err != nil || rel != "" {
		return ErrWorkingDirectoryNotVaultRoot
	}
	return nil
}

// RelStrict converts a path into a normalized RelPath and rejects paths outside the vault root.
func (v VaultPaths) RelStrict(p string) (RelPath, error) {
	if p == "" {
		return RelPath(""), nil
	}

	var rel RelPath
	if filepath.IsAbs(p) {
		relStr, relErr := relFromAbsPath(ResolveSymlinks(p).String(), v.root.String())
		if relErr != nil {
			return RelPath(""), relErr
		}
		rel = RelPath(relStr)
	} else {
		clean, cleanErr := cleanRelPath(p)
		if cleanErr != nil {
			return RelPath(""), cleanErr
		}
		return RelPath(clean), nil
	}

	clean, cleanErr := cleanRelPath(rel.String())
	if cleanErr == nil {
		return RelPath(clean), nil
	}
	if cleanErr != ErrOutsideVault || v.rootAbs == "" || !filepath.IsAbs(p) {
		return RelPath(""), cleanErr
	}

	// Symlink resolution can make an in-vault path appear outside the resolved
	// root when the root itself is reached through a symlink. Retry against the
	// unresolved absolute root before rejecting; this keeps strict storage keys
	// stable without forcing callers to know the user's symlink layout.
	absPath, absErr := filepath.Abs(p)
	if absErr != nil || absPath == "" {
		return RelPath(""), cleanErr
	}
	relFallback, relErr := relFromAbsPath(filepath.ToSlash(absPath), v.rootAbs.String())
	if relErr != nil {
		return RelPath(""), cleanErr
	}
	clean, cleanErr = cleanRelPath(relFallback)
	if cleanErr != nil {
		return RelPath(""), cleanErr
	}
	return RelPath(clean), nil
}

// RelNotePathStrict converts a path to a normalized authored note path and
// rejects paths outside the vault root without inferring an extension.
func (v VaultPaths) RelNotePathStrict(p string) (NotePath, error) {
	rel, err := v.RelStrict(p)
	if err != nil {
		return NotePath(""), err
	}
	return CleanNotePath(rel.String())
}

// RelCodeStrict converts a path to a normalized CodePath and rejects paths outside the vault root.
func (v VaultPaths) RelCodeStrict(p string) (CodePath, error) {
	rel, err := v.RelStrict(p)
	if err != nil {
		return CodePath(""), err
	}
	return NormalizeCode(rel.String()), nil
}

// Abs converts a RelPath into an absolute filesystem path rooted at the vault.
func (v VaultPaths) Abs(rel RelPath) (AbsPath, error) {
	return ToAbs(rel, v.root.String())
}

// AbsNotePath converts an authored NotePath into an absolute filesystem path rooted at the vault.
func (v VaultPaths) AbsNotePath(note NotePath) (AbsPath, error) {
	canonical, err := CleanNotePath(note.String())
	if err != nil {
		return AbsPath(""), err
	}
	return v.Abs(RelPath(canonical))
}

// AbsCode converts a CodePath into an absolute filesystem path rooted at the vault.
func (v VaultPaths) AbsCode(code CodePath) (AbsPath, error) {
	return v.Abs(RelPath(code))
}

func cleanRelPath(p string) (string, error) {
	trimmed := strings.TrimSpace(p)
	if trimmed == "" {
		return "", nil
	}
	cleaned := strings.ReplaceAll(trimmed, "\\", "/")
	cleaned = stdpath.Clean(cleaned)
	if cleaned == "." {
		return "", nil
	}
	if strings.HasPrefix(cleaned, "/") {
		return "", ErrOutsideVault
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrOutsideVault
	}
	// Drive letters are absolute roots in practice even when filepath.IsAbs is
	// false on non-Windows hosts; reject them so indexes never store "C:/..."
	// shaped keys during cross-platform tests or fixture generation.
	if len(cleaned) >= 2 && cleaned[1] == ':' {
		return "", ErrOutsideVault
	}
	return cleaned, nil
}

func relFromAbsPath(absPath string, root string) (string, error) {
	if absPath == "" {
		return "", nil
	}
	if root == "" {
		return "", nil
	}
	rootOS := filepath.Clean(filepath.FromSlash(root))
	absOS := filepath.Clean(filepath.FromSlash(absPath))
	rel, err := filepath.Rel(rootOS, absOS)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}
