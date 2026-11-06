---
summary: "Second untangle pass: core audit and concrete candidate evidence."
reference-kind: analysis
---

# Second untangle pass: core libraries

Baseline: `b0d0cf824a9b7b7fc9e1832fd31aa5e7555e2f0e`.

Read-only audit of `pkg/vault`, `pkg/store`, `pkg/sqliteutil`, `pkg/notemeta`, `pkg/paths`, `pkg/markdownsyntax`, and the small non-application file/path/text helpers. No repository edits, tests, builds, external provider calls, or subagents. The source census found 419 production Go files in the principal audited roots. This is inventory coverage, not a claim to have manually read every line.

I read the first-pass delivery evidence and retained its deliberate boundaries. Current imports keep foundational Markdown/path/file utilities free of application dependencies; no new dependency inversion is warranted there.

## C2-01: Remove unused alternate Store entry points

**CONFIRMED surplus, worth fixing.** Exact-word searches across tracked Go sources find only declarations and explanatory comments for all twelve methods/functions below. There are no production callers, test calls, interface requirements, or fixtures invoking them. These APIs retain alternate schema, maintenance, query, and validation paths that must otherwise track the real owners' future changes.

| Location | Unused API | What remains authoritative |
| --- | --- | --- |
| `pkg/store/drop.go:13` | `DropSchema` | Existing domain/reset/open paths; no code-only rebuild calls this old ad hoc table dropper. |
| `pkg/store/code_file_state.go:54` | `ClearCallEdgesStale` | Active code persistence and current call-edge reconciliation. Keep `MarkCallEdgesStale`, `StaleCallEdgePaths`, and other reads. |
| `pkg/store/embedding_schema.go:14` | `EnsureEmbeddingsVecPrimary` | `schema_baseline_vectors.go:166` already calls `migrateIntelEmbeddingsToVecPrimaryTx`; keep that migration and live mirror creation. |
| `pkg/store/embedding_state_reads.go:65` | `EmbeddingStats` | Current explicit diagnostics/readers; keep live `IntelChunkEmbeddingStates`. |
| `pkg/store/external_targets_store.go:320` | `PruneOrphanExternalTargets` | `pruneOrphanExternalTargetsTx` remains called inside the real writer transactions at `external_targets_store.go:171` and `symbol_ref_lifecycle.go:71`. |
| `pkg/store/purge_code.go:16` | `PurgeCodeFilesNotIn` | Current ownership/code retirement routes. Keep `PurgeIntelCodeNotInPaths` pending separate test-ownership analysis. |
| `pkg/store/graph_path_inputs.go:60` | `GraphDocPathsForPrefix` | `GraphDocPaths` remains used by `graph_readmodel_paths.go:11`; do not delete it transitively. |
| `pkg/store/graph_query.go:11` | `GraphDocEdgesForPath` | Keep live plural `GraphDocEdgesForPaths` and readmodel-backed `GraphDocEdges`. |
| `pkg/store/graph_query.go:324` | `GraphDocEdgesForPrefix` | Current prefix query goes through `GraphDocEdges` with a `readmodel.GraphDocEdgeQuery`. The similarly named test at `graph_inputs_test.go:275` already calls that current method at line 305. |
| `pkg/store/validation_state.go:12` | `GetBrokenLinksJSON` | Current validation snapshot/state reads. Keep setter until its fixture-specific use is separately addressed. |
| `pkg/store/validation_state.go:62` | `GetValidationResultJSON` | `GetValidationState` and generation-pinned snapshot APIs. |
| `pkg/store/validation_state.go:71` | `SetValidationResultJSON` | `SetValidationRunning`/`SetValidationResult` and current fenced publication. |

**Smallest fix:** delete only these public entry points and their now-unused imports. `drop.go` becomes empty and can be deleted. Do not move active SQL or rewrite transactions. Do not delete private helpers reached by the migration/ownership paths. Remove obsolete active documentation links to `EnsureEmbeddingsVecPrimary` in `docs/hubs/Code Index (Hub).md:124,133` or point them at the current migration owner, following the repository's note-edit mechanics; preserve archived review/evidence records.

**Why the pieces should change together:** the live migration, retirement, graph query and publication owners are the only paths that need to evolve with their domains. These unused alternate paths have no separate user behavior to preserve and create misleading discovery targets, including an old table-drop API whose comment claims it implements rebuilds.

**Verification:** source search after deletion must show no dangling code references; focused `go test -tags fts5 ./pkg/store/... ./pkg/sqliteutil/...`; then root's integrated compile/gates and schema/rebuild tests. Keep exact current graph prefix and migration tests. A new test asserting absence of an export would mirror the change and is unnecessary.

**Risk:** exported Go symbols could have consumers outside this repository. None were inspected. This is the same external-consumer limitation already recorded for first-pass export removals. There is no schema/data migration in the proposed deletion.

