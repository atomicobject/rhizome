# Core structural audit

Baseline: `900eee0b0036ee2699d84d5d02d5ffe6a39a4441`. Read-only audit; no production edits or tests run. User selected both optional comments and tests cleanup.

## Coverage and evidence limits

Owned paths: `pkg/vault` except watchhub; `pkg/paths`, `pkg/store`, `pkg/sqliteutil`, `pkg/indexstore`, `pkg/notemeta`, `pkg/fileio`, `pkg/noteformat`, `pkg/markdownsyntax`, `pkg/ontology` core/schema/idalloc/sectionparse, `pkg/sourcetxn`, `pkg/repositorytrust`. Reviewed subsystem constraints, package test ownership, dependency direction, source size/function census, candidate dead exports, and concrete call sites for the findings below. This was a structural sweep, not line-by-line proof of all 1,316 files in the broad source/test inventory.

`node .rhizome/modules/compounding/scripts/lookup.mjs --task ...` reported the configured worktree dev binary absent and no matching shards. Parent is restoring the launcher with its sole `make build`. No generic search is claimed as Rhizome retrieval. No performance claim is made.

## C1. Use the existing canonical alias parser in the cache

**Category/status:** hidden link, CONFIRMED duplicate and semantic divergence by source inspection. Runtime reproduction remains for implementation.

**Evidence:** `pkg/vault/cache/service.go:1372` calls local `aliasListFromFrontmatter`; its definition at `:1388-1421` accepts strings and lists of strings only. `pkg/vault/obsidian/wikilinks.go:74-118` already owns `AliasListFromFrontmatter`, accepting the same forms plus `fmt.Stringer` scalars/items. `pkg/noteformat/markdown/provider.go:112`, `pkg/vault/health/link_resolver.go:283`, `pkg/ontology/noderead/resolve_paths.go:198/204`, `pkg/validate/apply_edits.go:53`, and `pkg/app/web/markdown_compat.go:99` all use that canonical helper. `pkg/vault/obsidian/wikilinks_test.go:21` explicitly establishes Stringer semantics. Cache rebuild alone uses the narrower copy.

**Smallest fix:** replace the cache call with `obsidian.AliasListFromFrontmatter` and delete its private duplicate. No new helper or package needed.

**Observable scenario:** in the supported no-runtime Markdown cache, a YAML alias parsed as a time value is excluded from code-reference alias lookup although the canonical parser accepts it. A cache-level fixture with a date/timestamp alias and a code reference can reproduce this gap before changing the call. For runtime projection, keep source fact alias precedence intact; do not broaden this task into changing metadata exports.

**Affected files:** `pkg/vault/cache/service.go` (or its extracted code-reference file), `pkg/vault/cache/service_coderefs_test.go`; existing canonical parser tests stay. No config or persisted schema changes. Update cache CONTEXT only if it needs to state canonical alias ownership; invariant remains the existing canonical rule.

**Verification:** focused cache and obsidian tests, including literal alias expectations for strings, lists, Stringer, ignored nonstrings, and collisions. Full race/integration gates because cache participates in runtime indexing. Preserve the explicit raw filesystem source contract in notemeta.

## C2. Remove unused Store APIs and the flexibility they alone support

**Category/status:** surplus, CONFIRMED repository-wide references.

**Evidence:** repository-wide identifier search over all Go files and then all non-vendored files found only definitions for:

- `pkg/store/graph_doc_edges.go:280` `DeleteGraphDocEdgesByPath`.
- `pkg/store/ontology.go:57` `InvalidateOntologyState`.
- `pkg/store/ontology_edges.go:116` `OntologyStructuralEdgesBySources`.
- `pkg/store/ontology_edges.go:120` `OntologyAmbientEdgesBySources`.

The last two keep `ontologyEdgesByEndpoint` generic in both a column name and a structural boolean. Its only used route is `OntologyAmbientEdgesByTargets` at `:124`, always `dst_path` and `false`. The live consumer is `pkg/ontology/query/section_neighbors.go:121`, through `query/types.go:296`.

**Smallest fix:** delete the four unreferenced methods. Inline `ontologyEdgesByEndpoint` into `OntologyAmbientEdgesByTargets`, spelling `dst_path` and `structural = 0` in SQL; remove the impossible endpoint/structural options. Keep `invalidateOntologyStateTx`, which remains used by actual transactional writes. Preserve batching, relation filtering, deterministic global sorting and the final limit.

**Affected files:** `pkg/store/{graph_doc_edges.go,ontology.go,ontology_edges.go}`. No caller or interface migration is needed; do not change the live ambient-target method signature. No schema/migration change.

