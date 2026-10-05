---
type: TechnicalSpec
summary: "Defines the architecture contract for raw and explain search diagnostics without changing ranking, retrieval, packing, or answer behavior."
id: SPEC-0043
spec-status: active
last-updated: 2026-07-11
aliases:
  - SPEC-0043
  - search-diagnostics-explain-architecture
  - Search diagnostics explain architecture
---

# Search Diagnostics and Explain Payload Architecture

## Summary

Search diagnostics are an observation layer over the existing unified search pipeline. They explain how a query became a `QuerySpec`, how the planner chose retrievers and ranking policy, how retrieval degraded or partially succeeded, how ranked candidates were merged, rolled up, shaped, and packed, and how answer roles were selected.

This spec complements [[search-answer-workflow]], [[unified-search-answer-architecture]], [[search-quality-evaluation-corpus]], [[Search - Execution semantics]], and [[Search - Answer engine packets]]. The core invariant is simple: diagnostics MUST NOT alter ranking, result inclusion, pack order, answer role selection, or warning visibility.

## Goals

- define the raw/explain JSON direction for `rzm search` and `rzm agent semantic-query`
- make planner, signal-profile, execution, degradation, rollup, shaper, pack, and answer-selection decisions inspectable
- preserve current ranking and answer-packet behavior while adding observability contracts
- keep warnings first-class and caller-visible in every diagnostic shape
- give future implementation work precise block targets for coderefs, code anchors, tests, and corpus expectations

## Non-Goals

- changing runtime search behavior in this documentation slice
- changing ranking weights, retriever order, merge keys, rollup policy, shaper policy, or contextpack ordering
- replacing answer-shaped output as the default user-facing search packet
- making diagnostics require live LLM calls, embeddings re-runs, or extra retrieval passes
- hiding existing stderr/JSON warnings behind a diagnostics-only field
- designing a complete trace-storage or telemetry backend

## Requirements

### Surface parity and field ownership

- `rzm search --raw`, future `rzm search --json/--explain`, and `rzm agent semantic-query --explain` SHOULD share diagnostic vocabulary for target resolution, warnings, evidence, source/NodeRef provenance, pack metadata, and answer role-selection.
- Surface-specific adapters may omit fields that the surface cannot compute, but they MUST NOT rename equivalent concepts without a compatibility reason.
- Stage-owned diagnostics should be produced where the decision is made: planner fields in `pkg/search/planner`, retriever/degradation fields in retrievers or `pkg/search.Service`, merge/facet fields in unified-search orchestration, pack fields in contextpack/presentation, and answer role fields in `pkg/app/answer`.
- Adapter-owned diagnostics may assemble the final payload, but adapters MUST NOT recompute ranking, revisit indexes, or infer a different answer decision to fill trace gaps.

### Payload modes

