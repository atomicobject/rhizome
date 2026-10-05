---
summary: "Design constraints, responsiveness/async-init rules, and review checklist for MCP handlers, catalog dispatch, and per-call live capability snapshots."
reference-kind: guide
last-verified: 2026-10-04
code-paths:
  - pkg/app/mcp
  - pkg/app/agentapi
  - pkg/app/runtimeview
tags: [subsystem/mcp]
code-anchors:
  go:
    - label: subsystem.mcp-server.guidance.0
      ref: glob:pkg/app/mcp/**/*.go
    - label: subsystem.mcp-server.guidance.1
      ref: glob:pkg/app/agentapi/**/*.go
    - label: subsystem.mcp-server.guidance.2
      ref: glob:cmd/runtime_helpers.go
    - label: subsystem.mcp-server.guidance.3
      ref: glob:pkg/app/runtimeview/**/*.go
---

# MCP server guidance

## Scope

- `pkg/app/mcp/`: shared MCP-shaped JSON tool handlers (`FilesTool`, `SemanticQueryTool`, `FileContextTool`, `CodeSymbolTool`, ...). Wraps `pkg/app/cli` actions in structured request/response payloads using `mark3labs/mcp-go` types. `Config` reads runtime dependencies from a fresh `runtimeview.Snapshot` per call.
- `pkg/app/agentapi/`: thin in-process adapter. `CallJSON` dispatches through the `ToolDescriptor` catalog, normalizing args via JSON round-trip so in-process calls behave like real MCP requests. The agent CLI and MCP stdio server consume tools through this layer; `pkg/app/mcpserve/` owns only the transport adapter.
- `pkg/app/bootstrap/`: `LiveRuntime` capability-phased async init (Search → Semantic → Code → Leader) and the concrete producer of `runtimeview.View` snapshots.

## Diagnostics

Catalog handler construction wraps every shared tool with one execution boundary. The boundary records the catalog name, duration, current capability readiness, result block count, and outcome. A nil Go error with `CallToolResult.IsError` is an error outcome. A handler panic records `handler_panicked` and propagates the panic for the existing transport recovery policy. Arguments and response text are never persisted.

Persistent MCP and code-mode invokers also record request preparation and forwarding outcomes as parent events. The runtime executing the catalog handler owns the tool execution event; forwarding adds no duplicate execution. Frequent request and tool operations use events to avoid creating report files for normal polling. Informational diagnostics remain off protocol stdout and stderr; warnings and errors keep explicit severity and safe cause categories.

## Design constraints

- **Evaluation has no vault runtime dependency.** The Jev handler validates native question variants before credential resolution, sends explicit state through `pkg/typesafe`, and returns discriminated answers with usage and request IDs. It never initializes retrieval, reads note content implicitly, or exposes upstream error bodies in failures.

- **Compact output stays in shared handlers.** Semantic-query applies narrowing before packing and projects optional compact sources after session dedupe, preserving diagnostics and continuation controls. Ontology query-schema type fragments use the same compiled-schema helper as CLI and code mode; do not add local authoring operations to MCP solely for transport parity.

- **Code-client contracts remain catalog metadata.** The full agent task surface attaches input/output descriptions and CLI-leaf coverage to `ToolDescriptor`. Shared code-mode operations call the same handlers using request-scoped runtime preparation; local-only operations reuse application functions without becoming MCP tools. One generated client owns a persistent stdio process, but application runtime resources remain request-scoped. This is a code-mode protocol, not another MCP transport. Readiness remains handler/runtime-owned.

