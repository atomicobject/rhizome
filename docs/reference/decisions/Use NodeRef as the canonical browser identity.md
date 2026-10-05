---
type: ReferenceDoc
reference-kind: architecture
summary: "Use canonical NodeRef identity across ontology browser read, write, and live-update surfaces instead of keeping separate primary note, section, and structural payload identities."
decision-domain: architecture
status: active
last-verified: 2026-04-13
---

# Use NodeRef as the canonical browser identity

## Context

The ontology browser currently spans overlapping payload families: note-scoped workspaces, rendered section trees, and structural node trees. The ontology projection and edit-session core already carry a richer node identity model, but the browser still treats some panes as note-centric and others as client-side structural sidecars.

That split makes it harder to add new capabilities consistently. Live updates, validation status, dirty state, and future editing affordances all need one durable identity model that survives browser reads, edit-session replay, and runtime change propagation.

## Decision

Use canonical `NodeRef` semantics as the primary identity contract for ontology browser read, write, and live-update surfaces.

Author-facing locators such as note paths, heading fragments, or block fragments may remain accepted inputs, but they are input forms only. The server should canonicalize them before persisting operations, keying pane state, or publishing node events.

Derived payloads such as rendered sections, structural tabs, or note-scoped convenience views may remain as compatibility projections, but they are not peer primary identity models.

This also applies to lightweight ontology discovery/read flows such as type detail, summary, atlas, and CLI inventory output. Those surfaces may still use note-path indexes as lower-level storage helpers, but they should lift results into canonical `NodeRef` identities before returning them to browser or CLI callers.

## Consequences

- browser panes can share one durable identity contract whether they focus a file root, a structural section, or an embedded node
- dirty state, validation state, and live-update subscriptions can key off the same canonical node identity
- server APIs can grow node capabilities additively without creating a new primary payload family for each feature
- migration work is required to reduce dependence on note-only or structural-sidecar identities in the current web layer

## Follow-ups

- define the canonical node workspace snapshot around `NodeRef` and derived capability/status payloads
- define the SSE-first node subscription runtime around canonical node refs
- treat note-scoped browser payloads as transitional compatibility views during migration
