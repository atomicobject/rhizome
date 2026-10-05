---
type: ProductSpec
summary: "Defines the canonical search-quality evaluation corpus for answer packets, raw ranked results, primary-chunk evidence, warnings, fixtures, and update workflow."
id: SPEC-0041
spec-status: active
last-updated: 2026-09-05
aliases:
  - SPEC-0041
  - search-quality-evaluation-corpus
  - Search quality evaluation corpus
---

# Search Quality Evaluation Corpus

## Summary

Rhizome search quality needs a small, repeatable corpus that maintainers can run after ranking, primary-chunk, retrieval-planner, answer-packet, or fixture changes. The corpus compares answer-shaped output against raw ranked results, checks required evidence roles and source provenance, and treats warnings/confidence as first-class quality signals.

This spec complements [[search-answer-workflow]], [[unified-search-answer-architecture]], [[Search - Answer engine packets]], [[Search - Intent and weight tuning]], and [[Search - Seed expansion strategies]]. It defines what must be evaluated; implementation may live in Go tests, fixture-driven integration tests, or a future report command as long as these expectations remain observable.

## Goals

- preserve a canonical query set for search and `semantic-query` quality reviews
- make expected `mustRead` roles explicit per query instead of relying on subjective packet inspection
- compare raw ranked output with answer-shaped output so ranking and packet assembly regressions can be separated
- define when primary semantic hits are acceptable evidence
- make confidence, warnings, and `nextQueries` part of pass/fail review
- keep fixture/index freshness rules clear enough that stale chunk or embedding state is not mistaken for ranking behavior
- require a deliberate update workflow after ranking, primary-chunk, retrieval-planner, answer-packet, or fixture changes

## Non-Goals

- changing runtime search behavior in this documentation slice
- requiring live embedding providers or network access for the corpus
- snapshotting every score, rank, or rendered line exactly
- treating generated enrichment as authored source material
- replacing focused unit tests around rankers, retrievers, target resolution, and answer assembly
- making tests mandatory for every explanatory query; test evidence is required only for test-oriented modes or subsystem overview checks

## User Stories

### US1 - Run a canonical set of search queries with enough structure to compare surfaces, roles, raw sources, and diagnostics
- id:: ^SPEC-0041-US1
- summary:: Run a canonical set of search queries with enough structure to compare surfaces, roles, raw sources, and diagnostics.
- status:: ready

#### Acceptance Criteria

- The corpus names each canonical query, mode, seed or fixture, answer roles, and raw-result expectations.
- Explanatory queries require documentation plus implementation or entrypoint evidence before reporting high confidence.
- Known hard queries stay in the corpus until their failure mode has dedicated regression coverage elsewhere.
- Each corpus entry identifies the user-facing surface, mode, seed flags, fixture/index preparation, limit, and explain/raw setting needed to reproduce it.

### US2 - Distinguish retrieval/ranking regressions from answer-packet shaping regressions
- id:: ^SPEC-0041-US2
- summary:: Distinguish retrieval/ranking regressions from answer-packet shaping regressions.
- status:: ready

#### Acceptance Criteria

- Every corpus run captures answer-shaped output and raw ranked output for the same query, mode, seed, limit, and index state. ^SPEC-0041-US2-AC1
- Raw output may satisfy source-presence expectations, but answer output must satisfy role coverage, confidence, warnings, and `nextQueries`.
- Score or rank movements are acceptable only when must-read coverage and required raw sources still pass or the expected corpus entry is updated with rationale.

### US3 - Accept primary semantic hits only when they improve recall without replacing authored evidence
- id:: ^SPEC-0041-US3
- summary:: Accept primary semantic hits only when they improve recall without replacing authored evidence.
- status:: ready

#### Acceptance Criteria

- Primary semantic hits count as acceptable evidence only when the packet preserves the owning file, note, anchor, or NodeRef. ^SPEC-0041-US3-AC1
- Low-specificity semantic matches must not satisfy explanatory documentation or implementation coverage without direct source evidence. ^SPEC-0041-US3-AC2
- Parent-aware ontology checks require bounded identity/ancestor context in source-owned primary chunks, not live projection performed by answer assembly.

