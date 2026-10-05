---
type: ReferenceDoc
summary: "PathRef contract: vault-relative normalized paths for identities, absolute paths only for filesystem I/O."
reference-kind: architecture
derived-from:
  - docs/reference-notes/PathRef contract.md
last-verified: 2026-04-12
status: active
---

# PathRef contract

## Summary

Path handling must keep persisted identities vault-relative and normalized. Absolute paths remain an I/O concern only.

## Contracts

- stored keys, IDs, and FQNs use normalized relative paths
- filesystem access uses absolute resolved paths
- new code should use `paths.Resolve*Ref*` helpers instead of ad hoc `filepath.Abs` and `filepath.Rel` logic
