---
type: ReferenceDoc
summary: "Describes the intended watcher-to-broker-to-browser pipeline for node-scoped live updates, including canonicalization, fan-out, and degraded-mode handling."
reference-kind: analysis
derived-from:
  - thread analysis on ontology browser pipeline, watcher integration, and SSE-first node subscriptions
last-verified: 2026-04-13
status: active
---

# Node subscriptions and live update pipeline

## Summary

The browser needs a node-scoped publication path layered on top of the existing watcher and indexing runtime. File changes remain the raw signal, but browser delivery should happen in canonical node terms so panes can refresh independently even when they point into the same markdown file.

## Intended pipeline

1. watcher/runtime detects changed or stale paths
2. server refreshes note metadata, ontology state, and any affected runtime caches
3. changed paths are mapped to impacted canonical node refs
4. server broker coalesces and fans out node-scoped events
5. SSE stream delivers node events to subscribed browser clients
6. browser re-fetches or patches the affected node workspace snapshot

## Node-scoped fan-out

One changed note path may affect:

- the file-root node
- one or more structural sections
- one or more embedded nodes
- node-level status surfaces even when rendered content did not change visibly

The publication model should preserve those distinctions. It should not collapse all outcomes into one generic file reload event.

## Degraded behavior

Watcher and index fidelity are best-effort. When the runtime cannot safely compute precise node updates, it should emit stale or resync-required semantics rather than quietly leaving subscribers on old state.

## Operational notes

- SSE is the first transport because browser delivery is initially one-way
- canonical `NodeRef` identity should key subscriptions even when the client started from note-path locators
- initial reconnect behavior may prefer explicit re-fetch over durable replay logs

## Related

- [[node-workspace-capability-pipeline]]
- [[node-subscriptions-and-live-update-runtime]]
- [[Use SSE first for ontology node subscriptions]]
- [[Indexing pipeline - Live updating (watcher runtime)]]
