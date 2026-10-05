---
type: ReferenceDoc
summary: "Guidance for adding or expanding code-anchor language support, including label conventions and the required integration-test pattern."
reference-kind: guide
derived-from:
  - docs/reference-notes/Code anchors - language support (design + testing).md
last-verified: 2026-04-12
status: active
aliases:
  - Code anchors - language support (design + testing)
---

# Code anchors - language support (design + testing)

## Summary

Use this guide when adding a new `pkg/anchors` language indexer or expanding an existing one.

## Goals

- Make anchors useful by default so `file_context` can surface the right docs.
- Keep anchor labels stable and predictable.
- Prefer best-effort incremental indexing over brittle completeness.
- Favor tree-sitter and other robust parsing strategies when they fit the language.

## Minimum anchor kinds

Every language should support the anchors we can reasonably extract:

- `symbol` for definition sites
- `calls` for call sites and dependents
- `dir` for module or directory-level coverage

Optional when the language supports them well:

- `decorator` / `annotation`
- `baseClass`

## Label conventions

- Use `<lang>.<concept>` for cross-language parity.
- Prefer snake_case for `<concept>`.
- Use suffixes like `_def`, `_callers`, and `_module` when one concept needs multiple anchors.

## Implementation checks

- Choose a canonical package/import-path representation for the language.
- Make sure `symbol:` and `calls:` strings compose cleanly with the parser rules.
- Index at least top-level types and functions.
- Index package-qualified calls plus reasonable local calls where feasible.
- Wire the language into watcher updates and the live runtime path.

## Testing pattern

- Use the polyglot integration fixture under `testdata/integration/python-app/vault/`.
- Add a language subtree under the same vault.
- Add a note under `vault/notes/code-anchors/` for that language.
- Extend the integration test to cover coderefs plus `symbol` anchors at definition and caller sites.

## Known limitations

- Path aliases and package export maps are not fully resolved.
- Go method call-sites remain limited without type information.

## Related

- [[Code anchors (Hub)]]
- [[code-anchors-frontmatter-syntax]]
- [[code-anchors-matching-scopes]]
