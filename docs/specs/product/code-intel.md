---
type: ProductSpec
summary: "Defines the code-intel navigation product surface: deterministic code/document search, handle-based expansion, and compact kickoff packs for implementation work."
id: SPEC-0015
spec-status: active
last-updated: 2026-06-01
aliases:
  - SPEC-0015
  - code-intel
---

# Code Intel Navigation V1

## Summary

Rhizome should expose a compact, deterministic code-intel surface that helps humans and agents find implementation entry points, supporting docs, and related tests without dropping into a full IDE workflow.

## Goals

- provide fast task kickoff for implementation work
- keep navigation handle-based and deterministic
- unify code and documentation retrieval on the semantic code index spine
- support progressive disclosure instead of giant payloads

## Non-Goals

- replacing language servers or IDE navigation entirely
- promising perfect refactor-stable identities
- introducing a separate distributed search backend
- generating long-form explanations as the primary product

## User Stories

### US1 - Unified code and note context for development tasks

- id:: ^SPEC-0015-US1
- summary:: Search for relevant code entities and docs together, then open the right next file quickly.
- status:: ready

#### Acceptance Criteria

- Search returns mixed code-anchor and doc-section results with stable handles.
- **Ranking precedence**: Ranking prefers strong lexical and exact-symbol matches before broader similarity.
  Given a query exactly matches a code symbol
  When code-intel ranks mixed code and note results
  Then exact-symbol matches appear before broad semantic matches
- Result payloads stay compact and truncate with continuation support when needed.

### US2 - Provenance-preserving expansion from one selected entity

- id:: ^SPEC-0015-US2
- summary:: Expand from one handle into tests, callers, docs, or related anchors without losing provenance.
- status:: ready

#### Acceptance Criteria

- A selected result can expand through typed edges such as `calls`, `tests`, and `mentions`.
- Returned expansions preserve canonical handles and file provenance.
- Best-effort edge kinds stay clearly marked as best-effort.

### US3 - Budgeted kickoff packs for implementation context

- id:: ^SPEC-0015-US3
- summary:: Request a compact kickoff pack that surfaces likely entry points, tests, and docs within a strict budget.
- status:: ready

#### Acceptance Criteria

- The product can package a bounded starter pack for a task or seed entity.
- The pack prefers high-signal anchors, tests, and docs over exhaustive listings.
- The pack degrades gracefully when some index domains are unavailable.

## Requirements

- The code-intel product MUST use canonical `anchor_id` and `section_id` handles from the semantic code index spine.
- The surface MUST support search, single-item fetch, and typed expansion operations.
- Search MUST support deterministic lexical retrieval and MAY blend semantic signals when available.
- The product MUST expose code anchors and documentation sections in one navigation model rather than separate products.
- Result ordering MUST remain deterministic under equivalent scores.
- The product MUST preserve enough provenance for the caller to open the backing file and span.
- Kickoff or context-pack style responses MUST stay budgeted and support truncation/continuation.
- `mentions` edges from docs to code MUST remain the primary docs-for-code attachment path.
- The product SHOULD enrich existing retrieval surfaces where possible instead of adding many parallel commands.

## Open Questions

- whether the long-term user-facing surface should stay as enriched existing commands or converge on a single dedicated intel operation
- which best-effort edge kinds are mature enough to expose by default
