---
type: ProductSpec
id: SPEC-0141
aliases: [SPEC-0141]
spec-status: active
last-updated: 2026-11-17
summary: Compare file paths according to the vault filesystem while preserving stored spelling and distinct files.
---

# Filesystem-aware path casing

## Summary

A vault with an on-disk `mocs` directory and an AO KB schema selecting `MOCs/**/*.md` failed setup on a case-insensitive Mac filesystem. Discovery admitted the notes, but schema selection assigned fallback types and explicit references missed the indexed spelling. Rebuilding did not repair the mismatch.

## Goals

Accept casing differences that identify the same file. Keep discovery, schema selection, selector precedence, and explicit file references consistent. Preserve actual filenames in indexes and returned links.

## Non-Goals

Rename user files, rewrite authored links, change ontology type-name matching, change Obsidian's existing logical title and alias lookup, or modify the repaired AO KB vault.

## User Stories

### US1 - Use a vault despite equivalent filename casing

- id:: ^SPEC-0141-US1
- summary:: A person can set up and query a vault when the schema spelling differs from the same on-disk file, without losing distinct files.
- status:: ready

#### Acceptance Criteria

- On an insensitive filesystem, AO KB setup and indexing succeed when the actual directory is `mocs` and the schema selects `MOCs/**/*.md`.
- File references return the actual indexed spelling, and selectors agree on the winning type.
- On a sensitive filesystem, differently cased files remain distinct and ambiguous links remain unresolved.

## Requirements

- R1. Detect actual filesystem case behavior rather than infer it solely from the operating system.
- R2. On an insensitive filesystem, note includes and schema file selectors accept casing differences. The same policy governs matching and specificity, including admission and source selectors.
- R3. Explicit file references resolve to the canonical indexed spelling before exact catalog lookups. Returned paths retain their actual spelling.
- R4. On a sensitive filesystem, differently cased files remain distinct. An ambiguous logical link must never select an arbitrary file.
- R5. Ontology type names and other non-file identifiers retain their existing case-sensitive semantics.
- R6. A built-CLI fixture reproduces the AO KB failure before the change and passes afterward. Focused tests cover both case policies, specificity, canonical references, and observed filesystem behavior.
- R7. Existing indexes converge to the corrected types through ordinary indexing after the change.

## Open Questions

None. Drew approved this direction and authorized routine implementation choices and end-to-end delivery through full-send on 2026-11-17.