**Verification:** `pkg/store` and `pkg/ontology/query` tests. Existing `TestOntologyAmbientEdgesByTargets_PreservesGlobalLimitOrderAcrossBatches` at `ontology_delta_test.go:300` protects the subtle behavior; retain it. Retain snapshot/delta and deletion coverage. Full repository compilation proves no remaining direct caller. Repository policy allows deletion instead of compatibility shims; external Go imports are not inventoried here.

## C3. Delete obsolete Obsidian ignore forwarding APIs

**Category/status:** needless link and surplus, CONFIRMED.

**Evidence:** `pkg/vault/obsidian/ignore_defaults.go:6` and `:11` forward directly to `pkg/vault/ignore`. `DefaultIgnorePatterns` has no caller through obsidian. `DefaultIgnoreFile` has one, `pkg/app/operations/ignore.go:36`. Init code already calls `ignore.DefaultIgnoreFile` directly. The owning subsystem note explicitly assigns ignore policy to `pkg/vault/ignore`.

**Smallest fix:** migrate the one operations caller/import to `ignore.DefaultIgnoreFile`, then delete `ignore_defaults.go`.

**Affected files:** `pkg/app/operations/ignore.go` and deleted wrapper file; coordinate with the operations owner. Preserve `InstallDefaultIgnore` and its overwrite/permission/error behavior. No generated templates/config changed.

**Verification:** focused operations ignore tests, ignore package tests and repository compilation. The actual emitted bytes come from the same function, so an extra mirror test is unnecessary.

## C4. Make ordered fuzzy word matching have one implementation

**Category/status:** hidden link, CONFIRMED duplication.

**Evidence:** `pkg/vault/obsidian/file_filtering.go:100-128` and `:138-161` implement the same ordered word-boundary search. The explicit empty-word success in the first is identical to empty-loop success in the second. Both carry `found` state that cannot be false after their inner loop exits, since that loop either returns false or sets it true and breaks.

**Smallest fix:** retain the different wildcard behavior (`wildcardMatch` for directory-scoped content, `wildcardMatchAnywhere` for whole paths); put only the common nonwildcard ordered-word loop in one helper. Delete the impossible `!found` path and its variable. Do not merge wildcard dispatch or change single-character directory or dotted-segment behavior.

**Affected files:** `pkg/vault/obsidian/file_filtering.go`. Public `FuzzyMatch` signature remains. Its callers include operations `list_match.go`, `query_plan.go`, `list_indexed_match.go`, web `catalog.go`, and ontology `schema/matchers.go`. Existing `file_filtering_test.go` already tests ordered/out-of-order words, boundaries, wildcards, dotted segments and directory matching.

**Verification:** obsidian fuzzy tests, operations matching tests and ontology/schema tests. Add cases only if existing public behavior coverage misses an edge encountered in the refactor.

## C5. Separate cache method clusters into same-package files and remove restatement comments

**Category/status:** needless colocation/retrieval load; CONFIRMED source layout. Lower risk than changing service ownership.

**Evidence:** `pkg/vault/cache/service.go` is 1,782 lines, containing the state/types and constructor, lifetime management and initial-readiness gate (`:190-352`), read APIs/copying (`:356-477`), dirty refresh/recrawl (`:494-733`), crawl/path reads (`:759-1059`), index maintenance (`:1092-1164`), dirty event merging (`:1188-1250`), and code-reference scanning (`:1334-1782`). Existing `selection_policy.go`, `note_projection.go`, and `refresh_result.go` already show same-package concerns being split. Code-reference configuration/scanning can change without changing entry cloning/read methods; their current colocation forces unrelated reading.

**Smallest fix:** move intact method clusters into e.g. `crawl.go`, `refresh.go`, `entries.go`, and `code_refs.go`, leaving Service/types/constructor/lifecycle together. Keep one Service and its lock ownership; do not introduce new services, interfaces or concurrency boundaries. Size files under the approximate 500-line policy after cleanup. Move corresponding tests only if doing so improves ownership; tests need not mechanically mirror every file.

**Comments cleanup:** remove the decorative section rulers and implementation diagrams/lists at `:36-85`, repeated section introductions, and comments such as `:1238` stating that `bumpVersion` increments the version counter. The top prose incorrectly says initialization uses a spin gate (`:79`); actual `EnsureReady` waits on `crawlCh`. Preserve reasons for last-good serving, forced rereads, source identity comparison, admission and ignore distinctions, lock preconditions, dirty requeue/retry, and explicit remaining limits. Preserve comment links to rationale docs.

