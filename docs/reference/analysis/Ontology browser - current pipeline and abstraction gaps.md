---
type: ReferenceDoc
summary: "Describes the current ontology browser pipeline, the overlapping payload models in the web layer, and the abstraction gaps that motivate a canonical node workspace contract."
reference-kind: analysis
derived-from:
  - thread analysis on ontology browser pipeline, node abstraction, and live-update design
last-verified: 2026-04-13
status: active
---

# Ontology browser - current pipeline and abstraction gaps

## Summary

The ontology browser is converging on a node-centric experience, but some read surfaces still historically mixed note-path indexes, note metadata rows, and ad hoc projection walks before the browser consumed them. The projection and edit-session core already expose richer node identity and authored-source binding than those older summary/detail flows preserved.

## Current pipeline

- frontend pane stack mostly loads a rendered note plus a note-scoped workspace
- focused section handling trims a note workspace down to one section after the fact
- structural panes can still be created from client-side structural node payloads rather than a canonical server-resolved node workspace
- server web types expose overlapping payload families for rendered sections, structural nodes, and note workspaces
- edit-session flows already canonicalize browser inputs back into richer node identity before replay
- type detail, summary, and atlas flows have now started moving onto a shared type-instance read seam that returns canonical `NodeRef` items for both file-root and embedded-node examples

## Core gap

The browser currently spans three related but non-identical models:

- rendered markdown sections
- structural browser nodes
- ontology projection/edit-session nodes

That duplication creates avoidable friction for:

- node-scoped status surfaces
- field and collection dirty tracking
- validation at granular scope
- same-file multi-pane refresh
- live-update subscriptions
- future capability growth

## Architectural implication

The next abstraction layer should not be another browser-specific shape. The right long-term seam is a canonical node workspace snapshot derived from the ontology node model, with convenience projections such as outlines, structural tabs, and markdown rendering hanging off that shared core.

For lightweight discovery/read flows, the matching seam is a shared type-instance catalog that lifts persisted note-path indexes and structural projections into canonical `NodeRef` results. That keeps persistence optimized for note-root indexing while preventing browser endpoints from re-encoding ontology instance semantics in parallel.

## Related

- [[structural-node-model-and-ontology-read-path]]
- [[node-workspace-capability-pipeline]]
- [[node-subscriptions-and-live-update-runtime]]
- [[Use NodeRef as the canonical browser identity]]