## C2-02: Delete unconsumed path convenience constructors and extension-set builder

**CONFIRMED surplus, small safe reduction inside the approved scope.** Exact-word search across repository code, scripts, and source templates finds no call sites for:

- `pkg/paths/input.go:47` `ResolveNotePathInput`.
- `pkg/paths/input.go:82` `ResolveNotePathRef`.
- `pkg/paths/input.go:99` `ResolveCodeInput`.
- `pkg/vault/codepatterns/codepatterns.go:99` `DefaultExtensionSet`.

The path functions create `VaultPaths` from a root and immediately forward to their `WithVaultPaths` counterparts. All active callers already use the explicit precomputed-path boundary. `DefaultExtensionSet` constructs a map with no consumer; the independently tested `DefaultExtensions` API remains.

**Smallest fix:** remove these four unused functions and their associated declarations/comments. Keep all active `WithVaultPaths` input validation, `AbsFromInput`, `ResolveCodeRef` (it has a test consumer), canonical path types, and the existing coderef language set. No new wrapper or shared abstraction.

**Verification:** exact-symbol search, focused `pkg/paths` and `pkg/vault/codepatterns` tests, integrated compile. Preserve literal path/case/escape assertions. No new tests are needed for deleting uncalled forwarders.

**Risk:** external Go consumers are not checked; same API-removal limitation as C2-01. No path spelling or accepted-input change for remaining callers.

## C2-03: Replace duplicate split wrappers with `strings.Cut`

**CONFIRMED surplus, local simplification.** `pkg/vault/obsidian/file_filtering.go:59` and `pkg/store/note_path_sets.go:7` implement the same eleven-line split-at-first-slash helper under different names. Each has exactly one caller:

- `FuzzyMatch`, `file_filtering.go:30`.
- `CurrentNotePathsByFindPattern`, `note_path_search.go:140`.

Both callers have already established that the input contains `/`. Both use the first segment and remaining suffix. No caller relies on a special absent-delimiter result.

**Smallest fix:** call `dirPattern, contentPattern, _ := strings.Cut(pattern, "/")` at both sites and delete both helpers. Keep SQL candidate generation separate from final `obsidian.FuzzyMatch`, and keep the wildcard and dotted-pattern policies retained in pass one. This removes duplicated plumbing using the standard library rather than adding a cross-package matching abstraction.

**Verification:** existing FuzzyMatch directory, dotted and wildcard cases; current indexed find-pattern parity tests (`pkg/store/note_metadata_test.go`). Add nothing unless an existing boundary case is absent. This does not change the matcher or SQL's conservative candidate contract.

## Selected comment pass

Low-impact cleanup can accompany C2-03 in `pkg/vault/obsidian/file_filtering.go`. Inline comments such as `Normalize for case sensitivity` (24), `Handle directory-specific search` (28), `First check if directory part matches` (32), `If we have a content part to match as well` (37), `Split path to get content section` (39), `Word not found` (118), and `Split by hyphen, underscore, and dot` (168) narrate the adjacent code and can go. Keep comments explaining the one-character directory-prefix rule, anchored versus anywhere wildcard semantics, supported syntax, and links to the matching DSL. Do not flatten those different behaviors.

## Retained / no new issue demonstrated

- `PurgeIntelCodeNotInPaths`, `DeleteOntologyForPaths`, snapshot replacement, various graph fixture writers/readers, and several syntax convenience functions have test callers even where no production caller exists. A production-call count alone does not establish that their tests lack value. Do not mass-delete fixture-support APIs or tests without proving the real replacement preserves the independently asserted contract.
- `GraphDocPaths`, `GraphDocEdgesForPaths`, `GraphDocNoteEdgesBySources`, and `GraphDocNoteScoresByCommunities` are live. Similar names to the dead methods do not make them candidates.
- Schema-lock retry polling has an OS-lock reason; watcher readiness polling has real asynchronous backend constraints. No timing-driven test failure was reproduced and no retries/timeouts should be changed speculatively.
- Cache Markdown compatibility, provider/current-source separation, note snapshot/delta distinctions, property and URL normalization, typed config publication, and separate SQL writer transactions are documented requirements. Pass-one retentions remain intact.
- No core test was proved unable to catch a real bug or proved flaky in this read-only pass.
- The concrete store still refers to ingestion DTOs from anchors. Untangling that dependency would require a wider shared-domain decision; current evidence does not justify it for this pass.

## Evidence and retrieval limits

`node .rhizome/modules/compounding/scripts/lookup.mjs --files ...` returned `No current context shards match.` I read current subsystem notes and source directly. I did not run a Rhizome agent session or indexed retrieval in this pass, and make no claim that the indexed knowledge graph was consulted. Tests and runtime probes were deliberately left to root so they do not race the integrated review/gate work.
