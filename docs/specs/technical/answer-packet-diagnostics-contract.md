---
type: TechnicalSpec
summary: "Defines the answer-stage diagnostic contract for role selection, confidence, missing evidence, and next-query rationale without changing answer behavior."
id: SPEC-0050
spec-status: active
last-updated: 2026-07-11
aliases:
  - SPEC-0050
  - answer-packet-diagnostics-contract
  - Answer packet diagnostics contract
---

# Answer Packet Diagnostics Contract

## Summary

`pkg/app/answer` is the final deterministic shaping stage for search answers. It receives already-ranked evidence, normalizes each item into an answer role, selects `mustRead` by required role coverage, fills supporting evidence, computes coverage and confidence, then proposes narrower `nextQueries` when the packet is incomplete.

This spec defines the answer-stage diagnostic contract beneath [[search-diagnostics-explain-architecture]]. It is narrower than [[unified-search-answer-architecture]] and [[Search - Answer engine packets]]: it names the specific decisions that explain why a raw ranked candidate did or did not become `mustRead`, why a coverage family is missing, why confidence is high/medium/low, and why each follow-up query was suggested. Current `answer.Response` fields expose only the packet outcome and a coarse confidence reason; future trace fields are additive observability over those same decisions. Diagnostics are descriptive only; they must not change selection.

## Goals

- make answer-stage omissions explainable without rerunning retrieval or ranking
- keep role normalization, required-role coverage, specificity filtering, weak-decision deferral, duplicate selection, confidence, and `nextQueries` observable as one trace vocabulary
- give corpus and raw/explain comparisons a precise answer-stage contract
- preserve the purity boundary: `pkg/app/answer` describes decisions from already-adapted inputs only
- make code comments and future diagnostics link to exact acceptance criteria instead of broad subsystem prose

## Non-Goals

- changing answer role selection, confidence thresholds, required roles, or specificity filtering behavior
- adding a public JSON diagnostic payload in this documentation slice
- making answer assembly read indexes, hydrate ontology nodes, inspect files, or rerank candidates
- replacing raw ranked output, `semantic-query.matches`, source-provenance trace rows, or contextpack diagnostics
- requiring tests for explanatory queries unless the query or selected intent asks for tests

## Requirements

### Answer-stage trace ownership

- `pkg/app/answer` owns answer-stage trace vocabulary because it owns role normalization, `mustRead` selection, supporting inclusion, coverage, confidence, and `nextQueries`.
- Upstream adapters such as `pkg/app/unifiedsearch.BuildAnswer` and `pkg/app/mcp.buildSemanticAnswer` own conversion into `answer.Input`; they may attach ranked-result evidence, specificity, NodeRef, and link-target provenance, but they must not choose answer roles after calling `answer.Build`.
- Future raw/explain payloads may serialize answer trace data from adapters, but the trace must reflect decisions made by `pkg/app/answer`, not a recomputation in CLI or MCP presentation code.
- Trace generation must be side-effect free and derived from the same inputs used to build `answer.Response`.

### Candidate identity and role normalization

- Every traced candidate must have a stable identity key matching answer duplicate behavior: path when present, otherwise type/title/symbol.
- The trace should record raw input role, normalized role, type, path, title, score, specificity, direct specificity, granularity, and whether NodeRef/link-target provenance was present.
- Role normalization must explain path-derived roles such as decisions, examples, overview docs, implementation code, and ontology context targets.
- Ontology nodes should derive documentation, decision, or supporting roles from node type, structural relation, source, and path; execution-trail nodes such as efforts, stories, and tasks should remain supporting unless the caller has already provided a more specific role.

### Required roles and must-read selection

- Trace output must list the required roles computed for the intent and query frame before selection begins.
- Explanatory `how`, `what`, `where`, `why`, and `explain` queries require overview, implementation, documentation, and entrypoint coverage unless the selected intent is test-oriented.
- Precision intents such as `go_to_def`, `find_usages`, `callers`, `callees`, `implementers`, `overrides`, and `imports` should require only implementation and entrypoint evidence.
- Selection trace must distinguish the first coverage pass from the fill pass so maintainers can see whether an item became `mustRead` because it filled a required role or because open slots remained.
- Duplicate omission must name the duplicate key and the already-selected item that claimed it.

### Weak evidence and omission reasons

- Low-specificity hits in explanatory queries must be omitted from selection and coverage when direct specificity is below the configured threshold.
- Weak decision items in explanatory queries may remain supporting, but they must be deferred out of the fill pass when direct specificity is below the weak-decision threshold.
- Supporting-window overflow must be a distinct omission reason from low-specificity skip, weak-decision deferral, duplicate selection, and role already covered.
- Trace data should make raw-vs-answer comparisons answerable: a raw source absent from `mustRead` was not necessarily a retrieval failure.

