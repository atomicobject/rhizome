---
summary: "Lane report from the 2026-10-17 subsystem quality review: Noderead subsystem review. Findings are a snapshot of commit 5251923f."
---

# Noderead subsystem review

Scope: `pkg/ontology/noderead` (8.2k production lines, 6.0k test lines), `pkg/ontology/readmodel` (260 lines, no tests), and the read paths of their consumers in `pkg/app/web`, `pkg/app/views`, `pkg/app/mcp`, `pkg/app/cli`, `pkg/ontology/query`, `pkg/search`, and `cmd`. Read-only review at commit `5251923f`.

## Summary

The core design works. A request-scoped `Scope` memoizes catalog, metadata, type, and assessment reads, and the main entry points batch by path or source. The tests are fast (1.43 s) and mostly drive real SQLite fixtures. Several stated invariants do not match the code, though. The worst gap is in locator computation, which `Neighborhood`, `RelationCounts`, `Execute`, embedded content hydration, and the default `Resolve` all route through. It builds a fresh `ontology.NodeLinkService` per batch. That service walks the whole vault directory (`GetNotesList`) and re-parses each host note outside the scope's snapshot cache, and it never sees the edit-session overlay. Three more correctness problems are confirmed:

- **Overlay paging for interfaces:** interface type lists with predicates or sort do not over-fetch committed rows under an overlay, so pages come back short (and `Offset` would be applied twice if a caller passed one).
- **Graph truncation:** `Scope.Graph` cuts nodes and edges independently, alphabetically by ID, which leaves edges whose endpoints are not in `Nodes`.
- **Relation counts:** `RelationCounts` has a "legacy" path that counts edge rows instead of distinct targets, and the web type page uses it.

The hydration-profile contract is nominally four levels but works as two. `identity` costs the same as `summary`, which decodes `assessment_json` once per row. Several advertised seams are dead code: `SharedNodeCache`, `ScopeOptions.Budget`, and `HydrateWorkspace`. Store capabilities are discovered through 14 silent type assertions against a single production implementation. Consumers, mainly web's type page and note search, still assemble summary rows by reading the store directly and then patch overlay state by hand. That is the duplication the subsystem exists to prevent. `readmodel` is worth keeping as a leaf package of row types, but it carries a duplicate graph-node DTO and a 600-reference alias layer in `pkg/anchors`. It needs no tests. On the test side, the six cancellation tests and the paired apply tests should each be merged, about seven tests drive private helpers, and the important gaps are tests that would fail when a path turns N+1, starts walking the vault, or breaks interface overlay paging.

## Findings — production code

### NR-1 Locator canonicalization walks the whole vault and re-parses hosts outside the scope cache
- **Severity:** P1
- **Confidence:** High for the mechanism. Medium for real-world cost, which grows with vault size.
- **Location:** `pkg/ontology/noderead/records.go:1222-1283` (`locatorsLocked`); `pkg/ontology/node_link.go:176,210`; callers at `neighborhood.go:226-276`, `records.go:755`, `resolve.go:162`, `execute.go:119-123,223-227`.
- **Problem:**
  - Every locator cache miss builds a new `ontology.NodeLinkService`. `LinkTargets` calls `NoteReader.GetNotesList` unconditionally (`node_link.go:176`); for `*obsidian.Note` that is `DiscoverFiles`, a filesystem walk. It then loads each host with its own `LoadDocumentSnapshot` (`node_link.go:210`) instead of the scope's `snapshotByPath`.
  - The scope already memoizes a `NotePathCache` (`resolve.go:675-705`), but `NodeLinkService` cannot use it, so a single `Resolve` can walk the vault twice.
  - `NodeLinkService` takes no overlay. Under an edit session, locators and link targets are computed from committed files, and overlay-index records carry no `NodeLocator`. `TestScopeResolveStructuralSelectorReturnsOverlayCanonicalRef` avoids the problem by passing `OmitLocators: true`.
- **Evidence:**
  - `canonicalizeNodeRefsLocked` documents that note locators "list every vault file" and skips note refs. Embedded refs still go through `locatorsLocked`, and from there to `LinkTargets`, which lists every file.
  - The callers that touch embedded endpoints (Neighborhood edges and sources, projection-backed embedded hydration, `Resolve` with the default `OmitLocators=false`) all pay a directory walk per new batch of refs.
  - `pkg/app/web/ontology_sessions.go:296` calls `Resolve` once per field value in a loop without `OmitLocators`, and uses only `.Ref`.
  - `Execute` calls `s.Locators` when `IncludeLocators` is set and discards the result (`execute.go:119,223`). `NodeView` has no locator field, so the only effect is warming the cache.
- **Recommendation:**
  1. Canonicalize embedded refs from catalog rows. `NodeRow` already carries `SourceLocator` and `BlockID`, and `catalogLinkTarget` (`records.go:617`) already builds link targets from them. `Neighborhood` already batch-reads `OntologyNodesBySourceLocators` (`neighborhood.go:81`).
  2. When link rendering is genuinely needed, construct one renderer per scope from `s.notePathCache` and feed it the scope's snapshots, so overlay content is honored.
  3. Invert the default: replace `ResolveRequest.OmitLocators` with an opt-in `IncludeLocators`, and delete `OmitLocators`.
  4. Put the locator in `NodeView` so `Execute` stops computing and discarding it.
