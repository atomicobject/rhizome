---
summary: "Lane report from the 2026-10-17 subsystem quality review: Ontology core review. Findings are a snapshot of commit 5251923f."
---

# Ontology core review

Scope: top-level `pkg/ontology/*.go` excluding `edit_*`, plus `idalloc`, `sectionparse`, `reference`, `viewconfig`, `guide`. Worktree `t3-e36f9b1a` at `5251923f`. Read-only. I ran the package suite once (`go test -count=1 -json ./pkg/ontology/`). I also ran three tiny probe tests, injected with `go test -overlay` so nothing was written to the repo, to confirm two divergence claims. The probe source is at `/tmp/rhizome-subsystem-review/overlay/zz_review_probe_test.go`, and it reuses the package's own `assertOntologyBuildMatchesStore` harness.

## Summary

The ontology core is carefully reasoned locally. Spans, identity, and fail-closed edit rules are thoughtfully documented, and the suite is fast. The structure around that local care is weak. The core is one ~18k-line package (this lane alone) with two independent implementations of "materialize the read model", and those implementations have drifted.

The three biggest problems:

1. **The production indexing path silently drops a whole class of validation issues (ONT-1, P0).** These are embedded-node field issues, `contains_min/max`, embedded title rules, and block-ID issues. Full rebuild (`EnsureIndexed` → `buildIndexFromDocs`) emits them. `SyncPublishedPaths` (the only production sync entry point) never does. As a result, `rzm validate` output depends on which writer last touched the rows. I confirmed this with probes.
2. **Starter-specific domain logic is hardcoded in core sync (ONT-2, P1).** The code hardcodes `EffortNote`, `UserStory`, the heading "Stories In Scope (Frozen)", and the relation `frozenStories`. It contradicts the "schema is the single source of truth" constraint. It breaks delta-sync == fresh-rebuild (confirmed by probe). It also re-projects every typed note on every sync, even when no effort notes exist.
3. **Each note is re-projected 3–5 times per sync, and two helpers are quadratic (ONT-3, P1).** Every projection re-runs the inline-property scan and type resolution. Two helpers construct a resolver per child, which is exactly the O(children × document) pattern the subsystem note forbids.

**First refactor:** add a differential test harness (ONT-T-1) that runs every build fixture through all three paths: in-memory full build, `SyncPaths` full rebuild, and incremental touch. Then collapse the two build loops into one per-note "project once, assess once" function that both full and incremental paths call (ONT-1 plus ONT-3). That single change fixes the P0, removes most of the duplicated sync code, and makes the package split (below) mechanical.

## Findings — production code

### ONT-1 Full rebuild and incremental sync compute different validation results; the production path omits embedded/cardinality/block-ID issues

- **Severity:** P0 (validation silently missing; delta sync ≠ fresh rebuild)
- **Confidence:** High (code read plus probe)
- **Location:** `pkg/ontology/index.go:609-731` (`buildIndexFromDocs`), `pkg/ontology/sync.go:1118-1224` (`computeOntologyJob`), `sync.go:1226-1254`, `sync.go:1256-1327` (`buildOntologyDelta`), `index.go:1213-1324` vs `sync.go:1388-1423` (ambient edges)
- **Problem:** The full in-memory build and the sync path each have their own per-note assessment loop.
  - The full build runs `semanticValidationIssues`, `validationIssuesFromBlockIDs` (index.go:693), and `validateEmbeddedProjectionFields` (index.go:725) for every typed Markdown note.
  - `computeOntologyJob` runs only `semanticValidationIssues` for typed notes. It calls `validateEmbeddedProjectionFields` only for *untyped* global-source notes (sync.go:1243) and never calls `validationIssuesFromBlockIDs`.
  - The only production callers of sync are `SyncPublishedPaths` (pkg/app/indexing/validation_projection.go:336, unified.go:677, indexcore/publish.go:231), and it uses `computeOntologyJob` for both incremental and full rebuilds.
  - `rzm validate` reads issues through `EnsureFreshRuntime` (pkg/validate/suite.go:117, checks.go:224). That returns stored `ErrorJSON` when state is "materialized" and rebuilds with `buildIndexFromDocs` otherwise. Validation output therefore depends on which writer last ran.
- **Evidence:**
  - Probe `TestReviewProbeSyncFullRebuildFresh`: a fresh `SyncPaths` full rebuild of a spec with one embedded `UserStory` missing a required `status`, under a `min: 2` collection, reports 1 issue (`missing_required_field stories.stories.status`).
  - Probe `TestReviewProbeFullVsSyncRebuild`: `EnsureIndexed` on the same fixture reports 3 issues (adding `contains_min_not_met` and the node-scoped `missing_required_field status specs/graph.md#^graph-story`). After one incremental edit to the note, `SyncPaths` drops back to 1, and `assertOntologyBuildMatchesStore` fails on the assessment JSON.
  - Secondary drift, from reading the code:
    - Full build sorts each assessment's issues *after* appending inverse issues (index.go:772-779). Incremental sorts *before* (sync.go:1215) and appends inverse issues afterwards (sync.go:1316), so `AssessmentJSON` bytes differ between paths.
    - Full ambient edges fall back to `doc.Links` when the graph table is empty (index.go:1213-1277). Sync has no such fallback.
  - Existing `TestFullAndIncrementalOntologyBuildsConvergeAcrossRenameDelete` (source_convergence_test.go:47) uses a fixture with no sections, embedded nodes, or block IDs, so it cannot see this.
