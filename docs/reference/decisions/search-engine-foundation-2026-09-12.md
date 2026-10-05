---
type: ReferenceDoc
reference-kind: architecture
summary: "Defines ownership, request identity, index generation, continuation, evidence, and result contracts for the search engine excellence effort."
decision-domain: architecture
status: accepted
last-verified: 2026-09-12
---

# Search engine foundation decision

Status: accepted in the second Phase 1 foundation review on 2026-09-12.

## Ownership and entry points

`pkg/search` owns retrieval, evidence identity, ranking, eligibility, and bounded execution. `pkg/app/unifiedsearch` owns resource wiring, effective request resolution, grouping, selected-source hydration, answer adaptation, and continuation. CLI, MCP, and HTTP translate their public inputs into the application request and serialize its result. `pkg/app/answer` remains pure.

The target application contract is `EffectiveRequest -> ApplicationResult` in `pkg/app/unifiedsearch/contract.go`. It reuses `search.Intent`, `search.Filters`, `search.RankedResult`, `search.Warning`, and `search.LaneStatus`. It does not create a second source identity.

Current migration seams:

| Caller | Current path | Migration |
| --- | --- | --- |
| CLI raw and answer | `cmd/code_search.go -> unifiedsearch.Run` | Resolve a profile and return the transport-neutral result from the existing application entry point. |
| MCP `semantic_query` | `tool_semantic.go -> semantic_query_unified.go` | Move request policy, score adjustment, grouping, continuation, and confidence decisions into `unifiedsearch`; retain JSON and compact-body serialization in MCP. |
| HTTP Search Workspace | `pkg/app/web/search_notes.go` | Translate note filters into the same effective eligibility controls and map the shared source result to `NoteSearchResponse`. |
| Session body dedupe | MCP compact response assembly | Keep after canonical source pagination. An omitted body keeps its source reference and omission reason. |

No persistent schema is added. The implementation recomputes one fixed candidate window and binds the cursor to a membership-only request identity, vault identity, composite index generation, ranking-policy version, candidate bound, and ordered source digest. Because a supported external provider returned non-identical vectors for repeated identical text, the opaque cursor also retains a bounded exact snapshot of query vectors: at most 16 query texts and 4,096 dimensions per vector. This keeps recomputation stable across processes without query-time persistent writes. Version 1 and malformed cursors return refresh-required or invalid errors; a generation or ordered-window change returns stale. Presentation body budget and visible page size are outside membership identity. Facet text and per-facet mode remain inside it.

The composite generation comes from existing committed state: `note_metadata_state.notes_hash` and `raw_notes_hash`; a deterministic digest of the Intel `files` rows used by lexical and structural code search; the note and code embedding stores' `visible_generation` values published by `CommitSyncGeneration`; and the Intel store's scope-config hash and indexer version. In-progress embedding sync generations stay invisible. Note catalog/provider-region publication changes the note hashes. Code indexing changes the ordered files digest even when embeddings are disabled. `unifiedsearch.IndexGeneration` reads and hashes these values; it adds no table or compatibility shim.

## Effective policy

Intent states what evidence is requested. Profile states bounded work and presentation defaults. Precision intent overrides profile diversity preferences. Explicit valid controls override their corresponding defaults; a visible limit above the fixed candidate window is contradictory.

| Profile | Visible sources | Candidate window | Owner cap | Body budget | Deadline |
| --- | ---: | ---: | ---: | ---: | ---: |
| interactive | 10 | 100 | 2 | 8,000 characters | 3 seconds |
| agent | 20 | 240 | 3 | 24,000 characters | 8 seconds |
| either, precision intent | profile default | at most 100 | 1 | profile default | profile default |

Multiple query facets retain their individual normalized text and mode. Exact duplicate facets collapse before retrieval. Literal paths and symbols retain authored case and punctuation for canonical resolution; prose normalization is a separate form. Filters intersect. A contradiction returns an invalid-request status before retrieval.

## Evidence and ordering

Canonical handle identifies the entity. Evidence fact identity is signal type plus semantic details such as relationship, target, source locator, or independently meaningful facet. Retriever name and lane rank are provenance, not independent facts. Repeated observations merge provenance and contribute once. Different relationships or facets remain independent. Bounded retention orders facts by normalized strength and stable fact identity before applying caps.

Uniquely resolved exact targets occupy the highest eligible tier. Within a tier, continuous engine score orders sources with canonical handle as the stable tie-break. Grouping uses the best member score and never clips before ordering or manufactures differences from output position. Owner diversity cannot displace a unique exact target.

## Result and confidence

The application result carries effective request identity, canonical ordered sources, lane availability, warnings, bounded counts, continuation, and availability. Each source has a typed primary/supporting eligibility, requested relationship, assessed relevance level and reason, and the answer roles it can support. Target resolution has its own status, confidence, and candidates. The result also carries the pure `answer.Response`, whose must-read and supporting items reference those assessed sources. Adapters serialize this information and cannot independently infer roles, relevance, confidence, or target state.

Primary evidence must satisfy the requested relationship or exact target. Other relevant material is supporting evidence.

Confidence is assembled from source relevance, target resolution, relevant required-role coverage, and lane availability. Populated slots alone are insufficient. Strong complete evidence can be high; useful but incomplete evidence is medium; weak, empty, ambiguous, or unavailable evidence is low or abstains. Presentation truncation and optional diagnostics do not reduce semantic confidence.

## Failure mapping

| Condition | Status |
| --- | --- |
| Contradictory filters or changed cursor controls | invalid request |
| Legacy cursor | refresh required |
| Changed index generation or ordered window | stale continuation |
| Exact target missing | unresolved target |
| Exact target has several canonical matches | ambiguous target |
| Required exact resolver unavailable | unavailable exact evidence |
| Optional semantic lane unavailable with lexical evidence | partial availability |
| Empty eligible corpus | empty eligible corpus |
| Eligible corpus searched with no relevant match | no strong match |

## Foundation tensions and recommendation

- Precision purity versus supporting context: keep relationship-qualified primary evidence separate and allow relevant context only as supporting.
- Abstention versus suggestions: leave the primary answer empty when no strong match exists, while returning qualified suggestions and a narrower exact follow-up.
- Web latency versus agent breadth: use the same correctness rules with two internal bounded profiles and keep diagnostics inspectable.

These choices preserve the existing planner/service/answer boundaries, require no schema migration, and give all adapters one result contract. Phase 2 and later implementation should proceed after review confirms these concrete seams.

The numeric profile defaults are provisional until Phase 7 measures and tunes them. The eight-second agent deadline is the SPEC-0102 hard maximum and cannot be relaxed by tuning.
