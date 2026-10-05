# Cooperative file publication

`OpenRead` and `ReadFile` let readers finish a complete file snapshot while its publisher replaces or removes the namespace entry. Windows reads share READ, WRITE, and DELETE and preserve extended/UNC path handling. Other platforms use ordinary file reads.

`Replace` performs one namespace replacement. On Windows it uses the existing `FileRenameInfoEx` POSIX replacement behavior, with ordinary rename fallback only when that operation is unsupported. Other platforms use `os.Rename`. An external reader that denies deletion sharing, or a filesystem that cannot replace an open target, may still cause a truthful error. Callers retain the old target on failure and own cleanup of their temporary source.

This package owns only OS file access and replacement. Vault-relative identity remains in `pkg/paths`; serialization, temporary writes, syncing, permissions, and no-op checks remain with each publisher. Runtime discovery and repo config readers/writers share these primitives.
