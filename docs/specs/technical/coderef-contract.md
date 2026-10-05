---
type: TechnicalSpec
summary: "Defines the coderef contract for code-to-note binding: supported syntaxes, comment-family scanning, incremental indexing, and safe rewrite behavior on note rename or move."
id: SPEC-0016
spec-status: active
last-updated: 2026-04-12
aliases:
  - SPEC-0016
---

# Coderef contract

## Summary

Coderefs are Rhizome's code-to-note binding surface. They let source code explicitly point at durable notes using comment-local syntax that the scanner and rewrite tooling understand. This spec freezes the normative coderef contract: what syntax counts, how comment extraction works, what gets indexed, and how rename or move rewrites preserve links without overreaching.

## Goals

- keep code-to-note binding explicit, lightweight, and language-aware
- preserve cheap incremental coderef indexing on a per-file basis
- support safe rewrite behavior when notes move or rename
- keep coderef behavior stable enough that note links in code can act like durable contracts

## Non-Goals

- turning coderef extraction into a full AST or semantic analysis system
- making coderefs responsible for note-to-code binding or dependent surfacing
- rewriting arbitrary strings in code that merely resemble note links
- using coderefs as the only documentation attachment mechanism in the repo

## Requirements

### Must

- Coderefs MUST remain a code-to-note mechanism authored in code comments or docstrings.
- The scanner MUST support Obsidian wikilinks, markdown note links, and `@mentions` as coderef syntax.
- Comment extraction MUST remain language-aware through lightweight comment-family routing rather than full parsing.
- Files that are too large or appear binary MUST be skipped.
- Incremental indexing MUST remain cheap at file granularity.
- Query surfaces MUST support both directions needed by coderef consumers: notes mentioned by a file and files mentioning a note.
- Rename or move rewrites MUST preserve anchors in markdown links and preserve aliases or anchors in wikilinks where applicable.
- Rewrite behavior MUST avoid partial or ambiguous replacements that would corrupt unrelated text.
- Image embeds and non-coderef markdown constructs MUST NOT be rewritten as coderef moves.

### Should

- Supported comment families should continue to cover the repo's main source and template file types without conflating coderef support with code-anchor or code-intel support.
- Rewrite logic should remain stricter than raw scanning so bulk rename operations stay conservative.
- Coderef resolution should continue to rely on canonical note-path resolution rather than ad hoc basename guessing whenever a pathful reference is available.

### May

- Additional coderef syntaxes may be added later if they remain easy to scan, easy to rewrite safely, and compatible with incremental indexing.

## Open Questions

- whether any current rewrite limitations should be promoted into separate decision or guide material instead of staying implicit in code/tests
