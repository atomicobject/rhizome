# Search, code intelligence, embeddings, graph and viewscript structural audit

Baseline: `900eee0b0036ee2699d84d5d02d5ffe6a39a4441`. Read-only audit; no source edits or tests run. Compounding lookup returned no matching shards. Read the code-intel/search skills, normative subsystem notes, local AGENTS and current CONTEXT files. A source/file inventory and repository-wide caller census preceded focused source inspection. Coverage is structural sampling plus cross-repository reference tracing, not a claim that every branch of the approximately 59k production Go lines in the assigned directories was executed.

## Recommended changes

### SC-1. Remove the unconsumed per-path runtime version registry (confirmed surplus)

- Evidence: `pkg/anchors/service.go:53-54,100` allocates `runtimeCodeVersions` and `runtimeCodeSeq`; `pkg/anchors/runtime_code_sources.go:52-53` updates both for every published work item; `runtime_code_sources.go:82-102` exposes `RuntimeCodeSourceVersions`.
- A repository-wide exact-name census found no caller of `RuntimeCodeSourceVersions`. The version map has no consumer except that unused accessor. Actual fresh rebuilds read `runtimeCodeSources` and `runtimeCallFiles`, which remain necessary.
- Change: delete the accessor, two fields, constructor allocation and two publication statements. Keep source snapshots, clone helpers, runtime call-file map, publication mutex and cache generation intact.
- Scope: those two files. No existing tests mention the unused registry. Existing runtime-source/call-edge tests verify the behavior that remains.
- Verification: focused anchors runtime-source and call-edge rebuild tests, then anchors package tests. Run full race verification with the parent gate because this touches publication bookkeeping, although synchronization does not change.
- Risk: removing an exported Go method technically changes that Go API; no in-repository or current explanatory-document consumer was found. This is not the separate cache-generation or durable-ref version contract.

### SC-2. Use one language-by-extension registry (confirmed hidden link)

- Evidence: `pkg/anchors/watcher.go:26-32,338-349` has the canonical extension table and `LangForPath`; `pkg/app/codeintel/ingest.go:663-680` duplicates that policy in `DetectCodeLang`. Both currently recognize py/go/cs/php and the same eight TS/JS extensions. `cmd/code.go:636-638` adds a pass-through `detectCodeLang`, consumed at lines 83 and 572. `cmd/index_explain.go:58` independently uses `LangForPath`. `pkg/app/codeintel/candidate_test_helpers_test.go:40` uses `DetectCodeLang`.
- Change: make CLI and candidate helper consumers use existing `codeanchor.LangForPath`, preserving empty-language/false handling; remove `DetectCodeLang` and the CLI wrapper. Move the table/helper from `watcher.go` to the existing language contract area (`indexer.go` or a small language file) so basic language identity is not owned by the watcher implementation. The watcher consumes the same table.
- Scope: anchors language ownership, codeintel ingest/helper, cmd/code.go, cmd/index_explain.go remains a parity consumer. Preserve JS/TS family via `pkg/codefile` and all existing language-specific semantics.
- Verification: one behavioral table covering all supported extensions, uppercase, unsupported and extensionless paths; existing code commands, PHP and TS/JS fixture tests. No indexer-version bump is needed if outputs are byte-for-byte identical.
- Risk: do not replace this with a broader extension table for coderef-only formats. HTML/CSS/templates must not become structural languages.

### SC-3. Retire the unused pre-pipeline embedding Indexer (confirmed surplus with a documentation decision)

