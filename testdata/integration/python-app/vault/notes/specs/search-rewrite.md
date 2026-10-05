---
type: Spec
name: Search Rewrite
---

## Requirements

Keep ontology-backed query results stable while the search pipeline changes.

This section pulls in [[ambient-linked-decision]].

### Details

Coordinate the rollout with [[structured-ontology-decision]].

## Notes

This later section mentions [[backlink-decision]] but should not affect the
requirements subtree.

- [ ] Review search rollout notes #action-item
  assignee:: [[notes/people/alice]]
  due:: 2026-05-08

## Stories

### Stable typed retrieval

story-id:: STORY-001
status:: ready
spec:: [[search-rewrite]]
^story-001

As an agent, I can retrieve typed story sections with bounded parent context so
they do not float without their governing spec.
