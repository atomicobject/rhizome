---
summary: "Second untangle pass: runtime audit and concrete candidate evidence."
reference-kind: analysis
---

# Second untangle pass: runtime and agent surfaces

Baseline: `b0d0cf824a9b7b7fc9e1832fd31aa5e7555e2f0e`; inspected HEAD `2a9541828` adds review documentation only. Read-only source audit. No repository edits, tests, builds, provider calls, commits, or subagents.

The remaining useful work is smaller than the first pass: share graph-path preparation, centralize the Go event vocabulary, remove several uncalled APIs, and tighten one task-output boundary. Preserve the first-pass architecture and lifecycle fixes.

## R2-RT-1: Share graph-path fact preparation

**Confirmed hidden link; should fix.** `cmd/graph_path.go:106-179` and `pkg/app/mcp/tool_graph_path.go:85-159` independently implement the same graph-fact-to-shortest-path conversion, alias inventory, canonical target selection, ontology relation naming, and inferred/extracted confidence rule. Their bodies differ only in helper/local names. Callers are `runGraphPath` at `cmd/graph_path.go:78,92-93` and `GraphPathTool` at `pkg/app/mcp/tool_graph_path.go:53,70-71`; `cmd/graph_surprises.go:124` also uses the confidence helper.

These pieces must change together whenever a new endpoint identity or alias is introduced. A fix made only at one front gives human and agent callers different reachable nodes. This is distinct from the already-shared graph visualization/collapse behavior.

**Minimum fix:** move the pure preparation and canonical lookup to the existing `pkg/app/graphpresentation` package, which already depends on `noderead`; reuse its confidence conversion from the surprise command too. Keep CLI friendly output, MCP errors, readiness, store ownership, and shortest-path execution at their current callers. Preserve current edge order and alias collision behavior; do not sort or redefine either incidentally.

**Verification:** retain both public graph-path tests (`cmd/graph_path_test.go:19`, `pkg/app/mcp/tool_graph_path_test.go:22`), and add one literal preparation case covering a note alias, embedded/code endpoint, missing/self edge, ontology relation, and fractional confidence. No new graph policy is needed.

## R2-RT-2: Delete unused file-target tag mutation entrypoints

**Confirmed surplus; should fix.** `pkg/app/operations/tags_mutate_actions.go:34` (`DeleteTagsFromFilesWithWorkers`) and `:104` (`RenameTagsInFilesWithWorkers`) have no references outside their own declarations/comments across repository source, tests, or docs. They repeat the active validation/lease/path setup and mutation execution, but neither CLI nor agent handlers expose these paths. The live entrypoints are `DeleteTagsWithWorkers`, `RenameTagsWithWorkers`, and `AddTagsToFilesWithWorkers` in the same file.

**Minimum fix:** remove only the two uncalled functions and their comments. Do not merge the whole-vault and explicit-file modes or weaken their pre-write validation and shared mutation lease. There are no obsolete direct tests to remove.

**Verification:** package/command tag mutation tests, including dry runs, invalid tags, rename-to-self, and mutation locking. Repository non-use is established; external Go consumers cannot be established by this audit, so any approved retirement has the same external API caveat as first-pass retirements.

## R2-RT-3: Retire four unconsumed LiveRuntime accessors

**Confirmed surplus; should fix.** `pkg/app/bootstrap/live_capabilities.go:148` (`SearchReady`), `:171` (`CodeIndexReady`), `:288` (`EmbeddingsConfig`), and `:293` (`CodeEmbeddingsConfig`) have no references outside their declarations/comments. Current consumers use the fresh `Snapshot`, phase-specific waits, `Configuration`, and the live store/provider accessors. The four methods preserve parallel public representations that no caller consumes.

**Minimum fix:** delete these four methods only. Retain `SemanticReady`, which has callers, the underlying readiness channels/errors/configuration fields, and every active wait/snapshot method. Update the package entry map only if it names a removed method.

**Verification:** existing bootstrap configuration/readiness tests and command/MCP compilation. This is API retirement, not a readiness-policy change; external-consumer caveat applies.

## R2-RT-4: Give MCP environment parsing one shared owner

**Confirmed hidden link; should fix.** `pkg/harness/claude/options.go:108-118` and `pkg/harness/codex/options.go:65-75` have byte-identical `envMap` implementations. Their callers are Claude `mcpConfig` (`options.go:95`) and Codex `appServerArgs` (`options.go:53`). Both consume the same `harness.MCPServer.Env` representation (`pkg/harness/harness.go:45-49`), splitting at the first equals sign, rejecting absent/blank keys, and keeping the final duplicate value.

**Minimum fix:** put that conversion beside the shared `MCPServer` contract, such as a narrow `MCPServer.Environment` method, and have both existing vendor encoders call it. Preserve the vendor-specific server-name restrictions, wire encoders, wrapped error text, permission mapping, and launch arguments. Do not broaden this into a shared vendor options or session framework.

**Verification:** retain both public invalid-MCP-server and option/argument tests. Add literal shared cases for an empty value, equals signs inside a value, duplicate keys, missing equals, and blank keys; validate resulting encoded vendor arguments separately.

## R2-RT-5: Check EOF after a task check's JSON object

**Confirmed source-level boundary defect; should fix, reproduce before implementation.** `pkg/app/tasks/check.go:144-151` uses `Decoder.More()` to decide whether stdout contains more than one JSON value. `More` means “another array/object member,” not “input reached EOF”: the installed standard-library implementation (`encoding/json/stream.go:497-499`) returns false when the next byte is `]` or `}`. Consequently a command check emitting `{"needed":true}]` or `{"needed":false}}garbage` reaches the successful return at `:152`, despite not being one valid JSON document.