- Evidence: `pkg/embeddings/indexer.go:27-462` defines a standalone scanner, alternate chunk/embed/write lifecycle, `Indexer`, `NewIndexer`, `SyncVault` and `UpdateNote`. Repository-wide references show no production construction or invocation. Only `pkg/embeddings/indexer_ignore_test.go:24-25` constructs it and calls `ScanVault`.
- The actual raw-note compatibility path is `pkg/app/indexing/semantic_core.go:9-22` calling `embedindex.NoteSyncer.Plan`/`Embed`, built by `pkg/app/indexing/unified_semantic.go:138` and `pkg/app/bootstrap/live.go:1499`; live derived execution uses it at `pkg/app/bootstrap/derived_scheduler.go:326-353`.
- Current `docs/hubs/Embeddings (Hub).md` and `docs/reference/analysis/Embeddings - indexing pipeline.md` still call the old `indexer.go` the ontology-unavailable compatibility path. Correct these explanatory links in the same approved change. Unlike RankRelated below, I found no explicit prior decision preserving this Indexer as an external extension API.
- Change: remove the old Indexer and its private helpers/FileDiscoveryFunc. Migrate the ignore scenario to the active ownership/discovery-plus-NoteSyncer integration test, asserting the ignored path never reaches provider/persistence. Then remove the obsolete scanner test.
- Keep `embeddings.Index`, `LazyPruningNoteIndex`, `NoteChunkSyncIndex`, `NoteFileInfo`, chunk DTOs and typed SQLite adapters: these have real consumers in embedindex, MCP and runtime views.
- Verification: focused embedindex raw-note tests and indexing ignore/ownership tests; full integration/race gate. Test the ontology-unavailable compatibility path explicitly.
- Risk/decision: this removes an exported but unused-in-repo legacy Go API and changes a misleading current doc claim. Record that retirement in the plan. Do not silently delete the real raw-note compatibility behavior.

### SC-4. Remove stale command-era helpers and test-only discovery adapters from codeintel (confirmed surplus)

- Evidence: `pkg/app/codeintel/ingest.go:646-661` (`FormatLangCounts`) and `:682-733` (`PopulateTailIndexFromRoots`) have no call sites. The latter duplicates traversal/ignore policy and silently swallows all walk failures, but never runs. `MergeLangCounts` is live within the same file and can simply become private.
- `pkg/app/codeintel/doclinker.go:20-46` exposes `NewDocLinker` and `NewDocLinkerWithStore`, starting a new filesystem discovery and turning errors into nil. Their only outside caller is `doclinker_test.go:19`. Production uses `NewDocLinkerFromNotePaths`, matching the single ownership walk invariant.
- Change: delete unused FormatLangCounts/PopulateTailIndexFromRoots, make MergeLangCounts private, and remove the old DocLinker discovery constructors. Migrate `TestDocLinkerPersistsMixedCaseAuthoredNotePath` to `NewDocLinkerFromNotePaths([]paths.NotePath{"Decision.MD"}, nil)` and retain its real SQLite persistence assertions. This test catches a real bug and should not be deleted.
- Scope: ingest.go, doclinker.go, doclinker_test.go and now-unused imports. Existing codeintel documentation already describes canonical explicit candidates and one discovery owner.
- Verification: codeintel tests including mixed-case paths and alias resolution, package compilation, content-link validation if any source binding names a removed symbol.
- Risk: these are exported functions but have no production/documented extension consumers found. Keep all current discovery and ignore behavior in ownership components.

### SC-5. Remove unreachable progress plumbing, preserving the used planning observer (confirmed surplus)

- Evidence: `pkg/anchors/progress.go:5,7-13,20-28,30-43,60-80` provides call-edge execution, scope execution and scope planning observer hooks. No code calls `WithCallEdgeProgress`, `WithScopeProgress`, or `WithScopePlanProgress`. The last does not even have a read accessor. Execution branches at `service_intel.go:1164` and `service_scopes.go:182` therefore always see nil.
- Change: remove those three observer types/keys/setters, their private getters and their unreachable branches. Keep `WithCallEdgePlanProgress` and its planning observer: `service_referrer_batch_test.go:67` uses it to prove bounded referrer lookup batches, and `service_intel.go:1062` consumes it.
- Scope: progress.go, service_intel.go, service_scopes.go. Do not remove actual indexingperf diagnostics, their metrics, or call-edge progress used elsewhere by another owner.
- Verification: anchors call-edge/scoping suite and `Test...` in service_referrer_batch_test.go; integrated runtime/indexing tests.
- Risk: these exported context hooks are removable only as the approved unused surface retirement. No current user-facing progress consumer was found.

### SC-6. Centralize ranking eligibility tier calculation (confirmed hidden link)

