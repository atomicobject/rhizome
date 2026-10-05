---
type: ReferenceDoc
summary: "When to adopt the Agentic Engineering starter, what it scaffolds, and how legacy installations migrate."
reference-kind: guide
last-verified: 2026-07-19
status: active
---

# Agentic Engineering starter adoption guide

## Summary

Use the Agentic Engineering starter when a repository wants recoverable intent, bounded delivery work, durable rationale, and Rhizome-enabled engineering guidance. It is a repository harness, not a complete substitute for a team's process.

## What `rzm init` scaffolds

- specification-delivery ontology and query assets, including domain-accurate `spec-driven` filenames
- folder hubs for specs, efforts, and durable reference material
- six team-owned extension docs under `docs/engineering/`
- the `agentic-engineering` router (phase as argument), two workflow skills (`foundation-review`, `ingest-transcript`), and a compact managed agent-doc block

The extension docs are created once and owned by the repository. Teams tune their workflow, effort expectations, commands, test policy, and documentation policy there; ontology and note-shape mechanics remain managed.

## Legacy migration

`spec-driven` is accepted only as a migration input. Rerun `rzm init` to move starter state to `agentic-engineering`. It removes only legacy process documents that match the checked-in historical catalog. Modified or unproven documents remain at their old path with a retirement notice and a tracked reconciliation record at `.rhizome/migrations/agentic-engineering/README.md`.

No migration silently merges local policy. Reconcile retained material into `docs/engineering/`, then delete the migration record when the team is satisfied.

For starter selection across all options, see [Choosing Your Starter](choosing-your-starter.md).
