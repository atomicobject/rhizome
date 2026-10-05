---
type: ReferenceDoc
summary: "Review checklist for preserving important knowledge while consolidating Rhizome's legacy specs and notes into the spec-driven workflow."
reference-kind: analysis
derived-from:
  - docs/specs/process/development-loop.md
  - docs/specs/process/effort-lifecycle.md
  - docs/specs/process/specs-organization.md
  - docs/reference/README.md
status: draft
---

# KB Migration Preservation Review

## Purpose

Use this note for the final review pass on the knowledge-base migration. The goal is not to preserve every legacy file verbatim. The goal is to preserve every important idea, constraint, rationale, and operational warning while consolidating the corpus into the spec-driven workflow.

## Preservation Risks

- Distinct system constraints get merged into one generic spec and lose their original boundaries.
- Architecture rationale survives in prose but not in a durable decision note.
- Operational warnings, failure modes, and “gotchas” disappear because they lived only in older plans or research notes.
- One-off execution artifacts get mistaken for enduring knowledge and end up polluting the canonical docs.
- Recent, higher-value specs such as `032` and `033` get flattened even though they capture reusable system shape and should likely be retained or split into durable parts.
- Older specs that were written as “make this change” tasks are kept as if they were still authoritative, which hides the newer model of the system.
- Hub notes keep too much substance and become duplicate documentation instead of thin entry points.
- Code-anchor and coderef guidance gets dropped because it does not fit neatly into the starter ontology, even though it remains part of the retrieval contract.
- Provenance disappears, making it impossible to tell whether a note was derived from a source document, a decision, a migration, or a later backport.

## Review Checklist

### 1. Scope the note before moving it

- Identify whether the source is a normative spec, a decision, a reference note, or an execution artifact.
- Keep enduring intent and discard only transient delivery details.
- If a note mixes categories, split it instead of forcing everything into one destination.

### 2. Preserve durable meaning

- Confirm that each important constraint still exists somewhere after consolidation.
- Confirm that each lasting tradeoff or rationale exists in a decision note or an equivalent durable reference.
- Confirm that operational warnings, stale-cache behavior, watcher failure modes, and path or index invariants are not lost.
- Confirm that any system behavior users or agents depend on still has a clear home in the new structure.

### 3. Protect the highest-value recent specs

- Review the newest specs first, especially `032` and `033`, for reusable system structure.
- Keep recent specs that describe core architecture, ontology shape, read models, or cross-cutting workflows.
- If a recent spec is too implementation-specific, split it into a durable spec plus an effort or decision note rather than deleting it outright.

### 4. Retire old one-off change specs carefully

- Mark a legacy spec as archive-only only after its meaningful content has been backported elsewhere.
- Delete or demote a spec only when it adds no durable value beyond the migrated notes.
- If a legacy spec contains a unique constraint, example, or edge case, preserve that content in a reference or decision note before retiring the original.

### 5. Keep hubs thin

- Ensure hubs point to the canonical notes instead of repeating their content.
- If a hub page contains substantive guidance, move that guidance into a spec, decision, or reference doc.
- Keep the hub useful as a map, not as a second copy of the knowledge base.

### 6. Preserve retrieval-specific guidance

- Keep coderef and code-anchor guidance visible even if the ontology does not model it as a first-class family yet.
- Preserve any anchor precedence, matching rules, or retrieval caveats that affect how Rhizome finds knowledge.
- If a note exists mainly to support search, linking, or retrieval, make sure the replacement note still satisfies that operational role.

### 7. Check provenance

- Add or preserve `derived-from` links when a note comes from a source document, discussion, or earlier artifact.
- Keep the migration trail visible so later reviewers can trace why a note exists.
- Avoid rewriting migration provenance into generic prose where the source path would be clearer.

### 8. Verify the review outcome

- Every discarded note should be explainable in one sentence: archived because redundant, split because hybrid, or migrated because durable.
- Every important constraint should have a surviving home.
- Every important rationale should survive as a decision or durable reference.
- Every operational warning should remain findable from the new structure.
- The final corpus should be smaller, clearer, and easier to navigate without losing system memory.

## Exit Criteria

The migration review is complete when:

- the canonical docs have clear homes for specs, efforts, decisions, and references
- legacy notes that still matter have been migrated or backported
- legacy notes that do not matter anymore are safely archived
- no important constraint, warning, or rationale is only present in the old structure
- the new structure is usable for dogfooding Rhizome itself