- **Effort:** M
- **Dependencies:** none. This unblocks NR-5.

### NR-2 An embedded type with zero catalog rows triggers whole-vault projection on every request
- **Severity:** P1
- **Confidence:** High for the mechanism. Medium for how often it fires; it fires for any global-source embedded type with no instances yet, such as unused list-item or checkbox types.
- **Location:** `pkg/ontology/noderead/type_instances.go:203-214`, `1185-1215` (`ensureEmbeddedIndex`).
- **Problem:** When `catalogTypeItems` returns zero rows and `noteType.SourceShape != ""`, `typeItems` calls `ensureEmbeddedIndex`. That projects every path in `s.allNotePaths`: it reads and parses every note, then runs `GlobalSourceNodeRefsFromSnapshot` and `ProjectBoundNodeFromSnapshot` on each. An empty catalog result is treated as "maybe stale" instead of "empty". Scopes are per request, so this repeats on every request.
- **Evidence:** The web summary and atlas call `TypeInstances` for every schema type on one fresh scope (`pkg/app/web/ontology.go:325,334,460`). The CLI summary (`pkg/app/cli/ontology_summary.go:91`) and `cmd/ontology.go:196` loop the same way.
- **Recommendation:** Treat catalog rows as authoritative when `GetOntologySchemaState` is ready at the current `OntologyMaterializationVersion`, so an empty result means empty. Once NR-9 requires a catalog store, delete `ensureEmbeddedIndex`, `collectProjectionItems`, `embeddedByType`, `embeddedSeen`, and `embeddedReady`. If a not-ready fallback is still wanted, bound it and record a diagnostic.
- **Effort:** S
- **Dependencies:** NR-9 if the fallback is deleted outright.

### NR-3 Graph truncation is alphabetical and drops nodes independently of edges
- **Severity:** P1
- **Confidence:** High
- **Location:** `pkg/ontology/noderead/graph.go:341`, `1129-1162` (`sortedGraphEndpoints`, `sortedGraphEdges`); `graph.go:442-458` (`GraphFacts`); `graph.go:587-599`.
- **Problem:**
  - `Graph` sorts nodes by ID (`"code:"` < `"embedded:"` < `"note:"` < `"section:"`), cuts to `NodeLimit`, and separately cuts edges to `EdgeLimit` sorted by source ID. The result can contain edges whose endpoints are missing from `Nodes`, and truncation favors code endpoints over notes.
  - `GraphFacts` filters facts to the nodes it keeps, but it still keeps them alphabetically.
  - The SQL pre-limit is `limit*4` with no ordering contract, so which rows reach the Go sort is arbitrary.
- **Evidence:** Callers that set limits:
  - GraphQL `localGraph(nodeLimit: 50, edgeLimit: 100)`: `pkg/ontology/query/execute.go:2919-2933`, which passes `result.Nodes` and `result.Edges` straight through.
  - Workspace: `pkg/ontology/query/workspace.go:559` (201).
  - MCP: `pkg/app/mcp/tool_files.go:133`.
  - Search: `pkg/search/retrieval/graph_ppr.go:438`, where a frontier with many code files can crowd out every note endpoint.
- **Recommendation:** Pick edges first by typed precedence and weight, keep the endpoints of the kept edges, then fill with source endpoints. Always filter edges to the retained node set, using one helper shared by `Graph` and `GraphFacts`. Remove the `*4` SQL heuristic, or give the query an `ORDER BY`.
- **Effort:** S to M
- **Dependencies:** none.

### NR-4 Interface type lists under an overlay skip the committed over-fetch (short pages; `Offset` would apply twice)
- **Severity:** P1 (preview correctness)
- **Confidence:** High for the code path. Medium-high that it is reachable: `pkg/app/web/views.go:241` passes the overlay into views, and `pkg/app/views/source_ontology.go:180` requests interfaces with `Limit`, `Predicates`, and `Sort`.
- **Location:** `pkg/ontology/noderead/type_instances.go:94-113`, compared with the concrete-type path at `119-137`.
- **Problem:**
  - For concrete types under an overlay, `planReq.Limit = overlayTypeInstancesPlanLimit` and `Offset = 0` widen the committed fetch, and the merge pages afterwards.
  - For interfaces, `baseReq.Limit` is widened only when `!usePlan`. With predicates or sort, `interfaceItemsCollapsed` fetches exactly `Limit` committed rows, starting at `Offset`. `mergeOverlayTypeItemsLocked` then drops touched rows and applies `Offset` and `Limit` again (`type_instances.go:258-266`).
  - Result: a staged edit that moves a committed row out of the filter leaves the page short, and any caller passing `Offset > 0` gets a doubly offset, usually empty, page.
