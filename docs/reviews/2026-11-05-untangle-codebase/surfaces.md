# Agent/CLI/application surfaces structural audit

Revision inspected: `900eee0b0036ee2699d84d5d02d5ffe6a39a4441`. Read-only audit; no application files edited and no tests executed. Ownership covered `cmd`, `pkg/app/{agentapi,agentservices,agentcode,agentchat,mcp,mcpserve,contextpack,answer,presentation,operations,actions,tasks,finder,runtimeview}`. Inventoried 650 Go/source-related files, read the owning CLI/MCP/agent-surface guidance and focused on shared contracts, high-coupling composition, graph commands, action execution, and representative tests. This is a structural audit, not an exhaustive proof that every function is clean.

## S1. Share graph projection and collapse below CLI and HTTP

**Confirmed, high value.** `cmd/graph_web.go:271-452` independently converts `noderead.Graph` to `web.GraphNode`/`GraphEdge`, computes modules, collapses nodes, and remaps edges. `cmd/graph_web.go:337` explicitly labels the helpers as duplicated from web. Corresponding implementation lives at `pkg/app/web/ontology_graph.go:14-121` and `pkg/app/web/graph_collapse.go:39-197`. The command already depends on the HTTP package for response DTOs, so it pays the coupling cost without sharing the policy.

The copies have drifted: CLI `nodeLang` returns an empty language for code while web detects it from the path; CLI `moduleKey("file.go",2)` yields `root` while web yields `file.go`; CLI outputs map iteration order while web explicitly sorts; web gives embedded nodes a distinct `embedded:<id>` module but CLI groups them by file path. These are verified source differences, not a claim that every difference is unintended product behavior. Both collapse paths currently discard relationship metadata when aggregating edges, so that is not a CLI-only regression.

**Smallest coherent redesign:** Extract the shared DTOs and pure graph projection/module/collapse functions into an application-level graph-presentation owner, then use that owner in both fronts. Keep web-specific cache, readiness, community/score enrichment, notes-only behavior, and node-ref selection at the HTTP boundary. CLI remains argument parsing, explicit persisted target resolution, and rendering. Define the intended module/language/sorting behavior once before moving it; do not silently change those outputs under a mechanical move.

**Reach:** `cmd/graph_web.go`, `cmd/graph_web_test.go`, `pkg/app/web/{ontology_graph.go,graph_collapse.go,types.go,graph.go,graph_test.go}` and consumers of graph DTOs. Owning docs: CLI, web, noderead, nearest CONTEXT notes. No stored-data migration.

**Verification:** Same literal graph fixture through CLI and HTTP preparation; root-level file, nested file, embedded node, ignored node, language, large module collapse, edge endpoint remapping, stable order. Retain graph cache cancellation tests and persisted catalog target tests. Run focused cmd/web/noderead tests, then applicable full gates.

## S2. Move repository trust decisions out of scheduled tasks

**Confirmed, medium value.** `pkg/app/actions/actions.go:22,61-69` imports `pkg/app/tasks` only for `tasks.Untrusted`. The generic repository-code trust decision is defined at `pkg/app/tasks/runner.go:416-441` and uses the scheduler's `OpenRepo`/`Repo` from `pkg/app/tasks/git.go:30-56`. Adding or reviewing module actions therefore pulls in scheduling, inbox, draft and harness concepts for one repository trust check. The stable owner already exists: `pkg/repositorytrust/store.go` owns canonical checkout resolution, linked-worktree inheritance and trust records.

**Smallest coherent redesign:** Put generic trust-decision formatting/checking in `repositorytrust`; actions call it directly and tasks adapt their already-resolved checkout. Keep `tasks.Repo`, state directories and Git-history clocks in tasks. Preserve current root versus subdirectory resolution, bare repositories, linked worktrees, non-Git folders, and exact denial/error messages. Do not simply replace the function with a check of an arbitrary subdirectory, because that could change the trusted identity.

**Reach:** actions implementation and `actions_test.go:108-120`; tasks `runner.go` and `runner_test.go:280`; repositorytrust code/tests; any new caller found by `rg 'tasks.Untrusted|Untrusted\('`. Existing Store.Trusted inheritance is reusable. No trust-store format changes, no credential changes.

**Verification:** Trusted and denied primary checkout, linked worktree, bare checkout, non-Git directory, unreadable trust store, cancelled lookup. Keep the public action refusal test and task refusal test, adding trust-owner cases instead of cloning all cases in both workflows.

## S3. Colocate each code-operation contract instead of joining independent name maps

**Confirmed, medium/high value.** One operation is assembled across `agentapi/catalog.go:90-153` (identity/routing/access), `code_contracts.go:116-235` (input/example/effects/CLI leaves plus a second output-schema switch), `code_contracts.go:239` (response type map), `code_contract_metadata.go:27-205` (field descriptions/defaults), and `code_contract_validation.go:28-89` (name-switched conditions). Every addition or condition change requires knowing this distributed join. That is hidden co-change despite the final exported catalog being authoritative.

