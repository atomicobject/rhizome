---
type: OperationsSpec
summary: "Defines SSE-first node subscriptions, watcher-to-browser change propagation, and the runtime guarantees for keeping node workspaces fresh."
id: SPEC-0020
spec-status: active
last-updated: 2026-04-13
aliases:
  - SPEC-0020
  - Node subscriptions and live update runtime
---

# Node subscriptions and live update runtime

## Summary

The browser needs a runtime path that can keep open node workspaces fresh when files change outside the current edit session. The source of truth for change detection should remain the existing watcher/runtime pipeline, but browser delivery needs a node-centric subscription layer rather than a coarse file reload path.

This spec defines the SSE-first subscription model, the watcher-to-node change propagation pipeline, and the operational guarantees for freshness, degradation, and fan-out. It is the runtime complement to the node workspace capability pipeline.

## Goals

- propagate external file changes to open node workspaces without forcing users to manually reload panes
- key browser subscriptions by canonical node identity rather than only by file path
- let many panes subscribe to nodes in the same file without collapsing into one file-level refresh event
- make degraded watcher or indexing state visible and recoverable instead of silently stale
- define operational guarantees and NFRs separately from the core node workspace API contract

## Non-Goals

- implementing every client-side refresh behavior in the first server-side iteration
- providing durable infinite event replay logs in the first SSE implementation
- replacing the existing watcher/runtime source of truth
- specifying unrelated deployment or infrastructure concerns outside this runtime path

## User Stories

### US1 - Turn watcher-detected file changes into canonical node update events
- id:: ^SPEC-0020-US1
- summary:: Turn watcher-detected file changes into canonical node update events.
- status:: ready

#### Acceptance Criteria

- A changed note path can be mapped to one or more impacted canonical node refs before publication.
- The runtime publishes node-scoped events rather than only file-scoped invalidation messages.
- Events preserve cause metadata such as filesystem change, edit-session commit, schema refresh, or stale-resync.

### US2 - Subscribe to multiple node refs and refresh only the panes that need it
- id:: ^SPEC-0020-US2
- summary:: Subscribe to multiple node refs and refresh only the panes that need it.
- status:: ready

#### Acceptance Criteria

- One browser connection can subscribe to multiple canonical node refs.
- If two panes show different nodes from the same file, each pane can receive refresh information specific to its node.
- Unaffected panes do not need to be torn down when a sibling node in the same file changes.

### US3 - See when the live-update path degraded and when the browser should resync
- id:: ^SPEC-0020-US3
- summary:: See when the live-update path degraded and when the browser should resync.
- status:: ready

#### Acceptance Criteria

- The runtime can emit explicit stale or resync-required events when watcher or index fidelity is insufficient.
- The browser can distinguish normal node updates from degraded-mode refresh requirements.
- Logs and diagnostics are sufficient to trace missed, repeated, or bursty node publications.

## Requirements

### Must

- The first browser push transport for ontology node subscriptions MUST be SSE.
- The subscription contract MUST accept canonical node refs as the durable subscription key.
- The server MAY accept author-facing locator forms as subscription inputs, but it MUST canonicalize them before registering the subscription.
- A subscription connection MUST be able to watch multiple node refs at once.
- The runtime MUST translate changed file paths into impacted canonical node refs before publishing browser-visible events.
- The event envelope MUST include:
  - event identifier
  - event kind
  - canonical node ref
  - node version token
  - cause metadata
  - changed scope hints when known
- The runtime MUST support at least these event classes:
  - node updated
  - node deleted or no longer resolvable
  - node status updated
  - node stale or resync required
- If one changed file affects multiple subscribed node refs, the server MUST preserve node-level fan-out semantics instead of emitting one undifferentiated file reload event.
- Watcher/runtime degradation MUST be surfaced explicitly through stale or resync-required behavior rather than by silently leaving subscribed panes stale.
- The server MUST use the same canonical event model for filesystem-originated changes and server-originated changes such as committed edit sessions.
- The initial SSE implementation MAY require the client to re-fetch fresh node snapshots after reconnect instead of relying on a long durable replay log, but this reconnect behavior MUST be documented as part of the contract.

### Should

- The server should coalesce bursty file changes before publication while still preserving distinct affected node refs.
- Changed scope hints should distinguish content changes from status-only changes when the runtime can determine that cheaply.
- The runtime should be able to publish a full node invalidation event even when it cannot compute a fine-grained changed scope.
- The browser should be able to treat stale, conflicted, rebased, and refreshed states as first-class node status updates rather than bespoke side channels.
- The SSE event model should remain transport-extensible even though SSE is the first implementation.

### May

- Later implementations may add bounded replay windows, broader fan-out subscriptions, or alternative transports as long as the canonical node event model remains stable.

### Non-functional guarantees

- Freshness: near-real-time best effort after watcher/runtime ingestion, not strict instantaneous delivery.
- Correctness: canonicalization and impacted-node resolution happen before publication, so the browser never treats raw file paths as the durable node identity contract.
- Degraded mode: if watcher, ontology, or index state is not trustworthy enough for precise publication, the runtime prefers explicit stale or resync-required events over quiet drift.
- Fan-out safety: same-file multi-pane workflows should not require duplicate browser connections or duplicate file-level reloads.
- Backpressure: burst handling should coalesce or downgrade publication before it overwhelms the browser with redundant pane refreshes.
- Observability: publication cause, coalescing, stale transitions, and reconnect/resync behavior should be diagnosable from server logs or comparable runtime diagnostics.

### Public transport surface

- The runtime should grow around:
  - `GET /api/ontology/events?ref=<locator-or-canonical-ref>&ref=<locator-or-canonical-ref>`
- SSE payloads should be documented around a logical `NodeEvent` envelope with optional snapshot or invalidation semantics.
- The SSE contract should allow the browser to subscribe, reconnect, detect stale state, and request a fresh node workspace snapshot without inventing separate per-feature live-update channels.