- **Evidence:** `TestScopeReadOverlayTypeInstancesFillsLimitedPageAfterTouchedRowLeavesFilter` covers concrete types only. No overlay test combines an interface with predicates or sort.
- **Recommendation:** Apply the concrete-type rule to interfaces: under an overlay, set `baseReq.Limit = overlayTypeInstancesPlanLimit` and `baseReq.Offset = 0`. The cleaner fix is to hoist "overlay ⇒ over-fetch, then merge, then page" into one wrapper around both branches so the rule cannot diverge again. Also return an explicit truncation flag when the over-fetch cap is reached, because silent truncation above 10,000 rows is the same class of bug.
- **Effort:** S
- **Dependencies:** none.

### NR-5 `RelationCounts` has two semantics; the web type page uses the one that breaks the documented invariant
- **Severity:** P2
- **Confidence:** High
- **Location:** `pkg/ontology/noderead/counts.go:19-21`, `78-124`; consumer at `pkg/app/web/ontology.go:586`.
- **Problem:** With no selections or filters, `RelationCounts` goes to `legacyRelationCounts`. That path counts edge rows (`counts[...]++`), with inbound and outbound counted separately and one query per relation name. The selection path counts distinct target refs. `noderead.md` says "Relation counts equal unbounded relation lists … count distinct target refs in one batch."
- **Evidence:** `TestScopeRelationCountsUsesBatchedEdgesWithoutHydration` asserts that `notes/a.md` has a count of 3 when `c.md` is reached twice (`a→c` and `c→a`). By the distinct-target rule the answer is 2.
- **Recommendation:** Delete `legacyRelationCounts` and route through selections. Fix NR-1 first, because the selection path goes through `Neighborhood` canonicalization.
- **Effort:** S
- **Dependencies:** NR-1

### NR-6 Hydration profiles behave as two levels, and summary decodes `assessment_json` per row
- **Severity:** P2
- **Confidence:** High
- **Location:** `pkg/ontology/noderead/scope.go:227-232`, `records.go:175-177,631-633,1468-1475,1555`, `host_reads.go:37-45`, `types.go:296-308`.
- **Problem:**
  - `normalizedHydrateProfile` collapses identity into summary and workspace into content. `recordsForPaths` uses the profile only for `profileNeedsContent`.
  - `HydrateIdentity`, used by views at `pkg/app/views/source_relation_values.go:228,423`, therefore pays for metadata, properties, tags, type rows, assessments, and section summaries.
  - `HydrateWorkspace` has no caller outside noderead.
  - Summary hydration calls `assessmentsByPathsLocked`, which runs `ontology.AssessmentFromJSON` once per path only to compute `HasIssues`. The materialized flags exist, but only through a whole-vault store method (`OntologyAssessmentFlags`). The "Inventory decode" checklist item names this pattern for whole-vault reads; here it runs on every summary page.
- **Recommendation:**
  1. Reduce to `HydrateSummary` and `HydrateContent`. Delete `Identity` and `Workspace`, or make `Identity` metadata-only if views show a measurable need.
  2. Add `OntologyAssessmentFlagsByPaths` to the store. Summary uses it when materialization is current; decoded assessments stay behind `AssessmentsByPaths`.
  3. Update the profile ladder in `noderead.md`.
- **Effort:** M
- **Dependencies:** none.

### NR-7 Single-type and limited listings still load whole-vault inventory, and summary pages loop per type
- **Severity:** P2
- **Confidence:** High for the mechanism. Medium for impact.
- **Location:** `pkg/ontology/noderead/type_instances.go:36` (`ensureNoteState`), `scope.go:424-459`, `type_instances.go:845-878`; consumers at `pkg/app/web/ontology.go:285-340,459`.
- **Problem:**
  - Every `TypeInstances` call, including a limited, filtered plan query for one type, runs `ensureNoteState`. That reads all metadata rows (`CurrentNoteMetadataRows`), `OntologyTypesByPaths` for every vault path in batched `IN` clauses, and the flags. The only use for a plan query is `noteRootItem`'s title, mtime, and issue flag, which the returned `NodeRow` already carries (title, `updated_at`).
  - `catalogTypeItems` loads every row of a type and truncates in Go.
  - The web summary and atlas materialize full item lists for every type and interface to get `Count`, `IssueCount`, and five starting notes. The single-snapshot aggregate (`OntologyShape` and `readmodel.ShapeStore`) already exists.
- **Recommendation:**
  1. Plan and catalog paths build items from `NodeRow` plus per-path flags (NR-6).
  2. Push `LIMIT` into `OntologyNodesByType`.
  3. Keep `ensureNoteState` for `TypeScopeAll` and `TypeScopeIssues` only.
  4. Serve summary and atlas counts from one aggregate query, or from `OntologyShape`, instead of N `TypeInstances` calls.
- **Effort:** M
- **Dependencies:** NR-6 (flags by paths).

