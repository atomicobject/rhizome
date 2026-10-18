---
summary: "Lane report from the 2026-10-17 subsystem quality review: Typed query subsystem review. Findings are a snapshot of commit 5251923f."
---

# Typed query subsystem review

Scope: `pkg/ontology/query` (~20k lines including tests), `pkg/ontology/pushdown`, `pkg/ontology/queryrecipe`, and their consumers in `pkg/app/views`, `pkg/app/web`, `pkg/app/mcp`, `pkg/app/cli`, and `cmd`. Read-only review of worktree `t3-e36f9b1a` at `5251923f`.

How claims were checked: by reading the code, grepping the callers, and running throwaway probe tests through `go test -overlay` (the repository was not modified). The probe files are in `/tmp/rhizome-subsystem-review/qry-probe/`. They use the package's own `newCustomQueryTestEnv`, which builds a real SQLite index, and they can be re-run with:
`go test -overlay /tmp/rhizome-subsystem-review/qry-probe/overlay.json -run 'TestProbe' -v ./pkg/ontology/query/`

## Summary

The subsystem works, but it has a real correctness defect: the stated invariant that SQLite pushdown agrees with in-memory evaluation does not hold.

**Correctness.** The value normalization the indexer writes is a copy of the logic that pushed operands and residual evaluation use, and the copies have drifted:
- The indexer strips wikilink brackets and everything after `|` from every String and enum value, not just link values.
- Pushed operands are only lowercased.
- Residual evaluation strips them again.

The probes show the results:
- An eq filter returns `[]` when pushed and a match when evaluated residually.
- `eq "ship"` matches both `"Ship | Docs"` and `"Ship | Tests"`.
- `contains "docs"` misses `"Ship | Docs"`.
- A YAML-native timestamp is excluded by the pushed filter and included by the residual one.
- DateTime values compare as text that keeps the authored offset, so `08:00+02:00` (06:00Z) counts as later than `07:00Z`. An existing parity test pins this wrong answer as expected.

**Performance.** Measured, not inferred:
- A plain typed root with 40 rows issues 41 type reads and 41 metadata reads, one per row.
- A missed identifier lookup (`node(ref: "SPEC-9999")`) reads the content of every typed note in the vault.
- Selecting a section `@neighbors` field rebuilds a vault-wide inbound-link index from all note contents on every request.

**Structure.**
- There is no query IR. Filters and sorts flow as `map[string]any` and are re-parsed at every layer.
- The same predicate semantics are implemented four times: SQL, `query/root_filters.go`, the noderead overlay, and `pkg/app/views/filters.go` plus `sort_text.go`.
- `execute.go` is 5,268 lines: 197 functions, mostly methods on one executor.
- The query layer reads through three channels: a 22-method raw `Store`, `ontology.Service`, and `noderead.Scope`.
- Saved recipes have no registry. Five consumers each re-implement load → select → compile → execute, and they disagree on duplicate IDs and on unrelated load errors.

**Tests.** The suite is fast (3.3 s) and mostly behavior-level. `root_scalar_parity_test.go` already contains a good differential seed comparing pushed, selector-residual, embedded, and overlay paths. Its fixture quotes every value and avoids pipes and wikilinks, so it cannot see the bugs above, and it asserts the textual-offset DateTime ordering as correct. The highest-value change is to generalize that harness, as described in QRY-T-1.

## Findings — production code

### QRY-1 Scalar normalization forks between the index, pushed operands, and residual evaluation

- **Severity:** P0. Queries return silently wrong results, and pushed and residual evaluation disagree, which violates the stated invariant.
- **Confidence:** High. Reproduced end to end.
- **Location:**
  - `pkg/ontology/node_catalog.go:310` and `:387` (`normalizeOntologyFieldValue`, the indexer)
  - `pkg/ontology/pushdown/planner.go:25,116` (`Planner.ValueNorm` is never set, so `FieldValueRow` defaults to `strings.ToLower`)
  - `pkg/ontology/pushdown/values.go:43`
  - `pkg/ontology/pushdown/scalars.go:137` (`NormalizeScalarText`)
  - `pkg/ontology/query/root_filters.go:288-322`
  - `pkg/app/views/sort_text.go:97` (`indexedSortText`, a third copy)
  - `pkg/app/views/filters.go:92` (views `contains`/`eq` use their own rules)
  - `normalizePropertyValue` is copied three times, with drift: `pkg/notemeta/index.go:559`, `pkg/ontology/query/execute.go:5076`, `pkg/app/cli/list.go:521`

**Problem.** The indexer writes `value_norm` by lowercasing, stripping a leading `[[` and trailing `]]`, and truncating at the first `|`. This applies to every non-typed scalar, including plain String, enum, and ID fields. The three consumers then disagree:
- The pushed operand is only lowercased. `Planner.ValueNorm` is dead: no caller sets it, and the doc comment claiming that GraphQL uses `normalizeRootFilterValue` is false.
- Residual evaluation re-applies the indexer's truncation.
- The views residual evaluator lowercases without truncation for `contains`.

So:
- An exact value containing `|` or `[[ ]]` never matches when pushed.
- A prefix before `|` matches many distinct values.
- `contains` and `neq` behave differently in GraphQL and in views.
- The notemeta property normalizer only unwraps when both brackets are present, while the ontology normalizer trims each side independently. As a result, `task(property: {...})` and `task(filters: [...])` normalize the same text differently.

**Evidence.** Probe `TestProbeReview`, run against Task notes t1 `summary: "Ship | Docs"`, t2 `summary: "Ship | Tests"`, and t3 `summary: "[[Plain]]"`:

