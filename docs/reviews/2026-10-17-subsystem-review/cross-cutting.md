---
summary: "Lane report from the 2026-10-17 subsystem quality review: Cross-cutting architecture and test-suite review. Findings are a snapshot of commit 5251923f."
---

# Cross-cutting architecture and test-suite review

Reviewer lane: architecture between packages and the test suite as a whole. Worktree `t3-e36f9b1a` at `5251923f`. All numbers come from `go list`, greps, a go/ast scan, and one `golangci-lint` run with the repository's own `.golangci.yml`. Line counts include comments. "Prod" means non-test `.go` files under `pkg/`, `cmd/`, and `tools/`, excluding `testdata/`. Coverage was measured for 12 small packages only, because the machine load was 6–7 on 14 cores while other reviewers were running tests.

## Summary

The package graph mostly follows the write-path/read-path design. Three structural problems make it look cleaner on paper than it is.

1. **The whole database lives in a package named for one feature.** `pkg/anchors/sqlite` is the unified SQLite store: 27.7k LOC, 345 exported `*Store` methods, a 9,790-line `store.go`, and 33 importing packages. It holds ontology, note-metadata, validation, session, graph, and embedding tables. The stores subsystem note officially owns only `pkg/sqliteutil`, so nobody clearly owns this package.
2. **The vault-core primitives package drags in most of the system.** `pkg/vault/obsidian` should hold link, tag, and frontmatter primitives. In practice it also opens the database (13 `OpenIntelStore*` variants) and owns the config types of anchors, embeddings, and llm. As a result, its transitive closure is 26 internal packages, including the SQLite store and the embedding stores.
3. **Six domain packages import `pkg/app/*`.** The worst case is `pkg/search` importing `pkg/app/cli` for three expression-parser symbols. That one edge grows `pkg/search`'s closure from 37 to 62 packages.

`cmd/` is not a thin front end. It has 20k LOC, 43 direct domain-package imports, 190 mutable package-level flag/state variables, and raw SQL analytics.

Two policies are not enforced:

- **File size:** 40% of prod LOC (141k of 355k) sits in files over the documented 500-line limit.
- **Linting:** `.golangci.yml` enables `unused`, `staticcheck`, and `errcheck`, but no Make target or CI job runs it. It currently reports 519 issues, including 31 prod uses of deprecated `pkg/paths` shims, which the "no shims" policy forbids.

The test suite is large (263k LOC, 6,713 tests, 0.74 test-to-prod ratio) and mostly disciplined. Pinning constants, golden-file sprawl, unconditional skips, and "only asserts no error" tests are all rare. Its weaknesses are infrastructural:

- **No shared testkit:** 72 independent temp-vault builders across 25 packages.
- **Mutable global seams:** 156 overrides of three package-level variables, which prevent `t.Parallel`.
- **Heavy white-box coupling:** 93% of test files are internal-package tests, and tests call 1,083 distinct unexported prod functions.
- **Prose pinning:** about 350 assertions match exact sentences in templates, schema guidance, and live repository docs.

Overall verdict: **sound core with significant structural debt at the bottom of the graph (store, vault-core) and at the top (cmd, app wiring). The test suite is healthy in assertion quality and weak in shared infrastructure.**

## Dependency graph findings

Graph basics: 134 packages (`go list ./...`). Highest internal fan-in: `pkg/paths` 50, `pkg/vault/obsidian` 44, `pkg/anchors/sqlite` 33, `pkg/anchors` 31, `pkg/ontology` 28, `pkg/indexingperf` 27, `pkg/search/embeddings` 25. Highest fan-out: `cmd` 77, `pkg/app/web` 47, `pkg/app/mcp` 37, `pkg/app/bootstrap` 37, `pkg/app/cli` 31, `pkg/app/indexing` 30. There are no import cycles (Go forbids them). Several small packages exist to route around cycles; see ARCH-4.

### ARCH-1 — The unified SQLite store is a god package under a misleading name and owner
- **Severity:** P1 · **Confidence:** High · **Effort:** L
- **Evidence:**
  - `pkg/anchors/sqlite` has 27,717 prod LOC in 67 files, 866 exported declarations, and 345 exported `func (s *Store)` methods. `store.go` alone is 9,790 lines.
  - File-name prefixes show the domains it holds: graph (7 files), validation (6), note (6), ontology (5), intel (4), external (4), ownership (3), plus session and search generation.
  - It is imported by 33 prod packages, including `pkg/ontology` (15 symbols: `OntologyDelta`, `NoteMetadataRow`, …), `pkg/notemeta` (47 symbols), `pkg/ontology/noderead`, `pkg/ontology/query`, `pkg/search/*`, and `pkg/vault/obsidian`. It is aliased `semdb` almost everywhere.
  - Ownership mismatch: `docs/reference/subsystems/README.md` assigns `pkg/anchors` (and therefore `pkg/anchors/sqlite` by prefix) to **code-intel**. **stores** owns only `pkg/sqliteutil` (1k LOC). The note that owns the single-writer rule does not own the store that implements it.
  - Domain DTOs point the wrong way. Ontology node types live in the code-anchors package: `anchors.IntelOntologyNode`, `IntelOntologyNodeFieldValue`, and `OntologyNodeQueryPlan`. They are imported by `pkg/ontology`, `pkg/ontology/noderead`, and `pkg/search/retrieval`.
- **Recommendation:**
  1. Rename and move the store to `pkg/store` (or `pkg/db`) and make it the stores note's code path. A rename is mechanical but touches 33 packages.
  2. Split the store by domain behind one shared connection and write mutex: `store/ontology`, `store/notemeta`, `store/validation`, `store/graph`, `store/session`, `store/intel`. Each sub-store gets an interface owned by its consumer.
  3. Move the `IntelOntologyNode*` DTOs into `pkg/ontology/readmodel`.

  This is the highest-leverage refactor in the codebase. Most other layering oddities (ARCH-4) exist because everything must import this package.

### ARCH-2 — `pkg/vault/obsidian` is a grab bag whose closure includes the database, embeddings, and llm
- **Severity:** P1 · **Confidence:** High · **Effort:** M
- **Evidence:**
  - Direct imports: `pkg/anchors`, `pkg/anchors/sqlite`, `pkg/search/embeddings`, `pkg/search/embeddings/sqlite`, `pkg/llm`, `pkg/app/contextpack`, `pkg/search/graphalg`, `pkg/sqliteutil`, and others. Its transitive internal closure is 26 packages, including all SQLite migrations.
  - The heavy edges come from a handful of files:
    - `intel_store.go`: 13 `OpenIntelStore*` functions.
    - `local_config.go`, `code_config.go`, `embeddings_config.go`, `code_embeddings_config.go`, `graph_config.go`: config types for other subsystems.
    - `graph*.go`: graph analysis.
  - The remaining roughly 35 files are the link, tag, and frontmatter primitives that vault-core describes.
  - Fan-in is 44. Every one of those packages, including `pkg/notemeta`, `pkg/ontology/*`, and `pkg/search/*`, transitively pulls in the store and the embedding stores.
- **Recommendation:** Split into three pieces:
  1. `pkg/vault/obsidian`: pure markdown primitives, with no `sqlite`, `embeddings`, or `llm` imports.
  2. `pkg/vault/config`, which already exists and should absorb `LocalConfig` and the per-subsystem config merges, or let each subsystem own its own section.
  3. A single store opener in the new store package (see DUP-1).