**Verification:** same public API and no logical changes beyond separate C1/C6. Focused cache tests, race test and real watcher integration via parent gates. Diff review must verify moved bodies and lock ordering remain identical. No subsystem invariant move; refresh cache CONTEXT entry map to point at the final relevant filenames.

## C6. Make cache unit tests assert the synchronous contract directly

**Category/status:** surplus timing/retry, CONFIRMED by source contract; not a claim of a currently observed flake.

**Evidence:** `TestServiceRefreshUpdatesModifiedFile`, `pkg/vault/cache/service_test.go:53-87`, manually marks dirty then calls `Refresh` up to five times with 10ms sleeps. `Refresh` drains a manually supplied dirty batch synchronously; retries weaken detection of delayed/dropped handling. It also inspects the private `dirty` map although the assertion of the post-refresh content is the behavior wanted. `TestServiceStaleRevalidation` at `:407` sleeps to change mtime even though forced stale recrawls reread matching stats (`service.go:860-875`, `refreshPath` force branch). `TestCodeRefs_IncrementalCodeChange`, `service_coderefs_test.go:76`, sleeps despite rewriting to a different byte length; the code-reference stat check includes Size.

**Smallest fix:** first test performs one `MarkDirty`, one `Refresh`, then asserts new public Entry content/tags. Remove its retry/sleep and private-map precondition. Remove the other two unnecessary mtime sleeps; their observable assertions remain. Keep tests that exercise genuine asynchronous recrawls using channels/`waitResynced`; keep OS watcher tests as integration tests.

**Affected files:** `pkg/vault/cache/service_test.go`, `service_coderefs_test.go`. No production semantic change required.

**Verification:** focused named tests, repeat once with `-count` only if diagnosing an observed flake; package/race gates after refactor. Do not substitute arbitrary larger timeouts.

## C7. Remove redundant table-name cases from embedding corruption classification

**Category/status:** surplus, CONFIRMED logical subsumption.

**Evidence:** `pkg/indexstore/intel_store.go:132-136` checks `strings.Contains(msg, "emb_")` and then four concrete strings all beginning `emb_`. The later OR terms can never affect the result.

**Smallest fix:** retain nil handling and lowercase conversion, return `strings.Contains(msg, "emb_")`. Keep the broad current classification exactly; do not turn this into a corruption-repair redesign.

**Affected files:** `pkg/indexstore/intel_store.go`. Behavior-preserving deletion; existing configured-open tests and compilation suffice. No new test that merely repeats this expression.

## Useful exceptions to retain

- Store sharing currently uses `SetWriteMu` at application composition sites. Construction-time injection might express this better, but it would cross store/runtime/indexer interfaces. No missed live composition site was proven in this audit, so do not invent a broad API migration here.
- Different snapshot/delta paths and provider source/status checks are necessary to preserve complete publication proofs, source-only reads, and raw-vs-projected metadata contracts. Do not unify their intentional responsibilities merely because shapes repeat.
- `ontology.ValidBlockID` is not a pass-through alias: it normalizes caret/whitespace before calling the lexical Markdown predicate. Preserve that authored-input contract.
- Platform-specific path, lock, file replacement, case-folding, Unicode and hard-link code represents real OS semantics. Keep those branches and platform-specific tests.
- Source transaction file replacement and `pkg/fileio` have different settlement/durability/error contracts; their shared use of rename is insufficient justification to merge them.
- SQLite retry/backoff is bounded, cancellation-aware contention policy. Do not delete it as a generic sleep smell.
- `pkg/vault/cache/integration_test.go:53` skips real fsnotify tests on macOS under race, with an explicit platform reason. This is a coverage limitation worth recording, not evidence the test is worthless. A fix needs runtime reproduction and watchhub ownership; no larger timeout or retry should be used to hide it.
- `directNoteReader` in notemeta deliberately hides optional cache interfaces to guarantee an actual filesystem snapshot and one-read behavior. Removing the wrapper would change authority.
- Core package dependency direction is already sound: the parent's graph found no non-app core import of `pkg/app`.

## Cross-lane finding

The query auditor owns Q1, canonical section matching. It touches `ontology/build_assessment.go:302`, `projection_resolver.go:257`, `projection_bindings.go:27/36`, query and web copies. Do not double-count it here. Preserve compiled heading normalization when converging those copies.

## Outcome

Seven concrete, bounded refactors/cleanup groups are worth doing in this lane. Most are deletion or same-package organization; C1 warrants a behavior regression and C5 warrants concurrency/integration verification. No evidence justified wholesale rearchitecture of paths, stores, source transactions, or ontology publication.
