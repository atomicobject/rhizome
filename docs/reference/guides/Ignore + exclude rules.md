---
type: ReferenceDoc
summary: "Implementation guide for Rhizome's unified ignore matcher, including precedence, pattern semantics, include boundaries, and watcher invalidation behavior."
reference-kind: guide
derived-from:
  - docs/reference-notes/Ignore + exclude rules.md
last-verified: 2026-10-01
status: active
---

# Ignore + exclude rules

## Summary

Rhizome uses one unified ignore matcher across crawl, cache, semantic indexing, and code indexing so file visibility stays consistent.

## Contracts

- `.gitignore`, `.rhizome/ignore`, legacy `.obsidianignore`, and config excludes compose into one matcher
- later rules win, including negations
- literal dir-only negations (`!/app/`) are include boundaries: they suppress earlier rules that excluded the boundary directory itself, while the subtree's own nested `.gitignore`, defaults, and later rules keep applying ([[ignored-subtree-inclusion|SPEC-0064]]; see `pkg/vault/ignore/boundary.go`)
- a directory's `.gitignore` loads whenever the directory is included after all layers evaluate, so re-included subtrees contribute their own `.gitignore`
- `Matcher.Explain` reports the deciding layer/source/line/pattern for any path; `rzm index --explain <path>` is the CLI surface
- ignore-file changes force stale cache state and full resync because indexed scope may have changed globally
- new exclusion logic should route through `pkg/vault/ignore` instead of ad hoc skip lists
- note selection has one system exception: exact `CONTEXT.md` bypasses `notes.includes` and config-backed `notes.excludes`; built-in infrastructure skips, `.gitignore`, and `.rhizome/ignore` remain hard visibility boundaries
- use `.rhizome/ignore` (or the legacy `.obsidianignore` fallback) when a `CONTEXT.md` path must remain outside discovery and indexing; `notes.excludes` only selects ordinary notes
- `rzm init` writes only to its own commented sections of `.rhizome/ignore`: `# rhizome: included subtrees` (`!/path/` boundaries for folders Git ignores) and `# rhizome: suggested skips` (anchored paths for vendored, generated, minified, or larger-than-1 MB content, each under a one-line reason). A `# rhizome: keep indexed <path>` comment anywhere in the file tells init never to propose that path; deleting a suggested line by hand lets the next rerun propose it again ([[init-starter-workflow#^SPEC-0038-US10]])

## Related

- [[Ignore behavior]]
- [[ignored-subtree-inclusion]]
- [[Vault cache service (Service)]]