- **Recommendation:**
  - Extract one `assessProjectedNote(note *projectedNote, ctx assessContext) noteSyncResult` that owns field assessment, semantic, embedded, and block-ID validation, node-scoped plus ambient edges, and catalog rows.
  - Make `buildIndexFromDocs` a loop over that function plus the cross-note passes (inverses, catalog witnesses), using the same comparator (`sortValidationIssues`) at the end.
  - Delete the bespoke switch in index.go:657-688; it is a copy of sync.go:1166-1194.
  - Make `buildAmbientEdges` use the same incident-row algorithm as sync and delete the `doc.Links` fallback (or put it in both paths).
  - Bump `OntologyMaterializationVersion` so stored rows heal.
- **Effort:** M
- **Dependencies:** ONT-T-1 first (red test), then ONT-3 (shared `projectedNote`).

### ONT-2 Hardcoded starter-domain "frozen story" edges in core sync

- **Severity:** P1
- **Confidence:** High
- **Location:** `pkg/ontology/sync.go:569-752` (`appendFrozenStoryScopeEdges`, `storyCatalogNodesByID`, `walkStoryIDProjections`, `sectionBodyByHeading`, `storyIDsInText`); callers at index.go:735 and sync.go:327
- **Problem:** Core sync emits structural `frozenStories` edges only when the type name is literally `"EffortNote"` and the target type is literally `"UserStory"`. It looks for the heading "Stories In Scope (Frozen)" and tokenizes text for `SPEC-…US…`. This is agentic-engineering starter vocabulary (`pkg/app/cli/init/templates/starters/agentic-engineering/rhizome/ontology/spec-driven.graphql`) baked into the generic indexer. It violates the subsystem constraint "Nothing downstream may hardcode type names".

  It is also incorrect and expensive:
  - `storyByID` is built only from the notes recomputed in this sync. Adding a story *after* the effort references it never produces the edge until the effort is resynced.
  - The pass projects every typed note in `results`, plus one new resolver per section child (`ProjectBoundNodeFromSnapshot` at sync.go:695). It does this even in vaults with no `EffortNote`.
  - `sectionBodyByHeading` re-parses Markdown that `doc.Snapshot` already holds (sync.go:711).
  - `EffortWorkspace` (also an `Effort`) never gets these edges.
- **Evidence:** Probe `TestReviewProbeFrozenStoryIncremental` syncs the effort and an empty spec, then adds the story to the spec and syncs only the spec. The store has 0 `frozenStories` edges, while `BuildIndexWithStore` expects 1 (`edgeSignatures` mismatch). The repo schema already declares the inverse view as `efforts: [Effort!] @neighbors(direction: INBOUND, type: "Effort", scope: SUBTREE)` (`.rhizome/ontology/spec-driven.graphql:733`). The starter README says story links in that section are wikilinks (`templates/starters/agentic-engineering/repo/docs/efforts/README.md:19`).
- **Recommendation:** Delete the five functions and both call sites. If plain-ID references (no wikilink) must keep producing edges, add a declarative schema mechanism, for example an `@contains` section field typed `[UserStory] @link(source: IDENTIFIER_TOKENS)` resolved through the identifier index. That requires a product decision, so ask Drew. Otherwise rely on the existing `@neighbors(scope: SUBTREE)` over block wikilinks. Bump the materialization version.
- **Effort:** S (delete) / M (if a directive replaces it)
- **Dependencies:** Owner decision on plain-ID references; `cmd/graph_path_test.go:36` fixtures mention `frozenStories` and need updating.

### ONT-3 Each note is projected 3–5 times per sync; two traversals are O(children × document)

- **Severity:** P1 (performance; violates a stated invariant)
- **Confidence:** High on mechanism; not benchmarked end to end
- **Location:** `projection.go:418-440` (`newProjectionResolver`), `node_edges.go:25`, `sync.go:494-543` → `root_projection.go:17` → `node_catalog.go:89`, `sync.go:634,695` (story pass), `sync.go:972-1033` (`globalSourceLinkTargets`), `index.go:825`, `sync.go:874` (`prepareOntologyJobs` → `resolveNoteAssessment`)
- **Problem:** Each `newProjectionResolver` call:
  - re-scans the whole note for inline properties (`obsidian.ExtractInlineProperties(snapshot.Content)`);
  - clones frontmatter;
  - re-runs type resolution (`resolveNoteAssessment`);
  - re-indexes section and global-source types.

  For one typed note, sync builds resolvers in four places: `buildNodeScopedStructuralEdges` (1), `projectRootNodeForDoc` (1), `BuildIntelOntologyNodeReadModelWithOptions` (1, re-projecting every child the root projection already walked), and the frozen-story pass (1 + one per descendant). Type resolution also runs in `prepareOntologyJobs`.

  `globalSourceLinkTargets` (run in `loadExternalOntologyTypes` for every untyped note matching a global `@source`, such as daily notes with `#action-item`s) calls `ProjectBoundNodeFromSnapshot` once per ref. Each call builds a new resolver, which is quadratic for a note with many action items. `walkStoryIDProjections` does the same.

  The subsystem note says "Catalog, structural-edge, and embedded-validation traversal reuse one resolver per operation … never recomputed per node". That is honoured inside `validateEmbeddedProjectionFields` (and guarded by a scaling test) but not elsewhere.
- **Evidence:** `projection.go:279-292` builds a fresh resolver per `ProjectBoundNodeFromSnapshot`. `sync.go:1001` calls it inside `walk` per ref. `node_catalog.go:89` constructs a resolver even though `projection` came from one. `TestValidateEmbeddedProjectionFields_ResolverConstructedOnce` documents the measured cost of this pattern: 928k vs 45k allocations at 500 nodes.
- **Recommendation:** Introduce `type projectedNote struct { doc *sourceDoc; resolver *projectionResolver; root *NodeProjection; descendants []*NodeProjection }`, built once per note per sync or build. Then:
  - `buildNodeScopedStructuralEdges`, the catalog builder, embedded validation, and global-source link discovery take `*projectedNote`, not `(snapshot, schema)`;
  - `newProjectionResolver` accepts the already-resolved assessment instead of recomputing it (see ONT-4);
  - delete `ProjectBoundNodeFromSnapshot` call sites inside loops.