The separate action boundary already checks this correctly in `pkg/app/actions/actions.go:261-270` by reading one more token and requiring `io.EOF`. Keep the task's typed decoding and `DisallowUnknownFields`; the result shapes are intentionally different.

**Minimum fix:** use the same EOF-check pattern locally after the typed task object. No generic parser abstraction is necessary for this small correction. Preserve the existing explicit `needed` requirement and the scheduler's failed-check/no-agent-work behavior.

**Verification:** extend the public scheduled/draft check tests with the two trailing-closer inputs, an additional JSON value, clean trailing whitespace, and a valid object. Assert malformed output produces `OutcomeFailed` and never calls the agent. This audit did not execute the repro.

## R2-RT-6: Centralize Go vault event names below bootstrap and web

**Confirmed hidden link; should fix.** `pkg/app/bootstrap/live.go:154-162` copies six event strings and `reconcile-failed` from `pkg/app/web/global_events.go:22-30,44`. `pkg/app/bootstrap/watcher_events.go:34,76` separately spells `node.changed`. The explicit “Mirrors web...” comment marks the manual dependency.

This is operational state, not cosmetic text: `pkg/app/cli/serve/readiness.go:232-246` compares the web constants to invalidate/cancel retained validation and suppress a refresh after reconciliation failure. A spelling change at the producer alone breaks that transition. Web also owns the view-change and edit-session event vocabulary (`custom_view_watch.go:21`, `global_events.go:30`) while capability metadata inventories it separately; the browser lane is covering the missing `view_preferences.changed` capability entry.

**Minimum fix:** move the Go string constants/reason into a constants-only `pkg/app/runtime/global_events.go`, using this existing neutral package. Bootstrap already imports `app/runtime` through `live_election.go`; runtime has no web dependency. Migrate bootstrap, web, and serve consumers to it, including literal node/preference events. Keep the SSE envelope, broker, payload ownership, readiness transitions, and browser-side registry where they belong. No Go/TypeScript code generation or new event framework.

**Verification:** bootstrap freshness-event tests, serve validation-invalidation/reconcile-failure tests, web global-event tests, and a literal public capability event list. A failure event must still leave validation stale and admit no replacement refresh. Coordinate the capability-list edit with the browser lane.

## R2-RT-7: Synchronize the runtime cancellation test on an actual probe

**Confirmed test gap; selected cleanup.** `pkg/app/runtime/ensure_test.go:461-479` cancels after a fixed 50ms sleep. On a slow host that can cancel before `Ensure` reaches the unhealthy runtime, so the test passes while proving only pre-start cancellation. It does not establish the intended cancellation-during-wait behavior.

**Minimum fix:** run `Ensure` in a goroutine, wait until the existing `fakeRuntime.probes` counter (`fakeruntime_test.go:50,69`) advances, then cancel and observe completion. Retain the no-spawn assertion and bounded completion guard. No new production seam or longer timeout is needed.

**Verification:** focused repetition/race of this test. The observable precondition must fail if the probe is removed; cancellation must still return `context.Canceled` without spawning.

## R2-RT-8: Delete unused compression-needs collector

`pkg/app/credentials/credentials.go:140-149` exports `CollectCompressionNeeds`, but a whole-tree source/test/doc search finds only its declaration/comment. `CollectNeeds` already includes active compression and has behavior tests (`session_test.go:84,130`); `ActiveCompressionProvider` remains live. Delete this unused collector if exported API retirements remain approved. No replacement function or test is needed. This is a low-priority surplus finding independent of R2-RT-3.

## Retained boundaries and cleanup limits

- Keep vendor-specific messages, minimum versions/caches, permission mapping, option encoders, and session lifecycle ownership. Matching `classifyError` or `transportError` bodies alone do not justify a shared vendor framework. Do not revisit the delivered shutdown fix absent new evidence.
- Keep runtime legacy-lock detection, descendant tracking, and the known Windows process limitation. They are explicit safety/platform contracts, not surplus.
- Keep canonical/worktree trust checks and actions/tasks adapters. `repositorytrust.MainCheckout` verifies reciprocal Git metadata; a simpler path-only resolver would lose that proof. Provider alias order also remains deliberately different between persistence metadata and runtime resolution.
- Keep per-call snapshots, separate optional indexed-state policies, existing `CodeLocalService` adapters, and distinct operation/transport outcomes. Do not replace them with a universal dependency bundle.
- Keep the bootstrap index-lock source guard: it checks an explicit ownership invariant. Its purpose differs from brittle Makefile spelling assertions removed in the first pass.
- Retain workload/provider cancellation tests and OS-specific skips; no evidence supports deletion. The nearby timing sleeps that poll real child exit are not equivalent to R2-RT-7's unobserved precondition.
- Small comment correction while touching MCP: `pkg/app/mcp/config.go:5` still says handlers wrap `pkg/app/cli` actions; the owner is `pkg/app/operations`. No broad comment sweep is warranted.

## Evidence and scope limits

Read the first-pass delivery/retention record, CLI/agent-surface/MCP/harness/runtime guidance and the relevant package contexts. Compounding lookup completed with “No current context shards match.” Used the supplied source/import census, whole-tree Go symbol references, exact-body duplicate leads, and direct caller inspection. No semantic retrieval, runtime probes, tests, or builds were performed. Inspected representative state/locking paths and narrowed to the findings above; this is not a claim that every line in the broad assigned surface was read.

Verdict: the first-pass changes remain the right direction. Address graph-path and event-vocabulary ownership, remove proven surplus, and repair the task JSON boundary and cancellation test before adding broader abstractions.
