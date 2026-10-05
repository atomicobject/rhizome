---
summary: "Code anchors for ontology workspace-facing projection and durable NodeRef links."
tags: [type/reference, subsystem/ontology, subsystem/codeanchor]
code-anchors:
  go:
    - label: ontology-project-node
      symbol: github.com/atomicobject/rhizome/pkg/ontology.ProjectNode
    - label: ontology-node-workspace
      symbol: github.com/atomicobject/rhizome/pkg/ontology.BuildNodeWorkspace
    - label: ontology-link-service
      symbol: github.com/atomicobject/rhizome/pkg/ontology.NodeLinkService
    - label: ontology-node-link-targets
      symbol: github.com/atomicobject/rhizome/pkg/ontology.NodeLinkService.LinkTargets
    - label: ontology-edit-session
      symbol: github.com/atomicobject/rhizome/pkg/ontology.EditSession
    - label: ontology-edit-canonicalize
      symbol: github.com/atomicobject/rhizome/pkg/app/web.Server.canonicalizeOntologyEditOps
    - label: ontology-edit-preview-session
      symbol: github.com/atomicobject/rhizome/pkg/app/web.Server.previewSessionEditor
    - label: ontology-edit-diff
      symbol: github.com/atomicobject/rhizome/pkg/app/web.Server.diffOntologyEditSessionResponse
    - label: ontology-body-renders-inline
      symbol: github.com/atomicobject/rhizome/pkg/ontology.NodeBodyBlock.RendersInline
---

# Go anchor - Ontology workspace

Workspace-facing ontology code must preserve navigable identities for note roots, sections, and embedded nodes. Product behavior depends on stable links and browser/workspace payloads, not just internal projection success.

## Bound contracts

- Workspace snapshots implement [[ontology-browser-workspace#^SPEC-0014-US2-AC1]]: note, section, and embedded panes share one canonical node workspace shape.
- Derived browser views implement [[ontology-browser-workspace#^SPEC-0014-US2-AC3]]: tabs, outlines, local graph, and relation rails must stay projections over the canonical workspace.
- Link targets implement [[ontology-browser-workspace#^SPEC-0014-US5-AC3]] and [[linkable-embedded-node-identifiers#^SPEC-0023-US1-AC3]]: missing embedded block IDs are surfaced as fix plans, not fragile heading links.
- Edit sessions implement [[ontology-edit-replay-conflict-contract]]: canonicalize locators before persistence, rebase only through unrelated drift, and conflict on missing nodes, collection drift, or span ownership violations.
- `EditSession`, web canonicalization, preview, and diff paths implement [[ontology-edit-replay-conflict-contract]].
- Replay addresses note roots by path, so a sequence of edits to one note stages and saves; section and embedded refs keep structural verification.
- Ontology edit recovery preserves later source once a committed journal leaves only cleanup. Prepared journals retain source witnesses; artifact and receipt preflight protects evidence before deletion. The block-ID apply adapter uses this recovery barrier before publishing another edit.
- Workspace bodies render declared `INLINE` sections and undeclared headings inline through `NodeBodyBlock.RendersInline`, nesting undeclared headings under their containing section.

## Must-read contracts

- ![[ontology]]
- [[ontology-browser-workspace]]
- [[ontology-edit-replay-conflict-contract]]
- [[linkable-embedded-node-identifiers]]
- [[primary-semantic-chunks-and-noderef-search]]
- [[Ontology (Hub)]]
