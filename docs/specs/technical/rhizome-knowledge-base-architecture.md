---
type: TechnicalSpec
summary: "Defines the canonical note model and migration rules for aligning Rhizome's knowledge base with the spec-driven workflow."
id: SPEC-0010
spec-status: active
last-updated: 2026-04-12
aliases:
  - SPEC-0010
---

# Rhizome knowledge-base architecture

## Summary

Rhizome's knowledge base must use the spec-driven workflow as its canonical structure. Normative system intent belongs in `docs/specs/`, bounded execution belongs in `docs/efforts/`, durable rationale belongs in `docs/reference/decisions/`, and durable supporting context belongs in `docs/reference/`.

The legacy `specs/` tree and the older `docs/` families remain valuable as source material, but they are no longer the canonical topology. They should be triaged, backported, archived, or dropped based on enduring value rather than preserved wholesale.

## Goals

- make Rhizome dogfood the spec-driven workflow with high fidelity
- preserve durable knowledge while retiring change-shaped historical clutter
- establish a clear ownership boundary between specs, efforts, decisions, references, hubs, and code-anchor notes
- keep recent high-signal work, especially ontology-browser and structural-node work, easy to carry forward into the canonical model

## Non-Goals

- migrating every historical note in one pass
- forcing code-anchor notes into the starter ontology before their long-term shape is settled
- preserving the legacy `specs/` tree as an active parallel workflow
- rewriting old notes only for style when they do not carry durable value

## Requirements

### Must

- `docs/specs/` is the canonical home for normative intended-state contracts.
- `docs/efforts/` is the canonical home for bounded execution records, plans, deviations, and audit trail.
- `docs/reference/decisions/` preserves durable architecture or product rationale that must outlive one effort.
- `docs/reference/` preserves durable supporting context, provenance-aware analysis, guides, domain notes, and requirements material that should not be phrased as normative system contract.
- `docs/**/README.md` notes act as `DocumentationHub` entrypoints: brief reading order, folder purpose, and authoritative links only.
- The legacy `specs/` tree is treated as migration input, not as an active parallel specification system.
- Legacy items may be dropped when they are obsolete, change-shaped, redundant, or already superseded by newer canonical docs.
- Legacy items that still carry durable constraints, rationale, or architectural shape must be backported before they are dropped.
- `spec.md` content from the legacy tree is backported into a smaller set of enduring subsystem or workflow specs rather than preserved as one canonical spec per historical change.
- `plan.md`, `tasks.md`, and similar execution artifacts from the legacy tree are moved into effort-style preservation or dropped when they no longer add durable value.
- `research.md`, `data-model.md`, `quickstart.md`, and similar legacy supporting docs are split between reference docs, decisions, or technical specs based on whether they are descriptive, rationale-bearing, or normative.
- the recent ontology-browser and structural-node work is treated as high-signal migration input because it describes enduring structure rather than only closed change requests; that material is now preserved in the canonical browser and ontology specs.
- Current `docs/decisions/` content is backported into `docs/reference/decisions/` as architecture decisions.
- Current `docs/reference/` and `docs/runbooks/` content is backported into `docs/reference/` families where it still carries durable value.
- Current `docs/design/` and `docs/vision/` content is split into technical specs, decisions, and reference docs rather than preserved as free-standing canonical families.
- Code-anchor notes are preserved during this migration as a dedicated sidecar family and are not forced into a premature ontology fit.

### Should

- Migration work should start with canonical contract notes and recent high-signal subsystem clusters before older low-value history.
- Each migration effort should operate on a bounded cluster such as indexing, ontology/browser, or documentation-binding.
- Backported notes should preserve provenance with `derived-from` and `last-verified` when the source matters.
- Recent migration notes should link aggressively across specs, efforts, decisions, and references so the new structure becomes more queryable than the old one.

### May

- The repo may retain a small legacy archive or provenance index while the migration is in flight.
- A later ontology revision may introduce a dedicated type for code-anchor notes if they remain a durable first-class note family.

## Open Questions

- whether the repo should keep a visible legacy archive index after the durable backport is complete
- whether code-anchor notes should eventually gain a dedicated ontology type instead of remaining a specialized sidecar reference surface
