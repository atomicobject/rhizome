---
type: ReferenceDoc
summary: "How code anchors match code files, how scopes are recomputed, and which behaviors are cached or retried."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Code anchors - matching + scopes.md
last-verified: 2026-04-12
status: active
aliases:
  - Code anchors - matching + scopes
---

# Code anchors - matching + scopes

## Summary

`codeanchor.Service` resolves which notes apply to a file by combining symbol, call-site, annotation, and path-prefix signals with persisted anchor scopes.

## Matching model

- `symbol` anchors attach to the definition site and to files that reference the symbol.
- `calls` remains a legacy alias for `symbol`.
- `decorator` / `annotation` anchors attach to matching decorated or annotated symbols.
- `dir` anchors attach to files under a directory prefix.

## Scope recomputation

- `RecomputeAnchorScopes` is serialized to avoid races.
- Dirty tracking keeps recompute work targeted when the index is only partially changed.
- If dirty tracking overflows, the system falls back to a broader recompute.
- Batch indexing prefers `RebuildAnchorScopesForIDs` while work is still in flight.
- Adding a note with `code-anchors:` frontmatter still triggers recompute even if no code files changed.
- If function anchors exist but call edges are missing, `RecomputeAnchorScopes` can trigger a call-edge rebuild.

## Cache and lookup behavior

- Notes-for-file results are cached best-effort.
- The cache is an optimization, not the source of truth.

## Related

- [[Code anchors (Hub)]]
- [[code-anchors-frontmatter-syntax]]
- [[code-anchors-watcher-incremental-updates]]
