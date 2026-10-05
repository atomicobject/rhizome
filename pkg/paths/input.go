package paths

import (
	"path/filepath"
	"strings"
)

// AbsFromInput converts a user-provided path into an absolute path for filesystem I/O.
// Relative inputs are treated as vault-root-relative when possible.
func AbsFromInput(vaultRoot, input string) AbsPath {
	vaultPaths, _ := NewVaultPaths(vaultRoot)
	return AbsFromInputWithVaultPaths(vaultPaths, vaultRoot, input)
}

// AbsFromInputWithVaultPaths converts a user-provided path using a precomputed VaultPaths.
func AbsFromInputWithVaultPaths(vaultPaths VaultPaths, vaultRoot, input string) AbsPath {
	input = strings.TrimSpace(input)
	if input == "" {
		return AbsPath("")
	}
	if filepath.IsAbs(input) {
		if resolved := ResolveSymlinks(input); resolved != "" {
			return resolved
		}
		return AbsPath(filepath.Clean(input))
	}
	if vaultPaths.Root() != "" {
		// CLI/MCP relative inputs are interpreted as vault-relative first, not
		// process-cwd-relative. That keeps commands stable when invoked from
		// subdirectories or through long-lived agent processes.
		if rel, err := vaultPaths.RelStrict(input); err == nil && rel.String() != "" {
			if abs, err := vaultPaths.Abs(rel); err == nil && abs != "" {
				return abs
			}
		}
	}
	if strings.TrimSpace(vaultRoot) == "" {
		return AbsPath(filepath.Clean(input))
	}
	return AbsPath(filepath.Clean(filepath.Join(vaultRoot, input)))
}

// ResolveNotePathInput converts a user-provided authored note path into
// vault-relative and absolute paths without inferring an extension.
// Relative inputs are treated as vault-root-relative; absolute inputs must live
// under the vault root.
func ResolveNotePathInput(vaultRoot, input string) (NotePath, AbsPath, error) {
	vaultPaths, _ := NewVaultPaths(vaultRoot)
	return ResolveNotePathInputWithVaultPaths(vaultPaths, input)
}

// ResolveNotePathInputWithVaultPaths converts a user-provided authored note
// path using a precomputed VaultPaths without inferring an extension.
func ResolveNotePathInputWithVaultPaths(vaultPaths VaultPaths, input string) (NotePath, AbsPath, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return NotePath(""), AbsPath(""), ErrOutsideVault
	}
	if vaultPaths.Root() == "" {
		return NotePath(""), AbsPath(""), ErrOutsideVault
	}
	rel, err := vaultPaths.RelNotePathStrict(input)
	if err != nil || rel == "" {
		if err == nil {
			err = ErrOutsideVault
		}
		return NotePath(""), AbsPath(""), err
	}
	abs, err := vaultPaths.AbsNotePath(rel)
	if err != nil || abs == "" {
		if err == nil {
			err = ErrOutsideVault
		}
		return NotePath(""), AbsPath(""), err
	}
	return rel, abs, nil
}

// ResolveNoteInput converts a user-provided Markdown note path into vault-relative and absolute paths.
// Relative inputs are treated as vault-root-relative; absolute inputs must live under the vault root.
//
// Deprecated: generic note paths must use ResolveNotePathInput.
func ResolveNoteInput(vaultRoot, input string) (NotePath, AbsPath, error) {
	vaultPaths, _ := NewVaultPaths(vaultRoot)
	return ResolveNoteInputWithVaultPaths(vaultPaths, input)
}

// ResolveNoteInputWithVaultPaths converts a user-provided Markdown note path using a precomputed VaultPaths.
//
// Deprecated: generic note paths must use ResolveNotePathInputWithVaultPaths.
func ResolveNoteInputWithVaultPaths(vaultPaths VaultPaths, input string) (NotePath, AbsPath, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return NotePath(""), AbsPath(""), ErrOutsideVault
	}
	if vaultPaths.Root() == "" {
		return NotePath(""), AbsPath(""), ErrOutsideVault
	}
	// Note/code resolvers are stricter than AbsFromInput because their Rel side
	// is normally persisted or compared as an index key.
	rel, err := vaultPaths.RelNoteStrict(input)
	if err != nil || rel == "" {
		if err == nil {
			err = ErrOutsideVault
		}
		return NotePath(""), AbsPath(""), err
	}
	abs, err := vaultPaths.AbsNote(rel)
	if err != nil || abs == "" {
		if err == nil {
			err = ErrOutsideVault
		}
		return NotePath(""), AbsPath(""), err
	}
	return rel, abs, nil
}

