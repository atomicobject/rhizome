---
summary: "Second untangle pass: dataflow audit and concrete candidate evidence."
reference-kind: analysis
---

# Second untangle audit: data flow

Baseline code: `b0d0cf824a9b7b7fc9e1832fd31aa5e7555e2f0e`. Inspected HEAD `2a9541828116bada06127cb80ca1575c2ef4c03a`; the difference is review documentation. No repository files were edited, and no tests, builds, commits, pushes, provider requests, or subagents were run.

## Evidence and coverage

Read the pass-two handoff, first-pass delivery/retention record, indexing and search/code-intel audit records, owning skills, subsystem guidance and package contexts. Compounding lookup returned **No current context shards match**. Used source inspection, repository-wide symbol/caller searches, and a lexical declaration/reference census to locate candidates; every finding below was then checked in its actual caller context. The census is a lead generator, not proof by itself. No live semantic/Rhizome retrieval was attempted in this read-only, no-provider lane; governing evidence comes from the current checked-in notes and source. One attempted `pkg/app/codeintel/AGENTS.md` read found no file; `pkg/anchors/AGENTS.md` and the root instructions supplied test ownership rules.

Assigned coverage: anchors, embedindex, embeddings, search, graph, indexstore, application indexing/indexcore/indexwriter/unifiedsearch/codeintel and related command callers. Coverage is structural scans plus focused source tracing, not a claim that every line was manually read. The first-pass writer, provider-batching, and language-map changes were inspected before looking for remaining seams.

## I2-01: Remove remaining language reinterpretation after ownership is known

**CONFIRMED hidden links. Recommended.**

Two consumers still reproduce the extension policy centralized in the first pass:

- `pkg/app/unifiedsearch/application/assessment.go:334-378`: `codeResultLanguageConflicts` calls private `codeLanguageFromPath`, which repeats `.py`, `.go`, `.cs`, `.php` and the TS/JS family already owned by `codeanchor.LangForPath` (`pkg/anchors/indexer.go:24`). This is used by `AssessQuery` through facet assessment (`application/facets.go:144`). Adding a supported extension currently requires changing both the indexing registry and this answer-eligibility map.
- `pkg/anchors/service_indexing.go:133-152`: the parser timeout branch reconstructs language via another private extension map. This map already omits PHP. The selected `LanguageIndexer` is available and its `Lang()` is already used by the adjacent panic-result path (`:109-110`), so a timed-out PHP/custom-indexer invocation loses its known language solely because of this duplicate map. Callers are `batch.go:130` and `call_edge_rebuild.go:1227`.

**Smallest preserving design:** In answer assessment call `codeanchor.LangForPath(strings.TrimSpace(result.Path))` and preserve the current unknown-path behavior; delete `codeLanguageFromPath` and its now-unneeded imports. In parser timeout use `idx.Lang()` directly and delete `langFromPath`. The latter corrects the PHP omission and preserves the already-selected indexer identity instead of re-inferring it. The branch at `service_indexing.go:93` testing whether the positive constant `defaultIndexFileTimeout` is nonpositive is also unreachable and can be deleted in the same narrow cleanup.

**Verification:** Extend existing `TestAssessQueryDoesNotPromoteCodeFromTheWrongExplicitLanguage` / mixed-language coverage with uppercase extensions, whitespace around a path, the complete TS/JS extension family, PHP and unknown paths; assert literal relevance outcomes, not just registry equivalence. Add a timeout characterization with a synthetic PHP indexer and assert `ParseTimeout` plus `LangPhp`. `testing/synctest` is already used in this repository (`pkg/app/web/server_integration_test.go:330`), so the actual timeout branch can be exercised without a real 30-second delay or a mutable production timeout. Preserve the existing panic-result and prior-published-state behavior. Root should run focused anchors/application tests and the required gates.

**Risk/limit:** The timeout identity correction changes previously incorrect metadata for PHP or an indexer whose language disagrees with its path. Do not conflate this with FQN/parser output migration. Preserve `default_intent.go:isCodePath`, which intentionally recognizes a broader set of code-looking query paths than supported structural indexers.

## I2-02: Delete the retired explicit-seed normalization route and fix its stale documentation

**CONFIRMED surplus and misleading documentation. Recommended.**

`pkg/search/locality.go:11-38` exports `NormalizeExplicitSeedPaths`, but all references are its declaration, two tests in `locality_test.go`, and a stale active explanation in `docs/reference/analysis/Search - Seed expansion strategies.md:41`. Its private `normalizeExplicitSeedTokens` (`locality.go:203-221`) is used only by that retired function.

