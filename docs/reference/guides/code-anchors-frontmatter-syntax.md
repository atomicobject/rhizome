---
type: ReferenceDoc
summary: "How to author `code-anchors:` frontmatter and choose the right selector shape."
reference-kind: guide
derived-from:
  - docs/reference-notes/Code anchors - frontmatter syntax.md
last-verified: 2026-04-12
status: active
aliases:
  - Code anchors - frontmatter syntax
---

# Code anchors - frontmatter syntax

## Summary

Code anchors live in YAML frontmatter under `code-anchors:`. They tie a note to code by matching a symbol, call sites, a decorator or annotation, or a path-based selector.

## Authoring rules

- Put anchors in `code-anchors:` frontmatter.
- Make each inline anchor choose exactly one selector: `symbol`, `calls`, `decorator`, `dir`, `glob`, or `globs`.
- Use `symbol` or `calls` with fully-qualified names (`pkg.Name`).
- Treat `calls` as a legacy alias for `symbol`.
- Use `decorator` when you want package-wildcard matching by decorator name.
- Use `dir` for module or folder-level coverage when path matching is the intent.
- Use `glob` or `globs` for explicit path patterns.

## Legacy compatibility

- `anchors:` is still accepted for older notes.
- Keep new notes on `code-anchors:` so the current indexer and authoring guidance stay aligned.

## Related

- [[Code anchors (Hub)]]
- [[code-anchors-matching-scopes]]
- [[code-anchors-language-support]]