- Evidence: `pkg/search/weighted_ranker.go:76-86` and `pkg/search/service_results.go:52-60` independently define the same ordering contract: exact path/title navigation for non-precision intents, then intent-specific primary evidence, then numeric score. The first applies before owner-cap selection; the second restores order after rollup. Changes must reach both or selected representatives and displayed order diverge.
- Change: add one private ranking-tier computation next to existing `HasPrimaryEvidence` in eligibility.go and consume it at both stages. Keep WeightedRanker precomputation, MaxPerOwner, scoring weights, stage timing and intentional tie-break differences (handle in WeightedRanker; `rankedResultKey` after rollup). No new generic sorting abstraction or relevance package merge is needed.
- Scope: eligibility.go, weighted_ranker.go, service_results.go; focused ordering tests. This is behavior-preserving policy deduplication, not retuning.
- Verification: existing exact navigation/precision/rollup/rerank and archetype regressions; add only a small behavior test exercising the same mixed evidence population through both stages if current coverage misses parity. Existing latency benchmark can verify the helper did not move per-candidate work into repeated expensive operations.
- Risk: do not rescore or re-sort after provider reranking; pinned identity positions are deliberate. Do not change stable fallback identity order.

## Selected comment/test cleanup

- Delete comments made obsolete by SC-1 through SC-5 along with the retired implementation.
- `pkg/anchors/service_cache.go:24` (“Evict LRU (tail)”), `pkg/embeddings/indexer.go:65,84,87` (conditional discovery/normalization/validation narration), and several batching-test narration comments are direct restatements and may go when those files are touched.
- Keep external-cause/invariant comments: transaction freshness, FQN identity, provider credential redaction, raw-note eligibility, default-source-root naming, and why rollup order precedes provider reranking.
- The obsolete legacy scanner test in SC-3 and legacy constructor use in SC-4 should be migrated to real entry points, not deleted without preserving the useful scenario.
- I found no confirmed standalone remaining test that cannot catch a bug. Do not delete package-default, extension-family, alias, race, rollback or cancellation tests just because they are short or use doubles.
- `provider_ollama_concurrency_test.go:90-100` uses a polling sleep and a 50 ms negative observation. That is a candidate for deterministic start/release channels if reproduced as flaky, not established evidence that it currently fails without a bug. It already checks four batches against cap two, final maximum and ordered vectors, so keep its behavioral coverage.

## Reviewed and retained

1. `embeddings.RankRelated`, `RankConfig`, and `Graph`: no production callers, but current documentation explicitly advertises the extension and the previous accepted review expressly preserved it (`docs/reviews/2026-10-17-subsystem-review/delivery-evidence.md:27,117`). Do not relitigate that decision solely from a caller census.
2. Anchors `Watcher` is live through `NewHubSubscriber` in bootstrap/live.go:1189 and handlers :1229-1236. `NewWatcher` itself has no production caller, but deleting the whole watcher would remove live behavior.
3. Language-specific PHP member spelling, Go build-context analysis, TS resolver fallback and Python builtin shadowing are semantic requirements, not removable special cases.
4. Query-side semantic stores are appropriately narrower than embedding-index producer APIs. Search does not import embedindex. No reverse core-to-app dependency was found.
5. `pkg/search/relevance` has one production composition consumer in planner but owns coherent policy modules and behavioral tests. Single-consumer status alone does not justify merging it into planner or base search.
6. `viewscript` shares its importmap.json with the browser kit build. Its decoder panic validates a compile-time embedded asset and is appropriate; do not replace that with silent fallback.
7. Graph algorithms are separated from persistence, matching subsystem direction. No concrete worthwhile boundary change found from sampled source.
8. Best-effort schema loading and optional provider/resource reuse have documented degradation/lifetime requirements. Do not unify credential-sensitive provider equivalence with telemetry fingerprints: the former compares credentials, the latter deliberately excludes them.

## Non-obvious fact

The repository's advertised old raw-note Indexer is not the runtime compatibility path: both batch and live raw-note embedding work already use embedindex.NoteSyncer.

## Audit limits

No performance or flaky-test claim is measured. No external providers, actual vaults, browser or broad tests were run. No language parser/FQN changes are proposed. Parent owns integration checks and plan approval.
