/*
Package paths provides canonical path normalization for Rhizome.

# Path Invariants

All paths stored in SQLite MUST be:
  - vault-root-relative (RelPath/NotePath/CodePath)
  - forward-slash separated
  - cleaned (no ./, no ..)
  - notes: extension-neutral (NotePath)
  - code: no suffix manipulation (CodePath)

AbsPath is ONLY used for:
  - filesystem I/O (os.ReadFile, os.Stat)
  - symlink resolution during indexing
  - never stored in database

# Windows Behavior

On Windows, POSIX-style paths starting with "/" are preserved as-is
(for test determinism). Real Windows paths go through standard resolution.

# Migration

Existing code should migrate from:
  - obsidian.NormalizePath() → paths.Normalize()
  - codeanchor.normalizePath() → paths.ToAbs() for I/O, paths.ToRel() for storage

# Vault-aware helpers

Use VaultPaths to normalize any vault-relative identifier and to convert between
absolute filesystem paths (for I/O) and the vault-root-relative strings stored
in SQLite. All callers should pass through VaultPaths rather than duplicating
filepath.Abs/Rel logic to keep path handling consistent and OS-agnostic.

For any path that will be stored or compared as an index key, prefer the strict
VaultPaths helpers (RelStrict/RelNotePathStrict/RelCodeStrict). They reject paths
outside the vault root so SQLite never receives absolute or escaped paths.

# PathRef

PathRef bundles the two canonical forms of a path:
- Rel (vault-root-relative, normalized) for storage/IDs/FQNs
- Abs (absolute, symlink-resolved) for filesystem I/O only

Use PathRef at subsystem boundaries to avoid ad-hoc absolute/relative handling.
Never persist AbsPath or use it as an index key.

NotePath does not infer an extension. NormalizeNotePath performs only lexical
normalization; CleanNotePath and strict VaultPaths/input helpers enforce a
canonical vault-relative identity. NormalizeNote and its RelNote/ResolveNote
family are deprecated Markdown compatibility helpers.

Docs:
  - [PathRef contract](docs/reference/domain/PathRef contract.md)
*/
package paths
