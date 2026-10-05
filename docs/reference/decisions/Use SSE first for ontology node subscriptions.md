---
type: ReferenceDoc
reference-kind: architecture
summary: "Use server-sent events as the first browser push transport for ontology node subscriptions and live workspace refresh."
decision-domain: architecture
status: active
last-verified: 2026-04-13
---

# Use SSE first for ontology node subscriptions

## Context

The ontology browser needs a push path for keeping open panes fresh when external file changes affect visible nodes. The current runtime already has watcher and indexing infrastructure, but the browser lacks a delivery channel for node-scoped change events.

The first implementation needs a simple one-way server-to-browser transport that works well for incremental adoption and does not force a heavier connection model before the canonical node event contract is in place.

## Decision

Use server-sent events as the first browser push transport for ontology node subscriptions.

The canonical contract is the node event model, not SSE-specific transport semantics. SSE is the first wire format because the initial need is server-to-browser publication, not bidirectional messaging.

The initial SSE implementation may rely on browser re-fetch after reconnect instead of durable long-window replay, as long as stale or resync-required behavior is explicit.

## Consequences

- the first live-update implementation can stay simple and aligned with one-way publication needs
- browser clients can subscribe to node events without introducing a second command channel
- reconnect and replay semantics must be documented carefully so freshness degradation remains explicit
- if a future transport is added, it should preserve the same canonical node event model rather than redefining event identity or payload semantics

## Follow-ups

- define the node subscription runtime and event envelope in the operations spec
- add a server-side broker that maps changed paths to canonical node refs before SSE publication
- keep the event model transport-extensible even while SSE is the only implemented transport