- **Effort:** M
- **Dependencies:** Pairs with ONT-1 (the shared per-note function consumes `projectedNote`).

### ONT-4 Type resolution runs twice from different inputs (index noteDoc vs resolver noteDoc)

- **Severity:** P1 (risk of catalog type ≠ `ontology_note_types` type)
- **Confidence:** Medium. The mechanism is certain; I did not build a concrete failing fixture.
- **Location:** `index.go:177-199` (`projectRootNoteSource`: Title from `source.Title`, Inline from `source.InlineProps`, Tags from provider), `projection.go:418-428` (resolver: Title from `projectionNoteTitle`, Inline from `ExtractInlineProperties`, **no Tags**), `matchers.go:221-228`
- **Problem:** `resolveNoteAssessment` decides the note type and is the basis of `ontology_note_types` and assessments. The resolver then resolves the type again from a differently built `noteDoc`:
  - `tag:` matchers fall back to `obsidian.HasAnyTags(doc.Content, …)` because Tags is empty. That re-parses frontmatter per matcher per type, and its notion of tags can differ from notemeta's.
  - `find:` matchers use a different Title.

  Any disagreement makes catalog rows (typed via the resolver) disagree with note-type and assessment rows. It is also redundant work (ONT-3).
- **Recommendation:** Resolve the type once per note from the provider-current `sourceDoc` and pass it into the resolver (`newProjectionResolver(snapshot, schema, resolved *NoteAssessment)`). Keep `ProjectNodeFromSnapshot`'s self-resolving behavior only for callers with no source doc (edit/read paths), and add a parity test (ONT-T-3).
- **Effort:** S–M
- **Dependencies:** ONT-3

### ONT-5 Two writers and two full-build entry points; test-only branches in production sync

- **Severity:** P1 (design; side write path; whole-vault scans on reads)
- **Confidence:** High on facts. Medium on the ideal target, which crosses into vault-runtime and validate.
- **Location:** `index.go:246-342` (`EnsureIndexed` writes `store.ApplyOntologyDelta` / `ReplaceOntologySnapshot` directly), `index.go:344-456` (`ontologyStateMaterialized`), `runtime.go:100-156`, `sync.go:78-89, 111-162, 343-356, 371-378`
- **Problem:**
  - `EnsureIndexed` is the second full-build implementation (ONT-1). It writes outside the `deltaWriteQueue` lane. It is reachable from `EnsureFreshRuntime*` callers: `rzm validate` (suite.go:117, checks.go:224, identifier_check.go, frozen_scope_drift.go), CLI query (`cmd/ontology_query_runtime.go:130`), MCP (`pkg/app/mcp/tool_ontology.go:166`), next-id, health, and context.
  - Every such call, even when nothing is stale, runs `ontologyStateMaterialized`. That loads every current metadata row, every note state, every assessment (and JSON-decodes each), every catalog node, and every field-value row for the vault, then compares them. That is an O(vault) readiness check per CLI query or validate invocation.
  - Separately, the exported `SyncPaths` (metadata-discovering variant) has **zero production callers** (only 7 test files). The `writeQueue == nil` direct-store fallbacks (sync.go:117-123, 354-356, 374-376) and the `sourcePublished=false` branches (`EnsureIndexed` on schema error, metadata `Ensure` calls) exist only for tests and `EnsureIndexed`.
- **Evidence:** `rg 'ontology\.SyncPaths\('` in non-test code returns nothing. All three `SyncPublishedPaths` callers pass a non-nil `*indexwriter.Writer`.
- **Recommendation:**
  1. After ONT-1, make `EnsureIndexed` call the shared `syncPaths(fullRebuild)` pipeline.
  2. Replace `EnsureFreshRuntime*` in read surfaces with `PublishedRuntimeWithStore` (already read-only). Have stale state trigger the vault runtime's indexing lane rather than an inline rebuild. This needs vault-runtime and validate agreement.
  3. Delete exported `SyncPaths`, the `sourcePublished` flag, and the nil-queue branches. Tests call `SyncPublishedPaths` after a metadata sync with a small in-package queue.
  4. Replace `ontologyStateMaterialized`'s full scan with stored per-path witnesses already maintained by sync (count/digest), or a single aggregate digest in `ontology_schema_state`.
- **Effort:** M–L
- **Dependencies:** ONT-1; vault-runtime/validate owners.

### ONT-6 Scalar-type semantics are re-implemented five ways and disagree

- **Severity:** P1 (silent filter/sort misses); P2 for the dead parts
- **Confidence:** High on the mismatch; medium on user impact (depends on authored formats)
- **Location:** `index.go:1531-1565` (`validateScalarValue`: case-sensitive `"Date"`, accepts `obsidian.AnalyzePropertyValue` layouts including `2006/01/02` and `2006-01-02 15:04`), `node_catalog.go:346-381` (lower-cased; Date must be strict `2006-01-02` via `isISODate`, DateTime strict RFC3339), `field_capabilities.go:129-148`, `node_field_capability.go:131-152`, `pushdown/values.go:56`, `edit_session_frontmatter_scalar.go:101`
- **Problem:**
  - `due: 2026/10/17` or `at: 2026-10-17 10:00` passes validation with no issue but is stored with `ValueKind: "string"` (node_catalog.go:379). Indexed date range filters and sorts then silently skip it.
  - Five switch statements classify `TypeName` with different alias sets (`"bool"`, `"integer"`, `"number"` accepted by some, not by `validateScalarValue`).
  - `IndexedFieldCapability.Editable` and `FieldQueryCapability.Editable` are never read (`rg '\.Editable\b'` hits only `Board.Editable`). They also contradict `NodeFieldCapability`: identifiers are `Editable: true` there but read-only here, and list links are `!List` there but writable here.
  - `IndexedFieldCapabilitiesForType` has no callers.