### NR-8 Dead and speculative surface
- **Severity:** P2
- **Confidence:** High
- **Location:** `pkg/ontology/noderead/types.go:83-122,125-134,307,818`, `helpers.go:19-60`, `execute.go:119,223`, `resolve.go:266`.
- **Problem:** Each of the following is dead or does not do what its documentation says:
  - `SharedNodeCache`, `CachedNode`, and `NodeCacheMeta`: no implementation exists, and `Get` and `Set` are never called. The only call is `InvalidateAll` at `resolve.go:266`, plus a test probe.
  - `ScopeOptions.Budget`: no reader anywhere. The `noderead.md` must-do still says "respect … `ScopeOptions.Budget`".
  - `HydrateWorkspace`: see NR-6.
  - `NewScope(_ context.Context, …)`: the context is ignored.
  - Identity-key helpers with three names for one function: `NormalizeRef`/`normalizeNodeRef` and `RefIdentityKey`/`nodeRefIdentityKey`/`projectionCacheKey`.
  - `Execute`'s `IncludeLocators`: computes locators and discards them (NR-1).
  - `Scope.Walk(ctx, noteMgr, vaultDef, …)`: re-accepts dependencies that `Service` already holds.
- **Recommendation:** Delete them. Collapse the key helpers to `NormalizeRef` and `RefIdentityKey`, update `noderead.md`, and drop the `SharedCache` bullet from the must-dos.
- **Effort:** S
- **Dependencies:** none.

### NR-9 Store capabilities are discovered by silent type assertion
- **Severity:** P2
- **Confidence:** High
- **Location:** `pkg/ontology/noderead/types.go:20-62`. The 14 assertions are at `records.go:249,391,456,516`, `resolve.go:45,598,699`, `type_instances.go:164,846,1012`, `graph.go:114,859`, `neighborhood.go:75`, and `recent.go:25`.
- **Problem:**
  - `Store`, `CatalogStore`, `GraphStore`, `ShapeStore`, `RecentStore`, and `NoteAliasesProvider` have one production implementation, `*semdb.Store`.
  - When an assertion fails, noderead degrades silently: an empty graph, nil field values, projection-only embedded hydration, or `ensureEmbeddedIndex`.
  - Test fakes that implement only `Store` therefore exercise code paths production never takes. Examples are `pkg/app/presentation/ontology_context_test.go:647` and `bounded_hydration_test.go`'s `boundedHydrationStore`.
  - The "storage-neutral" claim is also half true: `Store` returns `semdb.NoteMetadataRow`, `semdb.OntologyEdgeRow`, and other `semdb` rows directly.
- **Recommendation:** Define one `ReadStore` interface that is the union of these, and require it in `NewService`. A missing capability then fails at compile time rather than degrading silently. Delete the degrade branches; most tests already use `sqlitefixture`. Remaining fakes should embed `*semdb.Store`, as `countingStore` does.
- **Effort:** M
- **Dependencies:** none. This unblocks deleting the NR-2 fallback.

### NR-10 Duplicate node DTOs and an alias layer around `readmodel`
- **Severity:** P2
- **Confidence:** High
- **Location:** `pkg/ontology/readmodel/graph.go:5-18`; `pkg/anchors/sqlite/graph_readmodel.go:14-44`; `pkg/anchors/intel.go:67-92`; `pkg/ontology/noderead/graph.go` (`nodeRefFromGraphNodeRow`) compared with `resolve.go` (`nodeRefFromCatalogRow`); `execute.go:64-68`; `pkg/ontology/query/types.go:508-521`.
- **Problem:**
  - `GraphNodeRow` is a column subset of `NodeRow`. `GraphOntologyNodes` fetches `NodeRow`s through `OntologyNodesByPaths` or `AllOntologyNodes`, converts them one-to-one, and applies `Limit` in Go.
  - `pkg/anchors/intel.go` re-exports every `readmodel` type under `Intel*` and `Ontology*` names, about 600 references across roughly 120 files. That is a compatibility shim, which the architecture policy bans.
  - `query.noteRecord` copies 11 fields of `NodeRecord`.
  - `NodeView` duplicates `Ref` and `TypeName` from its own `Record`.
  - `TypeInstances` returns `ontology.TypeListResult`/`NodeListItem`, a fifth shape of the same node.
- **Recommendation:**
  1. Delete `GraphNodeRow` and its converter; return `[]readmodel.NodeRow`.
  2. Rewrite the anchors aliases mechanically (`gofmt -r 'codeanchor.IntelOntologyNode -> readmodel.NodeRow'`, and so on) and delete them.
  3. Make `NodeView` just `NodeRecord`.
  4. Keep `readmodel` as the single leaf package for catalog and query row types. It earns its place as a cycle-free home for rows shared by `anchors/sqlite`, `noderead`, and `query`; it does not earn a second graph-row shape.
  5. Replacing `query.noteRecord` with `NodeRecord` belongs to the query lane.
- **Effort:** M (mostly mechanical)
- **Dependencies:** none.

