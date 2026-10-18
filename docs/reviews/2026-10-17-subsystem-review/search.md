---
summary: "Lane report from the 2026-10-17 subsystem quality review: Search subsystem review. Findings are a snapshot of commit 5251923f."
---

# Search subsystem review

Scope: `pkg/search/**` (about 32k production lines), `pkg/app/unifiedsearch`, `pkg/app/searchengine`, `pkg/app/semanticops`, `pkg/app/semanticruntime`. That is 46,212 production lines and 27,271 test lines in total. Worktree `t3-e36f9b1a` at `5251923f`. All findings were verified by reading code and grepping callers. Confidence is stated per finding.

## Summary

The ranking core is careful and well tested where it has been tuned recently: `MergeCandidate`, `queryframe`, link-text, and the answer-assessment layer. The plumbing around that core is not in the same condition.

1. **The CLI search path can destroy index data.** `unifiedsearch.Run` opens the note and code embedding stores with the indexer's write-capable constructors. On a provider, model, or dimension mismatch, those constructors reset the domain, dropping the embedding tables and the paid embedding cache. They also run schema migrations and a metadata `UPDATE` on every CLI search. This contradicts the subsystem's "query-only metadata is never repaired" constraint. The live runtime already uses the read-only validator; the CLI path does not (SRCH-1).
2. **The stated concurrency model is not what runs.** Every production caller sets a deadline, so `Service.Search` always takes the *serial* progressive path. The only real concurrency lives inside `AutoExpandRetriever`'s composite. That composite reuses a serial-stage budget formula keyed on list index, so the vector lane (index 0, the only lane with a network call) gets the *smallest* timeout. Any filter (including the GraphQL/Explorer `Types=["note"]`) disables auto-expand and makes every lane serial (SRCH-2).
3. **There is a lot of dead or duplicated ranking machinery:**
   - two of the ten ranking channels (fusion, recency) are never produced;
   - `FusionRanker` is never wired, yet every candidate still carries `retriever_rank:*` evidence for it;
   - a second hand-copied intent→weights table exists;
   - two scoring functions do the same job, and one of them sums in map-iteration order;
   - a dead answer builder (`Run`'s `Result.Answer`) ships with its own role vocabulary and tests.
4. **The package layout hides the request path.** About 9.5k of `pkg/search/semantic`'s 11.3k lines are indexing pipeline code. `graphdb`, `embeddings`, `intentstore`, and `semanticruntime` are indexing-side. `pkg/search` imports `pkg/app/cli` only for two filter fields nothing sets. `searchengine` is a 60-line pass-through with one caller.
5. **Per-request work is redundant.** Full note-catalog and code-file scans run several times per facet. The ontology schema is parsed into a field nothing reads. Doc-section FTS hits re-parse Markdown at query time. Graph-score SQL runs for text queries whose graph weight is zero.

The tests are fast (19s summed across 20 packages, 775 top-level tests, nothing over 1s). They are mostly behavior-named and valuable in the application layer. The main test problems:

- The CI ranking regressions build hand-weighted rankers, or build `search.Service` directly, rather than the production wiring.
- About a dozen tests pin dead code or constants.
- No test covers the three most consequential findings above (store mutation on search, lane budgets under real wiring, rerank pinning through `Service.Search`).

## Package map as it is vs. as it should be

**As it is.** Request path: `cmd`/`mcp`/`web` → `unifiedsearch.Execute` → `unifiedsearch.Run` → `searchengine.Execute` → `planner.Plan` → `search.Service.Search` → `retrieval.*` (which calls `semantic.Searcher` for vectors) → `relevance.*` rankers → rollup and shaping → `unifiedsearch/application` (assessment and answer) → `presentation` (packing).

Indexing-side code also living under `pkg/search`:
- `semantic` (syncers, batchers, pipelines, chunker);
- `embeddings` and its stores;
- `graphdb`;
- `intentstore` and `intent.Syncer`;
- `indexgeneration` (one caller);
- `pkg/app/semanticruntime`.

Eval tooling under `pkg/search`: `qualityeval` (used only by `tools/searchquality`) and `perfworkload` (used only by `tools/searchperf`, and it imports `pkg/app/unifiedsearch`, which inverts the layering).

**As it should be:**

- **`pkg/search` (query core):** `QuerySpec`/`Candidate`/`Evidence`, merge, one scorer, one lane executor, `Service`.
  - Sub-packages: `retrieval` (lanes), `relevance` (score adjusters and rerank), `queryframe` (the one tokenizer), `planner` (returns a ready-to-run `Service`; absorbs `searchengine`), `knowledge`.
- **`pkg/search/semantic`:** query-time `Searcher` and the query-embedding memo only.
- **`pkg/embedindex`** (or `pkg/search/semantic/index`): syncers, batchers, chunker, pipelines, plus `semanticruntime` lanes.
- **`pkg/embeddings`:** providers and stores, plus `semanticops.PrepareProvider` as a constructor.
- **`pkg/graph`:** `graphalg` and `graphdb`.
- **`pkg/app/unifiedsearch`:** request policy, continuation, assessment, answer.
- **`tools/searchquality/internal/qualityeval`** and **`tools/searchperf/internal/perfworkload`.**

## Findings — production code

### SRCH-1 CLI search opens embedding stores with destructive, write-capable constructors
- **Severity:** P0 (data-loss risk on the read path).
- **Confidence:** High for the code path. Medium for how often users hit a mismatch.
- **Location:**
  - `pkg/app/unifiedsearch/run.go:171-191` (note store) and `:198-233` (code store);
  - `pkg/search/embeddings/sqlite/open.go:95-106`;
  - `pkg/search/embeddings/sqlite/store.go:884-975` (`ValidateOrInitMetadata`) and `:420-450` (`ensureSchemaWithRecovery`);
  - `pkg/search/embeddings/codeindex/sqlite/store.go:1399-1450`.
- **Problem.** When `UseVector && EmbCfg.Enabled` (CLI `rzm search` and `tools/searchquality`; MCP and web inject providers instead), `Run` calls `sqlite.OpenWithMetadata` on the unified index path. That path:
  - takes the schema-init lock;
  - runs `ensureSchemaWithRecovery`, which can `rebuildDomainPreservingEmbeddings` or hard `ResetDomain`;
  - calls `ValidateOrInitMetadata`. On a dimensions, provider, or model mismatch it calls `resetAndInit` → `ResetDomain(dropCache=true)`, which drops `emb_chunk_embeddings`, `emb_notes`, `emb_index_meta`, **and `emb_embedding_cache`**. Otherwise it always executes an `UPDATE emb_index_meta`.

  The code store does the same through its own `ValidateOrInitMetadata`. The `errors.As(err, &metaErr)` branch in `Run` (`:183-186`) shows the author expected an error on mismatch, but a mismatch resets and *succeeds*.
- **Evidence:**
  - The subsystem note says "Query-only note metadata is never repaired" (`docs/reference/subsystems/search.md:99`).
  - The read-only `ValidateExistingMetadata` exists for exactly this case ("deliberately read-only for query-only runtime callers", `open.go:12-49`) and is used by the live runtime (`pkg/app/bootstrap/live.go:775,1382`), but not by `Run`.
  - `noteStore` is never used for retrieval. Its only references are open, close, and a nil check (`run.go:162,189-190,250`). `semantic.Searcher` reads vectors from the Intel store.
  - No test covers a mismatch on the search path (`grep MetadataError` in `pkg/app/unifiedsearch` tests: none).
- **Recommendation.** Delete the `noteStore` open from `Run`. For the code store (needed only as `presentation.ChunkBodyReader`), open read-only and validate with `codeemb.ValidateExistingMetadata(ctx, db, meta)`, returning the `MetadataError` to the caller. Add a guard so that query-path constructors cannot reach `ResetDomain`, for example a separate `OpenReadOnly` that never runs `ensureSchemaWithRecovery`.
- **Effort:** S.
- **Dependencies:** none. Do this first.

### SRCH-2 Retrieval concurrency and deadline budgets do not match the design; the vector lane gets the smallest budget
- **Severity:** P1.
- **Confidence:** High for the mechanism. Medium for user-visible frequency, since it depends on provider latency.
- **Location:**
  - `pkg/search/service.go:366-368,453-577`;
  - `pkg/search/timeouts.go:25-59`;
  - `pkg/search/retrieval/composite.go:51`, `auto_expand.go:122`;
  - `pkg/search/planner/planner.go:196-249,375,456-458,526-528`;
  - `pkg/app/unifiedsearch/application/application.go:51-57`;
  - `pkg/app/mcp/semantic_query_unified.go:172`;
  - `pkg/app/unifiedsearch/note_searcher.go:35`.
- **Problem.** The design says "Retrievers run concurrently (or progressively under deadline)." In production every request carries a deadline:
  - interactive profile 3s, agent 8s (`application.go:51-57`);
  - MCP wraps 8s (`semantic_query_unified.go:172`);
  - the CLI defaults `--timeout 8s`.

  So `Service.retrieve` always takes `retrieveProgressive`, which runs top-level retrievers **one at a time**. The concurrent path, along with `calcRetrieverConcurrency` and the `CostedRetriever` cost classes, runs only with `--timeout 0`.

  Text search is concurrent only because the planner wraps the base lanes in `AutoExpandRetriever` → `CompositeRetriever`. That composite, and `expandInParallel`, call `RetrieverStageContext(ctx, …, i, len)`, a formula built for *serial* stages (`stagesLeft = total-index`). Run concurrently, it gives lane 0 the smallest slice and the last lane the whole remaining deadline. The planner puts `VectorRetriever` first (`planner.go:197-199`). Under the 3s interactive deadline with six base lanes:

  | Lane (index) | Stage budget |
  | --- | --- |
  | vector (0) | about 0.96s, including its vector bonus |
  | rationale (5) | the full 3s |

  The vector lane is the one that makes a provider network call for the query embedding.

  Auto-expand is skipped whenever `hasConstrainedFilters` is true (any `Types`, `PathPrefixes`, `NoteTypes`, or test filter) or when refs and graph are off. GraphQL `NoteSearcher` always sets `Filters.Types=["note"]` (`note_searcher.go:35`). So the web Explorer's search runs vector, note_lexical, link_text, and intel_lexical **serially**. The same is true for `explain_symbol`, `subsystem_overview`, `consolidation`, `data_flow`, `security_audit`, and every filtered MCP or CLI search.
- **Evidence.**
  - `TestRetrieverStageBudgetGivesVectorLaneLargerShare` (`timeouts_test.go:28`) compares vector against graph *at the same index*, so it cannot see the inversion.
  - `TestRunRetrievers_StageBudgetsSlowBroadLane` pins the index-based budget.
  - `perfworkload.Measure` never sets `UseRefs` (`pkg/search/perfworkload/run.go:202`), so the perf harness always measures the serial plan, not the CLI or MCP plan.
- **Recommendation.** Replace the three executors (concurrent `Service.retrieve`, serial `retrieveProgressive`, `CompositeRetriever.Retrieve`) and `expandInParallel` with one `runLanes(ctx, spec, lanes) laneResults`:
  - run all lanes concurrently;
  - give each lane the same budget (`deadline - reserve`), with no index term;
  - record lane status and timing once;
  - apply the broad/precision error policy once;
  - merge in configured order.

  Keep progressive *priority* only for pruning order, not for execution. Then delete `retrieverPriority*`, `calcRetrieverConcurrency`, `CostedRetriever`/`CostClass`, and the dual paths in `Service`. `CompositeRetriever` becomes a thin name grouping over `runLanes`. Point `perfworkload` at the same option set as the CLI.
- **Effort:** M.
- **Dependencies:** SRCH-11 (one lane-recording helper) falls out of this.

### SRCH-3 Rerank identity pinning is undone by `Service.Search`'s post-rank re-sort
- **Severity:** P2 (the feature is opt-in through `RHIZOME_RERANK_PROVIDER`).
- **Confidence:** High.
- **Location:** `pkg/search/relevance/rerank_ranker.go:47-108`; `pkg/search/service.go:184-205`.
- **Problem.** `RerankingRanker` keeps identity results (`path_exact`, `note_title_exact`, `symbol_exact`, `definition_anchor`, `explicit_seed`) at their base *positions* but leaves their `FinalScore` unchanged. It blends the other top-30 scores upward or downward. `Service.Search` then re-sorts every result by (exactNav, primary, FinalScore, handle). Only `path_exact` and `note_title_exact` (the exactNav tier, non-precision) survive. A `symbol_exact`, `definition_anchor`, or `explicit_seed` result can move up or down. For example, a movable item below a pinned 0.85 can blend to 0.7·0.85 + 0.3·maxBase ≥ 0.865 and jump above it. This breaks the documented invariant "pins any result carrying identity evidence at its base position" (`search.md:94`).
- **Evidence.** The `rerank_ranker_test.go` tests call `RerankingRanker.Rank` directly and never run `Service.Search`. Also, the comment on `Weight` says "default 0.5" while the constant is 0.3 (`rerank_ranker.go:15,24`).
- **Recommendation.** Make identity a sort tier that the final sort honors, extending exactNav to all identity evidence for this purpose. Alternatively, move the rerank stage after the service sort as the true last stage. Either way, add the missing test (SRCH-T-9).
- **Effort:** S.
- **Dependencies:** SRCH-5 (single ranking stage) makes this natural.

### SRCH-4 Dead ranking channels and dead per-candidate evidence: fusion, recency, `retriever_rank`
- **Severity:** P2.
- **Confidence:** High.
- **Location:**
  - `pkg/search/relevance/fusion_ranker.go` (entire file);
  - `pkg/search/service.go:579-615` (`mergeRetrieverItems` / `annotateRetrieverRank`);
  - `pkg/search/merge.go:260-310` (`lane_ranks` merging);
  - `pkg/search/scoring.go:18,21,65-66,133-136`;
  - `pkg/search/relevance/ranker.go:23-27,145-168`;
  - every intent in `planner.weightsForIntent`.
- **Problem:**
  - `FusionRanker{` has zero production constructors; only `fusion_ranker_test.go` builds it. Nothing else produces `rank_fusion`, so the `Fusion` channel and its weights (0.6–0.85 in the approx table, 0.75 default) are dead.
  - Nothing produces `recently_modified` (grep: zero producers outside the registry), yet `Recency` is weighted 0.1–0.2 in every intent.
  - Every candidate in every lane still gets a `retriever_rank:<name>` evidence entry with a fresh `Details` map. `mergeEvidenceDetails` then parses, unions, sorts, and re-joins `lane_ranks` strings on every merge. The only reader of that data is the dead `fusionScore`. The entries score 0 and sort last, so they do not evict real evidence. The cost is allocation and CPU on the merge hot path, plus confusing explain output.
- **Recommendation.** Delete:
  - `FusionRanker`;
  - `annotateRetrieverRank`;
  - the `lane_ranks` logic;
  - the `retriever_rank:` special case in `evidenceSpecForKey`;
  - the `Fusion` and `Recency` fields in `Weights`, every weight table, and `EvidenceChannel`.

  If per-lane rank is needed for `LeadsFacet`, carry it as a field on the multi-query response, which is what `facetLeaderIndex` already uses (`application.go:304`).
- **Effort:** S.
- **Dependencies:** none.

### SRCH-5 Two scorers, two weight tables, and an optional interface that duplicates the ranker
- **Severity:** P2.
- **Confidence:** High.
- **Location:**
  - `pkg/search/scoring.go:175-351` (`DefaultApproxChannelWeights`, `ApproxChannelWeightsForIntent`, `NormalizedCandidateBaseScore`, `ApproxCandidateScore`, `ApproxScoringConfig`);
  - `pkg/search/relevance/ranker.go:135-169`;
  - `pkg/search/planner/planner.go:530-545` (`approxWeightsForRanker`) and `:601-713` (`weightsForIntent`);
  - six `Approx*` forwarding methods across the `relevance/*` decorators;
  - `pkg/search/service.go:676-678,748-800`.
- **Problem:**
  - `ApproxChannelWeightsForIntent` is a hand-copied second intent→weights table. It omits `weightsForIntent`'s hasText/hasSeeds/signal adjustments and adds fusion weights the planner never sets.
  - Every production decorator forwards `ApproxScoreProvider` to the planner's `WeightedRanker`, so in production the copy is reached only as a default. It is maintenance drift waiting to happen.
  - `NormalizedCandidateBaseScore` re-implements `scoreCandidate` but iterates `for channel, value := range channels` (map order). Float addition is not associative, so candidates with mathematically equal scores can compare differently by one ULP between runs. That leaves exact ties in pruning, auto-expand seed selection (`deriveSeeds`, `runRetrievers` sort), and the deadline fallback decided by noise instead of the handle tie-break.
  - The design asks "New Ranker implementations should also satisfy ApproxScoreProvider," but that is an unenforced type assertion.
- **Recommendation.** Give `Service` a pure `Scorer` (today's `WeightedRanker`: weights plus `MaxPerOwner`; CPU-only, never times out) plus an ordered list of optional I/O-bound `Adjusters` (graph doc/anchor scores, rerank). Each adjuster may time out and be skipped with a warning. The deadline fallback becomes "score without the remaining adjusters," so `approxRankCandidates`, `ApproxScoreProvider`, `ApproxChannelWeightsForIntent`, `DefaultApproxChannelWeights`, `ApproxCandidateScore`, and `approxWeightsForRanker` all go away. Use one `score(c, weights)` with a fixed channel order everywhere.
- **Effort:** M.
- **Dependencies:** SRCH-4 first (fewer channels), then SRCH-3.

### SRCH-6 Dead answer assembly in `Run`, with a divergent role vocabulary
- **Severity:** P2.
- **Confidence:** High.
- **Location:** `pkg/app/unifiedsearch/run.go:93,472,535,643-688`.
- **Problem:** `Run` builds `Result.Answer` through `BuildAnswer` and `roleForResult` on every request. Its only production caller is `Execute` (`execute.go:130`), which never reads `result.Answer`; it builds its own packet through `BuildAssessedAnswerForFacets` (`execute.go:190`). The dead `roleForResult` uses a different vocabulary (`doc`/`impl`/`entry_point`/`support`) and a different test-path rule (substring matching) from the live `application.roleForResult` (`application.go:1344`: `documentation`/`implementation`/`test` via `search.IsTestPath`).

  Separately, in the single-query branch, `Display` is built from unfiltered `resp.Results` while `Results` is note-type filtered (`run.go:530-534`). This is harmless today only because `materializePageDisplay` looks items up by the filtered page's identity.
- **Recommendation:** Delete `Result.Answer`, `BuildAnswer`, and `run.go`'s `roleForResult`. Build `Display` from the filtered results, or lazily inside `materializePageDisplay`, so it is computed only when `MaterializeDisplay` is set.
- **Effort:** S.
- **Dependencies:** none.

### SRCH-7 `pkg/search` depends on `pkg/app/cli` for two filter fields no caller sets
- **Severity:** P2 (layering).
- **Confidence:** High.
- **Location:** `pkg/search/types.go:19,53-54`; `pkg/search/filters.go`; `pkg/app/unifiedsearch/contract.go:124-125,166,189`.
- **Problem:** `search.Filters.Inputs []actions.ListInput` and `Expression *actions.InputExpression` pull the 111-file `pkg/app/cli` package into the search core. Nothing sets them: the only `search.Filters{…}` literals in production are `mcp/semantic_query_unified.go:284` and `perfworkload/run.go:205`, and neither sets them. No eligibility code reads them. `ParseFilters` has no callers. Their only effect is being hashed into continuation identity.
- **Recommendation:** Delete both fields, `filters.go`, and the matching `membershipFilters` fields. Bump `RankingPolicyVersion` if cursor compatibility matters (it shouldn't; the fields are always empty).
- **Effort:** S.
- **Dependencies:** none.

### SRCH-8 Too many packages on the request path, and too much indexing code under `pkg/search`
- **Severity:** P2.
- **Confidence:** High for the facts. The proposed shape is a recommendation.
- **Location:** `pkg/app/searchengine/engine.go`; `pkg/app/semanticops/provider.go`; `pkg/search/semantic/*`; `pkg/search/{graphdb,intentstore,indexgeneration,qualityeval,perfworkload}`; `pkg/app/semanticruntime`.
- **Problem:**
  - **`searchengine`** has one caller (`unifiedsearch/run.go:391`). It exists to be "cycle-free," but nothing else imports it. It also redefines `ErrTimedOutBeforePlanning`, which `run.go:399-401` translates into an identical error.
  - **Production wiring is split.** The graph rankers and rollupper are added in `searchengine`, not the planner. So `tests/integration/python/search_intents_test.go` and `planner_delivery_test.go`, which build `search.Service{Retrievers: plan.Retrievers, Ranker: plan.Ranker}`, test a different pipeline from production: no graph rankers, rollup, or shaper.
  - **`semanticops`** is a five-line function with three callers.
  - **`semantic`** is 11,268 production lines. The query side (`search.go`, `embedder.go`, `query_embedding_memo.go`, plus constants) is about 1k. The other ~10k (`syncer*`, `note_syncer*`, `ontology_node_syncer`, `*_batcher`, `*_pipeline*`, `chunker`, `writeback`) is indexing. That puts the whole indexing pipeline into the dependency closure of every retriever.
  - **Indexing-only importers.** `graphdb` and `intentstore` are imported only by indexing, bootstrap, and web. `semanticruntime` is imported only by `indexing` and `bootstrap`.
  - **`perfworkload`** (under `pkg/search`) imports `pkg/app/unifiedsearch`.
- **Recommendation:**
  1. Fold `searchengine` into `planner`: `Plan()` returns a ready-to-run `*search.Service` with rankers, rollupper, shaper, and packer. Every test then uses production wiring.
  2. Make `semanticops.PrepareProvider` into `embeddings.NewProviderWithKey(cfg, key)`.
  3. Split `semantic` into query-time `pkg/search/semantic` and an indexing package (`pkg/embedindex`), and move `semanticruntime` there.
  4. Move `graphalg` and `graphdb` to `pkg/graph`.
  5. Move `qualityeval` and `perfworkload` under `tools/*/internal`.
  6. Inline `indexgeneration` into `unifiedsearch/index_generation.go`, its only caller.

  Update the subsystem note's scope and the folder table.
- **Effort:** L for the semantic split (mechanical but wide); S for the others.
- **Dependencies:** coordinate with the indexing reviewer.

### SRCH-9 Repeated full-catalog scans and wasted per-request work
- **Severity:** P2.
- **Confidence:** High for the call counts. Low for absolute cost (not benchmarked; scales with vault size).
- **Location:**
  - `pkg/search/planner/planner.go:208-231` (note_lexical sources), `:740-775` (`inspectSignalProfile`), `:500-505`;
  - `pkg/search/target_resolution.go:591-613`;
  - `pkg/app/unifiedsearch/run.go:297-303,869,973-1041,1093-1133,356`;
  - `pkg/app/searchengine/engine.go:41-44`.
- **Problem.** For a broad text query, each facet (up to 4 concurrent) reads:

  | Caller | Reads |
  | --- | --- |
  | `inspectSignalProfile` | `IntelNotePaths` + `ListFiles(ctx, 100000)`, then uses only `len()` of each |
  | `NoteLexicalRetriever` | `IntelNotePaths` again, plus `CurrentNoteMetadataRows` |
  | Precision/repair intents (`pathKindsFromTargetResolver`) | `IntelNotePaths` + `ListFiles(100000)` again |
  | Raw seeds (`hydrateSearchPathKinds` and `persistedNoteDirectorySeeds`) | `CurrentNoteMetadataRows` + `IndexedFilePaths`, **twice** |

  The two path-kind builders disagree:
  - `hydrateSearchPathKinds` filters to current projections and adds directories;
  - `pathKindsFromTargetResolver` does not, caps at 100k, and silently swallows errors.

  Other waste:
  - `Run` parses the ontology schema on every request into `semantic.Searcher.OntologySchema`, a field nothing reads (`search.go:110`; grep finds no reader). It never sets `planner.Deps.OntologySchema`. So the packer re-parses the schema per facet when packing (`planner.go:500-505`), and the `noderead` scope gets a nil schema. With a nil schema, relation selection returns early (`noderead/relation_selection.go:66`); I have not measured the effect on the ontology lanes.
  - `GraphDocScoreRanker` and `GraphAnchorScoreRanker` run their SQL and append graph evidence on every text query. The graph weight there is 0 (`adjustWeightsForSignals` zeroes Graph when `!GraphReady`, and `Run` enables graph only with note seeds), so the work never changes a score. Answer assembly then has to exclude that "graph popularity" explicitly.
- **Recommendation.** Build one request-scoped `CorpusSnapshot` in `Run`: current note rows, code paths, path kinds including directories, counts, and the ontology schema. Pass it through `planner.Deps` to note_lexical, target resolution, signal profile, and seed resolution, and delete the four ad-hoc builders. Replace the `len(ListFiles)` reads with `COUNT(*)`. Skip graph adjusters when their channel weight is zero.
- **Effort:** M.
- **Dependencies:** SRCH-8 (planner owns wiring).

### SRCH-10 Intel FTS lane does N+1 reads and re-parses Markdown at query time
- **Severity:** P2.
- **Confidence:** High.
- **Location:** `pkg/search/retrieval/lexical_intel.go:70-101,113-116,259-275`.
- **Problem.** Each anchor row that matched only on its path issues `IntelModuleAnchorIDByPath`. Each distinct doc path loads `IntelDocSectionsByPath`, the content-bearing reader that returns every section body. The lane then runs `ontology.ParseSections(path, content)` to recover the canonical node identity that the indexer already computed. With the candidate window at 100–240 rows, that can mean hundreds of queries and parses per search. `semantic/search.go` already switched to the id-only `IntelDocSectionIDsByPath` for this reason (`search.md:96`).
- **Recommendation.** Persist the canonical section `NodeID`, `StartByte`, and block id on the FTS or doc-section row at index time, and return them from `SearchIntelFTSFiltered`. Batch the module-anchor lookups in one `IN (...)` query.
- **Effort:** M (needs a schema migration).
- **Dependencies:** stores subsystem (migration).

### SRCH-11 Copy-pasted lane execution, timings, and tokenizers
- **Severity:** P3.
- **Confidence:** High.
- **Location:**
  - **Lane execution:** the `retriever_degraded` warning block appears six times (`service.go:412,520,536,550`; `composite.go:71`; `auto_expand.go:143`). Lane recording plus timing appears four times.
  - **Status helpers:** three copies of `statusFromErr`/`timingStatus`/`timingErr` (`service.go:898-916`, `retrieval/timings.go`, `semantic/timings.go`), plus a mirrored `semantic.TimingEvent` with an adapter (`retrieval/semantic_timings.go`).
  - **Evidence helpers:** `hasAnyEvidence`, `hasEvidenceType`, `candidateHasEvidence`, `hasEvidence`, and `application.hasAnyEvidence`.
  - **Tokenizers:** six (`queryframe.tokenize`, `retrieval/query_tokens.go:tokenizeQuery`, `rationale.go:210`, `ontology.go:318`, `target_resolution.go:383`, `application.go:288`).
- **Problem.** The note_lexical lane scores by **substring** (`strings.Contains(haystack, tok)`) over raw tokens with no stop-word handling (`query_tokens.go:8-77`). "how does the search work" therefore matches "show" and "theme" and earns `note_title_match` lexical evidence. `queryframe`, which owns stop words and concept variants, later compensates through specificity, but the lexical channel still carries the noise. I have not measured the ranking impact.
- **Recommendation.** `runLanes` (SRCH-2) absorbs the lane boilerplate. Use one `search.TimingEvent` (`semantic` can import a leaf timings package). Use one `search.HasEvidence(evs, types...)`. Score note_lexical with `queryframe.Extract(...).ConceptCoverage(title + path)`, with a judged ablation per the subsystem rule.
- **Effort:** S–M.
- **Dependencies:** SRCH-2.

### SRCH-12 `Candidate` and `semantic.Result` duplicate a 20-field identity record three ways
- **Severity:** P3.
- **Confidence:** High.
- **Location:** `pkg/search/types.go:296-333`; `pkg/search/semantic/search.go:64-87`; `pkg/search/retrieval/vector.go:68-106`; `pkg/search/merge.go:18-99`.
- **Problem:**
  - Ontology identity is stored as parsed `NodeRef`, as `NodeRefJSON`, and flattened (`NodeID`, `NodeKind`, `NodeType`, `ParentNodeID`, `SourceLocator`).
  - `semantic.Result` mirrors `Candidate` field for field and is copied over field by field.
  - `MergeCandidate` is 80 lines of first-non-empty boilerplate.
  - `vector.go:73` falls back to evidence type `"semantic_similarity"`, which is not in `evidenceSpecs` and would score 0.
  - An unknown chunk owner type yields `Handle=""`, and `ParseHandle("")` then fails the whole vector lane. This is latent; only three owner types exist today.
  - `IntelAnchorsByIDs` errors are discarded (`search.go:394`), so code hits silently lose Symbol/FQN, which `ExactSymbols` filtering and rollup depend on.
- **Recommendation.**
  - Introduce `search.SourceRef` (path, title, symbol, FQN, kind, granularity, chunk index, breadcrumb, heading, anchor id, note id, `*ontology.NodeRef`). Embed it in `Candidate` and `semantic.Result`, and merge it with one `ref.FillFrom(other)`. Derive the flattened node fields from `NodeRef`.
  - Skip, rather than fail on, unknown owner types, and propagate the anchor-lookup error.
- **Effort:** M.
- **Dependencies:** SRCH-8.

### SRCH-13 The merge hot path JSON-encodes inside sort comparators
- **Severity:** P3.
- **Confidence:** High for the pattern. Magnitude not measured; benchmarks were not run.
- **Location:** `pkg/search/merge.go:200-222,229-248,312-366`; `pkg/search/merge.go:134-142`.
- **Problem.** `evidenceLess` calls `evidenceFactKey`, which `json.Marshal`s a details map, and on ties `canonicalEvidence`, which marshals again. It does this inside `sort.Slice` comparators. `EvidenceScore` re-normalizes zero-score evidence in comparators too. `mergeEvidenceBounded` runs on every same-handle merge across lanes and facets, and in `coalesceNoteCandidates`, `CoalesceNoteResults`, and `DedupeCanonicalSources`. `coalesceNoteCandidates` also recomputes `AggregateEvidenceScoresForRanking` (allocating a map) in its comparator.
- **Recommendation.** Compute fact keys and scores once per evidence item before sorting (a decorate-sort-undecorate pass), and cache the semantic score per member. Add `BenchmarkMergeCandidate` with 12-evidence candidates to `latency_benchmark_test.go` before and after.
- **Effort:** S.
- **Dependencies:** SRCH-4 removes `lane_ranks` work first.

### SRCH-14 Two intent-inference systems; the embedding detector serves only CLI free-form `--mode`
- **Severity:** P3.
- **Confidence:** Medium-high.
- **Location:** `pkg/app/unifiedsearch/run.go:317-331,1163-1194`; `pkg/app/unifiedsearch/default_intent.go`; `pkg/search/intent`; `pkg/search/intentstore`; `pkg/app/semanticruntime/runtime.go:224-262`; `cmd/code_search.go:155`.
- **Problem.**
  - `Execute` applies the keyword heuristic `InferDefaultIntent` when no intent is given.
  - The embedding-based detector (plus index-time exemplar syncing and its SQLite table) runs only when `IntentInput` is *present but invalid*. That happens only through the CLI's "free-form text for inference" `--mode`; MCP validates modes first (`semantic_query_unified.go:224`).
  - Its output must also pass `KeywordMatch`, so it can never pick an intent the keywords would not.
  - On the multi-facet path, `NormalizeExplicitQueryInputs` copies the free-form string into every facet's `Mode`, and `runMultiQuery` then plans with that unvalidated intent.
- **Recommendation.** Either route free-form `--mode` through `InferDefaultIntent` and delete `pkg/search/intent`, `intentstore`, and the exemplar lane, or validate `IntentInput` in `Execute` and reject invalid values. Decide with Drew; this is a feature removal.
- **Effort:** S (reject) or M (delete the subsystem).
- **Dependencies:** indexing (exemplar sync).

### SRCH-15 Oversized orchestration functions
- **Severity:** P3.
- **Confidence:** High.
- **Location:**
  - `unifiedsearch.Run`, about 410 lines (`run.go:133-541`);
  - `Planner.Plan`, about 390 lines (`planner.go:132-524`);
  - `Service.Search`, 207 lines;
  - files over the 500-line policy: `target_resolution.go` (1,249), `run.go` (1,247), `planner.go` (1,185), `service.go` (916), `application.go` (1,428), `semantic/note_syncer.go` (1,451), `semantic/syncer.go` (1,363), `codeindex/sqlite/store.go` (3,401), `embeddings/sqlite/store.go` (1,940).
- **Recommendation.**
  - Split `Run` into `openResources` (providers and stores, returns a closer), `resolveSeeds`, and `runFacets`.
  - Split `Plan` into `baseLanes`, `seedLanes`, `intentLanes`, `autoExpand`, and `weights`.
  - These splits largely fall out of SRCH-2, SRCH-5, SRCH-8, and SRCH-9. Do not split files mechanically before those land.
- **Effort:** M.
- **Dependencies:** SRCH-2, SRCH-5, SRCH-8, SRCH-9.

## Findings — tests

### SRCH-T-1 Delete tests that pin dead production code
- **Verdict:** delete, together with the code in SRCH-4 and SRCH-6.
- **Tests:**
  - `pkg/search/relevance/fusion_ranker_test.go`: `TestFusionRankerBoostsMultiRetrieverCandidates`, `TestRankerFallbackPreservesPolicy`, `TestFusionScoreCountsEveryRetainedLaneRank`;
  - `pkg/app/unifiedsearch/run_test.go`: `TestBuildAnswer_SelectsTaskEvidence`, `TestBuildAnswerPreservesNodeRef`, `TestBuildAnswerPromotesPrimaryOntologyNodeResult`, `TestBuildAnswerCarriesSpecificityForPrimaryChunk` (`BuildAnswer`'s output is discarded in production);
  - `pkg/search/service_test.go`: `TestMergeRetrieverItemsAnnotatesOneRankPerHandle`;
  - `pkg/search/merge_test.go`: `TestMergeCandidate_RetrieverRankMarkersDoNotEvictRankableEvidence`;
  - `pkg/search/scoring_test.go`: `TestNormalizeEvidence_MapsRetrieverRankAsNonCompetingMetadata`.
- **Why:** these tests pass while the behavior they guard is unreachable. Answer-assembly behavior is already covered on the live path by `application_test.go`'s `TestBuildAssessedAnswer*` (around 20 tests).

### SRCH-T-2 Delete tests that assert constants or restate the implementation
- **Verdict:** delete.
- **Tests:**
  - `pkg/search/timeouts_test.go:TestDefaultQueryTimeout` (asserts `== 8s`);
  - `pkg/search/scoring_test.go:TestApproxChannelWeightsForIntent_DiffersByIntent` (asserts that the duplicate table has two different numbers; it goes when the table goes, SRCH-5);
  - `pkg/app/unifiedsearch/run_provider_test.go`: `TestProviderConfigsMatch`, `TestSelectIntentProviderPrefersCodeThenFallsBackToNote` (a one-branch helper);
  - `pkg/app/unifiedsearch/run_test.go:TestMergeSeedTokens`.
- **Why:** they would pass with the behavior broken in any way a user would notice, and they fail on harmless refactors. Provider reuse is better pinned by `provider_lifetime_test.go:TestRunCleanupClosesSharedCachedProvider`, which observes one shared client.

### SRCH-T-3 Rewrite the archetype tests to use production weights
- **Verdict:** rewrite.
- **Tests:** `pkg/search/archetype_regression_test.go:TestArchetypeRegression_MixedVaultDocsForCodeKeepsDocsAndCodeNearTop`, `TestArchetypeRegression_OntologyHeavyPrefersStructuralOverAmbient`.
- **Why:** they hand-copy one branch of `planner.weightsForIntent` (docs_for_code with seeds, dropping Specificity and Recency) into a literal `relevance.Weights`. A production weight change can never fail them. They test only that a weighted sum is a weighted sum. The subsystem note names this file as *the* ranking-change guard (`search.md:105`).
- **Replacement:** build the ranker the way production does, through `planner.Plan` (or the post-SRCH-8 `Plan → *Service`) over a tiny fixture store, and keep the relative-order assertions. The other two tests in the file (`…WholeNoteLaneAgreementOutranksOneLane`, `…AgreedLinkLabelNamesTheTarget`) are good regression tests: real dogfood failures, `DefaultWeights`, the full `Service.Search`, and assertions on relative order. Keep them, but move them onto production weights as well.

### SRCH-T-4 Rewrite the stage-budget tests to observe behavior, not the formula
- **Verdict:** rewrite.
- **Tests:** `pkg/search/timeouts_test.go:TestRetrieverStageBudgetGivesVectorLaneLargerShare`, `TestRetrieverStageBudgetLeavesTimeForLaterStages`; `pkg/search/retrieval/auto_expand_test.go:TestRunRetrievers_StageBudgetsSlowBroadLane`.
- **Why:** they pin the index-based formula and compare lanes at the *same index*, so they bless the inversion in SRCH-2.
- **Replacement:** one test through the real lane executor: six fake lanes, the vector lane first with a 1.2s delay, a 3s deadline. Assert that the vector results are present and the other lanes are not starved.

### SRCH-T-5 Delete or rewrite tests of the rarely-used concurrent path
- **Verdict:** delete with SRCH-2.
- **Tests:** `pkg/search/service_test.go`: `TestCalcRetrieverConcurrency_CPUHeavyUsesGOMAX`, `TestCalcRetrieverConcurrency_IOBoundAllowsHigher`.
- **Why:** they test a private helper that runs only when a caller passes `--timeout 0`, and they mutate the global `GOMAXPROCS`.

### SRCH-T-6 Move test-only weight and wiring construction onto production wiring
- **Verdict:** rewrite.
- **Tests:**
  - `tests/integration/python/search_intents_test.go:TestSearchIntents_P3`;
  - `pkg/search/planner/planner_delivery_test.go:TestPlanner_SkipsVectorRetrieversForTypedNilIntelStore` (line 34);
  - the many service tests that use `stubRanker{}`.
- **Why:** they build `search.Service{Retrievers: plan.Retrievers, Ranker: plan.Ranker}` themselves, skipping `searchengine`'s graph rankers and `CodeRollupper`. CI therefore exercises a pipeline users never run. After SRCH-8, use the `*Service` that `Plan` returns. Keep `stubRanker` only for executor and merge tests where ranking is irrelevant.

### SRCH-T-7 Keep (and do not churn) the high-value suites
- **Verdict:** keep.
- **Tests:**
  - `pkg/app/unifiedsearch/application/application_test.go`: 56 behavior-named assessment and answer tests encoding measured decisions;
  - `pkg/search/target_resolution_test.go`: 31 tests, each a concrete resolution rule;
  - `pkg/search/queryframe/frame_test.go`;
  - `pkg/search/merge_test.go`: permutation, idempotence, and determinism tests;
  - `pkg/search/service_merge_order_test.go`, `retrieval/composite_test.go`: completion-order independence;
  - `retrieval/link_text_test.go`, `retrieval/note_lexical_test.go`;
  - `qualityeval/evaluate_test.go`: its 19 exact floats are metric math such as nDCG, where exactness is the point.
- **Why:** these assert relative order or explicit rules, not tuned scores. Exact-score pinning is rare (fewer than 15 non-metric `Equal(t, <float>)` across the subsystem), so brittleness is not a systemic problem here.

### SRCH-T-8 Add: search must not mutate the index (covers SRCH-1)
- **Verdict:** add.
- **Test:** `pkg/app/unifiedsearch:TestRunDoesNotMutateEmbeddingStoresOnMetadataMismatch`.
  - Seed a unified index with `emb_index_meta` for model A, plus chunk and cache rows.
  - Call `Execute` with `EmbCfg` for model B. Assert it returns a `MetadataError` and that row counts in `emb_chunk_embeddings` and `emb_embedding_cache` are unchanged.
  - Add a matched-metadata case asserting `PRAGMA data_version` (or file mtime) is unchanged after the search.

### SRCH-T-9 Add: rerank pinning survives `Service.Search` (covers SRCH-3)
- **Verdict:** add.
- **Test:** `pkg/search:TestRerankIdentityPinSurvivesServiceSort`. Use a fake reranker that scores the item below a `symbol_exact` result at 1.0, run the full `Service.Search` with a `Rollupper`, and assert the identity result keeps its base index.

### SRCH-T-10 Add: production-wiring navigation regressions in CI
- **Verdict:** add.
- **Tests:** `pkg/search/planner:TestProductionWiringNavigation`, a table test over a ~15-note fixture store with deterministic embeddings, through the same entry point `Run` uses. Each case asserts relative order:
  - exact title ("Innovation teams" leads its four title matches);
  - agreed link label;
  - a short query that must require every concept;
  - a `noteType`-filtered query, which also exercises the serial/filtered plan from SRCH-2.

  Today these are guarded only by the out-of-CI `tools/searchquality` corpus and the hand-wired archetype tests.

### SRCH-T-11 Add: deterministic auto-expand seed selection under exact ties (covers SRCH-5)
- **Verdict:** add.
- **Test:** `pkg/search/retrieval:TestDeriveSeedsTieBreakIsDeterministic`. Use many candidates with identical multi-channel evidence and differing handles, run `deriveSeeds` 50 times, and assert identical output. This can fail intermittently today because of map-order float summation, so treat it as the red test for the fix.

## Suggested refactor sequence

1. **SRCH-1 (S, P0).** Read-only store validation in `Run`; delete the `noteStore` open; add SRCH-T-8. Ship alone.
2. **Dead-code sweep (S, low risk, shrinks every later diff):**
   - SRCH-4: fusion, recency, `retriever_rank`;
   - SRCH-6: `Result.Answer`;
   - SRCH-7: `Filters.Inputs`/`Expression`, which removes `pkg/app/cli` from `pkg/search`;
   - the unused `Searcher.OntologySchema` field;
   - `vector.go`'s unmapped fallback evidence type;
   - along with SRCH-T-1, SRCH-T-2, and SRCH-T-5.
3. **Fold `searchengine` into `planner`** so `Plan` returns a `*Service` (SRCH-8, first part). Rewire the tests per SRCH-T-6 and add SRCH-T-10, so the following ranking-stage changes are measured against production wiring.
4. **One lane executor with fair concurrent budgets** (SRCH-2, SRCH-11 lane boilerplate). Rewrite SRCH-T-4. Point `perfworkload` at CLI options. Re-run `tools/searchperf` and `tools/searchquality` within one embedding-cache family before and after.
5. **One scorer plus adjusters** (SRCH-5). Then make the rerank pin a final tier (SRCH-3, with SRCH-T-9 and SRCH-T-11). Rewrite SRCH-T-3.
6. **Request-scoped `CorpusSnapshot`** and skipping zero-weight graph adjusters (SRCH-9). Fix the merge comparator cost (SRCH-13) with a benchmark.
7. **Package moves** (SRCH-8, rest): the `semantic` query/index split, `pkg/graph`, `pkg/embeddings`, tools-internal eval. Coordinate with the indexing reviewer. Update `docs/reference/subsystems/search.md` scope and `last-verified`.
8. **Schema work:** persist section identity for FTS (SRCH-10); consolidate tokenizers into `queryframe`, behind a judged ablation (SRCH-11); `SourceRef` (SRCH-12); the intent-detector decision (SRCH-14); function splits (SRCH-15).

## Slow-test table (measured)

One run: `go test -count=1 -json ./pkg/search/... ./pkg/app/unifiedsearch/... ./pkg/app/semanticruntime/... ./pkg/app/semanticops/...`. 20 packages, 775 top-level tests, 0 failures, 19.0s summed package time.

| Package | Elapsed (s) |
| --- | --- |
| pkg/search/semantic | 3.61 |
| pkg/search/retrieval | 2.71 |
| pkg/search/embeddings | 2.35 |
| pkg/search/planner | 1.45 |
| pkg/search/embeddings/sqlite | 1.40 |
| pkg/app/unifiedsearch | 1.17 |
| pkg/search | 1.09 |
| all others | under 1.0 each |

| Slowest tests | Elapsed (s) | Note |
| --- | --- | --- |
| semantic:TestQueryEmbeddingMemoExpiredStageAllowsLiveFollowerRetry | 1.00 | timing-based; acceptable |
| embeddings:TestOpenAIProviderDimensionsRace | 1.00 | |
| embeddings/sqlite:TestOpenWithOptionsSerializesConcurrentFirstOpens | 0.90 | |
| embeddings:TestOpenAIProviderAdaptiveBatchFromHeaders | 0.50 | |
| semanticruntime:TestRuntimePrefersCodeLaneForIntentMetricsWhenBothLanesExist | 0.40 | |
| everything else | 0.33 or less | |

Speed is not a problem in this lane. Only three test files use `time.Sleep` (`embeddings/provider_ollama_concurrency_test.go`, `semantic/provider_embed_test.go`, `semantic/derived_pipeline_test.go`).