- **Recommendation:** Add `schema.ScalarKind` and `ParseScalar(field *Field, raw string) (ScalarValue, ok bool)` in the schema layer. Validation is `ok`; the catalog stores `ScalarValue`'s canonical form; capabilities and pushdown read `ScalarKind`. Delete the per-file switches, `Editable`, and `IndexedFieldCapabilitiesForType`. Bump the materialization version.
- **Effort:** M
- **Dependencies:** pushdown owner (consumer)

### ONT-7 Swallowed errors and lost cancellation in the full build

- **Severity:** P2
- **Confidence:** High
- **Location:**
  - `index.go:598-606`: `CurrentNoteMetadataRows` and `GraphDocNoteEdgesForPaths` errors ignored, leaving empty `metaByPath`, which yields wrong `InputFingerprint` and empty `SourceContentHash` persisted as Ready.
  - `index.go:1214-1217`: `store.GraphDocNoteEdges(context.Background())`, whole-vault, error ignored, then falls back to a different algorithm.
  - `index.go:825-828,840`: resolver and child projection errors dropped, so validation silently skips nodes.
  - `index.go:1105-1108`: block-ID validation is skipped when snapshot building fails.
  - `sync.go:634-640,693-697`: story pass drops errors.
- **Problem:** Store failures produce a "successful" build with wrong freshness witnesses. `PublishedCatalogPaths` then refuses navigation admission (hash mismatch), or later syncs recompute unnecessarily, with no surfaced error.
- **Recommendation:** Return the errors. Pass `ctx` through `buildAmbientEdges`. Where partial projection is intentional (validation of a malformed child), emit a `projection_failed` diagnostic instead of silently skipping.
- **Effort:** S
- **Dependencies:** none

### ONT-8 Map-iteration and wall-clock order leak into persisted output