Concrete drift: `code_contracts.go:131` describes semantic-query continuation as “provide queries for a first page or continuationToken for a later page”, but `code_contract_validation.go:46-48` requires the queries on every page. Field metadata has the more accurate policy. This is evidence that descriptive and executable conditions currently change separately.

**Smallest coherent redesign:** Build a complete per-operation descriptor/contract at one named construction site, including output shape, descriptive metadata, access and the small extra input predicate. Group construction by domain to keep files bounded. Keep the reusable JSON-schema validator and final immutable catalog. Do not invent a plugin registry or make the dispatch dynamic. Preserve exported JSON schemas, methods, hashes, output types, ordering and existing conditional validation; correct the stale continuation description as an intentional diagnostic/documentation fix.

**Reach:** the five agentapi files above plus catalog/contracts/discovery tests. Code-mode generated clients and `cmd/agent_surface.go` are consumers, not duplicate owners. Owning docs: agent-surface/MCP CONTEXT. A description-only hash change is expected, but runtime request rules should remain identical.

**Verification:** Before/after snapshots of all descriptor names, surface order, input/output schemas and method names; literal accepted/rejected conditional requests (continuation, note move, view, user set); real describe/generated-client smoke. Retain explicit public-contract expected values rather than making tests derive expected schemas from the new constructor.

## S4. Delete pure forwarding names left by earlier extraction

**Confirmed, small value, independent of larger redesign.** `cmd/code.go:628-638` wraps `codeintel.ResolveRoot`, `ValidateNotes`, and `DetectCodeLang` without policy. Callers are only in `cmd/code.go`, `cmd/code_overview.go`, and `cmd/code_relatedness.go`. `cmd/code_anchor_service.go:8-12` aliases a config and forwards `codeintel.NewCodeAnchorService`; its callers are `cmd/code.go` and `cmd/code_anchors.go`. `pkg/app/operations/code_mode_workflows.go:31` wraps exported `CodeModeSuccess` for internal callers even though the exported function is already the owner.

**Smallest fix:** Replace callers with the existing owner function/type and delete these wrappers. Remove `code_anchor_service.go` if empty. Replace the internal lower-case code-mode success calls with `CodeModeSuccess`. These seams provide no customization or independent tests.

**Reach:** files above plus `operations/code_mode_{local_actions,read_workflows,workflows}.go`. Existing package imports may become unused; no schema or user behavior change. Focused package tests plus check-fast are sufficient before integrated gates. Do not generalize this into deleting meaningful interface adapters such as `commandCodeModeViewFactory`, `codeModeWorkflowOutcome`, or `presentation.AdaptCodeIndex`.

## Selected optional cleanup

### Comments

Remove narration when touching these owners: `cmd/graph_web.go:53` (“Resolve vault”), `:75` (“Load code config”), `:84` (“Open intel store”), `:93` (“Load vault definition and ignore matcher”), `:102` (“Create a minimal graph builder”), `:141` (“Summary output”), and the edge/node count labels around `:147-170`; `cmd/code.go:474` (“Show indexer version status”). Preserve explanations of persisted graph ownership, security/trust policy, runtime read-only behavior, compatibility contracts, and test fixture platform constraints.

### Tests

No test was proven useless or falsely failing in this read-only pass, so no blanket deletion is justified. Two test-design fixes remove incidental wall time without reducing coverage: `tasks/runner_test.go:249` and `tasks/review_fixes_test.go:300` sleep 1.1 seconds to order Git timestamps against item creation. Supply deterministic Git author/committer dates in the fixture and use the existing `Options.Now` seam for the scheduler. Keep the observable merged-work closure assertions. `runner_test.go:224-227` polls until a fake agent starts with no bound; replace the fake's start notification with a channel and bounded select so a regression fails locally instead of hanging until the suite timeout. These are test-harness improvements, not reproduced flakes.

## Retained exceptions / counterevidence

- Agent `CallOutcome` and operations `CodeModeOutcome` are intentionally separate to avoid a dependency cycle; the explicit adapter is not enough benefit to justify a new shared-envelope package.
- `agentservices.Compose` and `runtimeview.View` are useful ownership boundaries, not pass-through surplus: they capture a consistent configuration snapshot while retaining fresh independent runtime phases.
- CLI versus HTTP readiness, caching and read-write policy differ intentionally. They should not be unified while extracting pure graph policy.
- `answer.NodeRef`/`LinkTarget` are detached provenance DTOs and should not acquire ontology resolution behavior.
- `answer/answer.go` is 1,120 lines and can be split by selection versus rendering/coverage, but file length alone is not evidence that a larger redesign is needed; do that only as part of an otherwise justified edit.
- Conditional platform/Node/symlink skips are legitimate environment prerequisites. Do not remove tests because this checkout lacks those facilities.
- `cmd` code-context and code-symbols currently have different on-demand indexing/error policies. Shared preparation is promising, but their intended differences need a separate contract decision; do not erase them under wrapper cleanup.

## Verification limits

No tests, live UI, generated client, paid provider, or existing local personal state were exercised. Findings above distinguish confirmed structural duplication from behavior choices requiring plan approval. I did not inspect every body in the ownership inventory and do not claim that these are the only worthwhile findings.