### US4 - Rebuild the right indexes and diagnose stale primary-chunk state before judging search quality
- id:: ^SPEC-0041-US4
- summary:: Rebuild the right indexes and diagnose stale primary-chunk state before judging search quality.
- status:: ready

#### Acceptance Criteria

- Corpus fixtures use deterministic test embedding providers and rebuild local indexes before assertions.
- Primary-chunk convergence is checked with indexing tests and chunk/vector counts before corpus failures are interpreted as ranking regressions.
- Fixture edits that change expected search semantics update the corpus expectations in the same change.

### US5 - Update the corpus deliberately after ranking, primary-chunk, retrieval-planner, or answer-packet changes
- id:: ^SPEC-0041-US5
- summary:: Update the corpus deliberately after ranking, primary-chunk, retrieval-planner, or answer-packet changes.
- status:: ready

#### Acceptance Criteria

- Changes to rank weights, query framing, primary-chunk format/context, target routing, warning classes, or answer role selection run the corpus or document why it was not practical.
- Expected-query changes record the behavior reason, not just the new observed output.
- A recurring hard-query failure gets a focused regression test before the query is relaxed or removed.

### US6 - Turn documented quality expectations into tests or reports without brittle score snapshots
- id:: ^SPEC-0041-US6
- summary:: Turn documented quality expectations into tests or reports without brittle score snapshots.
- status:: ready

#### Acceptance Criteria

- Machine-readable corpus entries preserve the same fields as this spec: surface, mode, query, seed inputs, fixture/index prep, limits, role expectations, raw expectations, answer expectations, warning expectations, and allowed variability.
- Corpus assertions prefer structural checks for roles, paths, NodeRefs, warnings, source provenance, confidence, and next queries over exact rendered line snapshots.
- Any automated corpus runner reports whether a failure is raw retrieval/ranking, answer shaping, primary-chunk/index freshness, or diagnostic visibility before suggesting expected-output updates.

## Requirements

### Corpus entry contract

Every corpus entry must be reproducible from a cold checkout and explicit about which surface it exercises:

- `surface`: `rzm search`, `rzm search --raw`, or `rzm agent semantic-query`; pair answer and raw/explain surfaces when comparing behavior.
- `mode`: the applied search intent, not just inferred wording.
- `query`: the exact text; multi-query entries list each facet separately.
- `seedInputs`: CLI flags exactly as called, for example `rzm search --seed pkg/search/service.go` versus `rzm agent semantic-query --path pkg/search`.
- `fixture`: `current repo` or a named fixture such as `testdata/integration/python-app/vault`.
- `indexPrep`: required prep such as `rzm index`, or deterministic test helper setup.
- `limitBudget`: limit and budget when relevant; compare answer and raw with the same values.
- `mustReadRoles`: required answer-packet roles and any roles that must stay supporting-only.
- `rawExpectations`: paths, anchors, FQNs, NodeRefs, ancestry/identity context, or warning codes expected in raw results.
- `answerExpectations`: selected roles, coverage, confidence, warning visibility, and `nextQueries`.
- `primaryChunkExpectations`: owning source, bounded factual context, and low-specificity exclusions.
- `allowedVariability`: score/rank movement tolerated as long as required sources and roles still pass.
- `updateRationale`: required when an expectation changes.

These fields can live in this spec, Go test tables, JSON fixtures, or a future report definition. The contract is the same either way.

### Canonical query set

Each query is evaluated with the answer packet and the raw ranked result list. "Must-read roles" are answer-packet roles; "raw expectations" are source-presence checks in `--raw` output or `semantic-query.matches`.