### Coverage and confidence rationale

- Coverage trace must report which non-skipped candidates satisfied docs, code, tests, decisions, and examples.
- Missing coverage must be derived from required roles for the active intent/query frame, not from a universal checklist.
- Confidence trace must record the first deciding condition: weak target resolution, empty `mustRead`, complete coverage with no warnings, useful packet with warnings, useful packet with missing coverage, or several missing role families. This trace is more granular than today's rendered `ConfidenceReport.Reason` and must be computed beside it, not by changing packet selection.
- Warnings lower confidence only through the existing confidence rule; diagnostics must preserve warning visibility but not reinterpret warning severity outside answer assembly.

### Next-query rationale

- Each suggested query must map back to a missing coverage family or broad-intent narrowing rule.
- Missing docs should suggest `docs_for_code` against the first available path when possible.
- Missing code should suggest `code_for_docs` against the first available path when possible.
- Missing tests should suggest `tests_for_code` against the first available path when possible.
- Missing decisions should suggest an overview-style decision query derived from the original query text.
- If coverage is complete but the intent is broad, a `subsystem_overview` follow-up may be suggested against the first available path to tighten local context.

### Invariants

- Answer diagnostics must never change `mustRead`, supporting items, coverage, confidence, `nextQueries`, result ordering, warnings, or provenance fields.
- Answer diagnostics must not depend on live filesystem reads, embedding calls, index reads, ontology projection, structural-context expansion, or packing.
- If no trace is requested, answer behavior and allocation profile should remain essentially the same as today.
- Future tests should prefer structural assertions for role, omission reason, confidence reason, and `nextQueries` over exact rendered text snapshots.

## User Stories

### US1 - Explain why each ranked candidate became must-read, supporting, or omitted
- id:: ^SPEC-0050-US1
- summary:: Explain why each ranked candidate became must-read, supporting, or omitted.
- status:: ready

#### Acceptance Criteria

- The answer trace records candidate identity, normalized role, specificity, direct specificity, provenance presence, and final disposition.
- A source present in raw output but absent from `mustRead` has a concrete answer-stage reason such as duplicate, low specificity, weak decision, role already covered, or support overflow. ^SPEC-0050-US1-AC2
- Trace fields are generated from the same already-adapted inputs passed to `answer.Build` and do not trigger retrieval, ranking, packing, or ontology reads.

### US2 - Preserve the distinction between required-role coverage and score-based fill behavior
- id:: ^SPEC-0050-US2
- summary:: Preserve the distinction between required-role coverage and score-based fill behavior.
- status:: ready

#### Acceptance Criteria

- The trace lists the required roles for the active intent and query frame before selection.
- Must-read selection distinguishes coverage-pass selections from fill-pass selections. ^SPEC-0050-US2-AC2
- Explanatory queries require documentation plus code or entrypoint evidence without requiring tests unless the intent or query asks for tests.

### US3 - See why coverage is missing and why confidence is high, medium, or low
- id:: ^SPEC-0050-US3
- summary:: See why coverage is missing and why confidence is high, medium, or low.
- status:: ready

#### Acceptance Criteria

- Coverage diagnostics identify which candidates satisfied each coverage family and which required families remain missing.
- Confidence diagnostics name the deciding condition, including weak target resolution, empty selection, complete coverage with no warnings, incomplete coverage, warnings, or several missing families. ^SPEC-0050-US3-AC2
- Warning visibility remains caller-owned; answer diagnostics describe how warnings affected confidence without hiding or reclassifying them.

### US4 - Use next-query rationale to recover missing evidence with a narrower query
- id:: ^SPEC-0050-US4
- summary:: Use next-query rationale to recover missing evidence with a narrower query.
- status:: ready

#### Acceptance Criteria

- Each `nextQueries` item records the missing coverage family or broad-intent narrowing rule that caused it. ^SPEC-0050-US4-AC1
- Suggested follow-up modes use the same mapping as answer assembly: docs to `docs_for_code`, code to `code_for_docs`, tests to `tests_for_code`, decisions to overview decision search, and broad complete packets to `subsystem_overview`.
- Suggested paths come from already-ranked answer inputs and are omitted when no usable path exists.

## Open Questions

- whether answer trace structs should live in `pkg/app/answer` immediately or wait until `rzm search --json/--explain` lands
- whether direct-specificity thresholds should become named constants before a public trace exposes them
- whether multi-query facet evidence should be represented in answer trace once upstream merge provenance is available
