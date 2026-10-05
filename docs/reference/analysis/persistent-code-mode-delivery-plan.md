---
type: ReferenceDoc
reference-kind: analysis
summary: "Proposed delivery plan for one-process code mode across the full Rhizome agent surface."
status: approved
last-verified: 2026-09-07
---

# Persistent code mode delivery plan

## Intent and authority

Drew requested one process serving all calls in a code-mode script and the full agent surface. This plan replaces the proposed two-operation adoption-only follow-up. Drew approved implementation by requesting Astra Advisor to orchestrate and deliver this work after presentation of this plan and its six-launch cap. Worktree: `/private/tmp/rhizome-code-mode-adoption`, branch `codex/code-mode-adoption`, inspected baseline `5ee088c1` after PR #243. Execution lives in the persistent code-mode effort.

Preserve the three priorities: starter-free proportional use, Agentic Engineering execution, and Complex Domain evidence and requirements. Preserve approved-plan continuity and the lightweight local-task path. Project-KB and broader knowledge-maintenance infrastructure remain outside this work.

## Verified starting point

- `pkg/app/agentcode/template.go` launches a describe subprocess and operation subprocess per call. Only files and file_context have generated contracts.
- `pkg/app/agentapi/catalog.go` already owns operation identity, shared handler factories, CLI membership and mutation classification. It also lists local-only families, including start, validate, next-id, current-user, views and ontology authoring operations. Family membership is not an exhaustive inventory of executable leaves.
- `pkg/app/agentapi/agentapi.go` dispatches shared MCP-shaped handlers in process. Its current error extraction is insufficient as the sole transport result contract because local validation and other operations have richer status semantics.
- The MCP package uses live runtime snapshots; one-shot CLI paths have distinct indexed-read and initialization contracts. Sharing a handler does not establish identical freshness or side effects across runtime configurations.
- The agent-surface and MCP subsystem notes deliberately keep several commands local-only. Full code-mode coverage requires shared application entry points for those commands, without making them MCP tools or replaying Cobra commands in process.
- Installed base, Agentic Engineering and Complex Domain guidance does not yet route agents to code mode.

## Recommended decisions

1. **Transport:** one child process per client lifetime, communicating with JSON-RPC 2.0 over stdio. Proposed entry point: `rzm agent code serve`. No network listener or daemon installation. Keep stdout exclusively protocol output and stderr diagnostic output. Reuse existing protocol library facilities where suitable; do not assume an existing runnable MCP stdio server covers this surface.
2. **Scope:** all callable agent command leaves, including supported read/write modes and the advertised note-move exception. Record every leaf in a coverage matrix. Code-mode discovery/generation/lifecycle commands are control-plane operations, not recursively callable tasks. Interactive human commands and unrelated top-level command families are excluded. Any further exclusion requires an explicit scope decision, not silent omission.
3. **One catalog:** extend the existing catalog with code-mode schemas and dispatch metadata. Shared tools reuse their handlers. Local-only operations extract reusable application functions used by both CLI and code mode; MCP membership stays independent. Do not introduce a second operation registry or generic subprocess fallback.
4. **Progressive loading:** expose compact summaries for the full surface; describe and generate selected operations on demand. Generate an ESM client and TypeScript declarations, with no need to inject the entire catalog into model context. Dynamic ontology/query results remain honestly typed rather than inventing static schemas for arbitrary query selections.
5. **Compatibility:** negotiate protocol and selected operation contract hashes once per connection. Reject incompatible artifacts clearly. Replace the old subprocess transport and regenerate clients; do not retain a second legacy transport. Preserve operation payloads, warnings, references, structured diagnostics and outcome distinctions.
6. **Runtime:** bind a connection to one explicit executable and vault. Use request contexts and capability-specific readiness; do not initialize every capability simply because the catalog is broad. Preserve the existing minimal-start behavior. Resource reuse must not silently adopt different indexing or provider effects from the corresponding operation contract.
7. **Writes:** retain existing explicit write/apply authorization and configuration. Full exposure is not blanket permission. Start with serialized request execution, including reads, so ordering and shared-state behavior are simple and deterministic. Multiplex request IDs and allow queued cancellation; broaden parallel execution only with evidence that operation families are safe.
8. **Freshness:** request-local snapshots/caches; source reads observe completed file edits. Indexed reads either reflect the corresponding indexed revision or report stale/unavailable evidence and remediation. Successful mutation followed by a dependent read must not silently return pre-mutation evidence. External edits, index replacement and configuration changes need explicit refresh/reopen or reconnect behavior, tested and documented; do not promise instantaneous index freshness.
9. **Failure:** bound startup, request time, message size, queued work and buffered output. Cancellation targets a request rather than killing unrelated work. EOF/close shuts down and reaps the child. A crash rejects all pending calls; never silently replay writes. Cancellation after a write begins reports possible/known completion rather than claiming rollback.

## Execution phases and exits

### 1. Freeze contracts and coverage