| ID | Surface and mode | Query | Seed or fixture | Must-read roles | Raw expectations | Quality focus |
| --- | --- | --- | --- | --- | --- | --- |
| SQ-001 | `rzm search` / `rzm search --raw`, default `search` | `how do we query ontology nodes?` | current repo | overview, documentation, implementation, entrypoint | includes `pkg/ontology/query/execute.go`, neighboring ontology-query files, and `pkg/ontology/query/query_test.go` in raw/supporting evidence when limits allow | hard explanatory query; must avoid generic repo-global docs and weak unrelated decisions |
| SQ-002 | `rzm search --mode subsystem_overview` or `rzm agent semantic-query --mode subsystem_overview` | `search subsystem overview` | CLI: `--seed pkg/search/service.go`; agent: `--path pkg/search` | overview, entrypoint, implementation, test | includes `pkg/search/CONTEXT.md`, [[Search (Hub)]], `pkg/search/service.go`, planner/retrieval surfaces, and at least one search test | seed-local onboarding and subsystem shaper behavior |
| SQ-003 | `rzm search --mode docs_for_code` or `rzm agent semantic-query --mode docs_for_code` | `docs for pkg/app/unifiedsearch/run.go` | CLI: `--seed pkg/app/unifiedsearch/run.go`; agent: `--path pkg/app/unifiedsearch/run.go` | documentation, overview, decision or implementation | includes [[search-answer-workflow]], [[unified-search-answer-architecture]], [[Search - Answer engine packets]], and `pkg/app/unifiedsearch/CONTEXT.md` | code-doc binding for orchestration |
| SQ-004 | `rzm search` / `rzm search --raw`, default `search` | `how are notes embedded?` | current repo | overview, documentation, implementation or entrypoint | includes [[Embeddings (Hub)]] and `pkg/search/semantic` note/indexing code; tests are not required unless explicitly requested | explanatory docs-plus-code coverage without false test requirement |
| SQ-005 | `rzm search --mode tests_for_code` or `rzm agent semantic-query --mode tests_for_code` | `tests for pkg/ontology/query/execute.go` | CLI: `--seed pkg/ontology/query/execute.go`; agent: `--path pkg/ontology/query/execute.go` | test, implementation | includes `pkg/ontology/query/query_test.go` even when the seed came through explicit path handling | test discovery for package-level Go tests |
| SQ-006 | fixture `go_to_def` via `rzm search` or `rzm agent semantic-query` | `definition for push_updates` | `testdata/integration/python-app/vault` after `rzm index` | implementation or entrypoint | top raw result resolves the `push_updates` definition anchor, not only callers | deterministic precision-mode fixture |
| SQ-007 | fixture `callers` via `rzm search` or `rzm agent semantic-query` | `callers of push_updates` | `testdata/integration/python-app/vault` after `rzm index` | implementation, entrypoint | includes `src/todoapp/services/tasks.py` and `scripts/worker.py` | call-edge retrieval and target resolution |
| SQ-008 | `rzm agent semantic-query --mode overview --explain` | `UserStory STORY-001 ready` | `testdata/integration/python-app/vault` after `rzm index` | documentation, supporting | includes `notes/specs/search-rewrite.md`, the embedded story's canonical `nodeRef`, and bounded parent-spec identity from a primary chunk | parent-aware primary-chunk freshness |
| SQ-009 | `rzm agent semantic-query --scope code --explain` plus `code-symbol` baseline | `callers of SearchChunksByVector` | current repo after `rzm index` | implementation | `semantic-query` first page includes a `SearchChunksByVector` code anchor when the symbol probe lane finds one; `nextQueries` includes `code_references` for the symbol; `code-symbol --symbol SearchChunksByVector --language go` remains the exact baseline | symbol-probe visibility and exact-code routing |

### Expected must-read roles

- Explanatory `how`, `what`, `where`, `why`, and `explain` queries require docs plus code/pipeline evidence. High confidence is not acceptable when either role family is missing.
- `subsystem_overview` requires local overview docs, a concrete entrypoint, implementation evidence, and a test or example when one exists in the local cluster.
- `docs_for_code` requires task-specific documentation and local overview. Implementation can be supporting, but should remain visible so the docs can be checked against code.
- Precision modes such as `go_to_def`, `callers`, `callees`, and `tests_for_code` keep the packet narrow. They should not require unrelated docs or tests beyond the mode's purpose.
- Multi-query facets must preserve evidence that a source matched multiple facets before answer role selection.

### Raw-vs-answer comparison

