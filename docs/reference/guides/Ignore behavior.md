---
type: ReferenceDoc
summary: "User-facing guide to how Rhizome decides which files are indexed or ignored, including subtree inclusion boundaries and the index --explain diagnostic."
reference-kind: guide
derived-from:
  - docs/reference-notes/Ignore behavior.md
last-verified: 2026-10-21
status: active
---

# Ignore behavior

## Summary

When files are missing from search or indexing, the cause is usually the unified ignore system rather than a retrieval bug. Run `rzm index --explain <path>` to see the deciding layer, source file, line, and pattern for any path (the path does not need to exist).

## Precedence

1. `.gitignore` files, root to leaf
2. `.rhizome/ignore` or legacy `.obsidianignore`
3. config excludes

The ignore files are the complete list. Rhizome has a built-in starter list (`.git/`, `node_modules/`, `vendor/`, `dist/`, `build/`, caches, and similar), but it applies only while `.rhizome/ignore` has no rules. `rzm init` writes that list into `.rhizome/ignore`, where you can read and edit it. A `.rhizome/ignore` written by hand replaces it entirely, so copy in any starter entries you still want.

`.git/` is never discovered, indexed, or watched, whatever the ignore files say. Hidden directories in general are skipped by discovery and code indexing.

## Practical rules

- later matches override earlier ones
- `.rhizome/ignore` can force-index paths that Git ignores
- ignore-file edits trigger a full cache resync
- exact `CONTEXT.md` files are always selected inside the repository/vault even when `notes.includes` omits them or `notes.excludes` matches them
- hard boundaries still win for `CONTEXT.md`: strict vault containment, hidden directories, `.gitignore`, and `.rhizome/ignore`; use `.rhizome/ignore` for an explicit do-not-index boundary

## Including a gitignored subtree

A literal dir-only negation in `.rhizome/ignore` (e.g. `!/app/`) is an **include boundary** ([[ignored-subtree-inclusion|SPEC-0064]]): it cancels the earlier rules that excluded that directory (shapes like `app/`, `/app`, `app`, `app/**`) so the subtree indexes, while everything else keeps applying inside it — the subtree's own `.gitignore`, the rest of `.rhizome/ignore` (such as the starter list's `node_modules/`), and config rules. When `rzm init` adds a negation to a file with no rules, it writes the starter list first. This is the supported way to index a wrapper repo's gitignored submodule; `rzm init` detects candidates and writes the negation under a `# rhizome: included subtrees` comment.

- a boundary does **not** override contents-only patterns such as `app/*`; those target paths inside the subtree, not the subtree itself
- glob negations such as `!app/**` keep plain last-match-wins semantics: they blanket-reinclude contents, overriding even the subtree's own `.gitignore`
- nested boundaries work (`!/modules/app/`): ancestors stay traversable without re-including their sibling content

## Related

- [[Ignore + exclude rules]]
- [[ignored-subtree-inclusion]]