### ARCH-3 — Six domain→app upward edges
- **Severity:** P2 · **Confidence:** High · **Effort:** S (each edge is a small move)

| Edge | Site | What is used | Fix |
| --- | --- | --- | --- |
| `pkg/search` → `pkg/app/cli` | `pkg/search/types.go:19`, `pkg/search/filters.go:4` | `InputExpression`, `ListInput`, `ParseInputsWithExpression` (3 symbols) | Move the expression parser to a leaf such as `pkg/search/filterexpr`. **The closure of `pkg/search` drops from 62 to 37 packages.** |
| `pkg/search/planner` → `pkg/app/presentation` | `pkg/search/planner/planner.go:49,506` | `ChunkBodyReader`, `DefaultPacker` | Define the packer interface in `planner` and inject the presentation packer from `pkg/app/unifiedsearch`. |
| `pkg/search/perfworkload` → `pkg/app/unifiedsearch` | `pkg/search/perfworkload/run.go:15` | `Execute`, `Options`, `Profile` | This is a perf harness used only by `tools/searchperf`. Move it under `tools/`. |
| `pkg/validate` → `pkg/app/cli/init` (+`init/diff`) | `pkg/validate/checks.go:23`, `pkg/validate/repair_review_selection.go:267` | `ValidateBundledSkillOverlays`, `GenerateUnifiedDiff` | Inject the skill-overlay check from the app layer. Move the diff helper to a leaf (`pkg/textdiff`). |
| `pkg/ontology` → `pkg/app/cli/init/diff` | `pkg/ontology/edit_session.go:891` | `GenerateUnifiedDiff` | Same leaf `pkg/textdiff`. |
| `pkg/vault/obsidian` → `pkg/app/contextpack` | `pkg/vault/obsidian/local_config.go:1014,1272,1279,1283` | `DefaultBudgetChars` (a constant) | Move the constant into config, or have the app layer apply the default. |

Related same-layer smells:
- `pkg/app/indexing` imports `pkg/app/bootstrap` (`NewCodeAnchorService`, `ResolveRoots`; `pkg/app/indexing/commands.go:74-76`). Batch indexing depends on the live runtime composition root, and that is why `pkg/app/indexcore` exists (ARCH-4). Move the code-anchor service construction into `pkg/app/codeintel`.
- `pkg/app/bootstrap` imports `pkg/app/cli/init` only for `LoadAgentHelperTemplate` (`pkg/app/bootstrap/codex.go:537`).
- `pkg/ontology/query` imports `pkg/search/semantic` (`types.go:314`) for its `SemanticSearcher` port. The query layer should own its port types.

### ARCH-4 — Leaf packages exist mainly to route around the store and bootstrap hubs
- **Severity:** P3 · **Confidence:** Medium · **Effort:** S once ARCH-1 and ARCH-2 land
- **Evidence:**
  - `pkg/search/intentstore` (45 LOC of DTOs and interfaces) is imported by `anchors/sqlite`, `app/indexing`, `app/indexwriter`, `app/semanticruntime`, and `search/intent`.
  - `pkg/ontology/readmodel` (260 LOC, no tests) is imported by `anchors`, `anchors/sqlite`, and `noderead`.
  - `pkg/ontology/sectionparse` (349 LOC, **no tests**, and a different `slugifyHeading` from `pkg/ontology`; see DUP-6) is imported by `anchors` and `ontology`.
  - `pkg/search/embeddings/codeindex` (164 LOC of types and interfaces) has 8 importers.
  - `pkg/app/indexcore` (827 LOC) is shared by `bootstrap` and `indexing` because `indexing` already imports `bootstrap`.
  - These packages exist because a low-level package, usually the store, needs a type defined higher up.
- **Recommendation:** Keep `readmodel` as the ontology DTO home and grow it (see ARCH-1). Once domain sub-stores own their row types, fold `intentstore` into `search/intent` and `sectionparse` into `ontology`. Re-evaluate `indexcore` after the `indexing`→`bootstrap` edge is removed.

### ARCH-5 — `cmd/` is a second application layer
- **Severity:** P2 · **Confidence:** High · **Effort:** M
- **Evidence:**
  - `cmd` has 19,986 prod LOC in 130 files (pkg/app/cli has 18,048) and 658 non-test functions.
  - It imports 77 internal packages, 43 of them **domain packages directly** (`pkg/anchors/sqlite`, `pkg/ontology`, `pkg/ontology/query`, `pkg/search/*`, `pkg/sqliteutil/migration`, …). Only 47 of the 130 files import `pkg/app/cli`.
  - Raw SQL analytics: `cmd/code_stats.go` (5 queries, e.g. `queryLanguageCounts` runs `SELECT lang, COUNT(*) … FROM symbols`) and `cmd/code_stats_external.go` (19 queries, including a `queryLegacyReferenceClassifications` path), together about 770 LOC. They bypass the store API and the stores note.
  - Dependency composition:
    - `cmd/ontology_query_runtime.go` (777 LOC) implements `ontologyquery.Deps` providers (`NextID`, `CurrentUser`, `DocsForCode`, `CodeForNote`, `TestsForCode`).
    - `cmd/runtime_helpers.go` (404 LOC) builds `agentapi.Config` twice (lines 142 and 280).
    - `cmd/serve_agent_op_hooks.go:138` builds it a third time.
  - Global mutable state: 271 package-level vars in `cmd`; 81 are `*Cmd` and 190 are flag or state variables, such as `vaultName`. Tests must call `resetAgentTestGlobals()` and `resetFlags(rootCmd)` (41 call sites).
  - Most remaining files are rendering and flag parsing, which is legitimately CLI.
- **Recommendation:**
  1. Move the code-stats SQL into store methods.
  2. Move the ontology-query provider wiring and `agentapi.Config` composition into one app-layer composition function (see DUP-5).
  3. Replace package-level flag variables with per-command option structs bound in `newXCmd()`. That also unlocks parallel `cmd` tests.

  The cli note already states the target design; this measures the gap.

### ARCH-6 — The web server (18k LOC) has no owning subsystem note and bypasses the noderead seam
- **Severity:** P2 · **Confidence:** Medium (some direct reads may be sanctioned) · **Effort:** M
- **Evidence:**
  - `scripts/subsystem-guidance-map --json` covers 55 path prefixes. 22 `pkg/` packages (40,130 LOC) are covered by none. The largest are `pkg/app/web` (18,409), `pkg/app/views` (8,243), `pkg/vault/cache` (2,322), `pkg/diagnostics`, `pkg/llm`, `pkg/app/agentchat`, `pkg/app/update`, and `pkg/app/oneshotruntime`.
  - The overview says noderead is "the one read seam for typed nodes: Web/MCP/CLI all consume it". Yet `pkg/app/web/ontology.go` (3,491 LOC) reads ontology rows straight from the store:
    - `store.OntologyEdgesForPath` (line 1746, in `workspacePersistedRelations`)
    - `store.OntologyNodesByNoteFragments` (lines 1949 and 2404)
    - `store.OntologyNodesByPaths` (line 2392)
    - `store.OntologyNodeFieldValuesByNodeIDs` (line 2430, in `identityStatusByItemRefs`)