Create a new technical spec and bounded effort under the approved plan. Link SPEC-0091 as the historical starting point; explicitly reconcile its two-operation/one-shot invariant rather than silently rewriting frozen delivery history. Inventory actual agent CLI leaves and catalog entries, including nested modes and exceptional note-move routing. For each record: input/output, dispatch owner, write authority, runtime effects, freshness, outcome behavior, and verification fixture.

Specify handshake, request/result/error envelopes, cancellation, shutdown and schema projection. Resolve how current CLI exit 0/1/2 outcomes map to transport results separately from protocol errors. Inspect the installed protocol library before selecting reusable framing/dispatch machinery.

Exit: complete coverage matrix and concrete public API reviewed with foundation-review. This is a technical review checkpoint; continue under approved scope after resolving findings. Escalate only a material contract or scope change.

### 2. Deliver one process end to end

Implement stdio host and generated client against files and file_context first, using shared application paths. Negotiate once, dispatch many calls, preserve existing operation behavior, close cleanly. Replace the current per-call subprocess implementation. Build a real Node-to-compiled-Rhizome fixture proving one child PID and no per-call child spawn.

Exit: sequential calls, queued cancellation, deadline, malformed frames, startup failure, process crash, output limits, stale client and clean shutdown pass. A same-process edit/read scenario verifies freshness behavior. Measure startup and repeated-call costs against the existing direct and batched CLI harness without claiming a speedup in advance.

### 3. Complete operation coverage

Expand shared-handler contracts, then extract local-only operations at their existing owners. Keep command parsing in Cobra and business logic below it. Add catalog/CLI coverage checks that fail when a new executable leaf lacks a code-mode disposition. Test read/write modes, plans versus apply, identifiers, validation outcomes, session reuse and safe note mutations in disposable vaults.

Exit: every in-scope leaf is callable and has a contract/dispatch test; representative real behavior tests cover each operation family and all mutation families. No subprocess escape hatch or placeholder unsupported operation counts as delivered coverage.

### 4. Integrate the agent experience

Update canonical base skill references with discover/select/generate/connect/call/close examples, when to use a script, and freshness/write guidance. Add concise AE and Domain composition guidance owned by those workflows. Direct CLI remains sensible for a single simple operation. Keep minimal startup compact and generation explicit.

Exit: fresh no-starter, AE and Domain installations have closed references, working examples, and idempotent regeneration. Demonstrate base context gathering, AE fix with durable documentation, and Domain provenance/coverage in the same persistent-client architecture.

### 5. Verify, evaluate and deliver

Run focused behavioral tests and required `make check`; validate changed documentation and frozen scope; rebuild and verify generated surfaces. Obtain an independent final diff review. Record timings including client startup, Rhizome startup, and total workflow time; compare equivalent output/effects and disclose index/cache ordering.

Proposed bounded evaluation campaign: six Luna xhigh Codex-subscription launches, three task pairs (starter-free substantive fix, AE execution/resume, Domain requirement evidence), each comparing efficient CLI with persistent code mode. Preflight executable paths, subscription auth, fixture reachability, Python/Node versions and allowed tool execution without launching evaluated models. No API keys, Astra runs, automatic retries, or claims of statistical significance. Inspect traces for correctness, evidence selection, tool recovery, ceremony and maintenance; report cost/time only where actually observed. Approval of this plan includes this six-launch cap; a replacement campaign requires a revised budget.

Exit: acceptance matrix complete, full checks pass, reviews addressed, evidence limitations explicit, draft PR ready for merge approval. Main merge and release remain separate actions.

## Work allocation

Use one coordinator and bounded workers, with Ponytail simplicity throughout. Phase 1 precedes implementation fan-out. Transport/client work owns `pkg/app/agentcode/**` and its new CLI front. Shared contract work owns `pkg/app/agentapi/**`; local adapters remain with their owning packages and CLI fronts. Coordinate catalog edits through one owner. Guidance/evaluation can prepare fixtures after Phase 1, then integrate after transport behavior settles. Give each worker this plan, the approved spec/effort, coverage rows, exact baseline and owned paths; do not depend on chat history. Use repository model preferences, and reserve Luna xhigh specifically for the evaluated agents.

## Acceptance checklist

- One child process handles a complete multi-call script; no request launches another Rhizome executable.
- Full in-scope coverage is mechanically checked against executable CLI leaves and catalog membership.
- Contracts derive from the existing catalog; local-only operations remain independent of MCP membership.
- Outcomes, diagnostics, writes, freshness and sessions remain faithful to the documented operation semantics.
- Request failure/cancellation and process failure have bounded, observable behavior without write replay.
- Base/AE/Domain installed guidance teaches and demonstrates the capability.
- Real integration tests pass; six-run pilot evidence is reported without overstating general benefit.

## Approval

Drew approved this scope and its six-launch Luna xhigh subscription evaluation cap by explicitly requesting orchestration and delivery. JSON-RPC over stdio, serialized first implementation, full agent-leaf coverage, and explicit write authority are accepted decisions. No further routine approval is required; main merge and release remain separate.