| Query | Result | Correct? |
|---|---|---|
| `task(filters:[{field:"summary",op:eq,value:"Ship \| Docs"}])` (pushed) | `[]` | No, should return t1 |
| same filter with `path:"notes/tasks/t1.md"` (residual) | `[t1]` | Yes |
| same filter with `path:"notes/tasks/t2.md"` (residual) | `[t2]` | No, t2 is "Ship \| Tests" |
| `filters:[{field:"summary",op:eq,value:"[[Plain]]"}]` (pushed) | `[]` | No; the residual version returns t3 |
| `filters:[{field:"summary",op:eq,value:"ship"}]` (pushed) | `[t1, t2]` | No |
| `filters:[{field:"summary",op:contains,value:"docs"}]` | `[]` | No; the views `contains` matches |
| `filters:[{summary neq "Ship \| Tests"},{name eq "T1"}]` | `[]` | No, should return t1 |

**Recommendation.** Make one exported function in `pkg/ontology` the only producer of field-value rows: `ontology.FieldValueRow(field *Field, raw string) codeanchor.IntelOntologyNodeFieldValue`.
- Unwrap wikilinks and aliases only for `FieldKindLink` values, and only inside a balanced `[[...]]`.
- String, enum, and ID values get `lower(trim(text))`.
- Use this function for the indexer (`node_catalog.go`), the pushed operands (replacing `pushdown.FieldValueRow` and `Planner.ValueNorm`), and the residual and overlay extraction (`pushdown.StoredFieldValue`).
- Have views compare through `pushdown` instead of `indexedSortText` and `indexedScalarEquals`.
- Delete `normalizeOntologyFieldValue`, `NormalizeScalarText`, `normalizeRootFilterValue`, `indexedSortText`, and two of the three `normalizePropertyValue` copies. Notemeta keeps the canonical property normalizer, and the ontology package imports it or mirrors it under a test.
- Bump `OntologyMaterializationVersion` so existing indexes rebuild.

**Effort:** M. **Dependencies:** land QRY-T-1 first (red), then fix. A migration is needed because of the materialization version bump.

### QRY-2 YAML-native scalars are materialized differently by the indexer and by residual extraction

- **Severity:** P1. Same class as QRY-1, narrower trigger.
- **Confidence:** High for unquoted timestamps (reproduced). Medium for other YAML-native types such as ints, floats, and bools, which are not probed.
- **Location:** the indexer's flattening of frontmatter values on the path to `pkg/ontology/node_catalog.go:304-372`, versus `pkg/ontology/query/loaders.go:449` (`flattenValueStrings`) and noderead hydration.

**Problem.** For `due: 2026-01-01T00:00:00Z` (unquoted YAML) on a `DateTime` field, the three code paths see three different values:
- The index stores `value_text="2026-01-01"`, `value_kind="string"`, and `value_datetime=NULL`.
- The residual record sees `"2026-01-01T00:00:00Z"`.
- The selected `due` field resolves to `null`.

So `task(filters:[{field:"due",op:gt,value:"2025-12-31T00:00:00Z"}])` excludes t3 when pushed, while the same filter with `path:` includes it.

**Evidence.** Probes `TestProbeT3` (stored versus residual values) and `TestProbeReview` (the last four probe queries).

**Recommendation.** This falls out of QRY-1 if the single `FieldValueRow` also owns raw-to-text conversion for decoded YAML values (`time.Time`, numbers, bools). Expose it as `ontology.FieldTextValues(field, raw any) []string` and call it from the indexer, from noderead hydration, and from `query`. Delete the `flattenValueStrings` temporal special-case, which today guesses that a midnight timestamp is a date.

**Effort:** S on top of QRY-1. **Dependencies:** QRY-1.

### QRY-3 DateTime filters and sorts compare authored-offset text, not instants

- **Severity:** P1. Results are wrong but consistent between the engines.
- **Confidence:** High. Reproduced, and also asserted by an existing test.
- **Location:**
  - `pkg/ontology/node_catalog.go:367-372`: `value_datetime = t.Format(RFC3339)` keeps the authored offset.
  - `pkg/ontology/pushdown/values.go:70-75`: same for operands.
  - SQL compares the `value_datetime` text: `pkg/anchors/sqlite/store.go:8040`.
  - `pkg/app/views/sort_text.go:108`, whose comment documents the behavior as intended.

**Problem.** `2026-01-01T10:00:00+02:00` is 08:00Z, yet `gt "2026-01-01T08:30:00Z"` matches it. The reverse also happens: whenever authors mix offsets, a later instant can sort before an earlier one. The subsystem note calls this deliberate ("compare the canonical indexed text, including authored datetime offsets"), but it is not a meaningful ordering for a type named `DateTime`.

**Evidence.**
- Probe: the pushed query `due gt 08:30Z` returns t1 (`10:00+02:00`).
- `root_scalar_parity_test.go` asserts `{"at", gt "2026-06-01T07:00:00Z"} → ["a"]` where `a = 08:00+02:00 = 06:00Z`.

**Recommendation.** Store `value_datetime` as UTC (`t.UTC().Format(time.RFC3339Nano)`, fixed width) and keep the authored text in `value_text` for display. Normalize operands to UTC the same way. This is a product decision. Surface it as a correction to the subsystem note rather than changing it silently; the change rides the same materialization bump as QRY-1.

**Effort:** S. **Dependencies:** QRY-1 (same rebuild), QRY-T-2.

### QRY-4 Typed root lists hydrate one row at a time (N+1 against the store)

- **Severity:** P1. Performance on the most common query shape.
- **Confidence:** High. Measured.
- **Location:**
  - `pkg/ontology/query/execute.go:1709` (`noteRecordsFromCatalogRows`): calls `e.rootNote(ctx, notePath)` per row.
  - `rootNote` (`:407`) → `resolvedType` → `loaders.ontologyTypesByPaths([1 path])` → `Scope.TypesByPaths`, then `loaders.noteByPath` → `recordsForPaths([1 path])` → `Scope.Hydrate([1 ref])` → `recordsFromMetadata([1 path])`.
  - The same per-record pattern appears in `resolveNodeBatchSelectionSet` (`:316`, `nodes(refs:)` with up to 200 `rootNode` calls) and `resolveNoteIdentifierRefs` (`:660`).
  - When a type row is missing, `resolvedType` (`:4096`) falls through to `Deps.Service.ResolvedType`, which is an uncached `GetOntologyTypeByPath` per call.