- **Recommendation:** Add a `web` (and `views`) subsystem note with code paths. Route these five reads through `noderead`, or document them as a sanctioned exception in the noderead note.

### ARCH-7 — The file-size and no-shim policies are not met, and the configured linter is not run
- **Severity:** P2 · **Confidence:** High · **Effort:** S to enforce, L to comply
- **Evidence:**
  - 136 of 1,442 prod files exceed 500 lines. They hold 140,981 LOC, about 40% of prod. The largest are `pkg/anchors/sqlite/store.go` 9,790, `pkg/ontology/query/execute.go` 5,268, `pkg/app/web/ontology.go` 3,491, `pkg/search/embeddings/codeindex/sqlite/store.go` 3,401, `pkg/app/cli/context_text.go` 2,906, and `pkg/ontology/edit_session.go` 2,843.
  - `.golangci.yml` enables `errcheck`, `govet`, `staticcheck`, `revive`, `ineffassign`, and `unused`. However, `make lint` runs only `gofmt`, and neither `.github/workflows/*.yml` nor `scripts/` invokes `golangci-lint`.
  - One run (`golangci-lint run --build-tags fts5`, about 12 s) reports **519 issues**: errcheck 267 (108 in prod files), staticcheck 213, unused 23, ineffassign 16.
  - staticcheck SA1019 flags 42 uses of deprecated APIs, **31 in prod**: `paths.NormalizeNote` ×28, `paths.ResolveNoteInputWithVaultPaths` ×7, `RelNoteStrict` ×3, `AbsNote` ×2. There are 11 `// Deprecated:` shims in prod (`pkg/paths/input.go` 4, `pkg/paths/vault.go` 3, `pkg/paths/normalize.go` 1, and others). `docs/engineering/architecture.md` says "No backward-compatibility shims".
- **Recommendation:**
  1. Add `golangci-lint` with `unused` and `staticcheck` SA rules to `check-fast` now. Both are almost clean.
  2. Ratchet errcheck by directory.
  3. Delete the `pkg/paths` deprecated shims after migrating their 31 call sites.
  4. Add a CI ratchet: no file may grow past 500 lines unless it is already listed.

### ARCH-8 — `pkg/app/cli` is the application-service layer, not a CLI layer
- **Severity:** P3 · **Confidence:** High · **Effort:** S (rename) / M (split)
- **Evidence:** `pkg/app/mcp` uses **62** symbols from `pkg/app/cli` (`BuildFileContextTextResult`, `CodeRelatedness`, `CodeSimilarity`, …). `pkg/app/web` uses 4, `pkg/search` uses 3 (ARCH-3), and `agentstart` imports it too. Its fan-out is 31 packages.
- **Recommendation:** Rename it to reflect its role (for example `pkg/app/ops` or `pkg/app/service`), or split it by capability (file-context, list/query, tags/properties, code analytics) so MCP and web depend on capabilities rather than "cli".

## Package consolidation proposals

Sizes are prod LOC. "Uses" means distinct exported symbols referenced by the importer.

| Current packages | Proposed | Rationale |
| --- | --- | --- |
| `pkg/app/semanticops` (15 LOC, 1 func `PrepareProvider`) | Fold into `pkg/app/semanticruntime` or `pkg/search/embeddings` | A one-function package. Its 3 importers each use only that function. |
| `pkg/app/searchengine` (80 LOC, no tests, 1 importer using 3 symbols) | Fold into `pkg/app/unifiedsearch` | Single consumer; no seam. |
| `pkg/app/agent` (143 LOC, imported only by `bootstrap` for `PlanToolCalls`/`ToolCall`) | Fold into its consumer (the chat/planning code in `bootstrap/codex.go`) or into `agentchat` | Shallow. The planner is used in one place. |
| `pkg/app/agentstart` (231 LOC, only `cmd` imports it, 27.5% own coverage) | Fold into `pkg/app/agentcode` or the agent app layer | Single consumer. |
| `pkg/app/mcpserve` (159) + `pkg/app/cli/mcpserve` (160) | One `pkg/app/mcpserve` | Two packages with the same name, about 160 LOC each. The CLI one uses one symbol (`Dispatcher`) from the other. |
| `pkg/app/runtimestop` (320, 1 file) | Fold into `pkg/app/runtime` | It imports only `runtime` and `indexlock`, and `runtime` does not import it, so there is no cycle risk. Its 13 symbols from `runtime` show it is part of that module. |
| `pkg/app/runtimeview` (49) | **Keep** | A real narrow contract (`View`, `Snapshot`, `CapabilityState`) shared by bootstrap, mcp, and web. |
| `pkg/app/bootstrap/lane` (1,249) | **Keep** | A real seam: 7 importers use 1–15 symbols each. |
| `pkg/app/indexcore` (827) + `pkg/app/indexingpipe` (540) | Merge into one `indexcore` (discover → process → publish) | `indexingpipe` is used by `indexcore` (4 symbols), `indexing` (2), and `codeintel` (9). Together they are one structural pipeline. `indexcore` exists mainly because `indexing` imports `bootstrap` (ARCH-3). |
| `pkg/app/indexwriter` (2,558) | **Keep** | A real seam: the writer lane and stores policy. |
| `pkg/app/indexing` (6,543) | Keep, but move `ValidationProjection*` (12 symbols used by `validationproduct`) to `validationproduct` | Validation projection orchestration lives in the indexing package. |
| `pkg/app/answer/adapt` (48) | **Keep** or fold into `answer` | It keeps `answer` free of the `pkg/ontology` dependency. That is a deliberate purity seam, so the call is marginal. |
| `pkg/app/unifiedsearch/application` (1 file, 1,428 LOC) | Keep the package, split the file | A real policy seam (31 symbols used by `unifiedsearch`), but the file is nearly 3× the size limit. |
| `pkg/anchors/sqlite` (27.7k) | `pkg/store` + per-domain sub-stores | ARCH-1. |
| `pkg/vault/obsidian` (11.2k) | `obsidian` (primitives) + `vault/config` (config) + store opener in `pkg/store` | ARCH-2. |
| `pkg/search/embeddings/sqlite` (2.3k) + `pkg/search/embeddings/codeindex/sqlite` (3.6k) + `embeddings/internal/sqlstore` (164) | One embedding store parameterized by domain | DUP-4. |
| `pkg/search/intentstore`, `pkg/ontology/sectionparse` | Fold back after ARCH-1 | ARCH-4. |
| `pkg/search/perfworkload` | Move under `tools/searchperf` | Perf harness, not library code. It causes the ARCH-3 upward edge. |

## Duplicated concepts

### DUP-1 — About 22 ways to open the unified store
- **Severity:** P2 · **Confidence:** High · **Effort:** M
- **Evidence:**
  - `pkg/vault/obsidian/intel_store.go` defines 13 `OpenIntelStore*` functions: `FromConfig`, `FromConfigWithOptions`, `FromConfigIfVaultPresent`, `…IfVaultPresentWithOptions`, `OpenIntelStore`, `BestEffort`, `ForWriteFromConfig`, `…WithOptions`, `…IfVaultPresent`, `…IfVaultPresentWithOptions`, `ForWrite`, `WithRecovery`, `WithRecoveryWithOptions`.
  - `pkg/anchors/sqlite/store.go:2379-2504` adds 6 more: `Open`, `OpenWithOptions`, `OpenReadOnlyExisting`, `OpenSessionStoreExisting`, `OpenWithDB`, `OpenWithDBContext`.
  - Plus `codeintel.OpenOptionalIntelStore`, `ontology.OpenStoreForWrite` (`pkg/ontology/runtime.go:73`), and `sqlitefixture.Open`.
  - There are 36 prod call sites of `OpenIntelStore*` across 11 packages.
  - Five of the 13 obsidian variants have **no prod caller** (DEAD-1).