### NR-11 Consumers bypass the Scope for summary rows, overlay patches, and traversal
- **Severity:** P2
- **Confidence:** High
- **Location and evidence:**
  - **`pkg/app/web/ontology.go:527-600` (`enrichOntologyType`):** assembles each list row from four sources in goroutines that share one mutex-serialized scope:
    - tags through `noteTagsByPaths` (`:2456`, direct store);
    - identity status through `identityStatusByItemRefs` (`:2338`), which directly reads `OntologyNodesByPaths`, `OntologyNodesByNoteFragments`, and `OntologyNodeFieldValuesByNodeIDs` and re-implements `Scope.IndexedRecordFieldValues`;
    - issues through `AssessmentsByPaths`;
    - counts through legacy `RelationCounts` (NR-5).

    It then patches overlay state by hand (`overlayIdentityStatuses` `:2279`, `overlayNoteTags` `:2309`) and keys identity status by `Ref.String()`, the identity-collapse pattern the noderead guide forbids. `Hydrate(Summary)` plus `Scope.FieldValues` already returns title, type, tags, and issues, and applies the overlay uniformly.
  - **`pkg/app/web/search_notes.go:59,205,243`:** reads types from `OntologyTypesByPaths` directly, tags directly, and concrete types through `OntologyPathsByType`, while interfaces go through `TypeInstances`. That is two semantics for one filter, and a user-supplied `_Fallback*` name would bypass the fallback-visibility rule.
  - **Other direct type reads:** `pkg/app/web/ontology_diff.go:161` (error swallowed), `pkg/app/web/graph.go:115`, `pkg/app/mcp/semantic_query_unified.go:871`, `pkg/app/unifiedsearch/run.go:1229`. These are committed-only contexts, so the risk is low.
  - **`pkg/app/web/ontology.go:1940-1960` (`ontologyNodesByLocator`):** a direct catalog read that returns an empty map on error.
  - **`pkg/app/cli/context_text.go:2050-2080`:** in `IndexedReadOnly` mode it re-implements `Neighborhood` as a loop of `Traverse` calls over structural × relation, per note, swallowing errors with `return nil`. `:1847,1946` call `TypesByPaths` with a single path per target.
  - **`pkg/ontology/query/execute.go:2930-2933`:** when an embedded or section graph is empty, retries with the parent note. That breaks the "Endpoint widening" rule.
- **Recommendation:**
  1. Add one `Scope.Summaries(ctx, refs, fields)`, or document `Hydrate(Summary)` plus `IndexedRecordFieldValues`, as the list-row API, and migrate the web type page and note search to it.
  2. Delete `identityStatusByItemRefs`, `noteTagsByPaths`, `overlayIdentityStatuses`, and `overlayNoteTags`.
  3. Replace the CLI `Traverse` loop with `Scope.Edges`.
  4. Delete the query widening fallback.
  5. Make `Traverse` unexported once the CLI and `query/loaders.go:334,346` no longer call it.
- **Effort:** M
- **Dependencies:** NR-1, so that `Edges` with embedded endpoints is cheap; NR-5.

### NR-12 `Neighborhood` and `Traverse` over-fetch and loop per relation name
- **Severity:** P2
- **Confidence:** High for the mechanism. Medium for impact, which depends on hub degree.
- **Location:** `pkg/ontology/noderead/neighborhood.go:47-160`; `scope.go:268-288`; `counts.go:99`.
- **Problem:**
  - `Neighborhood` issues one `OntologyEdgesForPaths(paths, true, relation, 0)` query per relation name. Each query is unbounded and returns both directions. Direction, structural and ambient, provenance, and limits are all applied in Go, and `FirstPerSource` and `FirstTotal` only after the full load, so an outbound request on a hub note still loads every backlink.
  - `Traverse` always queries without a limit so the cache can serve lower limits. When any requested source is missing from the cache, it re-queries all sources rather than only the missing ones.
- **Recommendation:** One query per call with `relation IN (…)` and a direction predicate. Query only the missing sources in `Traverse`. Push per-source limits (window function) into SQL when the caller passes them.
- **Effort:** M
- **Dependencies:** none.

### NR-13 Swallowed errors, including a database-error fallback that reads files on the summary path
- **Severity:** P2
- **Confidence:** High
- **Location:**
  - `records.go:1269-1271`: `locatorsLocked` returns `(out, nil)` on errors other than cancellation.
  - `resolve.go:592-600`: `canonicalCandidateRefs` drops type and catalog read errors, cancellation included.
  - `resolve.go:688-690`: `notePathCache` drops the `GetNotesList` error.
  - `records.go:45-79`: `recordsForPaths`.
  - `web/ontology.go:1949`.
- **Problem:** Callers receive partial results with no signal. `recordsForPaths` is the odd one out: when the property or tag queries fail, it switches to parsing note files. That contradicts the rule "Never add file reads to summary paths" and hides store faults.
- **Recommendation:** Propagate the errors. Delete the snapshot fallback in `recordsForPaths` and its private test (NR-T-3).
- **Effort:** S
- **Dependencies:** none.

### NR-14 Lock juggling inside `Scope`
- **Severity:** P3
- **Confidence:** Medium
- **Location:** `pkg/ontology/noderead/neighborhood.go:195-201` (`hydrateLocked`); `scope.go:20-21`; `pkg/app/web/ontology.go:550-610`.
- **Problem:**
  - `Neighborhood` holds `s.mu` through a `defer`, but `hydrateLocked` unlocks and re-locks in the middle, with no `cacheGeneration` fence. The graph builders do use a fence. An `invalidateAfterApply` arriving in that window, or a panic in `Hydrate`, is handled differently on each path.
  - `Hydrate` and `TypeInstances` hold the mutex across SQLite and file I/O, so web's four goroutines on one scope mostly serialize.