**Problem.** `task(first: 40) { path name }` makes 41 `OntologyTypesByPaths` calls and 41 `CurrentNoteMetadataRowsByPaths` calls. With any residual filter or sort the candidate read widens to 5,000 rows, so up to roughly 10,000 single-row SQLite round trips plus the hydration reads. The batching invariant ("Per-parent field resolution must hit cache, not requery") is only enforced for relation fields; the root list itself is unbatched.

**Evidence.** Probe output: `N+1: rows=40 typeCalls=41 metaByPathCalls=41`.

**Recommendation.**
- Collect the paths first, then call `e.loaders.recordsForPaths(ctx, paths)` and `ontologyTypesByPaths(ctx, paths)` once each, and preserve the order. This is about a 15-line change.
- Better: route every typed root, not only the overlay case, through `Scope.TypeInstances`, which already supports `Offset`, predicates, sort, and the overlay merge. Then batch-hydrate the result. That deletes the dual path in `indexedRootTypedNotes`/`indexedRootEmbeddedNodes` (direct `Store.OntologyNodesByTypePlan` without an overlay, `Scope.TypeInstances` with one).
- Batch `nodes(refs:)` through one `Scope.Resolve` call with N targets.

**Effort:** S for the batching fix, M for unifying on TypeInstances. **Dependencies:** none. Add a batching contract test (QRY-T-3).

### QRY-5 A missed identifier triggers a vault-wide scan that hydrates and projects every typed note

- **Severity:** P1. Performance, and it violates the "roots must be bounded" constraint.
- **Confidence:** High. Measured.
- **Location:**
  - `pkg/ontology/query/resolve_targets.go:31,39,69` → `execute.go:598` (`resolvePreferredIdentifierTarget`)
  - `:645` (`resolveNoteIdentifierRefs`): when the property index returns no candidates, it falls back to `OntologyPathsByType(type, 100000)` plus `rootNote` per path.
  - `:674` (`resolveSectionIdentifierRefs`): `allTypedNotePaths` (100k per type), then `rootNote` and `sectionRecordsForNote` (a projection that reads content) per note.

**Problem.** Any `node`, `note`, or `resolve` ref that does not resolve by path or alias falls into these scans. That includes typos and IDs that do not exist yet. This happens whenever the schema has a preferred identifier on any embedded type, which the agentic-engineering starter schema does. The cost scales with the number of typed notes and reads every note's content, including note types that cannot contain the embedded type.

**Evidence.**
- Probe with 30 TechnicalSpec plus 43 Task notes: `node(ref:"SPEC-9999")` → `typeCalls=74 metaByPath=147 pathsByType=4 fileReads=73`.
- The design note only forbids these scans after a path or alias hit, so the miss path is unguarded.

**Recommendation.** Identifier fields are already materialized in `ontology_node_field_values` for both note and embedded nodes, with an index on `(type_name, field_name, value_norm, node_id)`. Replace both scans with one store query:
`OntologyNodesByFieldValue(ctx, typeNames []string, fieldName string, valueNorm string)`
or reuse `OntologyNodesByTypePlan` with an `eq` predicate per preferred-identifier type. Then delete `allTypedNotePaths`, the `100000` fallbacks, and the per-note section projection.

**Effort:** M. **Dependencies:** QRY-1, so that `value_norm` is trustworthy for IDs.

### QRY-6 Section `@neighbors` rebuilds a vault-wide inbound-link index per request

- **Severity:** P1. Performance.
- **Confidence:** High from reading; not measured.
- **Location:**
  - `pkg/ontology/query/execute.go:4735` (`inboundSectionNeighborIndex`)
  - `loaders.snapshot` (`loaders.go:63`)
  - `provider_section_neighbors.go:16`

**Problem.** The first section-neighbor field in a request does all of the following:
- Loads every Markdown note's full content through `NoteEntriesSnapshot`, or through `GetContents` per note.
- Re-scans every body for links.
- Reads the full metadata inventory again.
- Calls `OntologyEdgesForPaths(allPaths, false, "", 0)` with no limit.

The link scan duplicates work the indexer already did into edges. CONTEXT.md acknowledges that section neighbors are "GraphQL-local", but the cost is unbounded per request. `sync.Once` also captures the first caller's `ctx`, so a cancellation becomes the cached error for the rest of the execution.

**Recommendation.** Persist section-target link facts at index time: an inbound edge keyed by `(dst_path, dst_node_id | heading slug | block id)`. Serve this field through `Scope.Neighborhood` on embedded endpoints, which noderead already owns. Until then, at minimum bound the snapshot to notes whose edges target the requested note paths, using `OntologyAmbientEdgesByTargets`.

**Effort:** M to L, because the indexer change belongs to another subsystem. **Dependencies:** the noderead and indexing owners.

### QRY-7 No predicate IR; four implementations of filter and sort semantics

- **Severity:** P1. A design problem, and the root cause of QRY-1 recurring.
- **Confidence:** High.
- **Location:**
  - SQL: `pkg/anchors/sqlite/store.go:7945-8080`
  - GraphQL residual: `pkg/ontology/query/root_filters.go`. `rootFilterMatches` operates on `map[string]any` and raw strings. `sortRootRecords` is at `:160`.
  - noderead overlay: `pkg/ontology/noderead/type_instances.go:430-500` (filter) and `:585-625` (sort). It operates on built `OntologyFieldPredicate` and rows. The sort comparator and key preparation duplicate `sortRootRecords`.
  - Views residual: `pkg/app/views/filters.go:92-230` and `sort_text.go`, with their own normalizer and a different tie-break (`nodeRefKey` string versus SQL `(note_path, start_byte, node_id)`).

