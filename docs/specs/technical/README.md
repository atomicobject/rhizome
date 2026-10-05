---
summary: "Hub for technical specs that define implementation-facing contracts and architecture work."
---

# Technical specs

## Summary

Use this folder for normative technical targets that should outlive a single implementation session.

Technical specs can be shaped in either of these ways:

- requirements-only, when the work is best described as subsystem constraints, interfaces, or architecture boundaries
- user-story-driven, when the technical work still benefits from scoped embedded stories and acceptance criteria

Use architecture decision notes for durable cross-cutting choices or principles. Use reference docs for descriptive architecture overviews and supporting explanation that should not be phrased as normative requirements.

- [[unified-search-answer-architecture]] — unified search orchestration, answer packet shaping, NodeRef/source provenance, and context packing boundaries.
- [[search-diagnostics-explain-architecture]] — raw/explain diagnostic payload contract for search and semantic-query without behavior changes.
- [[answer-packet-diagnostics-contract]] — answer-stage role selection, omission, confidence, missing-evidence, and next-query rationale.
- [[indexing-observability-and-maintenance-policy]] — indexing progress, `--timings`, DB-write metrics, and SQLite maintenance/vacuum policy.
- [[ontology-edit-replay-conflict-contract]] — source-preserving ontology edit replay, locator capture, rebase, conflict, and span-ownership invariants.
- [[public-api-graphql-rest-contract]] — minimal public API split: GraphQL for composable reads, REST for operations and staged writes.
- [[configured-view-engine-and-repo-config]] — reusable configured-view engine, tracked repo view files, source adapters, mounts, variants, validation, and safe view execution.

## Goals

## Non-Goals

## Requirements
