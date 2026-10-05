---
name: traceability-review
description: Use when checking whether complex-domain requirements, specs, stories, sources, processes, workflows, and implementation evidence still trace together.
---

# Traceability Review

Produce findings about structural coverage and delivery evidence before any backport.

Load the shared retrieval and evidence guidance through the `complex-domain` router. Use `spec-domain-context-pack` for a spec, `requirement-trace-pack` for a requirement, and the maintenance packs/views for bounded triage. Expand only named gaps.

Check and classify:

- missing authored links and unreadable or invalid story/criterion locators;
- accepted obligations without relevant delivery evidence;
- partial delivery, where some evidence exists but the full atomic obligation is not demonstrated;
- candidate requirements incorrectly treated as commitments;
- stale or conflicting source evidence and superseded requirements;
- unresolved action items and affected processes or workflows;
- capped, degraded, or unresolved retrieval that limits the finding.

An authored link proves only that a relationship was recorded. Inspect the resolved delivery target and evidence for the entire obligation. Confirm incoming spec candidates through the Requirement's typed `specs` relation. Incidental backlinks and semantic matches are impact candidates, not satisfaction.

Lead with exact note paths, recipe/view ids, delivery evidence, and qualifications. Classify missing authored coverage separately from an unresolved anchor or unavailable capability. Read-only review requires no blanket validation. Route evidenced domain updates to `domain-backport`; keep spec alignment and completion truth with Agentic Engineering.