**Problem.**
- The planner emits a typed predicate only for pushable filters. Everything residual stays an untyped `map[string]any`, carried through `args` together with magic keys (`"__firstPresent"`, `"__selectorCandidateLimit"`) and re-parsed with `stringValue(filter["field"])` at each layer.
- `indexedOntologyNodeRootPlan` returns `(plan, any, any, []RuntimeWarning)`.
- Coverage state flows through the mutable `e.rootCandidateBound` field.
- The subsystem note claims that query residual and overlay share `pushdown/scalars.go`. They share helper functions, but each implements its own matcher loop, and views share nothing.

**Recommendation.** Introduce one IR in `pushdown` and make everything consume it.

```go
type Plan struct {
    Pushed   codeanchor.OntologyNodeQueryPlan // predicates + sort SQL can serve
    Residual []Predicate                      // same predicate type, incl. neq/contains
    Sort     []codeanchor.OntologyFieldSort   // full requested sort when any key is residual
    Warnings []Warning
}
func (p Planner) Plan(ctx, filters []FilterInput, sorts []SortInput) Plan // replaces CanPushFilter/BuildPredicate/PlanSort/CanonicalKey
func Match(pred Predicate, rows []codeanchor.IntelOntologyNodeFieldValue) bool
func SortItems[T any](items []T, sorts []codeanchor.OntologyFieldSort, rows func(T, string) []codeanchor.IntelOntologyNodeFieldValue, stable func(T) StableKey)
```

Residual records produce rows through the single `ontology.FieldValueRow` from QRY-1, so `Match` compares the same column representation SQL does. With this in place:
- Delete `rootFilterMatches`, `rootFilterAny*`, `sortRootRecords`, and the overlay matcher loop and comparator.
- Rewire views (`rowMatchesFilter`/`sortRows` for indexed fields) to `pushdown.Match`/`SortItems`.
- Pass `Plan` as a typed struct instead of `any`.
- Make the typed-root page a value (`rootPage{records, candidateBound, hasMore}`) instead of executor fields.

**Effort:** L. **Dependencies:** QRY-1, plus QRY-T-1 as the safety net.

### QRY-8 The planner API splits decide from build; consumers drop predicates or re-plan

- **Severity:** P2.
- **Confidence:** High for the code paths; medium for the user-visible views impact, which was not run.
- **Location:**
  - `pkg/ontology/pushdown/planner.go:30-112`
  - `pkg/app/views/source_ontology.go:402-445` (plan), `:575-600` (build), `:628` (`interfaceTypeInstancesPushdown`), `:531` (`indexedPlanWarnings`), `:62-98` (sentinel values)

**Problems.**

1. **Pushed filters can be silently dropped.** `CanPushFilter` (used by views to classify) can return true while `BuildPredicate` returns false. This happens for an empty eq value, an `in` whose values are all blank, or a link value that resolves to nothing usable. Views then drop the filter entirely, since it is neither pushed nor residual. `bindRuntimeFilterValues` writes sentinel strings such as `"__rhizome_current_user_missing__"` specifically to avoid this. Request-time `req.Filters` are not run through `viewconfig.validateFilters`. GraphQL avoids the problem because it decides with `BuildPredicate` alone and residualizes on false.
2. **The interface collapsed path uses only the first implementor's plan.** `interfaceTypeInstancesPushdown` returns inside the first loop iteration, so typing and link-target resolution come from the first implementor's field. If two implementors declare the same field name with different scalar types or link targets, the SQL predicate binds the wrong typed column for the others.
3. **Every indexed view request re-enters GraphQL to plan.** `indexedPlanWarnings` builds a GraphQL string, runs `PrepareWithVariables` and `ExecutePrepared` (a new loader set and a new noderead Scope), re-plans with a fresh `CatalogLinkResolver` (up to 5,001 candidate rows read again), probes the store with `Limit:1`, and parses the warnings back out of `map[string]any`. The link warnings already appended by `typeInstancesPushdown` are appended a second time, and nothing dedupes them. The comment "Cheap because the queryPlan root is cached" is false; nothing is cached.

**Recommendation.**
- Use the single `Planner.Plan` from QRY-7, which returns pushed, residual, and warnings together.
- Interface views call `Plan` per implementor and send per-type predicates.
- Views get "index probe" warnings from one direct `scope`/`Store` call, not via GraphQL.
- Delete `indexedPlanWarnings`, `viewFilterVariablesForType`/`viewSortVariablesForType` (if they have no other use), `CanPushFilter`, `CanPushSort`, `CanonicalKey`, `Planner.ValueNorm`, and the sentinel strings.

**Effort:** M. **Dependencies:** QRY-7. Coordinate with the views reviewer.

### QRY-9 The query layer reads through three channels and bypasses the noderead seam

- **Severity:** P2.
- **Confidence:** High.
- **Location:**
  - `pkg/ontology/query/types.go:283-305` (a 22-method `Store`), `:477-492` (`Deps`)
  - Direct store calls: 22 sites, for example `execute.go:654,753,1109,1251`, `note_path_cache.go:17,79`, `selector_inventory.go:27`, `provider_section_neighbors.go:20`, `loaders.go:72`
  - `Deps.Service` is used only for `Assessment`/`ResolvedType` (`execute.go:4106,4121`)
  - Executor-level caches duplicate the Scope's (`assessmentByPath`, `sectionsByPath`, `loaders.typeByPath`, `recordByPath`)
  - `pushdown/link_targets.go:11` imports `pkg/anchors/sqlite`

**Problem.**
- The design note says this layer "compiles selections into a `noderead.NodeReadPlan` rather than reading SQLite/files itself". In practice typed roots, identifier scans, the selector inventory, note-path caches, and section neighbors go straight to the store or to files.
- Five sites read the whole-vault `CurrentNoteMetadataRows`, each memoized differently or not at all.
- `executor` carries five mutexes and three `sync.Once`, but execution is single-threaded: there are no goroutines in the package. The loaders comment about "resolver goroutines" is stale.
- `pushdown` and `query` are typed against `semdb` rows instead of the storage-neutral `readmodel` DTOs.