- `rzm search` defaults to answer-shaped output; `rzm search --raw` is the ranked-result debugging surface.
- `rzm agent semantic-query` returns both answer fields (`mustRead`, `coverage`, `confidence`, `nextQueries`) and raw match fields (`matches`, `warnings`, NodeRef provenance, and lane diagnostics when `--explain` is used).
- A raw pass means expected paths, anchors, NodeRefs, or bounded primary-chunk context are present somewhere in the ranked/match set.
- An answer pass means the packet promotes the right roles into `mustRead`, keeps low-specificity matches out of unsupported coverage, reports confidence honestly, and suggests narrower `nextQueries` when coverage is incomplete.
- When raw passes but answer fails, inspect `pkg/app/answer` and adapters such as `pkg/app/unifiedsearch.BuildAnswer` or `pkg/app/mcp.buildSemanticAnswer`.
- When raw fails, inspect planning, retrieval, ranking, fixture freshness, and index state before changing answer selection.

### Primary-chunk acceptability

- Primary semantic chunks are source-owned recall evidence and must preserve their owning path, anchor, note, or durable `nodeRef`.
- A semantic match alone is not sufficient explanatory evidence when direct specificity is weak; the selected item must expose its owning source.
- Bounded ancestry and selected identity fields may explain an embedded node's local context, but relationship claims still require structural evidence.
- Effort, story, task, and other execution-trail targets should stay supporting unless the query explicitly asks for that workflow state.
- Primary-chunk identity and ancestry must come from deterministic source/schema projection. Stale or missing chunks are an index convergence failure, not a passable search-quality result.

### Confidence, warnings, and next queries

- Target-resolution warnings, vector/index availability warnings, and subsystem-overview fallback warnings must stay visible.
- `high` confidence requires required role coverage and no unresolved or ambiguous target warnings.
- `medium` confidence is acceptable for useful packets with explicit missing coverage or warnings.
- `low` confidence is expected for unresolved/ambiguous targets, empty selected evidence, or several missing role families.
- `nextQueries` should be present when missing coverage has a clear narrower follow-up, especially `docs_for_code`, `code_for_docs`, `tests_for_code`, or `subsystem_overview`.

### Fixture and index freshness

- The integration fixture at `testdata/integration/python-app/vault` is the canonical deterministic fixture for precision modes, code anchors, ontology-node primary chunks, and NodeRef provenance.
- Fixture `.rhizome/config.yml` uses deterministic `test` embedding providers; corpus tests must not require OpenAI, Voyage, Ollama, or network access.
- Before primary-chunk checks, run `rzm index` or the test helper that builds the same semantic index.
- Before code-edge checks, run `rzm index` or the integration helper that indexes anchors, refs, and call edges.
- If a semantic check fails after schema, primary-chunk, ontology, or indexing changes, verify chunk/vector convergence and source/context/format/provider fingerprints before changing expected search behavior.
- If fixture notes or code change, expected paths and roles must be reviewed in the same change so the corpus stays interpretable.

### Known hard queries

- SQ-001 is hard because broad explanatory wording can drift to generic docs or weak decision notes; the expected cluster is ontology-query docs plus query implementation.
- SQ-005 is hard because explicit file seeds can bypass normal seed handles; package-level Go test discovery must still find `query_test.go`.
- SQ-008 is hard because ancestor identity can change while stale embeddings still point at outdated context.
- Query-only targeted modes are hard when symbols or paths are ambiguous; the acceptable behavior is a structured warning, not silent broad fallback.

### Update workflow after ranking or primary-chunk changes

1. Identify affected query IDs before editing rank weights, query framing, retriever behavior, answer role selection, primary-chunk context/format, target routing, or semantic chunk families.
2. Rebuild indexes for the affected fixture or current repo.
3. Capture answer output and raw output for each affected query with the same mode, seed, limit, and index state.
4. Compare required roles, expected sources, NodeRef/source provenance, warnings, confidence, and `nextQueries`.
5. If behavior improved but expectations changed, update this spec with rationale and add or update focused regression tests around the underlying seam.
6. If behavior regressed, fix the ranking/retrieval/answer/index seam before relaxing the corpus.

## Open Questions

- whether the corpus should become a first-class `rzm agent report --op search_quality` command or remain a documented set of integration/unit expectations
- whether corpus runs should store compact JSON golden files or assert structural properties in Go tests only
- how strict score/rank tolerances should be once ontology-node primary-chunk indexing stabilizes
