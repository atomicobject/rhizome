---
type: ReferenceDoc
summary: "Explains why Rhizome keeps both coderefs and code anchors: explicit code-to-note links, note-to-code attachment for dependents, and layered documentation surfaces with different retrieval roles."
reference-kind: analysis
derived-from:
  - docs/reference-notes/Code - docs binding (coderefs + code anchors).md
  - docs/Rhizome documentation philosophy.md
  - docs/reference-notes/Rhizome documentation - Anchors + coderefs design.md
  - docs/reference-notes/Rhizome documentation - Layering + authoring workflow.md
last-verified: 2026-04-12
status: active
aliases:
  - Code - docs binding (coderefs + code anchors)
  - Rhizome documentation - Anchors + coderefs design
---

# Documentation binding rationale

## Summary

Rhizome needs both coderefs and code anchors because they solve different blind spots. Coderefs make contracts visible when reading code that explicitly names a note. Code anchors make contracts surface for dependent code that never added a coderef of its own. Together they create a bidirectional documentation-binding model that keeps the right notes near the code that needs them.

The repo also needs layered documentation surfaces because not every piece of knowledge belongs in the same retrieval tier. File-local comments and coderefs are tight, high-signal contracts. Broader rationale, operating guidance, and historical framing belong in reference docs and hubs that agents follow on demand.

## Why both directions matter

- Coderefs capture explicit "this code follows this note" intent close to the code.
- Code anchors capture "this note applies to implementations and dependents of this code surface" intent from the note side.
- Without coderefs, explicit source-local contract breadcrumbs disappear.
- Without code anchors, callers and dependents often operate blind because they never linked the note themselves.

## Layering implications

- The tightest retrieval surfaces should carry invariants, hazards, extension rules, and non-obvious behavior.
- Larger explanation, rationale, and workflow material should live in reference docs or hubs that can be followed intentionally.
- Code-anchor notes remain a useful sidecar family even though they do not yet fit the starter ontology cleanly.

## Practical migration guidance

- Treat the coderef contract as canonical technical-spec material.
- Treat code-anchor authoring and matching notes as supporting reference or guide material until their long-term ontology shape is clearer.
- Keep hubs thin and use them for navigation, not as the primary home of subsystem doctrine.
