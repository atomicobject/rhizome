package paths

import (
	"errors"
	"os"
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

// RequireCurrentWorkingDirectoryAtRoot verifies that the process is running
// from this vault's canonical root. It treats symlink aliases of the root as
// the same directory.
func (v VaultPaths) RequireCurrentWorkingDirectoryAtRoot() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	rel, err := v.RelStrict(cwd)
	if err != nil || rel != "" {
		return ErrWorkingDirectoryNotVaultRoot
	}
	return nil
}

// Rel converts an arbitrary path (absolute or relative) into a normalized RelPath.
func (v VaultPaths) Rel(p string) (RelPath, error) {
	if p == "" {
		return RelPath(""), nil
	}
	if filepath.IsAbs(p) {
		// Non-strict conversion preserves legacy behavior for display/search paths:
		// an absolute path outside the vault becomes a cleaned relative-looking key.
		// Storage and index keys should use RelStrict instead.
		return ToRel(ResolveSymlinks(p), v.root.String())
	}
	return Normalize(p), nil
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

// RelNotePath converts an arbitrary path to a normalized authored note path
// relative to the vault root without inferring an extension.
func (v VaultPaths) RelNotePath(p string) (NotePath, error) {
	rel, err := v.Rel(p)
	if err != nil {
		return NotePath(""), err
	}
	return NormalizeNotePath(rel.String()), nil
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

// RelNote converts an arbitrary path to a Markdown NotePath relative to the vault root.
//
// Deprecated: generic note paths must use RelNotePath.
func (v VaultPaths) RelNote(p string) (NotePath, error) {
	rel, err := v.Rel(p)
	if err != nil {
		return NotePath(""), err
	}
	return NormalizeNote(rel.String()), nil
}

// RelNoteStrict converts a path to a Markdown NotePath and rejects paths outside the vault root.
//
// Deprecated: generic note paths must use RelNotePathStrict.
func (v VaultPaths) RelNoteStrict(p string) (NotePath, error) {
	rel, err := v.RelStrict(p)
	if err != nil {
		return NotePath(""), err
	}
	return NormalizeNote(rel.String()), nil
}

// RelCode converts an arbitrary path to a normalized CodePath relative to the vault root.
func (v VaultPaths) RelCode(p string) (CodePath, error) {
	rel, err := v.Rel(p)
	if err != nil {
		return CodePath(""), err
	}
	return NormalizeCode(rel.String()), nil
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

// AbsNote converts a Markdown NotePath into an absolute filesystem path rooted at the vault.
//
// Deprecated: generic note paths must use AbsNotePath.
func (v VaultPaths) AbsNote(note NotePath) (AbsPath, error) {
	return v.Abs(RelPath(note))
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