- `raw` JSON MUST mean ranked-result observability for the same run inputs: normalized query inputs, mode/intent, target status, warnings, ranked results, candidate identity, final score, evidence summaries, NodeRef/source provenance, and raw match metadata where available.
- `explain` JSON MUST be additive over `raw`: it may include planner, execution, merge, rollup, shape, pack, and answer-selection diagnostics, but it MUST NOT request different retrievers, change limits, change weights, rerank candidates, or repack with a different budget.
- The answer packet remains the default output contract from [[search-answer-workflow#^SPEC-0034-US1-AC1]]. Raw/explain output exists for debugging, quality review, and agent self-diagnosis per [[search-answer-workflow#^SPEC-0034-US2-AC1]].
- `semantic_query --explain` already treats evidence as additive match metadata. Future `rzm search` JSON should follow that direction rather than inventing a second diagnostic vocabulary.

### Planner summary

- Diagnostics SHOULD expose the pre-execution planner summary after query repair and target resolution: text, query facets, seed handles, explicit seed paths, intent/mode applied, target status, resolution confidence, target candidates, limits, budget, retriever names, ranker family, shaper presence, rollupper policy, and packer presence.
- The summary MUST distinguish user input from inferred or repaired fields so target warnings stay understandable.
- The planner summary MUST cite when precision fallback is blocked by ambiguous or unresolved targets, matching [[search-answer-workflow#^SPEC-0034-US2-AC3]].

### Intent and signal profile

- Diagnostics SHOULD expose the selected intent profile and corpus signal profile before execution: vector readiness, refs readiness, graph readiness, ontology readiness, sparse notes/refs, immature vault, notes-first/code-first bias, and adjusted weight channels.
- Signal profile fields MUST be descriptive diagnostics, not hidden scoring inputs outside `pkg/search/planner`.
- Intent labels, weight channels, and readiness names SHOULD match [[Search - Intent and weight tuning]] and `pkg/search/planner` vocabulary.

### Retriever execution and degradation

- Diagnostics MUST preserve caller-visible warnings from query repair, target resolution, runtime degradation, and intent-specific missing evidence checks.
- Retriever diagnostics SHOULD include retriever name, execution status (`ok`, `error`, `timeout`, `canceled`, or `partial`), candidate count, duration when available, and degradation message when one was added to warnings.
- Agent-facing search responses SHOULD expose compact lane diagnostics for major retrieval families (`note_vector`, `code_vector`, `intel_fts`, `symbol_probe`, `refs`, `graph`, `ontology`, `call_edges`, `tests`) so callers can tell whether missing evidence means the lane was skipped, empty, timed out, or degraded.
- Broad-intent retriever failures may degrade into warnings as today; precision-intent retriever failures may remain hard errors. Diagnostics MUST report that distinction without changing it.
- Deadline fallback diagnostics SHOULD explain when retrieval returned partial candidates, ranking fell back to approximate scoring, or packing fell back to minimal text, matching [[Search - Execution semantics]].

### Merge, rollup, and shaper decisions

- Diagnostics SHOULD explain candidate merge decisions by stable handle: which retrievers contributed evidence, which duplicate keys collapsed, and which evidence groups survived bounded evidence merging.
- Multi-query diagnostics SHOULD preserve per-facet match evidence and the source-count boost used by merged results, matching [[search-answer-workflow#^SPEC-0034-US3-AC1]] and [[search-answer-workflow#^SPEC-0034-US3-AC2]].
- Rollup diagnostics SHOULD identify when fine-grained code results were collapsed to higher-level presentation units and which result represented the group.
- Shaper diagnostics SHOULD identify when `subsystem_overview` changed top-window ordering. They MUST report shaper movement separately from rank scores so rank changes are not confused with presentation shaping.

### Pack omissions

- Pack diagnostics MUST expose the requested budget, used budget, trimmed flag, included piece count, and omitted piece count when a packer returns metadata.
- Future explain JSON SHOULD include omitted piece keys and reasons when practical, but omitted-piece reporting MUST NOT require a second pack pass or a different budget.
- Packing diagnostics MUST preserve the existing invariant from [[search-answer-workflow#^SPEC-0034-US4-AC1]] and [[unified-search-answer-architecture#^SPEC-0035-US3-AC3]]: deterministic priority, score, key ordering, with the required first piece trimmed rather than omitted.

### Answer role-selection trace

- Answer diagnostics SHOULD expose the role-selection trace after ranked results are adapted into answer inputs: normalized role, direct specificity, selected must-read slot, supporting inclusion, specificity/decision reason, coverage missing slots, confidence reason, and generated `nextQueries`.
- The trace MUST make answer omissions distinguishable from retrieval/ranking misses. A source present in raw results but absent from `mustRead` should be explainable as role coverage, duplicate selection, low specificity, weak-decision deferral, or support-window overflow.
- `pkg/app/answer` MUST remain pure: diagnostics may describe answer decisions, but answer assembly MUST NOT read indexes, project ontology, retrieve more context, or rerank results. This extends [[unified-search-answer-architecture#^SPEC-0035-US2-AC1]] and [[unified-search-answer-architecture#^SPEC-0035-US2-AC3]].

### Invariants

- Diagnostics MUST NOT alter result ranking, result inclusion, warnings, target resolution, NodeRef/source provenance, packed text, or answer role selection.
- Diagnostics MUST be generated from already-computed planning, execution, ranking, packing, and answer-shaping state.
- Warnings MUST remain visible at the same caller surface that reports results, even when explain diagnostics are disabled.
- Diagnostic field names SHOULD be stable enough for tests and corpus checks, but optional sections MAY be omitted when the underlying stage did not run.

## User Stories

### US1 - Inspect ranked-result JSON and additive explain JSON for the same search run without changing search behavior
- id:: ^SPEC-0043-US1
- summary:: Inspect ranked-result JSON and additive explain JSON for the same search run without changing search behavior.
- status:: ready

#### Acceptance Criteria

- Raw JSON exposes ranked results, evidence, target status, warnings, and provenance for the same run inputs used by answer output.
- Explain JSON adds diagnostics without changing retriever selection, ranking, score order, packing budget, or answer role selection. ^SPEC-0043-US1-AC2
- Warnings remain caller-visible whether or not explain diagnostics are requested. ^SPEC-0043-US1-AC3

### US2 - See how the planner interpreted the query and how each retriever executed or degraded
- id:: ^SPEC-0043-US2
- summary:: See how the planner interpreted the query and how each retriever executed or degraded.
- status:: ready

#### Acceptance Criteria

- Planner diagnostics include repaired spec fields, target-resolution state, intent profile, signal profile, retrievers, weights, shaper, rollupper, and packer decisions. ^SPEC-0043-US2-AC1
- Execution diagnostics include per-retriever or lane-family status, timing when available, candidate counts, partial/timeout fallback, and degradation warnings. ^SPEC-0043-US2-AC2

### US3 - Understand why a source moved, collapsed, disappeared from packed text, or did not become must-read
- id:: ^SPEC-0043-US3
- summary:: Understand why a source moved, collapsed, disappeared from packed text, or did not become must-read.
- status:: ready

#### Acceptance Criteria

- Merge, rollup, and shaper diagnostics distinguish score ranking from grouping or presentation-window movement.
- Pack diagnostics report included and omitted pieces under the actual request budget without a second pack pass. ^SPEC-0043-US3-AC2
- Answer diagnostics explain role normalization, must-read selection, specificity/decision skips, coverage gaps, confidence, and next-query generation. ^SPEC-0043-US3-AC3

### US4 - Use the same concepts across `rzm search` and `rzm agent semantic-query` when investigating search behavior
- id:: ^SPEC-0043-US4
- summary:: Use the same concepts across `rzm search` and `rzm agent semantic-query` when investigating search behavior.
- status:: ready

#### Acceptance Criteria

- Equivalent diagnostic concepts use the same field names or documented aliases across CLI raw/explain output and MCP/agent semantic-query output. ^SPEC-0043-US4-AC1
- Each diagnostic section is owned by the stage that made the decision, while adapters only assemble and serialize already-computed state. ^SPEC-0043-US4-AC2
- Missing diagnostic sections are explicit when a stage did not run or did not record trace state; absence must not imply success.

## Open Questions

- whether `rzm search --raw` should grow `--json` first, or whether raw JSON should be a separate mode from raw text
- whether `search.Response` should own a typed `Diagnostics` field or whether diagnostics should be assembled in CLI/MCP adapters from stage-local trace structs
- how much per-candidate merge trace can be exposed before payload size becomes counterproductive for broad searches
- whether semantic-query's existing `packMeta`, NodeRef, and match metadata should become the naming baseline for CLI diagnostics or converge on a new shared DTO