- **Recommendation:** Pick one model. Either a `Scope` is single-goroutine (document it, remove the mutex, and give web's parallel enrichment separate scopes), or keep the mutex and apply the generation fence wherever the lock is released.
- **Effort:** S to M
- **Dependencies:** none.

### NR-15 File size and repeated work
- **Severity:** P3
- **Confidence:** High
- **Location:** `records.go` (1,562 lines), `type_instances.go` (1,286), `graph.go` (1,180), `types.go` (863), `resolve.go` (836), `execute.go` (650), `neighborhood.go` (544), `scope.go` (506); `scope_test.go` (3,158). The architecture policy caps files at about 500 lines.
- **Problem:** Beyond file size:
  - `nodeRefFromCatalogRow` JSON-decodes `NodeRefJSON` twice per catalog row (`recordFromCatalogRow` and `catalogLinkTarget`).
  - `Hydrate` deep-copies each embedded record three times (`scope.go:72-74,104`). Even so, `cloneMap` is shallow for nested frontmatter values, and `resolvedFrom` and `projectedFields` are not copied.
  - The `AsFallbackNoteProjection`/`AsFallbackSectionProjection` pair is repeated five times.
  - Under an overlay, `recordsForPaths` treats touched paths as always missing and re-queries their properties and tags on every call.
  - `BuildReadOverlayIndexReplacingPaths` copies previous entries without filtering by the new touched set. This is latent; the web caller only appends operations.
- **Recommendation:** Split by concern:
  - `records.go` into `hydrate_notes.go`, `hydrate_embedded.go`, `overlay_records.go`, `locators.go`, and `projection.go`;
  - `type_instances.go` into the base, overlay, and interface paths;
  - `graph.go` into `graph.go`, `graph_facts.go`, and `graph_diagnostics.go`.

  Clone once at the API boundary. Add a `projectFallback` helper. Filter carried overlay entries by `TouchedPaths`.
- **Effort:** M
- **Dependencies:** best done after NR-8, NR-9, and NR-10 shrink the code.

**Checked and found sound:** no consumer keeps a `Scope` across requests. Web, MCP, CLI, views, and query each mint one per request or plan (for example `pkg/app/views/source_ontology.go:728` and `pkg/search/planner/planner.go:182`). `ReadOverlayIndex` reuse is keyed correctly in `edit_session_overlay.go`. The single sanctioned write seam (`applyEmbeddedLinkTargets` and `Service.ApplyLinkTargets`) is the only mutation path, and preview scopes reject it.

## Findings — tests

The suite takes 1.43 s wall time across 124 top-level tests (262 including subtests). Most tests use a real SQLite fixture and public `Scope` methods. The overlay sort tests' differential oracle (`assertOverlaySortMatchesCommitted`) is the best pattern in the package: it asserts that the staged overlay matches a committed index built from the post-edit state.

### NR-T-1 Merge the six cancellation tests; drop the two that cannot fail
- **Verdict:** merge and delete.
- **Tests:** in `foundations_regression_test.go`: `TestCanceledHydrateCanRetrySameScope`, `TestExpiredReadsCanRetrySameScope`, `TestInFlightCanceledHydrateCanRetrySameScope`, `TestSourceCanceledHydrateCanRetrySameScope`, `TestCanceledLocatorsCanRetrySameScope`, `TestChildDeadlineLocatorsCanRetryWithHealthyParent`.
- **Why:** The two pre-canceled variants return at the first `ctx.Err()` check (`scope.go:22`, `records.go:1172`), before any cache code runs, so they cannot detect cache poisoning. The other four share one shape: cancel during a store or source read, then retry on the same scope and compare with a fresh scope.
- **Replacement:** one table-driven `TestCanceledReadLeavesScopeRetryable`. Rows: metadata-store cancel; snapshot-source cancel; locator-source cancel; and child deadline at the link-service, scope-locators, and scope-hydrate boundaries. Keep `TestOrdinaryLocatorSourceFailureRemainsDiagnostic` as the contrast case.

### NR-T-2 Merge the paired apply tests and drop the private-field assertions
- **Verdict:** merge and rewrite.
- **Tests:**
  - `ensure_apply_failure_test.go:TestResolveApplyRequiresLiveAuthorityBeforeWriting` and `foundations_regression_test.go:TestLocatorsApplyRequiresLiveAuthorityBeforeWriting` test the same dispatch (`applyEmbeddedLinkTargets`) through two entry points.
  - `ensure_apply_failure_test.go:TestResolveApplyRetriesConvergenceAfterSourceCommit` duplicates the `publication-failure` subtest of `TestLocatorsApplyConvergesAndRetriesPublication`, and asserts the private fields `scope.typeInstancesByID`, `noteStateLoaded`, and `notePathCacheLoaded`. The package's own `AGENTS.md` forbids that.
  - `refresh_invalidation_test.go:TestResolveApplyInvalidatesCollectionAndGraphCaches` overlaps the `healthy` subtest, which also rereads `TypeInstances`, `Graph`, and `GraphFacts`.
- **Replacement:** one authority test with an `{entry: Resolve | LocatorsWithEnsure} × {missing writer, preview, preview+writer}` table. Keep one convergence test, `TestLocatorsApplyConvergesAndRetriesPublication`, with an added `Resolve` entry-point row. Delete the other two, after moving `TestResolveApplyRetriesConvergenceAfterSourceCommit`'s plan-then-apply assertion (`planned.FixPlan == nil`, yet apply still converges) into the kept test. Remove `locatorSharedCacheProbe` with NR-8.

### NR-T-3 Rewrite the private-helper tests through the public API
- **Verdict:** rewrite.
- **Tests:**
  - `bounded_hydration_test.go`: `TestSnapshotEligibilityAndMultiHostMetadataBatch` (calls `ensurePathStateLocked` and `snapshotForPathLocked`), `TestMetadataRecordFallbackRequiresProvenEligibility` (`recordsForPaths`), `TestMetadataBatchErrorDoesNotReturnOrCachePartialRecords` (asserts the `recordKnown` map), `TestEmbeddedProjectionBatchStateErrorsFallBackPerHost` (`embeddedRecordsFromProjectionNoOverlay`), and `TestLoadedRecordAliasIndexPreservesKindAndRejectsAmbiguity` (`indexLoadedRecordsForRequests`).
  - `scope_test.go:TestScopeResolveTargetParsingAvoidsNotePathCacheForExplicitTargets` (`parseResolveTargets`).
- **Why:** Every behavior here is reachable through `Scope.Hydrate` and `Scope.Resolve` with the same fakes. Private-entry tests block the file splits in NR-15. `TestMetadataRecordFallbackRequiresProvenEligibility` should be deleted outright if NR-13 removes the fallback.
- **Keep:** `apply_cache_generation_test.go`'s direct `invalidateAfterApply` calls. The publication race fence cannot be driven deterministically through the public API.

### NR-T-4 Delete tautological helper tests
- **Verdict:** delete.
- **Tests:** `scope_test.go:TestCloneNodeRecordPreservesNilFieldValues` and `scope_test.go:TestRefHelpersPreserveNodeIdentity`.
- **Why:**
  - The clone test pins nil-versus-empty on a private clone, which is not a contract. Its sibling `cloneMap` and `cloneInlineProps` do the opposite, turning nil into an empty map.
  - The ref-helper test's identity separation is already pinned by `TestScopeProjectionCacheSeparatesNoteAndNodeIDRefs`. Its `NormalizeRef` assertion becomes trivial once the alias helpers collapse (NR-8).

### NR-T-5 Fold the hand-asserted overlay predicate and sort tests into the differential oracle
- **Verdict:** merge; delete one.
- **Tests:** in `scope_test.go`:
  - `TestScopeReadOverlayTypeInstancesPreservesTypedIntSort`: delete; the `rank`/`int` rows of `TestScopeReadOverlayTypeInstancesSortNullPlacement` subsume it.
  - `TestScopeReadOverlayTypeInstancesAppliesTypedBoolPredicate`, `TestScopeReadOverlayTypeInstancesFiltersStagedRows`, `TestScopeReadOverlayTypeInstancesFillsLimitedPageAfterTouchedRowLeavesFilter`, and `TestScopeReadOverlayTypeInstancesSortsCommittedRowsBySchemaFields`: convert to rows.
- **Why:** Each builds a bespoke two-note fixture and hard-codes the expected paths. The oracle (overlay result == committed-after result) checks the actual contract and catches regressions the hard-coded lists cannot.
- **Replacement:** generalize `assertOverlaySortMatchesCommitted` into `assertOverlayMatchesCommitted(schema, before, after, TypeInstancesRequest)`, and add rows for predicates (`eq`/`exists`/`gt` on staged rows that both match and fail), `Limit` with a row leaving the filter, `Offset`, and interface types (NR-T-7c).

### NR-T-6 Rewrite brittle assertions
- **Verdict:** rewrite.
- **Tests:**
  - `bounded_hydration_test.go:TestInboundNeighborhoodBatchesCandidateTypeLookup`: `require.Equal(t, 20, store.typePaths)` pins the over-fetch (types for 20 sources when one edge is returned). Keep the batching guard `typeCalls == 1` and drop the path count.
  - `scope_test.go:TestScopeRelationCountsUsesBatchedEdgesWithoutHydration`: pins the legacy edge-row count of 3 (NR-5). Rewrite to assert that the count equals the distinct targets from `Neighborhood`, keeping `EdgeLoads == 1` and `RecordLoads == 0`.
  - `scope_test.go:TestScopeAllowsConcurrentUse`: calls `require` inside goroutines (`FailNow` off the test goroutine) and exercises only `Hydrate`. Use `assert`, and mix in `Neighborhood` with hydration (the unlock and re-lock path), `Graph`, and `TypeInstances` on one scope so `-race` exercises the real hazard (NR-14).

### NR-T-7 Missing tests to add
- **Verdict:** add. (a) through (e) fail today.
- **a. `TestScopeSummaryHydrationStoreCallsIndependentOfRefCount`:** hydrate one note, then 25 notes, through `countingStore`, and assert identical call counts per store method. Do the same for `Neighborhood` over 1 versus 25 sources. The existing batching tests use one or two refs, which cannot tell batched calls from a per-ref loop.
- **b. `TestScopeNeighborhoodEmbeddedEndpointsDoNotListVault`:** run `Neighborhood`, `RelationCounts`, and `Execute` with embedded sources and targets, and assert `countingNoteReader.lists == 0 && contents == 0` (NR-1). Model it on the existing `reader.lists == 0` check in `TestScopeResolveTargetParsingAvoidsNotePathCacheForExplicitTargets`.
- **c. `TestScopeReadOverlayInterfaceTypeInstancesMatchesCommitted`:** an interface with predicate, sort, `Limit`, and `Offset`, where a touched row leaves the filter (NR-4).
- **d. `TestScopeGraphNodeLimitKeepsEdgesConsistent`:** with `NodeLimit` and `EdgeLimit` set, every edge endpoint appears in `Nodes`, and a note source's direct neighbors are not displaced by code endpoints (NR-3).
- **e. `TestScopeTypeInstancesEmptyEmbeddedTypeDoesNotReadSource`:** with a ready catalog and zero rows for a `SourceShape` type, assert `reader.contents == 0` (NR-2).
- **f. Overlay with locators:** `Resolve` on a staged embedded node with locators enabled either returns a locator derived from staged content or explicitly reports that it is unsupported. Today it silently reads committed disk content.
- **g. `TestHydrateProfileFieldContract`:** a table over {note, embedded, section, provider root} × {summary, content}. Assert which fields are populated, that `Content` is empty under summary, and that summary makes zero `GetContents` calls. This pins NR-6 after the profile collapse.
- **h. RelationCounts property:** for the shared fixture, `RelationCounts(req).Count == len(distinct targets of Neighborhood(req))` across directions and filters, for both the default and the selection requests.

**`readmodel`:** it needs no tests. It holds DTOs and interfaces with no behavior; the SQLite implementations are tested in `pkg/anchors/sqlite`. Keep the compile-time assertions in `store_contract_test.go`, which are the useful part.

## Suggested refactor sequence

1. **Delete dead surface (NR-8)** and the tautological tests (NR-T-4). This is small and shrinks the code before anything else changes. Update `noderead.md` for `Budget` and `SharedCache`.
2. **Fix the locator path (NR-1)** together with tests NR-T-7a and b. This is the largest performance and correctness win, and it unblocks steps 3 and 7.
3. **Unify `RelationCounts` (NR-5)** and rewrite `TestScopeRelationCountsUsesBatchedEdgesWithoutHydration` (NR-T-6).
4. **Interface overlay over-fetch (NR-4)** and **graph truncation (NR-3)**, each a small fix with its failing test first (NR-T-7c and d). Generalize the overlay oracle (NR-T-5) at the same time.
5. **Make the store contract one interface (NR-9)**, then remove the embedded-index fallback (NR-2, NR-T-7e) and the silent degrade branches. Fold the error-propagation fixes (NR-13) in here.
6. **Collapse profiles and add flags by paths (NR-6), then scope inventory (NR-7).** This needs one new store method; no migration, because the materialized columns already exist.
7. **Migrate consumers (NR-11):** the summary-row API, then delete web's overlay patches and direct catalog reads, the CLI `Traverse` loop, and the query graph widening. After that, unexport `Traverse`.
8. **Clean up DTOs (NR-10)** with mechanical alias rewrites. Coordinate with the query and anchors lanes.
9. **Neighborhood SQL shape (NR-12)**, guarded by NR-T-7a.
10. **Split files, settle the lock model, and consolidate tests** (NR-15, NR-14, NR-T-1, NR-T-2, NR-T-3) once the code has shrunk.

## Slow-test table (measured)

One run of `go test -count=1 -json ./pkg/ontology/noderead/... ./pkg/ontology/readmodel/...`: noderead finished in 1.434 s wall time (124 top-level tests, 262 including subtests, about 5.0 s summed test time with `t.Parallel`). `readmodel` has no test files. Speed is not a problem in this subsystem.

| Test | Seconds |
| --- | --- |
| TestScopeReadOverlayTypeInstancesSortNullPlacementPages (2 subtests) | 0.43 |
| TestScopeReadOverlayTypeInstancesSortNullPlacement | 0.16 |
| TestScopeProjectionDoesNotFallbackInvalidDeclaredType | 0.16 |
| TestLocatorsApplyConvergesAndRetriesPublication | 0.10 |
| TestScopeReadOverlayTypeInstancesFiltersStagedRows | 0.09 |
| TestScopeResolveReportsAmbiguousPreferredIdentifierCandidates | 0.08 |
| TestScopeReadOverlayTypeInstancesPreservesTypedIntSort | 0.08 |
| TestScopeNeighborhoodHydratesTargetsOnce | 0.08 |
| All others | ≤ 0.07 each |
