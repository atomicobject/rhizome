---
type: ReferenceDoc
summary: "How coderefs are extracted from code comments/docstrings, resolved against notes, and surfaced as code→note doc-link edges in retrieval."
reference-kind: guide
derived-from:
  - docs/reference-notes/Coderefs - scanning + indexing.md
last-verified: 2026-10-03
status: active
aliases:
  - Coderefs - scanning + indexing
---

# Coderefs - scanning + indexing

## Summary

Coderefs are lightweight code→note links extracted from comments and docstrings. They give source-local documentation breadcrumbs without requiring a full semantic index to explain intent. See [[coderef-contract|SPEC-0016]] for the normative contract and [[Coderefs (Hub)]] for the subsystem map.

## Supported syntaxes

Three coderef forms are scanned inside comment/docstring text only (`pkg/vault/coderefs/scanner.go`):

- **Wikilinks** — double-bracket form, with optional `#heading`, `|alias`, and `.md` path; `RefKindWikilink`
- **Markdown links** — `[text](path)` form with optional `#anchor`, angle destination, and title; `RefKindMdLink`. Resolved image destinations are scanned too, but are excluded from rename rewrites
- **`@mentions`** — `@Note` or `@path/Note`; `RefKindMention`. The regex rejects emails by requiring a non-word, non-`@` prefix

Explicit wiki and Markdown links inside extracted comment backticks, fenced examples, and indented examples are coderefs. This makes Markdown scanning agree with existing wiki/rewrite behavior; normal Markdown-note scans still protect those code spans. Source string literals remain outside extraction.

Markdown destinations split raw `#` before URL-decoding once. `%20` means a space; literal `%20` filenames require `%2520`, `+` stays literal, and malformed escapes retain authored bytes. Wikilinks do not URL-decode. Resolution returns the unique cached canonical path, so extensionless authored links still join to the note's real identity.

## Scan pipeline (per file)

1. **Guard**: skip files over 2MB (`MaxFileSizeBytes`) and binary files (null byte in first 8KB).
2. **Language detect**: extension → language via `DetectLanguage`; no match falls back to slash/block comment scanning.
3. **Comment extraction**: `ExtractCommentBlocks` routes to a comment family (`slash_line_block`, `block_only`, `hash`, `html`) or the Python extractor (handles `#` plus `"""`/`'''` docstrings). The parser is deliberately lightweight — it skips string/char literals so incidental string content does not become a link.
4. **Resolve**: each candidate is resolved through `obsidian.NotePathCache`. Only resolved targets become a `CodeRef`. Unresolved tokens are dropped — this is the ambiguity boundary that keeps the code-doc graph clean.

## CodeRef record (`types.go`)

Each ref carries `SourceFile`, `Language`, `Target` (normalized canonical note path), `Fragment` (anchor without `#`), `RawTarget` (authored destination, excluding a Markdown title/angle wrapper), `Kind`, `Line` (source line from the extracted comment offset), and `Snippet` (~100-char comment context).

## Indexing + retrieval surfacing

- **In-memory index** (`index.go`): `Index` keeps bidirectional maps `byNote` and `byFile`; `ReplaceFile` is the atomic per-file update used by the cache service. Keys are not re-canonicalized here so path-boundary bugs stay visible to downstream joins.
- **Cache service** (`pkg/vault/cache/service.go`): scans configured code files into the index; exposes `CodeRefsByNote()` / `CodeRefsByFile()` via `CodeRefProvider`.
- **Doc-link edges** (`pkg/app/codeintel/doclinker.go`): `DocLinker.LinksForCode` re-scans and emits `codeanchor.DocLink` rows (`code → note`) into the `doc_links` SQLite table, which is what makes "docs for code" surface in `file-context` and search.

## Contracts

- wikilinks are the preferred coderef form; `@mentions` are a lighter-weight alternative
- scanners stay tolerant of language/comment-style differences via comment families
- resolution uses the same `NotePathCache` semantics as the rest of the vault, so use Obsidian-compatible display links such as `[[coderef-contract|SPEC-0016]]` in comments)
- incremental indexing stays cheap at file granularity
