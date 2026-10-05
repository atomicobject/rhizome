---
type: TechnicalSpec
id: SPEC-0092
aliases: [SPEC-0092]
summary: "A single bounded stdio process exposes the complete agent operation surface to selectively generated JavaScript clients."
spec-status: active
last-updated: 2026-10-04
---

# Persistent agent code mode

## Summary

One generated client owns one Rhizome child process for all its task calls. The existing operation catalog supplies selective discovery, schemas and dispatch identity for the full agent surface. This is the next delivery after [[progressive-agent-code-mode|SPEC-0091]]; it replaces that pilot's one-shot transport and two-operation restriction. Existing CLI and MCP behavior remains the reference for each operation's applicable runtime and authority, not a claim that every surface shares the same runtime configuration.

## Goals

- Amortize executable startup and connection negotiation across a complete agent script.
- Make every agent task operation available, including local-only application operations, with selective schema loading.
- Preserve bounded execution, durable evidence, validation outcomes, write authority, freshness and explicit lifecycle.
- Teach and verify use in starter-free, Agentic Engineering and Complex Domain workflows.

## Non-Goals

Remote or non-loopback listeners, WebSockets, remote authentication, service-manager installation, a second tool registry, arbitrary command execution, automatic write retries, unrestricted parallel requests, automatic client generation at init, arbitrary statically typed ontology results, Project-KB redesign and main/release publication. The loopback vault runtime that hosts catalog operations is defined by [[vault-runtime-coordination|SPEC-0104]], not here.

## Requirements

### Process and protocol

`rzm agent code serve` is a long-lived machine-only stdio endpoint bound to one vault. The client starts it once using an explicit absolute executable and vault directory, no shell, and repository delegation disabled. Newline-delimited JSON-RPC 2.0 frames use stdout exclusively; diagnostics use stderr. The installed dependency only vendors MCP message/tool types, not its server package; use bounded standard-library framing rather than introducing a network dependency.

Initialize negotiates a protocol version, explicitly selected operation names and their contract hash once before task execution. Response includes the negotiated contract and server PID for lifecycle evidence. A mismatch rejects task calls with a stable artifact-stale error. Operations outside the selected set fail. No per-call describe or executable spawn occurs. Generated immutable artifacts retain deterministic publication and include no vault content, credentials or session state.

The transport supports request IDs, operation calls, cancellation notifications and explicit shutdown. Each call carries operation identity, object input and bounded timeout, with optional session identity. Its result preserves operation payload and diagnostics plus `ok` and `exitCode`, keeping CLI validation 0/1/2 outcomes distinct from malformed protocol, invalid operation, startup, timeout, cancellation and crash failures. Raw output is retained only where the existing operation actually produces it; no fabricated subprocess status.

Requests execute with bounded concurrency: generated clients default to eight in-flight calls, configurable from one to 32, and the host runs at most 32 calls concurrently. Independent provider and ignore calls can overlap; audited existing-index reads share access, while live vault refreshes and mutations take exclusive access per connection. Completion order is not receipt order; callers must await dependencies, including writes followed by reads. Queue size, incoming frame size, outgoing result size, startup time, request deadlines and retained diagnostic bytes are bounded. Cancelling queued work removes it; cancelling running work propagates a request context. An unacknowledged cancellation retains its client slot until a reply or connection close. Cancellation is not rollback: a caller must inspect completion evidence for a write that may already have executed. An unresponsive handler cannot lead to unbounded client shutdown; closing kills/reaps the owned process after a bounded grace period. Process death rejects every pending request and does not replay any operation.

### Catalog and application ownership

The existing `pkg/app/agentapi` catalog remains the authoritative operation identity and membership source. Extend it for code-mode contracts and explicit CLI-leaf coverage; do not fork a dispatch inventory in the client or host. Every callable agent leaf and advertised note-move exception has a callable code-mode equivalent. Discovery, generation and serve are control-plane commands and not recursively callable task operations. Supported nested modes and write/apply modes count toward coverage.

