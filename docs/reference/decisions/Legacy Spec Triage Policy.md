---
type: ReferenceDoc
reference-kind: architecture
summary: Treat legacy numbered specs as source material to triage by enduring value, keep recent transition-era specs under review, and allow obsolete change-shaped specs to be archived after their durable facts are backported
status: active
decision-domain: architecture
tags: [type/decision, docs/reference, migration/specs, provenance]
---
# Legacy spec triage policy

## Context

Rhizome's legacy `specs/` tree mixes several eras of writing.

Some numbered entries are real system specifications: they describe durable subsystem contracts, invariants, workflows, or architecture that should survive the move into the spec-driven knowledge base. Others are really change-shaped delivery notes: they capture a particular implementation task, a narrow plan, or a one-off slice of work that only matters as provenance.

The new spec-driven workflow needs a repeatable rule for deciding what to preserve as living knowledge, what to backport into a durable note, and what can be archived after the useful facts are extracted.

Recent entries near the end of the sequence deserve different treatment. `032-ontology-browser` is still plan-shaped, but it belongs to the transition into the current ontology/browser model. `033-structural-note-nodes` is closer to a durable spec because it defines a structural system shape, not just a one-off change. Those notes should be reviewed for enduring contract content before anything is dropped.

## Decision

Use content shape, not age or number alone, to triage legacy specs.

Classify each numbered spec into one of three buckets:

- **Durable spec material**: keep and backport into the new spec/reference model when the note defines stable behavior, invariants, contracts, or architecture.
- **Archive-only provenance**: preserve the file for history, but do not keep it in the active knowledge base when the note is primarily a change request, implementation plan, task list, or superseded migration artifact.
- **Transition-era review**: keep under special review when the note is recent and close to the current architecture, especially `032`/`033`-era material that may already be expressing the new system shape.

Practical triage rules:

- Prefer durable treatment for notes that explain a subsystem contract, an architectural invariant, or an enduring workflow boundary.
- Treat notes as archive-only when their value is mostly "what we decided to do for this change" rather than "how the system works".
- If a legacy spec contains durable facts but also a lot of change-shaped content, extract the durable facts into a spec, decision, or reference note, then archive the original.
- Do not keep obsolete change-shaped specs alive just because they are numbered or because they were once called "specs".
- Preserve provenance explicitly by linking to the source file or citing it in the receiving note before archiving the original.

Likely durable examples in the current corpus include late-stage system-shape notes such as `031-call-edge-reverse-index` and `033-structural-note-nodes`, because they articulate subsystem behavior that reads like a contract rather than a ticket.

Likely archive-only examples are the older change-shaped entries whose main purpose was to describe a specific delivery slice, especially when the spec already has a replacement in the new `docs/specs/`, `docs/reference/`, or `docs/efforts/` structure.

## Consequences

- The knowledge base can shed obsolete delivery-era specs without losing the durable facts they contained.
- Migration becomes a triage exercise, not a blind import of every numbered file.
- Recent transition-era notes get a deliberate review instead of being auto-deleted or auto-retained.
- Provenance stays visible, which makes the archive useful when we need to reconstruct why a decision existed.
- Some legacy files will be reduced to archive status after their contents are backported into specs, decisions, or reference notes.

## Follow-ups

- Backport durable facts from the legacy corpus into the new spec-driven notes before archiving the source file.
- Use this policy when classifying the remainder of `specs/` so the archive is intentional rather than accidental.
- If `032` or `033` need stronger treatment, promote their durable content into dedicated spec or decision notes and then retire the rest of the legacy file.
