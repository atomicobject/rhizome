---
type: TechnicalSpec
id: SPEC-0103
aliases:
  - SPEC-0103
summary: "Defines the shared effort contract across historical Markdown notes and HTML workspaces with explicit materials and delivery evidence."
spec-status: active
last-updated: 2026-09-13
---

# Effort representation contract

## Summary

An effort is a bounded execution record with stable identity, frozen scope, a plan, real approval, and evidence of actual delivery. This specification backports the implemented representation contract into a normative delivery source. It does not record a new human approval or declare the implementation complete.

The contract composes [[docs/specs/product/multi-format-html-notes|SPEC-0083]], [[docs/specs/technical/format-aware-root-ontology-projection|SPEC-0086]], and [[docs/specs/technical/format-aware-note-maintenance-mutations|SPEC-0087]] with [team review and approval policy](../../engineering/review-and-approval.md). Those specifications own HTML parsing, root projection, and safe mutations; this one owns effort meaning across representations. Release selection remains governed by [[docs/specs/technical/evidence-grounded-release-orchestration|SPEC-0078]].

## Goals

- Query both effort representations through one shared identity and lifecycle contract.
- Preserve historical Markdown records without migration or rewritten history.
- Make component ownership and lifecycle evidence explicit and reviewable.

## Non-Goals

- HTML DOM sections, normalized plan bodies, new approval APIs, or automatic approval.
- Inferring core effort membership from folder proximity.
- Changing release publication, accepting new product scope, or rewriting historical efforts.

## Requirements

### Representation and queryability

- `Effort` MUST remain the shared interface for `EffortNote` and `EffortWorkspace`, exposing identity, name, creation timestamp, summary, status, and approver. Mixed queries MUST return both concrete types; consumers MUST use concrete fragments for representation-specific fields rather than treating the interface as an allocation target.
- Markdown `EffortNote` MUST retain its existing inline section contract. Existing notes, identifiers, links, frozen selections, and historical lifecycle values MUST remain usable without a source rewrite.
- HTML `EffortWorkspace` MUST remain a file-backed root with canonical HTML metadata. Its entry MUST own effort identity and frozen governing scope. HTML headings MUST NOT become ontology sections or embedded stories.
- Saved effort context and frozen-spec recipes MUST cover both concrete types. Markdown lifecycle evidence MUST come from inline sections; workspace recipes MUST expose explicit component references and the linked Markdown work-log sections. A missing section MUST remain missing evidence, not an inferred approval or completion.

### Explicit components and materials

- A workspace MUST explicitly link `implementation-plan` and `work-log`; optional `materials` MUST contain only author-selected collateral. `governing-specs` MUST carry the explicit frozen spec selection.
- These fields MUST resolve only their authored values. Body links, backlinks, and adjacent files MUST NOT silently supply membership or frozen scope.
- `EffortMaterial` path selectors MAY classify plan, work-log, and supporting Markdown or HTML files. Classification MUST NOT establish membership. An unlinked file in the same folder MUST remain outside the workspace's component relations.
- The implementation plan MUST be read from its linked source. Work-log sections MAY expose lifecycle snapshots, but MUST NOT replace entry identity or silently override frozen scope. Conflicting sources MUST be surfaced before action.

### Lifecycle, scope, approval, and delivery

- Effort state MUST reflect execution truth. Planned or active frozen scope MUST NOT be silently widened; material changes require an explicit deviation or refreeze under team policy. Frozen selections MUST retain exact spec, story, and criterion targets where those units exist; requirements-only technical specs do not require invented stories.
- `plan-approved-by` MUST remain absent until actual human approval exists. Approval authorizes only the covered plan and scope. Neither a populated link, parsed section, status value, nor this specification supplies approval.
- Markdown efforts MUST retain inline original intended delivery, actual delivery, execution notes, deviations, and closure evidence. Workspaces MUST retain equivalent readable evidence in their explicitly linked Markdown work log, with append-only execution history and links to the plan and selected sources as needed.
- Closure MUST require actual delivery and the applicable verification and reconciliation evidence under [quality gates](../../engineering/quality-gates.md). `complete` and `archived` efforts MUST remain historical records; new execution belongs in a fresh effort. Missing evidence MUST NOT be treated as a passed gate.

### Release evidence

- Release collection MUST parse Markdown effort metadata and inline delivery sections, or canonical HTML workspace metadata plus its explicitly linked Markdown work log, at the same Git snapshot. Working-tree contents MUST NOT substitute for either snapshot.
- Workspace work-log paths MUST be canonical vault-relative Markdown paths. Missing, malformed, or invalid components MUST produce diagnostics and exclusion rather than fabricated delivery evidence.
- Changed explicitly linked components MUST collapse to their owning entry candidate. Folder proximity alone MUST NOT create a release candidate relationship.
- Both representations MUST retain the existing range-local selection rules: final completion, new non-placeholder actual delivery, and non-effort delivery paths in the same delivery unit. Already-complete administrative changes or post-closure evidence edits MUST NOT become a new release delivery. Related effort and pull-request evidence MUST remain deduplicated under SPEC-0078.

### Allocation and compatibility

- Allocation MUST target `EffortNote` or `EffortWorkspace` with the prospective timestamped path, never the abstract `Effort` interface. Both concrete types MUST share the `EFF` identifier namespace so collisions across representations are detected.
- Filename-derived local-minute identifiers MUST retain the `EFF-YYYY-MM-DD-HH-MM` form and deterministic collision suffixes. The allocated value MUST remain stable after creation and be mirrored in aliases. `created-at` MUST record the real creation timestamp independently; approval time MUST NOT replace it.
- Existing Markdown records MUST remain valid consumers of the shared interface and existing frozen-scope semantics. Adopting a workspace MUST NOT require conversion of prior efforts or treat their inline plans as missing linked components.

## Verification

Verify mixed interface queries and concrete fragments, inline and linked lifecycle evidence, frozen-spec discovery, cross-type identifier collisions, and exclusion of unlinked neighboring materials. Verify release parsing against parent and final Git snapshots, including missing logs and post-closure edits. Validate spec ontology shape, identifiers, links, and frozen-scope drift when this contract changes.

## Open Questions

Optional folder-cluster inference remains a future opt-in interpretation. Its authority, conflict handling, and presentation require a separate decision before implementation. Any such feature MUST distinguish inferred candidates from canonical authored membership and MUST NOT change core relations implicitly. There is no unresolved MUST-level decision in the representation contract recorded here.