- **Note metadata composition stays at the boundary.** Configuration carries the command-scoped note-metadata indexer for handlers needing metadata or ontology freshness; handlers reject a missing indexer before opening a durable store, and no MCP business package selects a provider.
- **Respond first, initialize later.** All slow work — directory enumeration/watch roots, session store opening, DB cleanup, embedding provider init, code anchor indexing, cache warming — runs in `LiveRuntime` background phases. Runtime construction returns after fast vault/config resolution.
- **Read live state; do not copy it.** `LiveRuntime.Snapshot()` reports independent search, semantic, code, and leader states plus currently available dependencies. MCP and web accessors take a fresh snapshot per call rather than synchronizing a second mutable runtime structure. The snapshot exposes the note reader once its cache is published, without warming or waiting for that cache; web queries must not retain a raw-reader fallback selected before asynchronous publication.
- **Tools wait or degrade — never assume initialized state.** Provider-backed handlers may call `WaitForSemantic`; code consumers use `WaitForCodeIndex` or inspect the code snapshot. Semantic readiness does not imply code or leader readiness. Missing capability returns a targeted unavailable error or graceful degradation.
- **Indexed one-shot semantic query is query-only.** The agent CLI waits only for provider-only Semantic plus the managed read-only CodeIndex/CodeEmbeddings state, never opens a handler-owned fallback store, and returns structured missing/stale/incompatible remediation. Long-lived MCP callers retain their live capability behavior. Optional diagnostics must report only measured operations; unavailable work is not a measured zero.
- **Semantic query is an adapter over `unifiedsearch.Execute`.** The handler maps scope, path prefix, note type, test, and exact-symbol controls onto `search.Filters`, calls `Execute` once, and only hydrates the returned page (bodies, previews, symbol hits, link targets, session dedupe, compact projection). It must not re-run retrieval, group from the full candidate set, re-assess sources, rebuild the answer, or keep its own cursor or offset. Continuation is the application's single cursor: callers resend the same queries, seeds, mode, and controls; stale or refresh-required cursors surface as `continuation_stale:` errors and mismatched requests as `invalid continuationToken:`. Callers that omit `mode` get the shared default intent inferred by `unifiedsearch.Execute`; the adapter reports it as `modeDetected`/`modeScore` and forwards routing warnings first, then its own provider note, then runtime warnings. `compact=true` keeps pagination and diagnostics top-level while one deduplicated source array owns selected bodies and answer roles reference its stable source refs.
- **Exact one-shot code reads are query-only.** `code-symbol`, `code-references`, `code-rationale`, and `graph-path` declare CodeIndex-only plans by `OperationID`, await only that capability, and receive one caller-managed existing read-only store. Their handlers must not use `OpenIntelStoreBestEffort`; this restriction does not change long-lived MCP fallback behavior. `code-symbol-context` remains request-derived until its source-snippet parity is frozen.
- **Live-note one-shot plans are request-derived.** Files, context, graph, health, report, and connection commands compile their plan after argument normalization. Session dedupe uses a narrow existing-only writer only when the response contract needs a session; it does not require or expose the CodeIndex reader. Optional live fallbacks stay handler-owned only when they are an explicit output contract. Managed report and connection reads never repair or open a second index when the caller-owned reader is unavailable.
- **Semantic directory seeds are index-bounded.** Indexed-only semantic-query resolves note directories through an ordered, limited path-prefix projection on the managed store and preserves unresolved directories for indexed code expansion. It never calls `GetNotesList`; live callers may retain the raw fallback, which must increment the repository-walk diagnostic.
- **Semantic delivery publishes at final encoding.** Candidate inspection only reads session history. Detailed body fields surviving JSON trimming and compact selected bodies reserve their exact represented fingerprints through the existing session batch owner, then commit only after successful final encoding. Distinct detailed bodies sharing one canonical source key reserve one deterministic represented-set fingerprint; single bodies retain their ordinary hash. Selected distinct siblings remain together until the final group decision, including after a subset replaced source history. Denied groups suppress every represented field and renderer-owned text range together. Source bytes normalize to their JSON wire representation before fingerprinting and range capture. Stubs, previews, clipped text, and omitted bodies cannot mark the original evidence delivered; failed shaping or cancellation releases ownership.
- **Result materialization is selection-driven I/O.** Semantic-query uses a request-scoped synchronized read-through cache and at most eight I/O workers for already-selected bodies/previews. Physical-read diagnostics count cache misses, not consumers; do not prefetch unselected candidates or derive worker count from CPU count.
- **Minimal, high-signal options.** The tool surface is intentionally pared down vs CLI flags: sensible defaults over knobs. Adding an option to a handler requires justifying it against this bias.
- **Catalog dispatch, handler-owned readiness.** `pkg/app/agentapi/catalog.go` is the authoritative tool inventory and shared-handler factory. `CapabilitiesTool` consumes its projection through `Config.ToolInventory`. `pkg/app/oneshotruntime` holds one-shot build/await/access plans and executable-front exhaustiveness outside the catalog. Readiness is deliberately absent from descriptors; handlers choose their phase-specific wait/degrade behavior.
- **Mutation waits preserve request cancellation.** Shared direct note mutation handlers pass the request context through to the CLI writer lease; disconnecting or canceling a request must stop a pending lock wait without editing files.
- **Background indexing belongs to the vault runtime.** Election on `.rhizome/runtime.lock` and the index lock (`pkg/vault/indexlock`, `.rhizome/index.lock`) prevent concurrent indexing across processes; only the runtime that won election runs the indexing lane ([[vault-runtime]]). Tool handlers must not spawn their own indexing; one-shot MCP commands only ensure a runtime exists.