**Recommendation.**
- Delete `Deps.Service`, `executor.assessmentByPath`/`assessmentKnown`/`assessmentMu`, and the `resolvedType` fallbacks; use `Scope.TypesByPaths`/`AssessmentsByPaths`.
- Give the Scope one memoized `MetadataInventory(ctx)` and have the selector inventory, `currentNotePaths`, and provider section neighbors call it.
- Shrink `query.Store` to the methods the Scope does not expose: the validation snapshot reads and `IntelAnchorsByPath` for code roots. Move everything else behind the Scope.
- Drop the mutexes, or document why concurrency is expected.
- Move `CatalogLinkTargetStore` to `readmodel` types.

**Effort:** M. **Dependencies:** QRY-4 and QRY-5 remove most of the direct calls.

### QRY-10 One 5,268-line executor and a 500-line SDL string builder

- **Severity:** P2. Maintainability. The architecture policy says "under about 500 lines".
- **Confidence:** High.
- **Location:**
  - `pkg/ontology/query/execute.go`: 197 functions, all on one `executor` type.
  - `schema.go:34-537`: `buildSDL` is a single function of about 500 lines.
  - `workspace*.go`: about 2,230 lines of browser view-model (`Node.workspace`) inside the query package.
  - Test files: `query_test.go` is 6,963 lines.

**Problem.** Root dispatch, identifier resolution, typed-root planning, section projection, neighbor indexing, code roots, search, and record facts all share mutable executor state. Adding a root means editing a 100-line `switch` (`resolveRootField`) and threading `map[string]any` arguments.

**Recommendation.** Do this after QRY-7 introduces typed plans. Split by root family into files, or into subpackages if a resolver interface emerges naturally:
- `roots_typed.go` (typed and embedded roots plus paging)
- `roots_identity.go` (`node`/`nodes`/`note`/`resolve` plus identifiers)
- `sections.go`
- `neighbors.go`
- `roots_code.go`
- `search.go`

Split `buildSDL` into one function per emitted block: roots, `NoteNode`/`Node`, per-type, interfaces, and record facts. Move `workspace*` into `pkg/ontology/query/workspace` behind a small interface (`ResolveWorkspace(ctx, scope, ref, selection) map[string]any`). Do not add a generic resolver registry or a plugin system; file moves plus typed arguments are enough.

**Effort:** L, mechanical but wide. **Dependencies:** QRY-7.

### QRY-11 Dead and vestigial code

- **Severity:** P2.
- **Confidence:** High.
- **Location and evidence:**
  - `execute.go:1251-1298`: the vault-wide fallback in `rootEmbeddedNodes` is unreachable. Its only caller (`:280`) requires `Role == TypeRoleEmbeddedNode`, and `indexedRootEmbeddedNodes` returns `ok=false` only when the role is not `EmbeddedNode`. The `deps.Store == nil` check at `:1244` is also unreachable, because `Execute` rejects a nil store. The dead fallback would also have ignored `offset`.
  - `pushdown/planner.go:25`: `Planner.ValueNorm` is never set by any caller.
  - `query.Execute` is exported, but every production caller uses `ExecutePrepared`. Unexport it, or make `Execute` the introspection-aware one.
  - Reported by `unparam`:
    - `rootNotesWithMax`: `path` is unused.
    - `boundedInt`: `minValue` is always 1.
    - `typeInterfaces`: `schema` is unused.
    - `stringListRecipeVariableValue`: always returns a nil error.
  - `loaders.snapshot` (`loaders.go:63`) has a `GetNotesList` plus `GetContents`-per-note fallback for readers without `NoteEntriesProvider`. Verify whether any production reader lacks it; if none does, delete the fallback.

**Recommendation.** Delete these. Run `golangci-lint run --enable-only unused,unparam` in CI for these packages.

**Effort:** S. **Dependencies:** none.

### QRY-12 Saved recipes have no registry; five consumers re-implement run and disagree

- **Severity:** P2. Consumer contract divergence plus avoidable per-request work.
- **Confidence:** High.
- **Location:**
  - `pkg/app/cli/query_recipes.go:82-138`
  - `pkg/app/mcp/tool_ontology.go:75-130,200-228`
  - `pkg/app/web/public_api.go:136-145,558-590`
  - `pkg/app/views/source_query_recipe.go:15-70`
  - `cmd/query_recipe.go:231`
  - `pkg/app/cli/code_mode_runtime_reads.go:80`

**Problem.** Every consumer repeats `LoadDefaultSources` → find by ID → `MergeAnchorInput` → `Compile` → execute → assemble `RunResult`, and they disagree on the edge cases:

| Consumer | Duplicate recipe ID | Unrelated broken recipe file |
|---|---|---|
| Views | Rejected | Ignored |
| Public API | Silently uses the first match | Fails every run with HTTP 400 |
| CLI and MCP `run` | Silently uses the first match | Fails every run |

There are also wasted repeats:
- `selectRecipe` and `selectQueryRecipeMCP` are byte-identical.
- CLI and MCP compile (which prepares) and then re-prepare from `compiled.Query`.
- MCP loads and builds the schema twice per run.
- A recipe-backed view walks all four roots, including every Markdown file under `.agents/skills` and `docs/rhizome-md-templates`, and runs `Validate` (compiling every recipe) on every request, just to filter issues for one ID.

**Evidence.** Measured on this repo (18 recipes): `LoadDefaultSources` about 6.1 ms and `Validate(all)` about 1.5 ms per request. The cost grows with the size of the skills tree. Prepare is about 6 µs, so the cost lies in discovery, not in GraphQL parsing.

**Recommendation.** Add one function in `queryrecipe`:

```go
func Run(ctx context.Context, recipes []Recipe, loadIssues []Issue, id string, inputs map[string]string, anchors []string,
         exec *ontologyquery.ExecutableSchema, execute func(*ontologyquery.PreparedQuery) ontologyquery.Result) (RunResult, []Issue, error)
```

It encodes one rule: duplicate ID is an error, and only issues for the target ID block the run. It executes `compiled.Prepared` directly. All five consumers call it, and `selectRecipe`/`selectQueryRecipeMCP` are deleted. Cache discovered recipes in long-lived processes (web, MCP) keyed on the roots' mtimes, or reload on watcher events. Do not cache prepared queries; they are cheap.

**Effort:** M. **Dependencies:** none.

### QRY-13 `Deps` is hand-assembled by five consumers with different provider sets

- **Severity:** P3.
- **Confidence:** Medium. Some differences may be intentional.
- **Location:** `pkg/app/web/ontology.go:106`, `pkg/app/mcp/tool_ontology.go:170`, `cmd/ontology_query_runtime.go:216`, `cmd/view.go:267`, `cmd/serve_agent_op_reads.go:43`.

**Problem.** The same GraphQL query degrades differently by surface:
- Only the CLI wires `OntologyRuntime`/`CodeRuntime`, so `ontology { nextId }` and `code` roots return "unavailable" warnings in web and MCP.
- Only web sets `PathOwner` and the `Exact*` loaders.
- MCP errors when embeddings are absent for `semantic`, while other surfaces degrade.
- `Execute(ctx, deps, schema, prepared)` and `ExecutePrepared(..., schema, execSchema, prepared)` let a caller pair a schema and an executable schema from different hashes.

**Recommendation.** Have one constructor in the app layer that takes the live runtime and returns a `query.Engine{schema, exec, deps}` with `Run(ctx, prepared)`. Surfaces that intentionally omit providers say so in one place.

**Effort:** S to M. **Dependencies:** QRY-12.

### QRY-14 Recipe loader duplication and silent skips

- **Severity:** P3.
- **Confidence:** High.
- **Location:** `pkg/ontology/queryrecipe/load.go:41-135,201-206`.

**Problem.**
- `LoadRoots` and `LoadPath` contain two near-identical `WalkDir` bodies.
- `yamlFileLooksLikeRecipe` reads each candidate YAML file, and `loadFile` reads it again.
- A YAML recipe under `.agents/skills` that still declares the old `apiVersion: rhizome.ontology-query-recipe.v1` is skipped silently, because the sniff only matches the new version string. The `types.go` comment promises that stale recipes "surface explicitly".

**Recommendation.** Make `LoadRoots` call `LoadPath` per root with a shared seen-set. Have the sniff match either API version, so the old one reaches `validateMetadata` and is reported. Read each file once.

**Effort:** S. **Dependencies:** none.

### QRY-15 Documentation drift in the normative notes

- **Severity:** P3.
- **Confidence:** High.
- **Location:**
  - `docs/reference/subsystems/graphql-query.md`: lines 44 and 49 are two "Not a second runtime" bullets, and line 49 lists an `agent` runtime root.
  - `pkg/ontology/query/CONTEXT.md` ("Runtime roots: `ontology`, `code`, and `agent`").
  - `runtime_schema.go:15` defines only `ontology` and `code`.
  - Line 59 says residual and overlay share `scalars.go`; views do not (QRY-7).
  - `pushdown/values.go:36-41`: the doc comment says GraphQL uses `normalizeRootFilterValue` for operands. It does not.
  - `views/source_ontology.go:305`: the comment says "cached". It is not.

**Recommendation.** Fix these alongside QRY-1 and QRY-7, and bump `last-verified`.

**Effort:** S.

## Findings — tests

### QRY-T-1 Add a generalized differential test (the most valuable addition)

**Verdict:** Add, by extending `root_scalar_parity_test.go`.

**Why.** The harness already runs each case through four paths and compares them: the pushed typed root, `find:` and `property:` residual selectors, the embedded root, and two read overlays (`executeScalarParityRows`). It misses QRY-1, QRY-2, and QRY-3 because of how the fixture is built:
- Every value is written with `%q`, so YAML-native types never occur.
- No values contain `|`, `[[`, `]]`, mixed case, or Unicode.
- Only scalar fields are covered, not enum or link fields.
- `contains`, `neq`, and offset paging are not compared.
- Views' residual evaluator is not in the comparison at all.

**Replacement.** Keep the engine runner and widen the inputs into a table of field kind × authored spelling × op.
- **Authored spellings:** quoted, unquoted YAML (`2026-01-01T00:00:00Z`, `7`, `true`, `1.50`), `"A | B"`, `"[[X]]"`, `"[[x|Alias]]"`, `"MiXeD"`, Unicode `"Ärger"`, blank, missing, and lists.
- **Ops:** eq, in, neq, contains, gt/gte/lt/lte, exists.
- **Engines:**
  1. pushed typed root
  2. `path:` residual (cheapest forced residual)
  3. overlay merge
  4. `noderead.Scope.TypeInstances` with the views planner's predicates
  5. `views.rowMatchesFilter`/`sortRows` over hydrated rows, through a small adapter in the views package test, or after QRY-7 through `pushdown.Match`
- **Comparisons:** compare the result sets for filters and full order for sorts.
- **Paging:** for each sort, assert that the concatenation of pages fetched with `offset` equals one large page. This checks pagination stability across ties.

Expected values should state semantics: "matches t1 only", not "whatever pushdown returns". Then the cases from QRY-1, QRY-2, and QRY-3 start red. The probe file `/tmp/rhizome-subsystem-review/qry-probe/probe_test.go` has the concrete fixtures.

### QRY-T-2 Rewrite the parity assertions that pin the DateTime offset bug

**Verdict:** Rewrite.