- **Severity:** P2
- **Confidence:** High on mechanism
- **Location:**
  - `index.go:646` (`for path, doc := range resolvedDocs`) feeds `BuildResult.ValidationIssues`, which is returned unsorted by `EnsureIndexed` (index.go:338) and persisted in `ErrorJSON` (index.go:318).
  - `index.go:779` and `sync.go:1215,1246` sort assessment issues only by `(Code, FieldName)` while `validateEmbeddedProjectionFields` visits `projection.Fields` and `projection.Collections` maps (index.go:873-890), so ties keep map order in `AssessmentJSON`.
  - `index.go:1401-1415` (`caseInsensitiveFrontmatterValue` picks an arbitrary key when `Status` and `status` both exist).
  - `index.go:1259,1272,1302,1315` (`time.Now()` per ambient edge instead of the build's `now`).
- **Problem:** The subsystem invariant is "projection is deterministic". Readiness comparison sorts before comparing, so it tolerates this. Persisted bytes, CLI output order, and the convergence harness's `AssessmentJSON` comparison do not.
- **Recommendation:**
  - Use one comparator (`sortValidationIssues`) at every sort site and sort `BuildResult.ValidationIssues`.
  - Expose `NodeProjection` fields as a schema-ordered slice (keep the map as an index) so callers stop re-sorting; `node_catalog.go:167` already does this by hand.
  - Choose case-variant keys deterministically (exact match, else lexicographically first).
  - Thread `now`.
- **Effort:** S
- **Dependencies:** none

### ONT-9 God package with layering inversions; primitives live in the wrong files

- **Severity:** P2
- **Confidence:** High
- **Location:**
  - `index.go` (1732 lines) defines `noteDoc`, `ValidationIssue`, `extractFieldValues`, `fieldPresent`, `stringValue`, `titleFromNotePath`, which projection files depend on.
  - `build_assessment.go` defines `resolveNoteAssessment` and `matchSectionsForAssessment`, which projection uses.
  - `node_link.go` (1044 lines) defines `ValidBlockID` and `preferredIdentifierField`, used by `sections.go`, `projection_inline.go`, `projection_bindings.go`, and `projection_resolver.go`.
  - `node_workspace.go` defines web DTOs (`NodeWorkspace`, `NodeEvent` (used only by `pkg/app/web`), `NodeRange`, which duplicates `ByteRange`).
  - `index.go:71-96` declares `Walk*` types whose logic lives in other packages.
- **Problem:** The schema → projection → materialize layering exists conceptually but is inverted in code. The projection layer imports helpers from the 1700-line build file and the link service. Files over the 500-line policy limit: index.go 1732, sync.go 1522, node_link.go 1044, build_assessment.go 671, source_spans.go 591, node_catalog.go 554, schema_types.go 543, reference_view.go 516, viewconfig/validate.go 569, reference/link_rewrite.go 520.

  Representation count: 171 structs in the top-level package. For one "field on a node" there are `Field`, `FieldBinding`, `FieldAssessment`, `NodeFieldState`, `NodeFieldCapability`, `IndexedFieldCapability`, `FieldQueryCapability`, `FieldDoc`, and `codeanchor.IntelOntologyNodeFieldValue`. The DTO layer (`NodeFieldState`, `NodeInlineFieldSpan`, `NodeRange`) re-states projection types for JSON. The two capability structs differ only by residual ops plus the dead `Editable` field.
- **Recommendation:** See "Proposed package split". Concretely:
  - move `noteDoc` (renamed `sourceDoc`) and type resolution into `projection_source.go`;
  - move block-ID grammar and minting to a `blockid.go` (or into `sectionparse`);
  - move `ValidationIssue` to its own `issues.go`;
  - merge `IndexedFieldCapability` into `FieldQueryCapability`;
  - move workspace DTOs plus `NodeEvent` into a workspace package.
- **Effort:** L (in steps)
- **Dependencies:** ONT-1/3 first (they remove most of index.go and sync.go)

### ONT-10 Markdown lexing duplicated; `sections.go` is not the "thin converter" the note requires

- **Severity:** P2
- **Confidence:** High on duplication; medium on divergent behavior
- **Location:** `sections.go:214-398` re-implements `slugifyHeading`, `substringBytes`, `countLeadingSpaces`, `inlineIdentifierBlockID`, `proseBlockIDCandidate`, `stripInlineCodeSpans`, `firstBlockIDToken`, `isBlockIDBoundary`, and `validBlockID`, all of which also exist in `sectionparse/sections.go:196-349`. Fence detection is implemented separately in `sections.go:376`, `sectionparse`, `markdown_links.go:23-40`, `fallback_projection.go:357,400`, and `projection_inline.go:52,112`.
- **Problem:**
  - The subsystem note says "change `sectionparse/sections.go` … keep `sections.go` a thin converter". In practice block-ID grammar has two owners (`ValidBlockID` trims `^`; `sectionparse.validBlockID` does not).
  - `projection_inline.go` toggles fence state on *any* ``` or ~~~ line, while `sectionparse` tracks the opening delimiter. A ~~~ line inside a ``` block flips one and not the other, so inline fields inside code blocks can be projected by one path and ignored by section parsing.
  - None of the five fence detectors handle longer fences or indented fences consistently.
- **Recommendation:** `sectionparse` exports one `FenceState` scanner and one block-ID grammar (`ParseBlockIDToken`, `ValidBlockID`). Delete the copies in `sections.go`, `node_link.go`, `projection_inline.go`, `markdown_links.go`, and `fallback_projection.go`. Add one table test of fence edge cases shared by all callers.
- **Effort:** S–M
- **Dependencies:** none

### ONT-11 Schema documentation is spread over three places; `reference` mixes two unrelated concerns; `viewconfig` is owned by views

- **Severity:** P2
- **Confidence:** High
- **Location:**
  - `reference_view.go` (`SchemaDocs`, `TypeDoc`);
  - `guide/render.go` (`RenderMarkdown` → `…WithResolver` → `…WithLookups`, three wrappers);
  - `reference/render.go` (`RenderMarkdown`, `RenderJSON`) and `reference/compact.go`;
  - `reference/identifier_rewrite*.go`, `link_rewrite*.go`, `review_candidates.go`;
  - `viewconfig/*`.
- **Problem:**
  - `reference` contains (a) authoring-reference renderers used by `cmd` and `agentapi`, and (b) identifier/link rewrite planners used almost only by `pkg/validate/identifierreconcile` (14 files) and `pkg/validate` (5 files). Its most-used symbol, `IdentifierRewrite`, has 81 references, all from validate.
  - `viewconfig` is imported by `pkg/app/views` (22 files), web (5), and validate (2), and depends only on schema types.
  - Two separate Markdown renderers describe the same schema (`guide.RenderMarkdown` and `reference.RenderMarkdown`).
- **Recommendation:**
  - Create `ontology/schemadoc` = `SchemaDocs` + guide + reference render/compact (depends on schema only).
  - Move rewrite planners into `pkg/validate/identifierreconcile/rewrite` (or `ontology/rewrite` if edit sessions need them).
  - Move `viewconfig` to `pkg/app/views/config`.
  - Collapse the guide wrapper trio into one function with an options struct.
- **Effort:** M (mostly mechanical)
- **Dependencies:** views/validate owners

### ONT-12 Small cleanups (each low risk)

- **Severity:** P3
- **Confidence:** High unless noted
- **Items:**
  - **`Schema.Enums` is redundant.** It duplicates `EnumTypes`, and `EnumValuesSet` lazily writes into the shared, otherwise-immutable schema (schema.go:439-461). This is unreachable for `LoadSchema` output, but it is a latent concurrent-map-write and nil-map panic for hand-built schemas used across goroutines (`computeOntologyJobs` workers). Fix: delete `Enums` and derive sets at compile time on `EnumType`.
  - **`TypePolicy.Overrides` is speculative.** It is a slice, but `@traversal` is not `repeatable` (schema_sdl.go:56) and `CompileTypePolicies` emits exactly one (policy.go:73-79). The intent-fallback loop in `OverrideForIntent` handles cases that cannot occur. The subsystem note's "intent-specific overrides win over the empty-intent fallback" describes impossible input. Fix: single `*TraversalOverride` (medium confidence that gqlparser rejects duplicates).
  - **`projectNoteSource`'s `Format == ""` "compatibility" branch** (index.go:166-171) contradicts the architecture rule "No backward-compatibility shims". Fix it in test fixtures instead.
  - **`SyncPaths` and `SyncPublishedPaths` are exported but take an unexported interface (`deltaWriteQueue`).** Export it or take `*indexwriter.Writer` behind a narrow exported interface.
  - **`LoadSchema` recompiles SDL on every call** (38 production call sites; `EnsureFreshRuntime` calls it twice, runtime.go:104 plus index.go:260). Web (`pkg/app/web/ontology.go:45`) and code mode (`code_mode_runtime_reads.go:100`) each built their own `SchemaSourceHash`-keyed cache. Fix: one `ontology.SchemaCache` keyed by source hash, and let `SyncPublishedPaths` accept a compiled schema.
  - **`Service`** (service.go) is a dependency bag (`VaultDef`/`NoteReader` are unused by its methods and read by noderead), with an un-invalidated assessment cache. Today it is constructed per request, so this is safe, but the doc comment says "process-level". Fix: shrink to the two cached lookups or fold into noderead.

### ONT-13 Subsystem note drift

- **Severity:** P3 (documentation is normative here, so drift misleads reviewers)
- **Confidence:** High
- **Location:** `docs/reference/subsystems/ontology.md`
- **Problem:** Statements contradicted by code:
  - "Full and incremental builds share canonical source projection" is true only for source loading; assessment and validation are not shared (ONT-1).
  - "one resolver per operation" (ONT-3).
  - "Nothing downstream may hardcode type names" (ONT-2).
  - "Sync owns the indexed rows and writes through one durable lane" (ONT-5).
  - "keep `sections.go` a thin converter" (ONT-10).
  - "prove determinism with a derive test (`projection_derive_test.go` pattern)": that file never runs a projection twice or compares outputs.
- **Recommendation:** Update each line in the same change that fixes the code, and bump `last-verified`.
- **Effort:** S

## Findings — tests

Suite facts: 432 top-level tests, package elapsed 6.2 s, wall time 23 s (mostly compile). No failures or skips. Test speed is not a problem; coverage *shape* is.

### ONT-T-1 Add: differential convergence harness (highest priority)

- **Verdict:** add, then rewrite existing tests onto it
- **Why:** About 40 `index_test.go:TestBuildIndex_*` validation tests assert only against `BuildIndexFromNoteSources` (via `buildIndexFromCanonicalSourcesForTest`), which is not the production path. That is why ONT-1 shipped green. The only convergence test (`source_convergence_test.go:TestFullAndIncrementalOntologyBuildsConvergeAcrossRenameDelete`) uses a two-type fixture with no sections, embedded nodes, block IDs, global sources, or inverses.
- **What:** `assertBuildPathsConverge(t, fixture)`:
  1. writes the fixture;
  2. runs `BuildIndexWithStore`;
  3. runs `SyncPublishedPaths` full rebuild into a fresh store;
  4. touches each note in turn and syncs only that path;
  5. compares all six signature sets (`assertOntologyBuildMatchesStore` already exists) plus `ErrorJSON`.

  Feed it a rich fixture covering the spec-driven shapes (embedded stories with missing fields, `min`/`max` collections, malformed/duplicate block IDs, an inverse link, a global `@source`, body links). Then convert the `TestBuildIndex_*` fixtures into table rows that run through it. My probe file is a ready starting point.

### ONT-T-2 Add: repeated-build determinism

- **Verdict:** add
- **Why:** No test runs a build twice and compares bytes. ONT-8's leaks would show up in `AssessmentJSON`/`ErrorJSON`.
- **What:** Run `BuildIndexFromNoteSources` 20× on a fixture with ≥2 embedded collections that share a field name (both missing `status`), plus an inverse mismatch. Assert identical `mustJSON(result)` each time, and assert `ValidationIssues` is sorted by `sortValidationIssues`.

### ONT-T-3 Add: targeted regression tests for confirmed or likely bugs

- **Verdict:** add
- **What:**
  - (a) **Frozen-story ordering** (ONT-2): story added after the effort, spec-only sync. This currently fails; delete the test together with the feature if ONT-2 removes it.
  - (b) **Date/DateTime agreement** (ONT-6): for each value that passes `validateScalarValue`, assert the catalog row's `ValueKind` is the typed kind.
  - (c) **Type-resolution parity** (ONT-4): a `tag:`-matched type whose tag comes only from provider metadata, and a `find:` matcher whose provider title differs from the H1. Assert catalog `TypeName` equals the `ontology_note_types` row.
  - (d) **Scaling guard for the sync path:** an alloc-ratio test like `TestValidateEmbeddedProjectionFields_ResolverConstructedOnce`, but over `SyncPaths` for an untyped note with 500 vs 2000 `#action-item`s. It catches `globalSourceLinkTargets` (ONT-3).
  - (e) **Fence edge cases** shared by all lexers (ONT-10).

### ONT-T-4 Delete: `sync_queue_test.go:TestSyncPaths_QueueIsSoleFieldRowsPlannedRecorder`

- **Verdict:** delete (move the intent to `pkg/app/indexwriter`)
- **Why:** It asserts that a perf summary *does not contain* `field_rows_planned`. No code in `pkg/ontology` emits that counter (only `pkg/app/indexwriter/submit.go:182,204` does), so the test passes regardless of ontology behavior. The real contract ("count exactly once per submission") belongs in indexwriter's tests.

### ONT-T-5 Delete or rewrite: assertions of codes the package never emits

- **Verdict:** delete one, rewrite one
- **Tests:**
  - `index_test.go:TestBuildIndex_ManagedDocTemplateLinksUseFutureRepositoryRoot` → **delete**. Its only assertion is "no `broken_note_link`", a code ontology never produces; it is emitted by `pkg/validate/broken_links.go:22`. The scenario belongs in validate's broken-link tests.
  - `index_test.go:TestBuildIndex_LeavesBrokenLinksToBrokenLinksCheck` → **rewrite**. Keep the `requireNoteType` assertion. Replace the tautological `NotEqual "broken_note_link"` loop with an assertion on the link field's `RelationAssessment` (unresolved target recorded, no edge emitted).

### ONT-T-6 Move or delete: repository-content tests in the ontology package

- **Verdict:** move to `pkg/app/cli/init` (template owner) or delete
- **Tests:**
  - `docs_test.go:TestProcessDocsUseClosureChecklistContract` greps prose in `docs/specs/process/compounding-work.md` and a starter skill reference for forbidden words. It is a docs lint unrelated to ontology code → move to init template tests or delete.
  - `docs_test.go:TestSchemaDocs_SpecDrivenSchemaCarriesLayeredGuidance` asserts prose substrings ("Bounded execution artifact", "real human approver") of this repo's `.rhizome/ontology/spec-driven.graphql`. Any copy edit breaks it. `SchemaDocs` behavior is already covered synthetically by `TestSchemaDocs_IncludeGuidanceAndEnumValuePolicy`. → delete the substring checks; keep field-presence checks only if init owns them.
  - `spec_driven_schema_test.go` (3 tests) loads the repo's `.rhizome` copy (identical to the starter template). → move to init tests and load the template source of truth.

### ONT-T-7 Delete: duplicates of better tests elsewhere

- **Verdict:** delete
- **Tests:**
  - `field_capabilities_test.go:TestIndexedFieldCapabilityForField_PlansIndexedOperators` restates the switch tables in `field_capabilities.go`. Operator planning is behavior-tested in `pkg/ontology/pushdown/planner_test.go`. After ONT-6, test `ParseScalar`/`ScalarKind` instead.
  - `fallback_projection_policy_test.go:TestFallbackNoteTypeIsInternalByDefault` asserts constant classification. The real contract (fallback types hidden from public surfaces) is covered by `pkg/ontology/query/discovery_test.go:44`. If a core-level guard is wanted, assert `SchemaDocs(schema, "")` never lists `_Fallback*`.

### ONT-T-8 Merge: twin tests that exist only because there are two build paths

- **Verdict:** merge into one table-driven test per contract once ONT-1/ONT-5 land
- **Pairs** (all `index_test.go`):
  - `TestEnsureIndexedRejectsFutureMaterializationVersionWithoutMutation` / `TestSyncPublishedPathsRejectsFutureMaterializationVersionWithoutMutation`
  - `TestEnsureIndexedRebuildsWhenOntologyStateIsMaterializedIncompletely` / `TestSyncPathsRebuildsWhenOntologyStateIsMaterializedIncompletely`
  - `TestEnsureIndexedRefreshesStaleReadyNoteMetadataBeforeOntologyReuse` / `TestSyncPathsSchemaRebuildRefreshesStaleReadyNoteMetadata`
  - `TestEnsureIndexedRequiresExplicitNoteMetadataIndexerBeforeMutation` / `sync_queue_test.go:TestSyncPathsRequireExplicitNoteMetadataIndexerBeforeSchemaOrQueueMutation`
  - `TestSyncPaths_PersistsValidationIssuesInSchemaState` / `TestSyncPaths_PersistsSectionValidationIssuesInSchemaState`

### ONT-T-9 Rewrite: `index_test.go` structure

- **Verdict:** rewrite incrementally
- **Why:** At 3185 lines, it is the largest file. It writes 200 inline schemas across the package's tests and opens 26 SQLite stores by hand. Each `TestBuildIndex_*` is "write schema, write notes, build, grep issue codes". After ONT-T-1, convert these into fixture tables (`name, sdl, notes, wantIssues []issueKey`) run through the convergence harness. That cuts lines and gives every fixture three-path coverage for free.
- **Also:** `TestBuildIndex_TreatsLegacyHubPathAsDocumentationHub` is a path-glob case, so fold it into a selector table. `TestSyncPaths_PersistsFrozenStoryEdgesToEmbeddedNodes` goes with ONT-2.

### ONT-T-10 Rewrite (low priority): error-string coupling in schema tests

- **Verdict:** rewrite after introducing typed compile errors
- **Why:** `schema_test.go` has 57 `ErrorContains` assertions on compiler message text. These are user-facing diagnostics, so some text pinning is legitimate, but most tests only need to know *which rule* fired.
- **What:** Introduce `CompileError{Code, Type, Field, Pos}`. Assert codes, keeping one golden-message test per code family.

### ONT-T-11 Move: misfiled projection tests

- **Verdict:** move (cosmetic)
- **Why:** `projection_edit_test.go` mixes edit-session tests with pure projection tests (`TestProjectNode_EmbeddedNodeByFragment`, `TestProjectNode_EmbeddedNodeLinkFieldsUseInlineValues`, `TestProjectNote_CapturesFrontmatterAndSectionBindings`, `TestProjectNode_RejectsAmbiguousHeadingFragment`). Move these to a `projection_test.go` so the edit reviewer's file split is clean.

**Keep as is:** `embedded_validation_scaling_test.go` and `catalog_scaling_test.go` (cheap at ≤0.03 s and they guard a real regression; extend per ONT-T-3d), `line_index_test.go`, `source_spans_test.go`, `source_span_identity_test.go`, and `projection_document_test.go` (these pin the span/identity contracts the note relies on).

## Proposed package split

Evidence: I mapped every `ontology.X` reference outside the package to the file group that defines `X`.
- `guide` (156 of 161 refs), `viewconfig` (12/12), `idalloc` (32/32), `pushdown` (29/30), and `pkg/app/views` (146/189) use almost only schema symbols.
- `noderead`, `query`, `identifierreconcile`, and `search/semantic` mainly use projection symbols.
- Only `app/indexing`, `indexcore`, `cmd`, `validate`, and `app/cli` use the materialization layer.

The schema files already depend on almost nothing else. The exceptions are `noteDoc` in `matchers.go`, `type_specificity.go`, and `schema.go:322`; `lifecycle.go`; `containsString`; `IsSectionSummary`; and `type_instances.go`.

| Target package | Moves in | Depends on |
|---|---|---|
| `pkg/ontology/schema` | `schema*.go`, `type_match.go`, `type_specificity.go`, `type_profile.go`, `matchers.go` (evaluating a small `schema.Subject` interface {Path, Title, Tags, Frontmatter, Inline}), `policy.go`, `identifier_format.go`, `property_names.go`, `lifecycle.go`, `field_capabilities.go` + the scalar classifier (ONT-6), `issue_variant.go`, `ValidationIssue` | stdlib, gqlparser, `pkg/paths` |
| `pkg/ontology` (projection; keeps the path because `NodeRef`/`NodeProjection` are the most-imported types) | `projection*.go`, `root_*.go`, `fallback_projection.go`, `sections.go`, `section_summary.go`, `source_spans.go`, `body.go`, `frontmatter_ranges.go`, `line_index.go`, `markdown_links.go`, `sourceDoc` (ex-`noteDoc`) + `resolveNoteAssessment`, `assessment.go`, `scalar_field_assessment.go`, block-ID grammar/minting from `node_link.go`, `type_instances.go` | `schema`, `sectionparse`, `notemeta`, `noteformat` |
| `pkg/ontology/index` (materialize) | `index.go`, `sync.go`, `build_assessment.go` (field assessors), `node_catalog.go`, `node_edges.go`, `catalog_completeness.go`, `semantic_validation.go`, `fixes.go`, `materialization.go`, `runtime.go`, `service.go`, `inspect.go` | `ontology`, `schema`, `anchors/sqlite`, `notemeta` |
| `pkg/ontology/workspace` | `node_workspace.go`, `node_field_capability.go`, `NodeLinkService` (rest of `node_link.go`), `NodeEvent`; edit sessions can join here or become `ontology/edit` (edit reviewer's call) | `ontology`, `schema` |
| `pkg/ontology/schemadoc` | `reference_view.go` (`SchemaDocs`), `guide/*`, `reference/render.go`, `reference/compact.go` | `schema` |
| `pkg/validate/identifierreconcile/rewrite` (or `ontology/rewrite`) | `reference/identifier_rewrite*.go`, `link_rewrite*.go`, `review_candidates.go` | `ontology` |
| `pkg/app/views/config` | `viewconfig/*` | `schema` |
| `pkg/ontology/sectionparse` | unchanged, plus sole owner of fence scanning and block-ID grammar (ONT-10) | stdlib |

Dependency direction: `schema` ← `sectionparse`-using `ontology` (projection) ← `index`, `workspace`/`edit`. Separately, `schema` ← `schemadoc` and `views/config`. `noderead` and `query` depend on `ontology` + `schema` (+ `index` only for row-type helpers, which should move into `readmodel`).

Per architecture.md ("no compatibility shims"), do each move as one mechanical rewrite of importers (gopls rename / `gofmt -r`), not with type aliases.

## Suggested refactor sequence

Each step is independently shippable.

1. **Red tests.** Land ONT-T-1 (harness with rich fixture), ONT-T-2, and ONT-T-3a/b/c as expected-failure tests, or land them alongside step 2. Delete ONT-T-4, ONT-T-5, ONT-T-6, and ONT-T-7 at the same time (pure test hygiene).
2. **One per-note pipeline (ONT-1 + ONT-3 + ONT-4).**
   - Introduce `projectedNote` and `assessProjectedNote`.
   - Route `computeOntologyJob` and `buildIndexFromDocs` through it, with one sort comparator.
   - Pass the resolved type into the resolver.
   - Bump `OntologyMaterializationVersion` and update ontology.md.
3. **Remove the frozen-story hardcode (ONT-2),** after Drew decides whether plain-ID references need a schema directive. Bump the materialization version if this does not ship together with step 2.
4. **Error and determinism hygiene (ONT-7, ONT-8).** Small; can run in parallel with step 3.
5. **Single scalar semantics (ONT-6).** `schema.ScalarKind`/`ParseScalar`; delete the switches and dead capability fields; bump the materialization version.
6. **Single writer (ONT-5).**
   - `EnsureIndexed` delegates to the shared pipeline.
   - Delete exported `SyncPaths`, the nil-queue branches, and `sourcePublished`.
   - Move read surfaces to `PublishedRuntimeWithStore`, coordinated with the vault-runtime and validate owners.
   - Replace the whole-vault readiness scan.
   - Merge the ONT-T-8 twins.
7. **Lexing consolidation (ONT-10).** `sectionparse` owns fences and block IDs.
8. **Package split.** Do `schema` first (smallest blast radius, unblocks guide/viewconfig/idalloc/pushdown), then `index`, `workspace`, `schemadoc`, and the `reference`/`viewconfig` relocations (ONT-9, ONT-11). Convert `index_test.go` to fixture tables (ONT-T-9) as `index` moves.
9. **P3 cleanups (ONT-12) and note drift (ONT-13)** ride along with whichever step touches the file.

## Slow-test table

Single run: `go test -count=1 -json ./pkg/ontology/`. Package elapsed 6.20 s; sum of top-level test elapsed 5.75 s; wall 23 s (compile dominated). 432 top-level tests, 0 failures, 0 skips.

| Test | Seconds | Lane |
|---|---|---|
| TestEditSessionPackedInlineFieldsPreserveUntouchedSource | 0.69 | edit (other reviewer) |
| TestRecoverCommittedEditPreservesLaterSource | 0.48 | edit |
| TestRecoverCommittedEditPreflightsAllEvidence | 0.45 | edit |
| TestRecoverInterruptedEditsContextWaitsForWriterBeforeReplayingJournal | 0.18 | edit |
| TestRecoverEditRejectsReplacedArtifactsAndParents | 0.15 | edit |
| TestSyncPathsRequireExplicitNoteMetadataIndexerBeforeSchemaOrQueueMutation | 0.15 | core |
| TestCatalogCompletenessAcceptsIntentionalAbsence | 0.11 | core |
| TestEnsureIndexedRequiresExplicitNoteMetadataIndexerBeforeMutation | 0.11 | core |
| TestNodeLinkServiceApplyRecoversCommittedEditAfterLaterSourceChange | 0.08 | core/edit |
| TestPublishedRuntimeWithStore_PreservesSchemaStatus | 0.07 | core |
| TestValidateEmbeddedProjectionFields_DiagnosticsAcrossScales (scaling, 1–500 nodes) | 0.01 | core |
| TestValidateEmbeddedProjectionFields_ResolverConstructedOnce (scaling, 500 vs 2000 nodes) | 0.03 | core |

No test in this lane is expensive enough to cut on cost grounds. The `*_scaling_test.go` benchmarks do not run under `go test` without `-bench`.