- **Recommendation:** One opener with an options struct, `store.Open(ctx, vaultPath, store.Options{Mode: Read|Write|Session, IfPresent, Recover})`, in the store package. Delete the variants.

### DUP-2 — Config is re-read from disk per call through nine loaders, and vault-core owns other subsystems' config
- **Severity:** P2 · **Confidence:** High · **Effort:** M
- **Evidence:**
  - Nine loaders: `LoadLocalConfig`, `LoadLocalWorkflowConfig`, `LoadLocalConfigForWorkflowMigration`, `LoadCodeConfig`, `LoadEmbeddingsConfig`, `LoadCodeEmbeddingsConfig`, `LoadGraphConfig`, `LoadCliConfig` (all in `pkg/vault/obsidian`), and `config.LoadGlobalConfig`.
  - Each `Load*Config` calls `LoadLocalConfig`, which reads and decodes `.rhizome/config.yml` again (`pkg/vault/obsidian/local_config.go:382`; `code_config.go:11`; `embeddings_config.go:44`).
  - Prod call sites: `LoadLocalConfig` 28, `LoadCodeConfig` 26, `LoadEmbeddingsConfig` 10, `LoadGraphConfig` 3, `LoadCodeEmbeddingsConfig` 3. That is about 70 independent reads.
  - A single command or runtime can see config change mid-operation and pays repeated YAML decodes.
  - `obsidian` also has dead per-language normalizers (`NormalizePythonCodeConfig` … `NormalizePHPCodeConfig`; DEAD-2).