**Tests:**
- `pkg/ontology/query/root_scalar_parity_test.go:TestExecute_TypedScalarFilterParity`, case `{"at", "op: gt, value: \"2026-06-01T07:00:00Z\"", ["a"]}`. Entry `a` is `08:00+02:00`, which is 06:00Z.
- `TestExecute_TypedScalarSortParity`, case `{"at", ["c","b","d","e","a",...]}`. By instant, `a` ties with `c`.

**Why.** These assert that a wrong temporal answer is correct, so they would fail a correct fix.

**Replacement.** Use expected values by instant: `gt 07:00Z → []`, and ascending order `a,c` (tie broken by path) followed by `b,d,e`. Land this together with QRY-3.

### QRY-T-3 Add batching and boundedness contract tests for the unguarded paths

**Verdict:** Add.

**Tests to add,** reusing the existing spies (`spyQueryStore`, `identifierScanCountingStore`, `countingNoteReader` in `query_test.go`):
1. `TestTypedRootHydratesCandidatesInOneBatch`: `task(first: 40)` must make no more than 2 `OntologyTypesByPaths` and 2 `CurrentNoteMetadataRowsByPaths` calls. The current count is 41 of each. Repeat with a residual filter so 5,000-candidate reads are covered.
2. `TestNodesRefsResolvesInOneBatch`: `nodes(refs: [...50])` must make a bounded number of resolve reads.
3. `TestUnknownIdentifierDoesNotScanVault`: `node(ref: "SPEC-9999")` must make zero `GetContents` calls and no `OntologyPathsByType(…, 100000)` calls. The current figures are 73 and 4.
4. `TestSectionNeighborsDoNotReadUnrelatedNotes`: selecting a section `@neighbors` field must not read the content of notes without edges to the target.

**Why.** The existing `TestExecute_CachesNoteRecordsWithinRequest`, `TestExecute_BatchesRelationFieldsAcrossNodeLists`, and `TestExecute_ResolveRefKeepsNoteAliasPrecedenceOverUnrelatedEmbeddedIdentifier` guard relation fields and alias hits only. The root list, the miss path, and section neighbors are unguarded, and those are where the measured N+1 and scans are.

### QRY-T-4 Replace SDL substring assertions with AST assertions

**Verdict:** Rewrite.

**Tests** (count of `require.Contains(t, ...SDL, "...")` assertions):
- `query_test.go:TestBuildExecutableSchema_IncludesBuiltinsAndNeighborFields` (18)
- `TestBuildExecutableSchema_AuthoredNoteInterfaceFieldsAreUniversalOnNoteNode` (6)
- `TestBuildExecutableSchema_IncludesBuiltinsForEmbeddedSectionNodes` (5)
- `TestBuildExecutableSchema_AuthoredNoteInterfaceSkipsRuntimeFieldDuplicates` (5)
- `TestBuildExecutableSchema_EmitsEnumAndEnumValueDescriptions` (4)
- `TestBuildExecutableSchemaEmitsRelationCountsAndPreservesAuthoredCollision` (4)
- Smaller counts in `reverse_relationships_test.go`, `overlay_scalar_selection_test.go`, and `TestRelationListFieldsTakeFirst`

**Why.** The assertions are on exact SDL text, including argument order and default formatting. For example, `"project(path: String, find: String, property: PropertyFilterInput, semantic: [String!], first: Int = 20, offset: Int = 0, filters: [FieldFilterInput!], sort: [SortInput!]): [Project!]!"` breaks if one argument is added or reordered without any contract change. They are also unsafe for the splitting proposed in QRY-10.

**Replacement.** Assert on the already-parsed schema, for example:
- `exec.Schema.Query.Fields.ForName("project").Arguments.ForName("offset").DefaultValue.Raw == "0"`
- `exec.Schema.Types["Project"].Interfaces` contains `"NoteNode"`

Add a 10-line helper `requireField(t, exec, "Type", "field", "Type!")`.

### QRY-T-5 Move the relation-cache tests to noderead

**Verdict:** Delete from `query`; move to `noderead/scope_test.go` if they are not already covered there.

**Tests:** `pkg/ontology/query/relation_cache_test.go:TestRelationLoadersShareConcurrentReadsAndEmptyGroups`, `:TestRelationLoadersKeepFiltersAndLimitsIndependent`, `:TestRelationLoadersRetryCanceledReads`.

**Why.** `loaders.structuralEdges`/`ambientEdges` are one-line pass-throughs to `Scope.Traverse`. These tests exercise noderead's cache behavior through query's private wrappers, including 16-goroutine concurrency that the single-threaded executor never produces.

**Replacement.** Equivalent `Scope.Traverse` tests in noderead, which owns the invariant. Only `TestScopeTraverseLimitPerSourceDoesNotStarveLaterSources` exists there today.

### QRY-T-6 Merge the overlapping selector-inventory tests

**Verdict:** Merge or delete.

**Tests in `pkg/ontology/query/selector_inventory_test.go`:**
- Merge `TestSelectorInventoryKeepsOneSuccessfulSnapshotWithinExecution` into `TestSelectorInventorySharesSuccessfulReadAndRefreshesNextExecution`, which already asserts one read per execution and a fresh read on the next. `TestB14SelectorRootInstrumentation` also asserts `metadataCalls == 1` across three roots.
- Delete `TestSelectorInventoryHonorsCancellationAfterSuccessfulRead`. It calls the private `loaders.selectorMetadataRows` directly.

**Replacement.** One `Execute` with an already-canceled context asserting `context.Canceled` in the errors. After QRY-9 moves the inventory into the Scope, the remaining tests move with it.

### QRY-T-7 Replace hard-coded repo-content inventories in the recipe tests

**Verdict:** Rewrite.

**Tests:** `pkg/ontology/queryrecipe/queryrecipe_test.go:TestAgenticEngineeringStarterBundleValidatesAgainstRepoSchema` (an `ElementsMatch` over 11 recipe IDs) and `:TestDefaultRepoRecipesUseGenericNoteByPath` (7 `Contains`/`NotContains` ID assertions).

