---
type: TechnicalSpec
summary: "Defines the implementation contract for unified search orchestration, answer packet shaping, NodeRef provenance, and context packing boundaries."
id: SPEC-0035
spec-status: active
last-updated: 2026-07-11
aliases:
  - SPEC-0035
  - unified-search-answer-architecture
  - Unified search answer architecture
---

# Unified Search Answer Architecture

## Summary

Unified search is a staged pipeline: normalize inputs, resolve seeds/intent, plan retrieval, execute retrievers, rank and shape candidates, optionally pack text, then assemble an answer packet. Runtime boundaries matter. `pkg/search` owns retrieval/ranking mechanics, `pkg/app/unifiedsearch` owns CLI-facing orchestration, `pkg/app/answer` owns pure answer-packet selection, `pkg/app/presentation` owns ranked-result text rendering, and `pkg/app/contextpack` owns budget-aware packing.

This spec complements [[search-answer-workflow]], [[Search - Answer engine packets]], and [[primary-semantic-chunks-and-noderef-search]].

## Goals

- keep search/answer behavior split across stable implementation boundaries
- make NodeRef metadata additive provenance, not a second retrieval path inside answer assembly
- preserve deadline-aware partial results and warnings
- keep context packing deterministic and caller-budgeted
- make docs and code anchors surface the architecture from orchestration entrypoints

## Non-Goals

- changing ranking weights or runtime behavior in this documentation slice
- replacing `knowledge.Handle` as the current merge key
- moving ontology projection into `pkg/app/answer`
- adding new public flags or MCP tools
- introducing a new search backend

## Requirements

### Package boundaries

- `pkg/app/unifiedsearch` MUST own provider/store bootstrap, query normalization, seed handling, intent inference, planner wiring, multi-query fan-out, and conversion into answer inputs.
- `pkg/search/planner` MUST build plans without executing retrieval.
- `pkg/search.Service` MUST execute already-built retrievers, rankers, shapers, rollups, and optional packers under the caller context.
- `pkg/app/answer` MUST remain pure: no retrieval, embeddings, filesystem walks, schema projection, or live ontology reads.
- `pkg/app/presentation` MAY perform bounded reads for already-ranked items when rendering packed text.
- `pkg/app/contextpack` MUST remain a deterministic priority/score/key packer with optional compression delegated through `Compressor`.

### Answer packet contract

- Answer assembly MUST preserve candidate provenance, score, role, specificity, NodeRef metadata, and link targets already resolved upstream.
- Answer assembly MUST select must-read items by required role coverage before filling supporting items by score.
- Confidence MUST reflect target status, warnings, selected evidence, and required role coverage.
- `nextQueries` MUST be returned when coverage is incomplete enough that a narrower follow-up is better than broader retry.
### Ontology context

- Presentation-time ontology context MAY use a request-scoped `noderead.Scope` only for already-ranked results.
- Search ranking MUST remain selective; broad embedding similarity MUST NOT crowd out direct docs/code evidence in explanatory packets.

### Context packing

- Default contextpack budget MUST track `contextpack.DefaultBudgetChars`.
- Agent CLI surfaces MAY impose a higher local default floor when no explicit budget or config budget is larger.
- Packed search output MUST pass an explicit search budget instead of relying on global agent defaults.
- Packing order MUST stay deterministic: priority descending, score descending, key ascending.
- The first piece MUST remain includable by trimming when it alone exceeds budget.

### Observability

- Orchestration comments and module docs SHOULD link to the smallest durable docs that explain behavior.
- Target-resolution warnings, vector/index availability warnings, and coverage gaps MUST remain visible to callers.
- Validation SHOULD include broken links, ontology shape, code frontmatter, and code anchors for documentation-only changes.

## User Stories

### US1 - Read the orchestration path and see the product/technical docs that constrain search and answer behavior
- id:: ^SPEC-0035-US1
- summary:: Read the orchestration path and see the product/technical docs that constrain search and answer behavior.
- status:: ready

#### Acceptance Criteria

- `file-context` on unified search or answer packages surfaces the search hub, answer packet reference, and this technical spec.
- Code comments identify where planning, execution, answer conversion, and packing boundaries sit.

### US2 - Change answer packet selection without accidentally adding retrieval or projection reads to the answer layer
- id:: ^SPEC-0035-US2
- summary:: Change answer packet selection without accidentally adding retrieval or projection reads to the answer layer.
- status:: ready

#### Acceptance Criteria

- `pkg/app/answer.Build` consumes only answer inputs, warnings, target status, and query text. ^SPEC-0035-US2-AC1
- NodeRef and link-target fields are preserved as provenance already supplied by upstream adapters.
- Required role coverage is selected before supporting evidence fills the packet. ^SPEC-0035-US2-AC3

### US3 - Keep planning, retrieval execution, presentation hydration, and context packing separated while still producing budgeted search output
- id:: ^SPEC-0035-US3
- summary:: Keep planning, retrieval execution, presentation hydration, and context packing separated while still producing budgeted search output.
- status:: ready

#### Acceptance Criteria

- `pkg/search/planner` constructs retrievers, rankers, shapers, and optional packers without executing retrieval.
- Presentation may hydrate already-ranked snippets but must not broaden the result set or choose answer roles.
- Contextpack keeps priority, score, and key ordering deterministic and includes the first required piece by trimming when necessary. ^SPEC-0035-US3-AC3

## Open Questions

- whether `pkg/app/presentation` should eventually move ontology rendering behind an explicit enrichment interface
- whether multi-query dedupe should expose per-facet coverage in the answer packet
