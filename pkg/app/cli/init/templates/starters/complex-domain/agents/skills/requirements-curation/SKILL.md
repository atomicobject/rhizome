---
name: requirements-curation
description: Use when reviewing, accepting, rejecting, deduplicating, prioritizing, or refreshing complex-domain requirements and their source-backed lifecycle state.
---

# Requirements Curation

Present evidence and a recommended disposition for requirement decisions. Metadata quality and structural links do not authorize acceptance.

Load the shared retrieval and evidence guidance through the `complex-domain` router. For a named requirement use `requirement-trace-pack`; for a known changed source use `changed-domain-impact-pack`, then inspect each affected requirement with its trace pack. Use backlog and source-review packs or views only as triage.

## Review

- Read the cited source evidence and confirm source identity, version/date, provenance, and source locations. Preserve earlier evidence during a refresh.
- Keep each requirement atomic and testable. Record duplicate, conflict, and supersession relations without erasing history.
- Distinguish candidates from accepted obligations. Present candidate wording, evidence, conflicts, affected delivery targets, and a recommended disposition.
- Apply acceptance, rejection, supersession, priority, confidence, or other lifecycle changes only when that decision is explicitly authorized. Authorization already carried by the approved workflow need not be requested again.
- Link accepted requirements to the smallest justified domain and delivery targets. A link does not prove the target satisfies the obligation.
- Use `ActionItem` for unresolved accountable decisions and preserve uncertainty instead of weakening the requirement.

A changed source result must distinguish observed source changes, potential affected links, verified delivery implications, and decisions still needed. Apply only supported, authorized updates; report capped, degraded, unresolved, or stale evidence precisely. Validate changed notes in proportion to the mutation.