**Why.** These test this repository's data, not the package. Every recipe added or renamed breaks a library test, and the ID list reviews nothing.

**Replacement.** Keep one test that asserts every recipe from `LoadDefaultSources(repoRoot)` validates against the repo schema with unique IDs, which is the drift guard that matters. Alternatively rely on the `query-recipes` validation check already wired in `pkg/validate/checks.go`. Drop the ID inventories.

### QRY-T-8 The pushdown planner tests check predicate shape, not meaning

**Verdict:** Rewrite part of the test; keep the rest.

**Tests:** `pkg/ontology/pushdown/planner_test.go:TestPlannerPredicateRoundTripsTypedValues`. Keep `TestPlannerPlanSortIsPrefixMatch` and the resolver tests.

**Why.** It asserts struct fields such as `ValueInt == 3` and `TargetNotePath == "specs/foo"`. It would pass with QRY-1 present, because nothing compares the operand's `ValueNorm` to what the indexer writes. The guidance's must-do ("add equivalence coverage proving pushed and residual paths agree") is not met at this layer.

**Replacement.** After QRY-1, add one table test asserting that `FieldValueRow(field, raw)` for an operand equals the materialized row for the same authored text, for every field kind and spelling in QRY-T-1. Behavioral equivalence lives in QRY-T-1.

### QRY-T-9 Add a recipe consumer-consistency test

**Verdict:** Add, after QRY-12.

**Test:** `queryrecipe.TestRunRejectsDuplicateIDAndIgnoresUnrelatedLoadIssues`. Fixture: three recipe files (target, a duplicate of another ID, and one malformed unrelated file). Assert one outcome. Once all five consumers call `queryrecipe.Run`, this one test covers them.

### QRY-T-10 Split `query_test.go` by concern

**Verdict:** Housekeeping, low priority.

**Test:** `pkg/ontology/query/query_test.go`, which has 6,963 lines and 128 tests covering SDL, prepare, execute roots, workspace, validation, and helpers. Split into `schema_test.go`, `prepare_test.go`, `roots_typed_test.go`, `identity_test.go`, `workspace_test.go`, and a `testenv_test.go` holding `newCustomQueryTestEnv`, the spies, and the fakes. No assertion changes; this makes QRY-10 reviewable.

## Suggested refactor sequence

1. **Red tests first (S).** Add QRY-T-1 (the differential harness, red on QRY-1, 2, and 3) and QRY-T-3 (the batching contracts, red on QRY-4 and 5). Rewrite QRY-T-2 to state instant semantics.
2. **One normalization source (M).** Add `ontology.FieldValueRow` and `FieldTextValues`. Make the indexer, pushdown operands, residual extraction, overlay, and views use them, and bump `OntologyMaterializationVersion`. This fixes QRY-1, QRY-2, and QRY-3 and deletes about six normalizer copies. Fold in QRY-T-8. Update the subsystem note (QRY-15).
3. **Cheap performance wins (S).** Batch-hydrate typed roots and `nodes(refs:)` (QRY-4, first half). Delete the dead code (QRY-11).
4. **Indexed identifier lookup (M).** Covers QRY-5.
5. **Predicate IR and a single evaluator (L).** Add `pushdown.Plan`, `Match`, and `SortItems` (QRY-7), and replace the planner's split API (QRY-8). Delete `root_filters.go` residual logic, the overlay matcher duplicates, the views indexed-scalar copies, `indexedPlanWarnings`, and the sentinel strings. The QRY-T-1 harness becomes the gate.
6. **Read through the Scope (M).** Unify typed roots on `Scope.TypeInstances` (QRY-4, second half). Delete `Deps.Service`, the executor caches, and the mutexes, and add a Scope metadata inventory (QRY-9). Move QRY-T-5 and QRY-T-6 with the code.
7. **Section neighbors on persisted edges (M to L).** Covers QRY-6 and needs the indexing and noderead owners.
8. **Split the executor (L, mechanical).** Covers QRY-10, plus QRY-T-4 and QRY-T-10 first.
9. **Recipe run path (M).** Add `queryrecipe.Run` and a long-lived recipe cache (QRY-12), fix the loader (QRY-14), add QRY-T-9, and add one engine constructor for `Deps` (QRY-13).

Steps 2 and 5 carry the correctness value. Steps 3 and 4 carry most of the performance value. Step 8 should wait until 5 and 6 have shrunk the executor.

## Slow-test table

Measured once with `go test -count=1 -json ./pkg/ontology/query/... ./pkg/ontology/pushdown/... ./pkg/ontology/queryrecipe/...`. All tests pass.

| Package | Wall time |
|---|---|
| `pkg/ontology/query` | 3.32 s |
| `pkg/ontology/queryrecipe` | 0.24 s |
| `pkg/ontology/pushdown` | 0.20 s |

There are 215 top-level tests, and their elapsed times sum to 3.35 s. The slowest:

| Test | Seconds |
|---|---|
| `query:TestExecute_TypedScalarFilterParity` | 0.29 |
| `query:TestExecute_TypedScalarSortParity` | 0.28 |
| `query:TestExecute_NodeWorkspaceNavigationFiltersBeforeOwnerLimit` | 0.13 |
| `query:TestExecute_NodeWorkspaceNavigationReportsOwnerTruncation` | 0.10 |
| `query:TestExecute_NodeWorkspaceNavigationReportsMemberTruncation` | 0.10 |
| `query:TestExecute_NodeWorkspaceNavigationAllowsExactOwnerLimitBeforeEmptyTypeBatch` | 0.10 |
| `query:TestExecute_AllowsLargePublicRootFirstLimit` | 0.10 |

There is no slow-test problem in this lane. Widening the differential harness (QRY-T-1) will add time. Build one fixture per field-kind group and share it across ops, as `scalarParityEnv` already does, rather than one environment per case.
