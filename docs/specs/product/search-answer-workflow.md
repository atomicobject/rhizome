---
type: ProductSpec
summary: "Defines the user-facing search and answer workflow: focused queries, mixed code/doc evidence, answer-shaped packets, and follow-up guidance."
id: SPEC-0034
spec-status: active
last-updated: 2026-07-11
aliases:
  - SPEC-0034
  - search-answer-workflow
  - Search answer workflow
---

# Search Answer Workflow

## Summary

Rhizome search should help humans and agents begin work with a compact evidence packet, not a bare ranked list. The product surface is `rzm search` and `rzm agent semantic-query`: accept a focused question or seed, retrieve code and docs together, return must-read sources with provenance, call out coverage gaps, and suggest the next precise query when evidence is incomplete.

## Goals

- make search output directly usable for task kickoff and implementation planning
- keep answer packets evidence-first, source-linked, and budgeted
- support focused multi-query facets without asking users to write one giant prompt
- preserve warnings for ambiguous targets, unavailable indexes, and missing edge evidence
- make contextpack behavior predictable across CLI, MCP, and agent surfaces

## Non-Goals

- replacing deterministic retrieval with generated prose
- requiring an LLM to assemble answer packets
- hiding raw ranked output needed for debugging and evaluation
- guaranteeing complete code intelligence when indexes are stale or unavailable
- making search a long-lived chat memory system

## User Stories

### US1 - Ask a focused search question and receive the docs, entrypoints, and follow-up queries needed to begin safely
- id:: ^SPEC-0034-US1
- summary:: Ask a focused search question and receive the docs, entrypoints, and follow-up queries needed to begin safely.
- status:: ready

#### Acceptance Criteria

- Search returns an answer-shaped packet with `mustRead`, supporting evidence, coverage, confidence, and `nextQueries`. ^SPEC-0034-US1-AC1
- Must-read selection includes both documentation and implementation evidence for explanatory questions.
- Missing coverage and target-resolution warnings are surfaced instead of being hidden behind a confident answer.

### US2 - Compare answer packets and raw ranked results without changing the underlying retrieval contract
- id:: ^SPEC-0034-US2
- summary:: Compare answer packets and raw ranked results without changing the underlying retrieval contract.
- status:: ready

#### Acceptance Criteria

- `rzm search` defaults to answer-shaped output while retaining raw ranked output for debugging. ^SPEC-0034-US2-AC1
- Primary semantic hits point users at source files, notes, or NodeRefs and preserve their source-owned provenance.
- Query-only targeted modes report ambiguity or unresolved targets when precision cannot be established. ^SPEC-0034-US2-AC3

### US3 - Run a small set of related query facets and receive one merged answer packet with deduped evidence
- id:: ^SPEC-0034-US3
- summary:: Run a small set of related query facets and receive one merged answer packet with deduped evidence.
- status:: ready

#### Acceptance Criteria

- Multi-query input runs bounded independent searches and merges duplicate sources into one answer. ^SPEC-0034-US3-AC1
- The merged answer preserves evidence that a source matched multiple query facets. ^SPEC-0034-US3-AC2
- Documentation guides callers toward two to four focused facets, not a single compound question.

### US4 - Ask search for packed context and receive deterministic, budgeted text that respects the search request instead of inheriting unrelated agent defaults
- id:: ^SPEC-0034-US4
- summary:: Ask search for packed context and receive deterministic, budgeted text that respects the search request instead of inheriting unrelated agent defaults.
- status:: ready

#### Acceptance Criteria

- Packed search output includes the query header and already-ranked results in deterministic priority, score, and key order. ^SPEC-0034-US4-AC1
- Search packing honors the explicit search budget or search fallback budget before global agent-local floors.
- When the first required piece exceeds the budget, it is included in trimmed form rather than omitted.

### US5 - Ask a task-shaped question and receive target-specific structural evidence without generated retrieval vocabulary
- id:: ^SPEC-0034-US5
- summary:: Ask a task-shaped question and receive target-specific structural evidence without generated retrieval vocabulary.
- status:: satisfied
effort:: EFF-2026-07-11-16-30

#### Acceptance Criteria

- `tests_for_code` resolves the named path or symbol and uses test/ref evidence without requiring vector retrieval or generic test vocabulary in source embeddings. ^SPEC-0034-US5-AC1
- `refactor_impact` resolves a bounded target, combines incoming/outgoing calls, refs, anchored docs, and related tests, and fails closed with structured candidates when the target is ambiguous or unresolved. ^SPEC-0034-US5-AC2
- Vague discovery such as “where is X configured?” can use lexical/vector/symbol retrieval over factual primary-chunk identity, surface labels, and named signals, while relationship claims still require structural evidence. ^SPEC-0034-US5-AC3

## Requirements

- Search and semantic-query MUST preserve source provenance for every answer item.
- Answer packets MUST be assembled from retrieved/ranked candidates; they MUST NOT invent sources.
- Explanatory queries SHOULD require documentation and implementation evidence before reporting high confidence.
- Tests SHOULD be required only when the user asks for tests or the selected intent is test-oriented.
- Primary semantic hits MUST remain evidence for their canonical owning sources.
- Targeted modes MUST prefer explicit path seeds and MUST warn when query-only resolution is ambiguous or unresolved.
- Multi-query use SHOULD be a small set of task facets and MUST merge duplicated ranked results before answer assembly.
- Budgeted output MUST document the difference between the 70k contextpack default and the 150k `rzm agent` local CLI floor.
- Packed search output MUST use deterministic piece ordering and MUST keep at least the required header/context piece visible under tight budgets.

## Open Questions

- whether raw ranked debugging should remain a flag on `rzm search` or move behind a report-style tool
- which warning classes should lower confidence versus remain informational
