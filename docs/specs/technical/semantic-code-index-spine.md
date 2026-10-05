---
type: TechnicalSpec
summary: "Defines the canonical semantic code index spine: stable addressable items, ID/path rules, unified SQLite persistence, and directed edge semantics for code and documentation."
id: SPEC-0011
spec-status: active
last-updated: 2026-04-12
aliases:
  - SPEC-0011
  - Rhizome semantic code index spine
  - Semantic Code Index (Design)
---

# Rhizome semantic code index spine

## Summary

Rhizome's semantic code index is the shared substrate for code-intel retrieval, docs-to-code attachment, semantic ranking, and future code-aware navigation. It defines one rebuildable local index with one canonical handle system for addressable code and documentation items.

This spec freezes the durable contract behind the current design notes and implementation work: canonical IDs, vault-relative path normalization, the unified SQLite persistence shape, and the typed edge model that downstream read surfaces consume.

## Goals

- define one stable contract for addressable code and doc items
- keep code-intel persistence rebuildable and local
- preserve deterministic retrieval semantics across MCP, CLI, and future UI surfaces
- make docs-to-code attachment flow through the same spine instead of ad hoc side indexes

## Non-Goals

- promising immutable IDs across arbitrary refactors or repo reshapes
- treating parser-inferred or model-inferred relationships as authoritative without provenance
- replacing language-native tooling such as LSPs
- specifying every higher-level ranking policy that consumes the spine

## Requirements

### Must

- The canonical addressable item types are code anchors and documentation sections.
- Code anchors MUST be addressed by `anchor_id`.
- Documentation sections MUST be addressed by `section_id`.
- Persisted paths used in IDs, retrieval keys, and stored rows MUST be vault-root-relative, normalized to forward slashes, and cleaned of `.` / `..` segments.
- Absolute paths MUST remain an I/O concern and MUST NOT participate directly in canonical IDs.
- The semantic code index MUST live in the existing unified SQLite code index database rather than a separate code-intel database.
- The spine MUST preserve enough location metadata for each addressable item to support deterministic snippets, spans, and follow-on expansion.
- The durable minimum edge vocabulary is directed and typed, including `defines`, `mentions`, `calls`, and `tests`.
- `mentions` edges MUST represent documentation-to-code attachment from doc sections toward code anchors.
- `calls` and `tests` edges MAY remain best-effort, but their provenance and limitations must stay explicit to downstream consumers.
- Downstream retrieval surfaces MUST treat `anchor_id` and `section_id` as the canonical handles, not `(lang, path, symbol)` tuples or other derived lookup keys.
- The index MUST remain rebuildable from source code and notes without manual repair of persisted semantic rows.

### Should

- `anchor_id` generation should prefer stable semantic keys such as language, kind, normalized relative path, FQN, and signature before falling back to span-based disambiguation.
- `section_id` generation should prefer normalized relative path plus structural breadcrumb and byte-range inputs so ordinary edits do not cause unnecessary churn.
- The stored edge model should support reverse lookups cheaply enough for note-to-code and code-to-doc workflows without secondary ad hoc caches.
- The persisted shape should remain small and explicit enough that schema migrations, rebuilds, and diagnostics stay understandable.

### May

- Additional typed edges may be added later when they can be derived with clear provenance and without changing the canonical handle model.
- Higher-level convenience views may expose grouped or aggregated entities, but those views should derive from canonical items rather than inventing parallel identities.

## Open Questions

- whether future code-item types beyond anchors and doc sections need first-class handles or can remain derived views
- whether some currently best-effort edge kinds should graduate to stronger contracts after more language coverage and profiling