## Must-dos when changing this subsystem

- New init/dependency code: answer "will this block the caller becoming responsive?" If yes, move it into a `LiveRuntime` phase and expose it through `runtimeview.Snapshot`.
- New tool: add the handler in `pkg/app/mcp/tool_*.go`, one catalog descriptor with the correct memberships/factory and CLI display metadata, and the CLI front when applicable.
- New tool option: mirror it between the handler's request parsing, the agent CLI flags, and the surface/capabilities descriptions; update `README.md` when user-facing. Keep options minimal — prefer a default over a knob.
- Tool needs async state: take it from `Config` accessors or `Runtime.Snapshot()`, use only the relevant phase-specific wait when blocking is worthwhile, otherwise degrade with a targeted error code.
- Update `pkg/app/mcp/CONTEXT.md` when entry points or the async-init invariant change; it is the retrieval surface agents see first.
- Tests beside handlers (`tools_test.go`, `*_tool_test.go` patterns); run `go test ./pkg/app/mcp/... ./pkg/app/agentapi/...`.

## Review checklist — problems to catch

- Blocking calls executed synchronously during runtime construction instead of inside a LiveRuntime phase.
- Handlers reading stale copied fields rather than a per-call snapshot, or waiting for semantic when only code state matters.
- Treating semantic readiness as "all capabilities ready."
- New tool missing its catalog descriptor, applicable CLI display metadata, or CLI front.
- Tool option drift: handler accepts an arg that the agent CLI/surface doesn't expose (or vice versa); descriptions diverging from actual flag behavior.
- Unbounded option proliferation — new knobs that a default would cover.
- Handlers triggering index/embedding writes without the index lock / leader coordination.
- Indexed semantic-query paths calling the raw note reader for directory seeds or graph fallback, or result shaping rereading the same canonical file through independent body/preview helpers.
- Errors returned as bare Go errors instead of the structured `IsError` text payload `agentapi.CallJSON` expects.

## Key files

- `pkg/app/mcp/config.go` (`Config`, `ToolInventory`, live snapshot accessors)
- `pkg/app/mcp/CONTEXT.md` (entry points + async invariant), `pkg/app/mcp/tool_semantic.go` (Runtime.Wait usage)
- `pkg/app/agentapi/catalog.go` (`ToolDescriptor` catalog and handler factories), `pkg/app/agentapi/agentapi.go` (`CallJSON`, catalog dispatch, arg normalization), `pkg/app/agentapi/doc.go`
- `pkg/app/mcpserve/server.go` (stdio transport), `pkg/app/cli/mcpserve/invoker.go` (request-scoped CLI orchestration), `cmd/mcp.go` (Cobra adapter)
- `pkg/app/runtimeview/view.go` (snapshot/view contract), `cmd/runtime_helpers.go` (`buildAgentConfigForOperation`, `buildAgentConfigForRequestPlan`)
- `cmd/agent.go`, `cmd/agent_surface.go` (CLI fronts + descriptor-projected surface)
- `pkg/app/bootstrap/live.go`, `pkg/app/bootstrap/CONTEXT.md` (capability phases, leader/follower)

## Related docs

- [[LiveRuntime (async server bootstrap)]]
- [[Indexing pipeline - Live updating (watcher runtime)]]
- [[Index lock and background coordination runbook]]
- [[Rhizome documentation - Tool guide (agent CLI tools + tradeoffs)]]
- [[Cache (Hub)]]

## Bulk audits and concurrency

`check_paths` returns ordered decisions from the unified ignore matcher, including nested git rules, Rhizome ignore files, configuration excludes, and physical path containment. It does not infer note inclusion. `evaluate_batch` runs independent typed requests with bounded workers, one retry policy, shared batch throttle pauses, and per-item completion evidence. Callers persist checkpoints and must not replay uncertain requests automatically.

Code-mode dispatch admits concurrent calls, with exclusive access for live vault refresh/mutation, shared access for audited existing-index reads, and independent provider/ignore calls. Lock waits honor cancellation. The client defaults to eight concurrent calls, configurable up to 32; the server admits at most 32 active handlers. Callers use `await` to order dependencies. This updates the initial serial behavior described by the persistent code-mode spec. Whole-script execution allows an explicit deadline up to 24 hours; default and per-call limits remain bounded.

Agent metadata discovery (`agent surface` and `agent code surface|describe|generate`) remains free of diagnostic writes; explicit generation writes only its requested task artifacts. Commands returning coded JSON failures persist their terminal ERROR event and report without adding stderr prose. Other warnings retain their explicit console severity.