The live route is explicit in `pkg/app/unifiedsearch/run.go:116-122`: resolve typed handles using the request's corpus/path ownership, then derive paths with `search.ExplicitSeedPathsFromHandles`. The retired route duplicates earlier raw-token parsing and silently drops bad inputs; testing it does not protect the live route, which rejects invalid raw paths during `canonicalizeRawSeedPaths`.

**Smallest fix:** Delete `NormalizeExplicitSeedPaths` and the now-unused private token normalizer. Rewrite the two relevant tests to exercise the production seed resolution/canonicalization route and `ExplicitSeedPathsFromHandles`, or retain equivalent existing coverage there and delete only the redundant tests. Update the active seed-expansion note to describe the current corpus/handle route. Keep `explicitSeedPathFromVault` and `explicitSeedPath` because `search/repair.go:178` still consumes them. Keep the explicit Markdown compatibility and ownership rules unchanged.

**Verification:** Literal canonical note/file handles, duplicates/order, mixed-case Markdown, and outside-vault raw-path rejection. Run search and unifiedsearch tests. Do not port the old function's silent-dropping behavior into the production path to preserve a dead test.

**Risk:** Removes an exported Go helper with no repository runtime consumer; unknown external Go consumers would require migration. No agent/CLI/HTTP wire contract changes.

## I2-03: Make answer tests exercise the production entry points

**CONFIRMED test-only forwarders. Recommended.**

- `pkg/app/unifiedsearch/application/assessment.go:15-17`: `Assess` only calls `AssessQuery("", ...)`; every caller is in `application_test.go`.
- `pkg/app/unifiedsearch/application/answer_assembly.go:109-111`: `BuildAssessedAnswer` only calls private `buildAssessedAnswer` with nil facets; all callers are in `application_test.go` and `audit_owner_test.go`.

The live assessment route is `AssessQuery`, and the live answer route is `BuildAssessedAnswerForFacets` (`application/answer_assembly.go:289`, called through `unifiedsearch/execute.go:373-378`). Its empty/single ordinary facet branch already selects the same private assembly.

**Smallest fix:** Remove the two exported forwarders. Call `AssessQuery("", ...)` for tests intentionally characterizing absent query text, and call `BuildAssessedAnswerForFacets(..., nil, ...)` for no-facet answer cases. Preserve literal expectations and strengthen query-aware tests where the intended claim depends on actual query text. No new wrapper or dependency is needed.

**Verification:** Run all `pkg/app/unifiedsearch/application` tests and the unifiedsearch execution contract tests. Keep facet coverage, target-established confidence, wrong-language demotion, weak corroboration and no-answer tests intact.

**Risk:** Exported Go API removal only. Test readability may use a local test helper if repetition becomes material, but no production API should exist solely to shorten tests.

## I2-04: Remove unused projections of call-edge residuals

**CONFIRMED surplus. Recommended.**

`pkg/anchors/call_edge_backfill.go:66-104` defines `CallEdgeResidual.SymbolKeys`, `ModuleKeys`, and `FallbackFiltersBySymbolKey`. Repository-wide searches found only three assertions in `TestCallEdgeResidualKeyViewsDeduped` (`call_edge_backfill_test.go:31-49`). Production consumes `Residual.SymbolRefs`, `.Modules`, and `.Fallbacks` directly in `PlanCallEdgeRebuildIncremental` (`service_intel.go:904+`) and `MergeCallEdgeResidual` (`:882`). These getters add another normalization representation without a consumer.

**Smallest fix:** Delete the three getters and their getter-only test. Preserve the `CallEdgeResidual` type, `HasPending`, merge function, live planner, actual residual retry behavior, `DurableRefFootprint`, and its live test.

**Verification:** Existing `TestMergeCallEdgeResidual_DedupesFallbacks`, residual replay/planning tests, and referrer-batch tests. The latter's `WithCallEdgePlanProgress` hook was explicitly retained in the first pass and is outside this deletion.

**Risk:** Exported Go API removal with no in-repository runtime or documentation consumer. Do not remove the underlying residual state or treat fallback filters as obsolete merely because this accessor is unused.

## I2-05: Remove two dormant code-intel adapter names

**CONFIRMED surplus. Recommended.**