// ResolveNotePathRef converts a user-provided authored note path into a PathRef
// without inferring an extension. Rel is for storage/IDs/FQNs; Abs is for
// filesystem I/O only.
func ResolveNotePathRef(vaultRoot, input string) (NotePathRef, error) {
	vaultPaths, _ := NewVaultPaths(vaultRoot)
	return ResolveNotePathRefWithVaultPaths(vaultPaths, input)
}

// ResolveNotePathRefWithVaultPaths converts a user-provided authored note path
// into a PathRef using a precomputed VaultPaths without inferring an extension.
func ResolveNotePathRefWithVaultPaths(vaultPaths VaultPaths, input string) (NotePathRef, error) {
	rel, abs, err := ResolveNotePathInputWithVaultPaths(vaultPaths, input)
	if err != nil {
		return NotePathRef{}, err
	}
	return NotePathRef{Rel: rel, Abs: abs}, nil
}

// ResolveNoteRef converts a user-provided Markdown note path into a PathRef.
// Rel is for storage/IDs/FQNs; Abs is for filesystem I/O only.
// Relative inputs are treated as vault-root-relative; absolute inputs must live under the vault root.
//
// Deprecated: generic note paths must use ResolveNotePathRef.
func ResolveNoteRef(vaultRoot, input string) (NotePathRef, error) {
	vaultPaths, _ := NewVaultPaths(vaultRoot)
	return ResolveNoteRefWithVaultPaths(vaultPaths, input)
}

// ResolveNoteRefWithVaultPaths converts a user-provided Markdown note path into a PathRef using a precomputed VaultPaths.
// Rel is for storage/IDs/FQNs; Abs is for filesystem I/O only.
//
// Deprecated: generic note paths must use ResolveNotePathRefWithVaultPaths.
func ResolveNoteRefWithVaultPaths(vaultPaths VaultPaths, input string) (NotePathRef, error) {
	rel, abs, err := ResolveNoteInputWithVaultPaths(vaultPaths, input)
	if err != nil {
		return NotePathRef{}, err
	}
	return NotePathRef{Rel: rel, Abs: abs}, nil
}

// ResolveCodeInput converts a user-provided code path into vault-relative and absolute paths.
// Relative inputs are treated as vault-root-relative; absolute inputs must live under the vault root.
func ResolveCodeInput(vaultRoot, input string) (CodePath, AbsPath, error) {
	vaultPaths, _ := NewVaultPaths(vaultRoot)
	return ResolveCodeInputWithVaultPaths(vaultPaths, input)
}

// ResolveCodeInputWithVaultPaths converts a user-provided code path using a precomputed VaultPaths.
func ResolveCodeInputWithVaultPaths(vaultPaths VaultPaths, input string) (CodePath, AbsPath, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return CodePath(""), AbsPath(""), ErrOutsideVault
	}
	if vaultPaths.Root() == "" {
		return CodePath(""), AbsPath(""), ErrOutsideVault
	}
	rel, err := vaultPaths.RelCodeStrict(input)
	if err != nil || rel == "" {
		if err == nil {
			err = ErrOutsideVault
		}
		return CodePath(""), AbsPath(""), err
	}
	abs, err := vaultPaths.AbsCode(rel)
	if err != nil || abs == "" {
		if err == nil {
			err = ErrOutsideVault
		}
		return CodePath(""), AbsPath(""), err
	}
	return rel, abs, nil
}

// ResolveCodeRef converts a user-provided code path into a PathRef.
// Rel is for storage/IDs/FQNs; Abs is for filesystem I/O only.
// Relative inputs are treated as vault-root-relative; absolute inputs must live under the vault root.
func ResolveCodeRef(vaultRoot, input string) (CodePathRef, error) {
	vaultPaths, _ := NewVaultPaths(vaultRoot)
	return ResolveCodeRefWithVaultPaths(vaultPaths, input)
}

// ResolveCodeRefWithVaultPaths converts a user-provided code path into a PathRef using a precomputed VaultPaths.
// Rel is for storage/IDs/FQNs; Abs is for filesystem I/O only.
func ResolveCodeRefWithVaultPaths(vaultPaths VaultPaths, input string) (CodePathRef, error) {
	rel, abs, err := ResolveCodeInputWithVaultPaths(vaultPaths, input)
	if err != nil {
		return CodePathRef{}, err
	}
	return CodePathRef{Rel: rel, Abs: abs}, nil
}
