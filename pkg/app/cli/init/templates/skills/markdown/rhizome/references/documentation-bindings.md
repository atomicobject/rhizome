# Documentation bindings

Use Rhizome bindings so durable intent appears where future work needs it, without flooding every context packet.

## Choose the documentation layer

- comments/docstrings for local behavior and rationale close to code
- `CONTEXT.md` or a directory README for subsystem boundaries and operational constraints
- typed notes and reference docs for cross-cutting decisions, workflows, or domain knowledge
- hubs for navigation, not as a substitute for the smallest owning node

Choose the smallest doc-stack that makes a future question cheap: local rationale for local behavior, a `CONTEXT.md` boundary note for a subsystem contract, and a typed note or cross-cutting document only when the fact crosses that boundary. Do not create all layers by default. Update an existing explanation when work changes reusable rationale, intent, or an operating boundary. Create a note only when useful knowledge has no suitable home; do not manufacture documentation for task narration or a trivial edit. Preserve provenance and uncertainty.

`CONTEXT.md` should explain ownership, invariants, integration edges, and the reason a directory is shaped as it is. Keep it operational enough that a later agent can decide where to read next; move detailed procedure into the owning workflow or reference document.

In code, record rationale rather than narration. A short `WHY:`, `RATIONALE:`, `IMPORTANT:`, or `NOTE:` comment earns its cost only when the constraint is non-obvious, likely to be rediscovered, or connected to a durable contract.

## Bind it to work

- **coderefs**: author-facing wikilinks or `@NotePath` references in comments/docstrings
- **code anchors**: note metadata binding guidance to files, directories, or symbols so `file-context` can surface it
- **typed relations**: ontology-modeled relationships between durable note nodes
- **companion docs**: deeper authoring or workflow guidance attached to a type

Inspect existing ontology and representative content before choosing a binding. Link to the smallest durable node that explains the constraint. Keep automatically retrieved notes short, concrete, and operational; link outward for depth.

`contextInclude: true` is an ontology-relation choice, not a general documentation default: its targets consume context budget on every source read or draft. Companion docs are on-demand guidance unless an existing relation or binding causes them to surface.

After binding changes, run focused `file-context` on the intended code path and the relevant link/type validation. If the guidance does not surface where expected, diagnose indexing, anchor/coderef syntax, and scope rather than duplicating the prose elsewhere.

## Retrieval critique

Run a retrieval critique after binding work: does file context surface the right durable node, is the node small enough to be useful, and can a reader distinguish current constraint from historical background? Tighten the binding or split the document when retrieval is noisy; do not compensate by broadening ambient context.