- `pkg/app/codeintel/ingest.go:357-361` retains `ValidateNotesWithWriter`, whose sole implementation forwards to `ValidateMarkdownCodeAnchorSyntaxWithWriter`. No Go caller exists; the only other occurrence is a stale architecture-test exemption in `pkg/noteformat/architecture_test.go:445`. The command uses the separate, still-live `ValidateNotes` default-output adapter (`cmd/code.go:135`).
- `pkg/app/codeintel/ingest_candidates.go:106-113` retains `IngestMarkdownCandidates`, a nil-sources wrapper used only by `ingest_candidates_test.go`. The production structural publisher calls `IngestMarkdownCandidatesWithSources` (`pkg/app/indexcore/publish.go:184`).

**Smallest fix:** Delete `ValidateNotesWithWriter` and its architecture exemption. Delete the no-sources production wrapper and migrate its tests to `IngestMarkdownCandidatesWithSources(..., nil, ...)`. Transfer any still-useful boundary comment to the live function. Keep `ValidateNotes`, the explicit Markdown syntax validator, preclassified candidate validation, sealed-source checks, failure propagation and source reuse.

**Verification:** Codeintel candidate tests (especially invalid candidate sets, unchanged/forced input and sealed source mismatches), code-anchor command tests and noteformat architecture tests. The latter is outside this lane's source ownership; coordinate with root/core before editing. Core auditor was notified of the exact exemption.

**Risk:** Exported Go helper removals. No change to actual Markdown compatibility or ownership classification.

## I2-06: Remove unused progress mode rather than testing a retired path

**CONFIRMED surplus. Recommended.**

`pkg/app/indexing/unified_progress.go:113-129` implements `SetTailStep`; its only call is in `TestIntegratedProgressScalesStagesIntoSingleBar` at `unified_progress_test.go:35`. Live indexing uses named checkpoints via `AdvanceTo`, then `Finish`, and never expresses the tail as step/total.

**Smallest fix:** Remove `SetTailStep`. Keep the progress test but drive its tail using the actual `AdvanceTo` checkpoints and assert literal monotonic output and final completion. Preserve scaled main-phase updates, visible finalization headroom, verbosity, and the existing progress locks.

**Verification:** Existing unified-progress tests plus a literal live checkpoint sequence. No timing, concurrency or total/percentage policy changes.

**Risk:** Internal helper only. Avoid collapsing the real named progress checkpoints into a new generic phase abstraction.

## I2-07: Delete an entirely unused chunk-row counter

**CONFIRMED surplus. Recommended, very small.**

`pkg/app/indexwriter/estimates.go:68-74` exports `CountCodeChunkRows`. The declaration is its only repository occurrence, including tests and documentation. Actual row accounting belongs to the typed payload kind in `payload_kinds.go`.

**Smallest fix:** Delete it. No replacement or new test is needed. Existing writer batching/metrics tests cover row accounting where it is actually used.

**Risk:** Exported Go API removal, no in-repository caller.

## Retained after inspection

- The first-pass `RankRelated`, live watcher, raw-note fallback, single durable writer and distinct batch/live scheduling remain justified. No new contrary evidence.
- Current `indexwriter` code-kind identity and one-call provider helper match their first-pass resolutions. No attempt to reopen them.
- `ResetDomainPreservingCache` has test-only Go calls but represents a documented storage-reset capability with meaningful cache-preservation, read-only rejection and cross-domain isolation contracts. Unlike the forwarders above, removing it is not justified solely by its caller count.
- The language names in target parsing and the broader code-looking filename heuristic are not blindly collapsed into the structural extension registry. Their token/intent policies differ. Any further consolidation needs precise behavior characterization, particularly ordinary English `go`, quoted identifiers and unsupported language-looking files.
- The whole-process ontology goroutine-bound test and retained Ollama concurrency checks are unchanged; this pass adds no evidence supporting weaker substitutes.
- No new generic registries, adapters, runtime knobs, storage/schema migrations, or broad folder moves are proposed. The tiny dead helpers above are grouped for execution convenience, not evidence that their domains should merge.
- Graph-path fact/edge/alias projection duplication in cmd/MCP belongs to the runtime audit lane and was deliberately left to that owner. The core lane owns the separate unused GraphDoc* Store methods.

## Verification limit

This lane ran no application execution, tests, builds or benchmarks. Findings are confirmed source/caller relationships; the PHP timeout effect follows the visible duplicate map, but its actual timeout branch has not been executed in this pass. Root should validate accepted fixes through the real calling paths and preserve the first-pass retention record.