- **Recommendation:** Load config once into an immutable `vaultconfig.Resolved` (local, global, and each subsystem's merged section) at command or runtime start, and pass it down. The runtime reloads it on watcher events.

### DUP-3 — Parallel issue and warning DTOs
- **Severity:** P3 · **Confidence:** High · **Effort:** M
- **Evidence:** Field sets, as listed in each struct:
  - `ontology.ValidationIssue` (`pkg/ontology/index.go`): Code, NotePath, TypeName, FieldName, NodeRef, NodeID, Line, LinkKind, LinkTarget, Message, Fix*, Variant*.
  - `obsidian.OntologyIssue` (`pkg/vault/obsidian/health.go`): Code, NotePath, TypeName, FieldName, Line, LinkKind, LinkTarget, Message. **A strict subset of the previous one, with identical names.**
  - `validate.Issue`: Key, Code, Path, Type, Field, Source, Target, Message, Line, Data, Affected*, Location, Variant.
  - `semdb.ValidationDiagnostic`: IssueKey, Check, Code, Message, Evidence, PrimaryPath, Affected*, Type, Field, Source, Target, Location, ActionIDs, Variant. This is the persisted twin of `validate.Issue`.
  - `queryrecipe.Issue` and `viewconfig.Issue`: Code, Message, Path, Line, Field (+Recipe/View, Severity, Variant).
  - Three structurally identical `{Code, Message, Path}` warnings: `ontology/query.RuntimeWarning`, `ontology/pushdown.Warning`, `app/views.Warning`.
- **Recommendation:**
  - Introduce one `diag.Issue{Code, Severity, Message, Location{Path, Line, Field}, Data}` in a leaf package. `pkg/diagnostics` exists but is about process diagnostics; it could be renamed or a new package added.
  - Have `queryrecipe`, `viewconfig`, `pushdown`, `views`, and `query` warnings use it.
  - Delete `obsidian.OntologyIssue` in favor of `ontology.ValidationIssue`.
  - Keep `validate.Issue` and `ValidationDiagnostic` as the rich product types, but embed the common core.

### DUP-4 — Two parallel embedding stores
- **Severity:** P2 · **Confidence:** High · **Effort:** M
- **Evidence:**
  - `pkg/search/embeddings/sqlite` (notes, 2,279 LOC) and `pkg/search/embeddings/codeindex/sqlite` (code, 3,591 LOC; `store.go` 3,401 lines) share about 50 prod function names, for example `CacheEmbedding`, `ChunkHashes`, `CommitSyncGeneration`, `DeleteChunksNotIn`, `EnsureSchema`, `IncrementSyncGeneration`, `PruneStaleCache`, `ResetDomain`, `ResetDomainPreservingCache`, `SearchChunksByVector`, `SyncGeneration`, `ValidateOrInitMetadata`, `VisibleGeneration`, `bulkUpsertEmbeddingCacheTx`, `chunkVecTableName`, and `dropVecTablesByPrefix`.
  - Both expose the same 6-function open family (`Open`, `OpenWithOptions`, `OpenWithDB`, `OpenWithMetadata`, `…WithOptions`, `…WithDB`).
  - A shared `embeddings/internal/sqlstore` (164 LOC) exists but captures only DB opening.
- **Recommendation:** One generation-and-cache-aware vector store keyed by domain (`note` or `code`), with domain-specific item tables as a thin layer. The search reviewer should confirm the ranking-sensitive differences.

### DUP-5 — Agent service composition is done five times
- **Severity:** P2 · **Confidence:** High · **Effort:** M
- **Evidence:**
  - `agentapi.Config{…}` is constructed in `cmd/runtime_helpers.go:142` and `:280`, `cmd/serve_agent_op_hooks.go:138`, `pkg/app/bootstrap/codex.go:199`, and `pkg/app/web/handlers.go:358`.
  - The note-format runtime plus metadata indexer pair (`builtin.NewRuntime()` → `notemeta.NewIndexer`) is composed in prod at `pkg/app/bootstrap/codex.go:130`, `pkg/app/bootstrap/live.go:333`, and `cmd/note_metadata.go:27`, and again in **10** test packages (TEST-2).
  - Runtime acquisition paths: `bootstrap.NewLiveRuntime` (4 sites), `oneshotruntime` plans, `runtime.Ensure`, `ensureVaultRuntime`, `buildAgentOneShotRuntime`.
- **Recommendation:** One composition root, `bootstrap.AgentServices(rt, opts) agentapi.Config`, with a runtime-free variant. Add `notemeta.NewBuiltinIndexer()` as a prod constructor and use it in prod and tests.

### DUP-6 — Copied helpers, some with diverging domain semantics
- **Severity:** P2 for the domain copies, P3 for the stdlib ones · **Confidence:** High · **Effort:** S
- **Evidence (prod, same name defined in at least 4 packages):**
  - **`titleFromPath` ×8, with 6 distinct bodies.** `pkg/app/views/filters.go` strips only `.md` and splits on both separators. `pkg/anchors/frontmatter.go` normalizes `\`. `web`, `mcp`, `cli`, and `obsidian` strip any extension using the OS separator. So `notes/page.html` (HTML notes are supported) titles as `page.html` in views but `page` in mcp and web.
  - **`slugifyHeading` ×4, with 3 distinct bodies.**
    - `pkg/ontology/sections.go` and `pkg/app/web/ontology.go` are identical copies.
    - `pkg/ontology/sectionparse/sections.go` returns `""` instead of `"section"` for empty input.
    - `pkg/anchors/intel_util.go` only lowercases and replaces space, `/`, and `\`. So "What's new?" becomes `what's-new?` there but `what-s-new` in ontology. Heading slugs used as section identity diverge between code-intel and ontology.
  - **`preferredIdentifierField` ×5:** `validate`, `ontology`, `ontology/guide`, `ontology/idalloc`, `ontology/query`. All are the same loop over `NoteType.Fields`; it should be a method on `NoteType`.
  - **Stdlib replaceable:** `firstNonEmpty` ×14 and `firstNonEmptyString` ×7 → `cmp.Or` (Go 1.22+; module is 1.24). `dedupeStrings` ×7 and `uniqueStrings` ×4 → `slices.Sort` + `slices.Compact`. `sortedKeys` ×5 → `slices.Sorted(maps.Keys(m))`. `cloneMap` ×5 → `maps.Clone`. `containsAny` ×5 → `slices.ContainsFunc`.
- **Recommendation:**
  - One `paths.NoteTitle(path)` with a test table covering `.md`, `.html`, dotted names, and both separators.
  - One `markdown.HeadingSlug` used by ontology, sectionparse, anchors, and web.
  - `(*NoteType).PreferredIdentifierField()`.
  - Delete the stdlib reimplementations.

## Dead code clusters

Method: tokenized every non-test `.go` file (excluding `vendor/` and the top-level `web/`). An exported top-level func or type is "unreferenced" when its name appears in prod code only at its own declaration(s). Result: **138 of 3,543 exported declarations have no prod reference; 58 of those are referenced only by tests.** This is conservative: name collisions hide some dead code. `golangci-lint --enable-only unused` separately finds **23** unused unexported symbols.

### DEAD-1 — Store-open variants (`pkg/vault/obsidian/intel_store.go`)
- No callers at all: `OpenIntelStoreForWrite`, `OpenIntelStoreWithRecovery`, `OpenIntelStoreFromConfigWithOptions` (internal delegation only), `OpenIntelStoreFromConfigIfVaultPresentWithOptions` (internal only), and `OpenIntelStoreForWriteFromConfigIfVaultPresentWithOptions` (internal only).
- Test-only: `OpenIntelStore` (21 test calls, plus internal use by `BestEffort`) and `OpenIntelStoreForWriteFromConfigIfVaultPresent` (2 test calls).
- **Verdict:** delete as part of DUP-1. **Confidence:** High.

### DEAD-2 — Convenience wrappers left behind by `…WithWorkers` and `…WithOptions` twins
- `pkg/app/cli/tags_mutate_actions.go` and `properties_mutate.go`: `DeleteTags`, `DeleteTagsFromFiles`, `RenameTags`, `RenameTagsInFiles`, `AddTagsToFiles`, `DeleteProperties`, `RenameProperties`, and `SetPropertyOnFiles` have no prod caller; prod uses the `…WithWorkers` forms. Five are called only by tests, so tests exercise a wrapper rather than the code path prod uses.
- There are 40 `XWithOptions`/`XWithWorkers`/`XWithDB` twin functions in prod.
- **Verdict:** delete the wrappers and make the worker count an option with a default. **Confidence:** High.

### DEAD-3 — Other exported clusters with no prod reference
- **Confidence:** Medium. Verify each before deleting; some may be kept for build-tag variants.
- `pkg/anchors` (17): `RawExternalTargetMapping`, `ExternalSymbolAssociationEvidence`, `DocLinkStore`, `IndexedContextStore`, `NewCSharpIndexer`/`PythonIndexerAvailable`/`TSIndexerAvailable` (cgo/nocgo pairs with no caller), and `EdgePriority`/`AnchorNoteStore`/`NewPythonIndexer` (test-only).
- `pkg/vault/obsidian` (16): the five `Normalize<Lang>CodeConfig` functions, `CompileTagsRegex`, `MatchDocPattern`, `TagEditResult`, and `ExtractAllLinks`/`FirstMarkdownH1Title` (test-only).
- `pkg/app/mcp` (7 payload types): `GraphNodePayload`, `OrphansResponse`, `RelatedNotePayload`, `VaultContextResponse`, and `CodeSimilarity{Report,Row,Anchor}Payload`. These look like a retired tool surface.
- `pkg/app/cli` (13, including DEAD-2): `AuthorityScoresToPayload`, `CodeModeOutcomeForExit`, `CodeModeFailure`, `ExpandPatterns`, and `ParseInputs` (test-only).
- `pkg/validate` (7): `RunIdentifiersWithStore`, `ApplyDiagnosticNoteInventory`, `CheckHelpText`, `DuplicatePreferredIdentifierData`, and `RunSuiteOnce`/`RunCheck`/`RunBrokenLinks` (test-only).
- `pkg/search/embeddings` (6): `DefaultOllamaCodeConfig`, `DefaultVoyageCodeConfig`, `DefaultCodeIndexPath`, `NewIndexerWithDiscovery`, `RankRelated`.
- `pkg/search` (5): `IsLocalToExplicitSeeds`, `NormalizeEvidenceList`, `ApproxCandidateScore`, `ParseFilters`.
- `pkg/paths` (6): the deprecated `Resolve*Input` and `Resolve*Ref` shims (ARCH-7).

### DEAD-4 — Unused unexported code (golangci `unused`, 23 findings)
- **Confidence:** High. The Linux-only `pkg/sqliteutil/filesystem_mountinfo.go` hits are likely build-tag false positives on darwin.
- Examples: `pkg/app/bootstrap/live_health.go:96` `(*liveHealthTracker).degrade` and `:198` `stringSliceContains`; `pkg/app/bootstrap/live_ownership_paths.go:11` `mergeLivePaths`; `pkg/app/indexing/reverse_index.go:117,121` (`markReverseIndexBackfillComplete`, `shouldMarkReverseIndexComplete`); `pkg/search/service.go:891` `statusFromContext`; `pkg/app/repoexec/selection.go:19-20` (two env-var constants); `pkg/vault/obsidian/discovery.go:110`; `pkg/vault/obsidian/health.go:369`.
- Test-side: an unused `unifiedOwnershipTestStore` fake (`pkg/app/indexing/unified_ownership_integration_test.go:416-435`) and `recordingChunkWriter` (`pkg/search/semantic/syncer_test.go:373`).
- **Verdict:** delete, and turn on `unused` in `check-fast` (ARCH-7).

### DEAD-5 — Test doubles compiled into the shipped binary
- `pkg/llm/mock.go` `MockProvider` is selectable from user config (`provider: mock`, `pkg/llm/provider_factory.go:16`).
- `pkg/app/bootstrap/lane/fake.go` `Fake`/`NewFake` is used only by tests in 3 packages.
- **Verdict:** move to `_test.go` or a `lanetest` package; remove `mock` from the provider factory unless it is a documented feature. **Confidence:** Medium (the mock provider could be an intentional offline mode).

## Test-suite findings

Size: **262,899 test LOC vs 354,627 prod LOC (0.74)**, 1,279 test files, 6,713 `Test*` functions, 47 benchmarks. 47 files carry the `integration` build tag: `tests/integration/*` (41) plus `pkg/anchors` (3), `pkg/validate` (2), and `pkg/vault/cache` (1). There are 5,952 LOC under `tests/`. 1,155 of 1,236 `pkg`/`cmd` test files (93%) are internal-package tests.

Assertion quality is good, which is worth recording so the refactor does not churn these areas:
- Only 53 tests assert nothing but `NoError`/`Nil`, and most are legitimate concurrency, panic-recovery, or "does not fail" regressions.
- Only 23 tests have no assertion, and all are subprocess helpers or wrappers.
- Constant-pinning tests are essentially absent: one, `pkg/app/agentcode/agentcode_test.go:71` `require.Equal(t, "4", GeneratorVersion)`.
- All 132 `t.Skip` calls are environmental (symlinks, Unix-only scripts, git or node missing, opt-in env).
- Golden files are few and small (10 files, all under 50 KB).

### TEST-1 — No shared testkit: 72 independent temp-vault builders
- **Verdict:** add (testkit), then merge
- **Evidence:**
  - 72 test helpers in 25 packages call `t.TempDir()` and write vault content. All have distinct names; for example `setupAgentTestVault` (`cmd/agent_test.go:1606`), `writeValidationProjectionVault`, `writeNodeLinkTestVault`, `setupNamespaceCommandVault`, `writeTypeViewsFixture`, `setupAgenticEngineeringRecipeVault`. The largest concentrations are `pkg/validate` 12, `cmd` 7, `pkg/app/web` 6, and `pkg/app/indexing` 6.
  - Repo-wide there are 2,921 `t.TempDir()` calls, 2,304 `os.WriteFile` calls, and 142 inline writes of `.rhizome/config.yml`.
  - `setupAgentTestVault` alone does seven jobs: mkdir, files, config, db, `obsidian.json`, global seam override, and cleanup that resets cmd globals.
  - The one curated workspace helper, `tests/integration/internal/fixture.NewWorkspace` (306 LOC), sits under `internal/` and cannot be imported by `pkg/` tests.
  - `pkg/testutil/sqlitefixture` (74 LOC; used by 7 packages) and `pkg/testutil/indexingworkload` (40 LOC) are the only shared pieces.
- **Recommendation:** See "Proposed shared testkit" below.

### TEST-2 — Copy-pasted helpers and fakes
- **Verdict:** merge
- **Evidence:**
  - `testNoteMetadataIndexer` is defined in **10** packages (`tests/integration/python`, `pkg/app/{bootstrap,web,cli,cli/init,indexing,views}`, `pkg/ontology`, `pkg/ontology/noderead`, `pkg/ontology/query`) with 4 slightly different bodies. `descriptorOnlyHTMLNoteMetadataIndexer` appears in 4 packages.
  - There are 129 test-local fake, stub, or recording types. `NoteReader` is faked 10 times, plus `mocks/note.go`. `Provider` and `Store` are faked 5 times each.
  - About 19 store-opening helpers have distinct names (`openTestStore`, `newIntelStore`, `setupIntelStore`, `openTestNotesDB`, …).
- **Recommendation:** Add a prod `notemeta.NewBuiltinIndexer()` (DUP-5). Provide a canonical `testkit.FakeNoteReader` (or an in-memory `NoteReader` over a map) and `testkit.FakeEmbeddingProvider` (deterministic vectors).

### TEST-3 — Global seams force serial tests
- **Verdict:** rewrite (inject instead of overriding)
- **Evidence:**
  - Tests reassign three package-level variables **156 times**: `obsidian.ObsidianConfigFile` 47 times in 10 files, `obsidian.CliConfigPath` 62 in 18, and `vaultconfig.UserHomeDirectory` 47 in 16.
  - `cmd` tests reset 190 global flag and state variables (`resetAgentTestGlobals`, `resetFlags(rootCmd)`: 41 sites).
  - `t.Parallel()` is rare in the biggest packages: `pkg/ontology` 4 of 432 tests, `pkg/anchors/sqlite` 6 of 390, `pkg/validate` 34 of 446, `pkg/app/cli` 34 of 280.
  - CI already shards unit tests four ways to cope (`.github/workflows/ci.yml:82-84`: `cmd|validate`, `anchors|app/{indexing,mcp,bootstrap,views,codeintel,runtime}`, `app/{web,cli}`, rest).
- **Recommendation:** Replace the three globals with a `vaultconfig.Env{HomeDir, ObsidianConfig, CliConfig}` value resolved once and passed in (fits DUP-2). Have the testkit build it. Then mark the store-only packages (`ontology`, `anchors/sqlite`) parallel. These are the cheapest large wins for suite wall-clock.

### TEST-4 — Prose pinning of templates, schema guidance, and live repository docs
- **Verdict:** rewrite most; keep the structural guards
- **Evidence:**
  - 347 `Contains`/`NotContains` assertions match a literal of at least 5 words. By package: `pkg/app/cli/init` 141, `pkg/ontology/guide` 45, `cmd` 27, `pkg/app/cli` 27, `pkg/ontology` 22, `pkg/ontology/query` 19.
  - `pkg/app/cli/init` tests alone contain 629 `Contains`/`NotContains` assertions (`run_test.go` 235, `agent_surfaces_test.go` 91, `helper_templates_test.go` 70).
  - Examples that pin wording rather than a contract:
    - `helper_templates_test.go:646-665` `TestRhizomeManagedBlockIsLeanIntegrationRouter` pins 7 sentences of the managed block, such as "Never silently substitute generic shell behavior".
    - `helper_templates_test.go:278` `TestCoreSkillCreatorStaysTokenDense` asserts "Rhizome integration is opt-in" and "general skill-authoring guidance" (the line budget in the same test is a real constraint).
    - `pkg/ontology/schema_test.go:179-187` pins guidance prose such as "Controls whether the spec is still evolving".
  - Tests that read **the live repository**, not fixtures:
    - `pkg/ontology/docs_test.go:130` `TestSchemaDocs_SpecDrivenSchemaCarriesLayeredGuidance` loads this repo's `.rhizome/ontology` and pins phrases like "real human approver".
    - `pkg/ontology/docs_test.go:245` `TestProcessDocsUseClosureChecklistContract` reads `docs/specs/process/compounding-work.md`.
    - `pkg/app/cli/init/helper_templates_test.go:667` walks the live `.agents/skills` and `.claude/skills`.
    - `pkg/ontology/guide/render_test.go:357,383`, `pkg/ontology/queryrecipe/queryrecipe_test.go:468,508`, `pkg/ontology/query/effort_contract_test.go:20`, `pkg/ontology/idalloc/allocator_test.go:426`, `pkg/app/cli/init/run_test.go:1349`, and `pkg/app/web/server_integration_test.go:4226`.
    - In total, 15 test files resolve the repo root.
- **Judgment:**
  - **Real contracts worth keeping:**
    - `TestSkillTemplateReferencesAndRecipesAreReachable` (link and recipe reachability).
    - `TestStarterTemplateScaffoldsHaveNoCrossTemplateConflicts`.
    - Retired-name absence: `TestRepositoryLiveAgentSurfacesDoNotRouteToRetiredCoreSkills` and the `NotContains` lists.
    - Line and token budgets.
    - "init output contains the managed block markers and required sections".
    - `pkg/ontology/docs_test.go`'s `RenderReferenceMarkdown_*` tests, which assert that rendered output reflects the input schema. That is behavior.
  - **Change detectors:** sentence pins inside templates and live-repo guidance. They duplicate what `rzm validate` and `rzm init --check` are designed to guard, and they fail on every copy edit.
  - Tests that read live `docs/` or `.rhizome/` couple package unit tests to the repository's own content. A docs edit breaks `go test ./pkg/ontology`.
- **Recommendation:**
  - Replace prose pins with structural assertions: headings or sections present, required links resolve, forbidden names absent, size budgets.
  - Move live-repository content checks into one `tests/repo_content_test.go` (or into `rzm validate` checks run by `rzm ci`) so they are named as content gates.
  - Keep the behavior tests that use temp fixtures.

### TEST-5 — Heavy white-box coupling in app packages
- **Verdict:** rewrite selectively (prefer the exported or HTTP surface)
- **Evidence:**
  - Tests call **1,083 distinct unexported prod functions** (versus 1,886 exported). By package (unexported vs exported functions called): `pkg/app/web` 132 vs 40, `cmd` 117 vs 33, `pkg/app/cli/init` 92 vs 9, `pkg/validate` 87 vs 71, `pkg/app/indexing` 55 vs 43, `pkg/app/bootstrap` 54 vs 27, `pkg/app/views` 45 vs 7.
  - `pkg/app/web` already has a 4,349-line HTTP-level `server_integration_test.go`, so many handler-internal tests duplicate behavior that the HTTP tests could cover.
- **Recommendation:** During the ARCH refactors, rewrite tests of packages being split against the exported or HTTP surface first, so the split does not have to drag hundreds of internal-call tests along. Do not mass-rewrite stable domain packages (`ontology`, `anchors/sqlite`); their internal tests are mostly parser and algorithm table tests and are fine.

### TEST-6 — `time.Sleep` used as synchronization
- **Verdict:** rewrite (inject a clock or debounce hook)
- **Evidence:**
  - 72 `time.Sleep` calls in 38 test files. Most are "nothing happens within window" negative checks or watcher settling.
  - `pkg/vault/cache/integration_test.go:63-305` uses `Sleep(eventDelay)` 13 times.
  - `pkg/app/cli/serve/readiness_test.go:78,96,114,129` sleeps `2–3 * validationRefreshDebounce` after `Eventually`.
  - `pkg/app/indexing/unified_streaming_test.go:614,622` sleeps `unifiedStreamingBatchIdle+25ms`.
  - `pkg/app/indexingpipe/files_test.go:194` sleeps 200 ms.
  - `pkg/app/update/*_test.go` sleeps 100 and 200 ms.
  - Only 16 of the sleeps sit inside a polling loop.
- **Recommendation:** Inject the debounce and idle durations, or a fake clock, into the readiness coordinator and the unified streaming coordinator so the negative windows are deterministic. Keep the watcher-integration sleeps behind the `integration` tag; they already are for `pkg/vault/cache`.

### TEST-7 — Thin or missing tests in important packages
- **Verdict:** add (targeted)
- **Evidence (own-package coverage measured with `go test -cover` on 12 packages; ratio = test LOC / prod LOC):**
  - `pkg/app/repoexec` **7.3%** (602 prod LOC; ratio 0.09). It selects and executes the repo-pinned `rzm` binary. `cmd/repo_delegate_test.go` covers some of it, but the decision table (`DecisionFor`, `Bootstrap`) is security-relevant.
  - `pkg/sqliteutil/migration/domains` **10.3%** (ratio 0.09). Migration plans are mostly covered indirectly by `pkg/anchors/sqlite` migration tests (7 files).
  - `pkg/app/agentstart` 27.5%.
  - `pkg/llm` 45.2%.
  - `pkg/ontology/pushdown` 54.1% (ratio 0.32). The graphql-query note requires pushdown to stay equivalent to in-memory evaluation; an equivalence property test belongs here (graphql-query reviewer's lane).
  - No tests at all: `pkg/ontology/sectionparse` (349 LOC, a parser; 1 indirect reference), `pkg/ontology/readmodel` (DTOs; fine), `pkg/app/searchengine` (80 LOC), `pkg/search/embeddings/codeindex` (types; fine), `pkg/validate/catalog` (91 LOC; lookup table).
  - Low ratio despite size: `pkg/search/embeddings/codeindex/sqlite` 0.43 and `pkg/search/embeddings/sqlite` 0.45 (DUP-4 will change these anyway).
- **Recommendation:** Add a decision-table test for `repoexec`. Add a table test for `sectionparse`, or fold it into `ontology`, where its slug divergence (DUP-6) would surface.

### TEST-8 — An opt-in spec evidence test never runs in CI
- **Verdict:** add CI wiring or retire it
- **Evidence:** `cmd/agent_code_runtime_fixture_test.go:31` `TestNodeScriptCallsRunInOneVaultRuntime` is described as "SPEC-0104 US4's evidence". It skips unless `RZM_CODE_MODE_RUNTIME_FIXTURE` is set. `make test`/`test-fast` set it only when a dev binary already exists, and `.github/workflows/ci.yml` never sets it. Other env-gated tests (`RHIZOME_HARNESS_LIVE`, `TYPESAFE_LIVE_TEST`) are intentionally live-only; `typesafe-live.yml` covers the latter.
- **Recommendation:** Build `rzm` with `NO_WEB=1` in one CI job and set the variable there, or downgrade the spec's evidence claim.

### TEST-9 — Struct-literal equality pins full DTO layouts
- **Verdict:** keep; rewrite opportunistically
- **Evidence:** 518 `require.Equal(t, T{…}, got)` struct-literal comparisons and 21 `reflect.DeepEqual`. Most compare small value types. For agent-facing payloads this is the intended contract ("output shapes are an API"). For internal intermediate structs, it makes the DTO consolidation in DUP-3 noisier.
- **Recommendation:** None up front. When DUP-3 or ARCH-1 changes a DTO, convert the affected internal assertions to field-level checks rather than re-pinning the new layout.

## Proposed shared testkit

Location: `internal/testkit` at module root. It must be importable by both `pkg/...` and `tests/integration/...`. `tests/integration/internal/fixture` is not importable from `pkg/`. No build tags.

What it provides (each item names the helpers it replaces):

1. **`testkit.Vault(t, opts...) *Vault`**: a temp vault root with `.rhizome/config.yml` (default `notes: {}`) and an optional schema. It returns `{Root, Name, Env}`. Options: `WithFiles(map[string]string)`, `WithConfig(yaml)`, `WithSchema(graphql)`, `WithStarter(id)` (copies a starter's ontology), `WithCopyOf(testdataDir)` (absorbs `fixture.NewWorkspace`/`CopyDir`).
   - Replaces the 72 vault builders (TEST-1) and most of the 142 inline config writes.
2. **`v.Env() vaultconfig.Env`**: home, obsidian config, and CLI config paths scoped to the test, with vault registration under `Name`. No global mutation.
   - Replaces 156 global overrides (TEST-3). Depends on the `vaultconfig.Env` injection change.
3. **`v.Store(t, mode)`**: opens the unified store through the single prod opener (DUP-1), with cleanup.
   - Replaces `sqlitefixture.Open` and about 19 `open*Store`/`new*Store` helpers.
4. **`v.Index(t)`**: runs the structural index (`indexcore` discover and publish) synchronously, without provider-backed lanes.
   - Replaces bespoke "write the vault, then build the ontology index" sequences in `pkg/app/indexing`, `pkg/app/web`, `pkg/app/views`, and `pkg/validate` tests.
5. **`testkit.NoteMetadataIndexer(t)`**, a thin wrapper over the new prod `notemeta.NewBuiltinIndexer()`.
   - Replaces the 10 `testNoteMetadataIndexer` copies and the 4 `descriptorOnlyHTMLNoteMetadataIndexer` copies.
6. **Canonical fakes:** `FakeNoteReader` (map-backed), `FakeEmbeddingProvider` (deterministic hash vectors, configurable dimensions), and `lanetest.Fake` (moved out of prod `lane/fake.go`).
   - Replaces about 25 of the 129 test-local fakes and `mocks/` (180 LOC, still on `testify/mock`).
7. **`testkit.CLI(t, v).Run(args...)`** for `cmd` tests, once flags are per-command. It returns stdout, stderr, and the exit code.
   - Replaces `setupAgentTestVault` plus the `resetFlags`/`resetAgentTestGlobals` pattern (41 sites).

Non-goals: no generic assertion DSL, and no new mocking framework. Keep `require`.

## Suggested refactor sequence

The order minimizes rework: each step either removes a dependency the next step would otherwise have to carry, or adds the safety net the next step needs.

1. **Gates first (S).** Wire `golangci-lint` (`unused` and staticcheck SA only) into `check-fast` and CI. Delete DEAD-4. Add a 500-line ratchet listing current offenders. Add a `web`/`views` subsystem note (ARCH-6). *Prevents new debt during the refactor.*
2. **Cut the upward edges (S).** Do the ARCH-3 moves: the `filterexpr` leaf, the `textdiff` leaf, moving `perfworkload` to `tools/`, the planner packer port, the contextpack constant, and moving `NewCodeAnchorService`/`ResolveRoots` from `bootstrap` to `codeintel`. *Each is a small mechanical PR and immediately shrinks closures (`pkg/search` 62→37).*
3. **Testkit, step 1 (M).** Introduce `internal/testkit` items 1, 5, and 6. Add prod `notemeta.NewBuiltinIndexer()`. Migrate the 10 `testNoteMetadataIndexer` copies. *A safety net before the big moves, and it proves the API.*
4. **Config and env injection (M).** DUP-2 (load once into `vaultconfig.Resolved`) together with the `vaultconfig.Env` seam (TEST-3). Add testkit item 2. Move the per-subsystem config types out of `obsidian`. *Unblocks parallel tests and is the first half of ARCH-2.*
5. **Single store opener (M).** DUP-1 and DEAD-1. Move the opener out of `obsidian`. Add testkit item 3. *The second half of ARCH-2; `obsidian` drops sqlite, embeddings, and llm from its imports.*
6. **The store split (L, several PRs).** ARCH-1: rename to `pkg/store`, then extract domain sub-stores one at a time (validation snapshots, session, graph, ontology, note metadata). Move the `IntelOntology*` DTOs to `ontology/readmodel`. Move the `cmd/code_stats*` SQL into store methods. Update the stores note's code paths. Afterward, fold the ARCH-4 leaves (`intentstore`, `sectionparse`).
7. **App-layer consolidation (M).** The package-merge table above (`semanticops`, `searchengine`, `agent`, `agentstart`, `mcpserve`×2, `runtimestop`, `indexcore`+`indexingpipe`). One agent-services composition root (DUP-5). Rename `pkg/app/cli` per ARCH-8. Move `ValidationProjection*` out of `indexing`.
8. **Thin `cmd` (M).** Per-command option structs instead of 190 globals. Move the ontology-query provider wiring to the app layer. Add testkit item 7. `cmd` tests become parallel-safe.
9. **Embedding store unification (M).** DUP-4, with the search reviewer's ranking regression suite as the gate.
10. **Opportunistic cleanups, alongside the above:**
    - DUP-6 helper consolidation. The `titleFromPath` and `slugifyHeading` fixes need regression tests because they change behavior.
    - DUP-3 issue DTO core.
    - TEST-4 prose-pin rewrite and moving live-repo checks to a content gate.
    - TEST-6 clock injection.
    - TEST-7 `repoexec` table test.
    - TEST-8 CI wiring.
    - Deleting the deprecated `pkg/paths` shims (31 call sites).

## Application consolidation disposition, 2026-10-17

The audit measurements above describe the inspected baseline. The application implementation now has these owners:

| Recommendation | Disposition and evidence |
| --- | --- |
| ARCH-5 mutable CLI flag state | Fresh command trees and command/family option values in `cmd/root.go`, `cmd/command_constructors.go`, and `cmd/command_options.go`. Production and test execution bind the factory Env to the actual cancellation context. Pager lifetime belongs to the invocation; list diagnostics use an explicit writer. Global flag/command reset helpers are removed. A/B/A vault, repeated array/slice, default/explicit-false, session alias, inherited/required-flag and cancellation fixtures live in `cmd/command_invocation_test.go`. |
| ARCH-8 shared application ownership | Top-level `pkg/app/cli` capabilities are `pkg/app/operations`, consumed by CLI, MCP, and web. CLI-specific init, serve, and diagnostics workflow subpackages retain their ownership. Current importers and maintained guidance use the new paths. |
| DUP-5 agent configuration | `pkg/app/agentservices.Compose` owns the populated Config composition. All five production callers supply one atomic runtime snapshot's vault/configuration pair plus explicit dependencies and policy. `bootstrap.NewBuiltinNoteRuntime` returns the explicit format-runtime/indexer pair without opening stores or starting work. |
| Planner and agent startup packages | Planner/parser belong to `agentchat`; startup belongs to `agentcode`. Startup retains the provider-hash-aware indexed ontology summary proof. |
| Two MCP serving packages | `pkg/app/mcpserve` owns request preparation and transport. Actual JSON-RPC fixtures preserve request handling, write admission, cleanup, and bound recipe planning. |
| Runtime stop wrapper | `pkg/app/runtime/stop_command.go` owns stop orchestration beside the runtime shutdown protocol. CLI, desktop, and integration callers are migrated. |
| `indexcore` plus `indexingpipe` | Adjusted with dependency evidence: `indexcore` imports `codeintel`, which consumed `indexingpipe`; a literal merge creates a cycle. Candidate workers/types now belong to their codeintel ingestion owner. Shared indexcore Discover/Publish remains. |
| Validation projection placement | Projection, postapply, and node-link application helpers belong to `validationproduct`. Shared lease/lane contracts and the indexing lock waiter remain explicit. Source admission and repair lock waiting stay with their source-worker owner. |
| Already completed or separate owners | Semantic wrapper packages were removed by the search owner. Ontology providers already live in `queryengine/runtimeproviders`; code-stats SQL belongs to the store owner. No duplicate extraction was introduced. |

Focused command isolation and suppression fixtures pass. Integrated checks, the compiled CLI/runtime fixture, PR review, and final audit closure remain with the coordinating root task.