Shared MCP-shaped tools reuse their existing handlers. Local-only CLI operations extract reusable application functions consumed by both CLI and code mode; they do not become MCP tools solely to obtain code-mode access. No generic Cobra replay, process-global stdout capture, or subprocess fallback. Known inputs and outputs have useful concrete declarations; dynamic query data may be unknown. Input validation rejects malformed requests while preserving the actual supported selectors/options rather than keeping pilot-only path restrictions.

### Execution host

The stdio host is the client-facing transport; it is not the execution host for catalog operations. At start it ensures the vault runtime defined by [[vault-runtime-coordination|SPEC-0104]] and forwards each catalog operation call to that runtime's loopback agent-operation endpoint with the connection's read-write flag and session id. Local-only operations keep executing in the host process. When no runtime can be used (auto-start disabled or spawn failure) the host executes catalog operations in-process as it did before this revision and reports that fallback once in diagnostics. Request cancellation propagates to the forwarded request. The runtime reports a configuration generation with each result so the host can raise the existing configuration-changed outcome. The catalog decides the execution host: an operation with a local override stays in the host process unless the catalog admits that call as a runtime read under [[code-mode-skill-scripts|SPEC-0115]], and any other local-override call that reaches the runtime is refused as `code_mode_local_only` rather than served twice. The fallback is reported on the first call that would have been forwarded, so a connection using only local-only operations stays silent, and it covers the remainder of the connection: a runtime that fails mid-connection is not retried, because alternating execution hosts would make freshness and write effects unexplainable. The configuration generation travels inside the outcome diagnostic, which the host removes before the outcome reaches the generated client, so the wire shape is identical on either host. Forwarding grants no additional authority: the runtime applies the same plan-only boundaries the one-shot host applies.

### Runtime, freshness and authority

Serving does not bootstrap every capability. Each operation retains its request-derived initialization, handler-owned readiness and runtime effects. Minimal start stays minimal. Reuse is allowed only where the resource lifetime is safe: never cache a stale ontology projection, parsed note or configuration across requests without invalidation. Indexed reads preserve existing stale/missing/incompatible warnings and do not silently repair. Configuration changes can require reconnect, but this must be detected or clearly defined. External file edits and index replacement have real integration coverage.

Successful writes followed by dependent reads either observe the new state or return an explicit stale/unavailable result, never silently return old evidence as current. Preserve existing index locks and single-writer ownership. Write-capable invocation remains explicit, and operation-specific preview/apply controls remain necessary. Exposing a method does not grant the model permission. CLI-only read restrictions remain unless the existing shared operation already defines a separately authorized write-capable mode.

### Guidance and evidence

Canonical base, AE and Domain guidance covers selection, generation, connection, calls, result interpretation, write authority and cleanup. One small call may remain direct CLI; scripts compose multiple operations and control how much evidence returns to model context. Fresh installs and repeated init prove generated guidance is complete and idempotent.

Acceptance includes a real compiled Rhizome/Node process test, catalog/CLI coverage checks, meaningful family parity and mutation tests, request lifecycle failures, same-process edit/read freshness, required repository checks and independent final review. A six-launch Luna xhigh Codex-subscription paired pilot checks three workflows; it is qualitative evidence, not a statistical claim or API-billed evaluation.

## Delivery and migration

Revision 2026-09-17: catalog operations move from a private per-host runtime to the shared vault runtime (SPEC-0104). Protocol, framing, artifacts, and coverage obligations are unchanged; the persistent code-mode effort that delivered the first revision is closed history and this revision is delivered by the vault runtime coordination effort.

The approved [[../../reference/analysis/persistent-code-mode-delivery-plan|delivery plan]] defines phases and ownership. Replace the old subprocess client and require artifact regeneration through an explicit generator-version change. Reconcile the pilot spec and subsystem notes on actual delivery; preserve prior frozen effort history and acknowledge any resulting drift through supported tooling.
