---
type: TechnicalSpec
summary: "Defines the policy for Obsidian-compatible block identifiers on embedded ontology nodes across source shapes: preferred identifier fields double as block anchors, standalone generated anchors are fallback locators, and validation/autofix keeps links migrated."
id: SPEC-0023
spec-status: proposed
last-updated: 2026-07-16
aliases:
  - SPEC-0023
  - Linkable embedded node identifiers
---

# Linkable embedded node identifiers

## Summary

Rhizome should make ontology nodes addressable without polluting source markdown with duplicate locator lines. Top-level note nodes already have durable author-facing links through their filenames, aliases, and ordinary Markdown links. Embedded nodes can be resolved internally by `NodeRef`, structural fingerprint, authored identifier fields, and rendered heading text. When an embedded node already has an identifier field, that identifier should be the durable Obsidian block target too.

The revised policy is **identifier-backed first, usage-aware second**. If a schema-modeled embedded node has a preferred identifier field, the preferred authored form is the identifier value itself marked as an Obsidian block ID: write the `id::` value with a leading caret. Rhizome parses the semantic identifier as `SPEC-0023-US1` while also treating the same token as the node's durable external block target `#^SPEC-0023-US1`. Standalone generated `^block-id` lines remain only as a fallback for embedded nodes with no usable identifier field.

This is a refinement of the usage-driven correction: validation still must not emit "missing standalone block id" as a default-on issue, and vault-wide sweeps still must not add generated anchor lines everywhere. But validation and autofix should now be strong about the identifier-backed shape: if an identifier field is the node's stable identity, Rhizome can normalize it into block-ID form, migrate old links to the new `#^identifier` target, and prevent duplicate standalone anchors that repeat the identifier.

The spec keeps three things stable: (1) the `EnsureLinkTargetNever | Plan | Apply` contract for emitter-driven on-demand fix-up, (2) deterministic fallback block-ID generation when a node has no usable identifier, and (3) the link-target API surface used by search, agent citations, GraphQL, node workspace, and code-comment flows. What changes is the preferred locator form — from "generated block ID near the node" to "identifier field as the block ID whenever the schema gives us one."

This spec complements [NodeRef batch traversal read API](noderef-batch-traversal-read-api.md). The traversal API efficiently finds and hydrates nodes by canonical identity. This spec owns the author-facing block-ID lifecycle: when one is added, when it is kept, when it is removed, and how validation surfaces drift.

It also complements [Non-section embedded node source shapes](non-section-embedded-node-source-shapes.md). That spec generalizes embedded nodes beyond heading sections into source shapes such as list items and checkbox items. This spec's locator lifecycle applies to all embedded source shapes: a node's source shape changes where a locator can be authored, but not when linkability is required or how external links resolve.

## Policy correction note

The original revision of this spec framed block IDs as a property every embedded ontology node should eventually have, with vault-wide validation flagging any embedded node that lacked one. EFF-0007 implemented that contract literally and a vault-wide apply pass inserted block IDs on hundreds of embedded nodes — primarily acceptance criteria — that have no external references. Source markdown became visually noisy and a self-reinforcing classification loop emerged: a node became "embedded" because it had a block ID, which then re-justified its block ID on the next sweep.

EFF-0017 first narrowed the contract to usage-driven addition and removed the historical standalone-anchor noise. This revision keeps that cleanup, but changes the forward policy: embedded nodes with identifier fields should not need a separate generated anchor line at all. The identifier field is the anchor. The on-demand `EnsureLinkTargetApply` path remains; its preferred edit is now to convert or author the identifier field as a block-ID value before falling back to a generated standalone anchor.

## Goals

- preserve durable, externally-stable addressability for embedded nodes that are referenced from outside their containing file
- keep source markdown clean by avoiding duplicate standalone `^block-id` lines when the node already has an identifier field
- make schema-modeled identifier fields double as Obsidian block targets for embedded nodes
- apply the same usage-driven locator lifecycle to section-backed, list-backed, checkbox-backed, and future embedded-node source shapes
- expose a single link-target service that callers (search, agent citations, GraphQL, node workspace, copy-link, coderef emitters, agents) can use to render a link or request on-demand fix-up
- expose that link-target service through agent CLI and MCP surfaces so generators that emit links to embedded nodes (coderefs, agent agent citations, generated docs, copy-link flows) never hand-construct `note#Heading Text` and always go through the service
- treat broken external block-id references as soft references for cleanup decisions so a typo'd link does not cause loss of a real anchor
- provide deterministic, idempotent, reviewable on-demand block-ID insertion or identifier conversion when an emitter or author needs a durable target
- preserve author intent, markdown readability, heading structure, inline properties, and existing block IDs
- expose validation diagnostics that target real drift (broken external references, fragile heading-only external references, malformed/duplicate block IDs, orphan block IDs) rather than eager absence

## Non-Goals

- changing the definition of canonical `NodeRef`
- requiring block IDs for top-level note nodes
- requiring block IDs on embedded nodes that have no external citation
- using schema type as a forcing function for block-ID presence (schema/companion docs may hint at likely citation, but do not mandate eager insertion)
- using generated block IDs as the only internal identity source
- rewriting every heading or embedded-node title just to normalize style
- replacing frontmatter identifiers such as `id: SPEC-0023` with block IDs
- putting block IDs on heading text, where they pollute semantic titles and may be interpreted by other Markdown tools as part of the heading
- making all structural-only sections permanent graph nodes
- mutating files during read-only search or GraphQL queries without an explicit write/fix request
- running orphan-block-id cleanup as a default validation step
- guaranteeing that external tools will preserve block IDs after arbitrary manual edits

## Requirements

### Locator contract

#### Must

- Every embedded ontology node MUST be resolvable internally by canonical `NodeRef` using authored `id::` fields, structural fingerprints, and rendered heading text — independent of `^block-id` presence.
- An embedded node MAY have an Obsidian-compatible `^block-id` as its durable external locator. When present, it MUST use Obsidian block-link syntax (`^id`).
- When an embedded node has a schema-declared preferred identifier field, Rhizome MUST support authoring the identifier value with a leading caret.
- For identifier fields, Rhizome MUST strip the leading caret when producing semantic field values, queryable values, aliases/identifier validation, and typed projections. The semantic identifier for a caret-prefixed `SPEC-0023-US1` value is `SPEC-0023-US1`, not `^SPEC-0023-US1`.
- The same caret-marked identifier value MUST be treated as the node's Obsidian block ID for link targets, inbound reference accounting, duplicate detection, and cleanup.
- The locator lifecycle MUST apply to any schema-modeled embedded node source shape. Heading-backed sections, list items, checkbox items, and future source shapes may differ in insertion point, but not in link-target semantics.
- Rhizome MUST preserve existing valid block IDs on embedded nodes.
- Rhizome MUST treat a valid existing block ID as the preferred author-facing locator for the node it owns.
- Rhizome MUST expose a link-rendering helper that converts a canonical `NodeRef` into:
  - Markdown link target
  - Wikilink target
  - display label
  - whether the target already exists in source
  - whether a fix is required before the link is durable
- Rhizome MUST distinguish "canonical internal ref" from "author-facing link target" in API payloads.
- The link-rendering helper MUST report `RequiresFix: true` only when the caller asked to render a durable external link and no `^block-id` exists yet — not as a vault-wide assertion that the node "should" have one.

#### Should

- Link rendering should prefer aliases/frontmatter identifiers for top-level note nodes and identifier-backed block IDs for embedded nodes that have a preferred identifier field.
- Standalone generated block IDs should be short, stable, readable, and collision-resistant inside the note when no usable identifier field exists.
- Standalone generated block IDs should avoid leaking volatile byte offsets unless no better stable material exists.
- Un-cited embedded nodes without an identifier-backed block ID should render with heading-text fragments and a "requires fix to make durable" hint when the caller needs durability.

### Identifier-backed block IDs

Identifier-backed block IDs are the preferred locator for schema-modeled embedded nodes with stable IDs.

#### Must

- A schema-declared preferred identifier field on an embedded node MUST be eligible to act as that node's block ID.
- The preferred authored shape for embedded identifiers that need external addressability is:

  ```md
  - id:: ^SPEC-0023-US1
  ```

- Rhizome MUST NOT include the leading caret in typed field values, identifier comparisons, `@identifier` uniqueness checks, or rendered labels.
- Rhizome MUST treat the identifier token after the caret as the block ID, so external links target `note#^SPEC-0023-US1`.
- Identifier-backed block IDs MUST use an Obsidian-safe identifier shape. Existing dotted embedded identifiers such as `SPEC-0023.US1` MUST be canonically migrated to block-safe identifiers such as `SPEC-0023-US1`; Rhizome MUST NOT preserve a separate dotted semantic value behind a different block target.
- If a node has both an identifier-backed block ID and a standalone block ID line that normalizes to the same identifier, validation MUST flag the standalone anchor as redundant and offer safe removal.
- If a node has an identifier field and a different standalone block ID that external links still target, validation MUST preserve the standalone anchor until links are migrated or the user explicitly confirms removal.

#### Should

- Rhizome should prefer changing a dotted `id::` value to a block-safe, caret-prefixed identifier-backed value over adding a separate generated anchor line when `EnsureLinkTargetApply` makes the node durable.
- Link migration should update existing `note#^old-generated-id` references to the identifier-backed target when validation can prove the old anchor and identifier belong to the same node.
- Authoring guides should teach identifier-backed anchors for externally cited embedded nodes and avoid adding block IDs to headings.
- Identifier-backed anchors should be normalized consistently across parser, indexer, GraphQL, search packets, validation, and autofix output.

### Block ID lifecycle

The lifecycle below replaces the prior "every embedded node should have one" framing.

#### Must

- A `^block-id` MUST be added to an embedded node only when one of the following triggers fires:
  1. an emitter calls the link-target API with `EnsureLinkTargetApply` for that ref (coderef generation, agent agent-citation emission, copy-link UI, programmatic citation flow), or
  2. an author writes an external `note#^new-id` link whose target does not yet exist and the editor/edit-session resolver inserts the matching anchor, or
  3. an author or maintainer explicitly invokes the on-demand "make this node linkable" action through the node workspace or validation workspace.
- Rhizome MUST NOT insert block IDs on embedded nodes through default validation/apply flows when no external citation exists for the node.
- Rhizome MUST keep an existing `^block-id` while at least one external reference targets it, where "external" means a link from outside the node's containing file.
- Rhizome MUST treat broken external `note#^id` references as soft references for retention purposes: a `^id` whose only inbound references are broken (typo'd or stale) MUST still be retained by default, and the broken-link diagnostic remains the surfacing channel for the typo.
- Rhizome MAY remove a `^block-id` only through an explicit, opt-in orphan-cleanup pass that the caller invokes deliberately.

#### Should

- The lifecycle should be agnostic to schema type. The schema describes what a node *is*; usage decides whether the node has a durable external address. Type-level hints (such as primary semantic retrieval) may correlate with high citation likelihood but must not auto-trigger insertion.
- The implementation should classify a node as `EMBEDDED` based on schema role and parent containment, not on the presence of a `^block-id`. A stray block ID on a structurally non-embedded section should surface as a separate diagnostic rather than promote the node.
- When a node has a preferred identifier field, on-demand linkability should convert that field into identifier-backed block-ID form instead of inserting a separate line.

### Fallback block ID generation

Standalone generation is the fallback for nodes without a usable identifier field.

#### Must

- Fallback generated IDs MUST be unique within the owning markdown file.
- Fallback generated IDs MUST be deterministic for the same node when the node has stable authored identity, such as an embedded story id.
- When deterministic generation collides, Rhizome MUST add a deterministic suffix rather than replacing an existing block ID.
- Fallback generated IDs MUST use a conservative character set compatible with Obsidian block references.
- Generation MUST avoid changing user-authored block IDs unless the ID is invalid and the caller explicitly requests repair.

#### Should

- For embedded nodes without authored ids, generation should derive from type plus heading text, then add a short stable suffix if needed.
- The generation policy should be centralized so on-demand fix-up, edit sessions, and link rendering agree.
- Fallback standalone anchors should be migrated to identifier-backed anchors when a stable identifier field is later added.

### Validation

Validation surfaces real drift, not eager absence. The default check set is **broken-incoming**, **malformed**, **duplicate**, and identifier/block-id mismatch. Two opt-in checks (**fragile-external** and **orphan-block-id**) target cleanup and migration workflows.

#### Must

- Validation MUST NOT report missing `^block-id` on embedded nodes as a default-on issue.
- Validation MUST understand identifier-backed block IDs and normalize leading-careted identifier field values before checking required identifier values, identifier uniqueness, aliases, typed queries, and ontology projections.
- Validation MUST detect duplicate block IDs inside one note.
- Validation MUST detect malformed block IDs that Rhizome cannot safely target.
- Validation MUST detect broken external `note#^id` references and report them through the existing broken-link diagnostic, separately from block-ID diagnostics.
- Validation MUST detect identifier/block-id drift: a preferred identifier field and a standalone block ID attached to the same node disagree, duplicate each other, or use incompatible normalized forms.
- Validation MUST detect old links that target removable/generated anchors when the same node now has an identifier-backed block ID, and MUST offer a rewrite fix for those links before removing the old anchor.
- Validation MUST keep ontology diagnostics separate from block-ID diagnostics.
- Validation MUST surface stray `^block-id` lines that sit on sections the schema does not classify as embedded as a distinct diagnostic, separate from missing/duplicate/malformed.
- Validation issues that do reference a block-ID concern MUST include:
  - note path
  - canonical `NodeRef`
  - resolved type (when known)
  - current heading or label
  - expected/generated block ID when deterministic
  - fix safety classification

#### Should

- Validation should support a scoped mode for one file, one type, or one set of `NodeRef`s.
- Validation should expose a default-safe fix that converts dotted `id::` values to caret-prefixed block-safe values when the schema declares that field as the preferred identifier and the block-safe value is unambiguous.
- Validation should expose a link-migration fix that rewrites `#^old-generated-id` links to `#^SPEC-0023-US1` when the old anchor and identifier-backed anchor resolve to the same node.
- Validation should expose a redundant-anchor cleanup fix that removes a standalone anchor only after all known inbound links target the identifier-backed block ID.
- Validation should expose an opt-in **fragile-external** check that flags external `note#Heading Text` references whose target heading has drifted (no longer present, or matches a different node by structural fingerprint) and offers a block-ID upgrade as the suggested fix.
- Validation should expose an opt-in **orphan-block-id** check that flags `^block-id` anchors with zero external references (resolved or broken) and offers safe removal. The check MUST treat broken external references as soft holds — a `^id` is not orphan if any broken external reference plausibly targets it.
- The orphan check MAY apply a fuzzy similarity heuristic between broken-link fragments and candidate orphans in the same target file. When such a near-match is detected, the orphan MUST be excluded from the auto-applied removal batch and surfaced with a "possibly referenced via broken link" warning instead.
- Validation should explain when an embedded node is not safely fixable because the source range is ambiguous.

### On-demand fix-up and editing

`EnsureLinkTargetApply` remains the only write-capable path that makes a node externally durable. The trigger conditions in the lifecycle section above bound when this path may be invoked.

#### Must

- Rhizome MUST provide a safe fix operation that inserts a `^block-id` for an embedded node when an emitter or author explicitly requests it.
- When the target node has a preferred identifier field, fix-up MUST prefer converting or authoring that field as an identifier-backed block ID over adding a standalone generated anchor line.
- For non-section embedded nodes, fix-up MUST choose a source-shape-owned insertion point rather than assuming a heading-owned standalone anchor line. For example, a list-style node may use an item-local identifier field, a trailing Obsidian block marker, or an owned continuation line according to the source-shape contract.
- Fix-up MUST be available through:
  - the link-target service in `EnsureLinkTargetApply` mode (called by coderef emitters, agent agent-citation flows, and copy-link UI),
  - the edit session / node workspace "make this node linkable" action,
  - the validation apply-session for the explicit on-demand fixes the caller selected (not as a default sweep).
- Fix-up MUST preserve markdown body text, inline properties, child sections, and existing block IDs.
- Fix-up MUST write the block ID at a stable, Obsidian-compatible location for the node.
- Fix-up MUST NOT put block IDs on heading lines when a usable identifier field exists.
- Fix-up MUST be idempotent: running it twice should not add duplicate block IDs.
- Fix-up MUST be reviewable as a normal file edit.
- Fix-up MUST fail closed when the target node cannot be matched to a current source range.

#### Should

- The preferred insertion point for identifier-backed embedded nodes should be the identifier field value, written with a leading caret when a durable target is needed.
- The preferred fallback insertion point for nodes without identifier fields should be a standalone anchor line owned by the node, not the heading text.
- For list-style and checkbox-style nodes, the fallback insertion point should remain visually attached to the item while preserving the item text and checkbox token.
- Fix-up should operate on a parsed document snapshot and byte ranges rather than ad hoc string replacement.
- Batch fix-up (when caller has explicitly selected multiple refs) should group edits per file and apply them from bottom to top so byte ranges remain valid.
- Fix-up should integrate with conflict detection in edit sessions.

### Cleanup pass (opt-in)

The orphan-block-id cleanup is a deliberate, reviewed operation. It is never default-on.

#### Must

- The cleanup pass MUST be invoked explicitly (`rzm validate fix orphan-block-ids`, validation workspace cleanup action, or equivalent).
- The cleanup pass MUST treat broken external references as soft holds: a `^id` with any broken inbound reference (resolved or near-match) is excluded from automatic removal.
- The cleanup pass MUST preserve authored identifier-backed block-ID lines whose schema field is populated on creation as the node's **durable structural identity** (e.g. `UserStory.id` with `populate: ON_CREATE`) even when they are not externally referenced. Removing the authored line on these fields would destroy author-facing identity that other notes and efforts cite.
- The cleanup pass MUST treat authored identifier-backed block-ID lines as **opt-in linkability** when the schema models the field as derivable identity with `populate: ON_LINK`. When such a line has no inbound external references (resolved or broken near-match), the cleanup pass MAY remove the entire `id::` line; the semantic id remains derivable from structure, and the heading text plus parent id continue to resolve internal references. Removal still goes through the same reviewable fix list and soft-hold rules as standalone-anchor cleanup.
- Embedded nodes with no identifier field, such as spec-driven acceptance criteria, use plain standalone block locators for opt-in linkability. Cleanup MAY remove those locators under the same orphan rules; it MUST NOT invent or require an `id::` field.
- The cleanup pass MUST present its proposed removals as a reviewable fix list before any source mutation.
- The cleanup pass MUST be idempotent and MUST NOT remove block IDs gained between plan and apply.
- The cleanup pass MUST NOT depend on prior resolution of broken-link diagnostics; it operates on the current evidence and reports cross-cutting findings (broken inbound references near orphans) as warnings rather than as blocking gates.

#### Should

- The cleanup pass should integrate with the validation apply-session and edit-session conflict detection so it composes with concurrent edits.
- The cleanup pass should order migration before removal: rewrite old links to identifier-backed targets, then remove redundant standalone anchors.
- The cleanup pass should report removed-anchor counts, kept-anchor counts (with reason: "external references", "broken-link soft hold", "fuzzy near-match"), and skipped-anchor counts after each run.

### On-demand linkability

Search, agent-citation, and code-comment workflows often discover a node before it has an authored block ID. The API MUST support a two-phase contract:

1. read-only link preview: "this node would be linkable as X, but source needs fix-up"
2. explicit fix-up: "modify the identifier field or fallback anchor to insert X, then return the durable link target"

Read-only workflows MUST NOT mutate files just because they rendered a result. Write-capable workflows MAY request fix-up before emitting a link into code comments, notes, or generated documentation. The on-demand path is the primary insertion trigger; eager vault-wide insertion is excluded by the lifecycle rules above.

### API shape

The core API SHOULD expose linkability as a node service, not as a GraphQL-only feature.

```go
type NodeLinkService struct {
    Store      OntologyReadStore
    NoteReader obsidian.NoteReader
    Schema     *ontology.Schema
}

type LinkTargetRequest struct {
    Refs       []ontology.NodeRef
    Format     LinkFormat
    Ensure     EnsureLinkTargetMode
    Purpose    string
}

type LinkTargetResult struct {
    Targets     map[string]NodeLinkTarget
    FixPlan     *NodeLinkFixPlan
    Diagnostics []NodeLinkDiagnostic
}

type NodeLinkTarget struct {
    Ref          ontology.NodeRef
    Markdown     string
    Wikilink     string
    DisplayLabel string
    Exists       bool
    RequiresFix  bool
    BlockID      string
}
```

`EnsureLinkTargetMode` should make write behavior explicit:

```go
const (
    EnsureLinkTargetNever EnsureLinkTargetMode = "never"
    EnsureLinkTargetPlan  EnsureLinkTargetMode = "plan"
    EnsureLinkTargetApply EnsureLinkTargetMode = "apply"
)
```

- `never`: read-only; return existing targets and missing-target diagnostics
- `plan`: return a safe edit plan but do not mutate files
- `apply`: apply safe edits through the edit/apply-session path and return durable targets

### Integration points

#### NodeRef traversal/read API

- Node traversal results SHOULD optionally include link targets for returned nodes.
- Traversal diagnostics SHOULD identify nodes that need link fix-up before being used in generated markdown or code comments.
- Batch hydration SHOULD reuse link-target generation across repeated refs in one scope.

#### Search and agent citations

- Search results that surface embedded nodes SHOULD include link-target metadata when requested by the caller.
- Semantic indexing MAY store canonical link targets for embedded nodes when identifier-backed or standalone block IDs already exist.
- Semantic indexing MUST NOT silently mutate source files during read-only indexing.
- A write-capable indexing or maintenance command MAY run identifier-backed block-ID fix-up before indexing link targets.

#### Code comments and coderefs

- Any workflow that emits links to embedded nodes in code comments MUST either:
  - use an existing durable block target, or
  - request `EnsureLinkTargetPlan`/`Apply` before writing the comment.
- Generated coderefs SHOULD use the durable block target when linking to an embedded node.
- If fix-up is required but write access is not available, the generated output MUST say that the node is not yet linkable rather than emitting a fragile heading-only link.

#### Validation workspace

- Missing embedded-node block IDs MUST NOT appear as default validation issues.
- Identifier/block-id mismatch, redundant standalone anchors, and stale links to old generated anchors SHOULD appear as validation issues with reviewable fixes.
- The opt-in **fragile-external** check SHOULD surface external heading-only references whose target heading has drifted, with block-ID upgrade as the suggested fix.
- The opt-in **orphan-block-id** check SHOULD surface anchors with no inbound external references and offer reviewable removal.
- The modified-file review flow MUST show inserted or removed block IDs before apply/commit.
- Batch selection SHOULD allow users to apply identifier conversions, link rewrites, on-demand block-ID insertions, or orphan removals to multiple selected refs at once, but the selection MUST be the user's, not a default sweep.

#### Node workspace

- Node workspace SHOULD expose the current link target for the focused node.
- If the focused embedded node lacks a durable block target, node workspace SHOULD expose an explicit "make this node linkable" action that triggers `EnsureLinkTargetApply` for that single ref. For nodes with identifier fields, the edit should convert the identifier to the identifier-backed form. Absence of a block ID is not, by itself, a workspace-level warning.
- After fix-up, workspace refresh SHOULD return the updated canonical ref/link target.

#### Agent CLI and MCP

The link-target service MUST be reachable from the standard agent surface (read-only) and from the read-write MCP / local CLI (apply-capable), so emitters never have a reason to hand-construct `note#Heading Text` for embedded nodes.

##### Must

- Rhizome MUST expose a dedicated agent command (`rzm agent node-link`) and a corresponding MCP tool that wrap `NodeLinkService.LinkTargets` / `Locators` for one or many `NodeRef`s. The command MUST accept refs by canonical `NodeRef` payload, by `path#^id`, by `path#Heading Text`, or by typed `find` selector, and MUST return per-ref `markdown`, `wikilink`, `displayLabel`, `exists`, `requiresFix`, `blockId`, status, diagnostics, and (when in plan/apply mode) the fix actions.
- The agent command MUST default to `EnsureLinkTargetNever` when invoked without a mode flag and MUST accept `--ensure plan` to return the safe edit plan without mutating files.
- The agent command MUST gate `--ensure apply` behind an explicit flag and the read-write capability check that already governs other apply paths. When invoked through a read-only agent surface, requesting apply MUST return a structured "apply requires read-write surface" diagnostic instead of a bare error string.
- The MCP tool MUST refuse `EnsureLinkTargetApply` when the server is read-only and MUST surface a structured diagnostic that names the read-write requirement (mirroring the existing `file_context` apply guard).
- The agent command and MCP tool MUST batch many refs in one call without reparsing the same note repeatedly, mirroring the same per-note snapshot reuse `LinkTargets` already provides.
- Emitter contracts that already exist (coderef generation, agent agent-citation emission, copy-link UI, generated-doc flows) MUST go through this surface rather than hand-rendering `path#fragment` strings; documentation MUST point at the agent/MCP entry point and not at internal Go APIs.

##### Should

- The agent surface should accept either single `--ref` flags (repeatable) or a JSON refs payload for batch use.
- The agent surface should expose `--purpose` (free-form string) so diagnostics carry the caller intent, matching `LinkTargetRequest.Purpose`.
- The agent surface should also surface the same locator status enum (`linkable | requires_fix | unsupported | unresolved`) used by the GraphQL/web layer so callers branch on a single contract.
- Agent-workflow guidance should reference this command as the only sanctioned way to obtain or repair an embedded-node link target outside the workspace.

### Storage and indexing

- The canonical block ID SHOULD be stored in indexed node/link metadata when available.
- Indexed field values MUST store identifier-backed values without the leading caret.
- The index SHOULD preserve a mapping from canonical `NodeRef` to author-facing link target.
- Reindexing MUST notice newly inserted block IDs and update node/link metadata.
- If a block ID changes manually, the index SHOULD treat it as an updated author-facing locator while preserving the best possible canonical node match.

### Performance and safety

- Link-target rendering for many refs MUST batch note snapshot loads.
- Fix planning for many refs MUST group work by note path.
- Batch fix-up MUST apply a bounded number of edits per file and report truncation when limits are reached.
- The API MUST avoid reparsing the same note snapshot repeatedly within one request/job scope.
- The API MUST expose diagnostics for parsed files, planned fixes, applied fixes, skipped refs, and unsafe refs.

### Migration strategy

The original migration delivered the eager-insertion contract (EFF-0007). This revision retires the eager pieces and introduces lifecycle/cleanup pieces.

Steps (incremental; existing read-side primitives remain in place):

1. Retire the default-on `missing_embedded_block_id` validation issue and the corresponding `BuildOntologyFixes` default-fix emission. Keep the underlying detection function callable by explicit/scoped runs.
2. Decouple `sectionNodeRef` classification from block-ID presence: schema role and parent containment determine `EMBEDDED`, and stray block IDs on non-embedded sections become a separate diagnostic.
3. Add the **orphan-block-id** validation check (opt-in flag, plan/apply via existing fix-session machinery, broken-link soft-reference handling).
4. Add the **fragile-external** validation check (opt-in flag, suggests block-ID upgrade for drifted heading-only references).
5. Run a one-shot vault cleanup pass (using the same orphan-cleanup logic) to remove the ~330+ block IDs that were inserted by the prior eager sweep and have no external references. Surface near-matches as warnings rather than auto-stripping.
6. Add identifier-backed block-ID parsing and projection: a caret-prefixed `id::` value yields semantic field value `IDENTIFIER` and block target `#^IDENTIFIER`.
7. Add validation/autofix for identifier conversions, redundant standalone anchors, and stale links to old generated anchors.
8. Run a link migration over this repo so surviving links target identifier-backed block IDs where the target node has a preferred identifier.
9. Update agent skills, rhizome-md templates, and authoring guides so new embedded nodes use identifier-backed block IDs when durability is needed and otherwise omit standalone anchors.
10. Persist/index link-target metadata stays as today; cleanup-driven removals and identifier-backed conversions must invalidate the relevant rows in `ontology_nodes`.

### Testing requirements

- Unit tests MUST cover the lifecycle invariants: a block ID is inserted only via `EnsureLinkTargetApply` triggers; an existing block ID with external references is preserved; an orphan with broken-only references is preserved; an orphan with no inbound references at all is removable.
- Unit tests MUST cover existing block IDs, malformed block IDs, duplicates, collision suffixing, and idempotent fix-up.
- Unit tests MUST cover identifier-backed fields: a caret-prefixed `id::` value parses to semantic value `IDENTIFIER`, exposes block ID `IDENTIFIER`, and does not include the caret in typed query results.
- Validation tests MUST cover identifier conversion fixes, redundant standalone-anchor fixes, and link rewrite fixes from old generated anchors to identifier-backed anchors.
- Projection tests MUST cover heading-backed embedded nodes and nested embedded nodes.
- Projection and edit-session tests MUST cover every supported non-section embedded-node source shape once such shapes are implemented, including locator insertion and removal for list-style nodes.
- Edit-session tests MUST prove on-demand fix-up composes with other field edits and conflict detection.
- Validation tests MUST prove the default check set does NOT emit `missing_embedded_block_id` and that the opt-in **fragile-external** and **orphan-block-id** checks emit only when explicitly requested.
- Validation tests MUST cover the broken-link soft-reference behavior: an orphan whose only inbound reference is a broken external link is retained.
- Validation tests SHOULD cover the fuzzy near-match warning path so a typo'd inbound reference excludes a candidate orphan from auto-removal.
- Search/card tests MUST prove read-only link rendering does not mutate files.
- Code-comment/coderef tests SHOULD prove generated links either use durable block targets or request `EnsureLinkTargetApply` before emission.
- Integration fixtures SHOULD include embedded nodes with and without block IDs, plus broken external references targeting both classes.
- Integration fixtures SHOULD include identifier-backed block IDs and migrated links that target them.

## User Stories

### US1 - Link target for any surfaced node

- id:: ^SPEC-0023-US1
- summary:: Callers can ask for the Markdown/Wikilink target for any surfaced node so they can cite it in notes or code comments.
- status:: ready

#### Acceptance Criteria

- Top-level note nodes return existing file/alias-based link targets.
- Embedded nodes with block IDs return `path#^block-id` targets.
- Embedded nodes with identifier-backed block IDs return `path#^identifier` targets while typed field values omit the caret.
- Embedded nodes without block IDs return a heading-based preview target plus `RequiresFix: true` only when the caller asked for a durable external link, and the generated block ID is exposed in plan mode. ^SPEC-0023-US1-AC3
- The API can process many refs in one call without reparsing the same file repeatedly.

### US9 - Default validation reports drift, not absence
- id:: ^SPEC-0023-US9
- summary:: Default validation flags duplicate, malformed, broken, and stray block-ID drift but does not flag missing block IDs on un-cited embedded nodes, so source markdown stays clean by default.
- status:: ready
effort:: EFF-2026-05-01-12-00

#### Acceptance Criteria

- Default validation does not emit `missing_embedded_block_id` for embedded nodes that have no external references.
- Default validation continues to report duplicate and malformed block IDs.
- Default validation reports identifier/block-id drift, including redundant standalone anchors that duplicate identifier-backed anchors and stale links to generated anchors when an identifier-backed target is available.
- Broken external `note#^id` references continue to surface through the existing broken-link diagnostic, separately from block-ID diagnostics.
- Stray `^block-id` lines on sections that the schema does not classify as embedded surface as a distinct diagnostic, separate from missing/duplicate/malformed.
- `BuildOntologyFixes` no longer emits an `ensure_block_id` action by default; the apply session can still execute one when the caller selects an on-demand insertion through the link-target service.

### US10 - On-demand make-linkable flow
- id:: ^SPEC-0023-US10
- summary:: Workflows can make a discovered embedded node linkable before emitting the link.
- status:: ready

#### Acceptance Criteria

- A write-capable caller can request `EnsureLinkTargetApply` for a specific embedded `NodeRef`.
- The file is modified only through the safe edit/apply path.
- If the target node has a preferred identifier field, fix-up converts that field to identifier-backed block-ID form and returns the identifier-backed link target.
- If the target node has no usable identifier field, the returned link target is durable and points at the newly inserted fallback block ID.
- If the fix is unsafe or write access is unavailable, the caller receives a structured diagnostic instead of a fragile link.

### US4 - Orphan block-id cleanup with broken-link soft holds

- id:: ^SPEC-0023-US4
- summary:: Validation identifies block IDs with no inbound external references and offers reviewable removal, while preserving anchors that broken external references plausibly meant to target.
- status:: ready
effort:: EFF-2026-05-01-12-00

#### Acceptance Criteria

- `rzm validate orphan-block-ids` (and the matching workspace cleanup action) is opt-in and never runs as part of the default check set.
- A block ID with zero inbound external references (resolved or broken) is reported as removable.
- A block ID with at least one broken inbound external reference targeting it (exact-string match) is retained and surfaced as "broken-link soft hold" rather than removable.
- A block ID whose only inbound references are broken near-matches (configurable similarity heuristic) is retained and surfaced with a "possibly referenced via broken link" warning, excluded from the auto-applied removal batch.
- The cleanup pass is idempotent, presents a reviewable fix list before mutation, and reports removed/kept/skipped counts after each run.
- The cleanup pass does not depend on prior resolution of broken-link diagnostics; unrelated broken links elsewhere in the vault do not block orphan removal.
- Identifier-backed block IDs are not removed as disposable orphans by default; cleanup treats them as semantic identifiers and removes only redundant or fallback standalone anchors unless explicitly told otherwise.

### US5 - Schema role drives embedded classification

- id:: ^SPEC-0023-US5
- summary:: A node's `EMBEDDED` classification comes from schema role and parent containment so that adding or removing a `^block-id` does not change a section's identity.
- status:: ready
effort:: EFF-2026-05-01-12-00

#### Acceptance Criteria

- A heading-derived section is classified `EMBEDDED` if and only if the schema declares the section's type as embedded or the parent type's containment binding declares the child as embedded.
- A stray `^block-id` on a section that the schema does not classify as embedded does not promote the section to `EMBEDDED`; instead, validation reports a stray-block-id diagnostic.
- Removing a `^block-id` from an embedded node leaves the node's `EMBEDDED` classification unchanged. The rendered `NodeID` and the current structural fingerprint may shift because the fingerprint fast-path keys on authored anchors when present, but the projection resolver continues to find the node through heading slug plus sibling ordinal. Making the fingerprint fully anchor-invariant is tracked as a follow-on rather than scoped into this effort.
- The change does not break the existing block-ID-keyed resolution path: nodes that legitimately carry a `^block-id` continue to resolve through it.

### US11 - Cleanup of historical eager-inserted block IDs
- id:: ^SPEC-0023-US11
- summary:: A one-shot cleanup that removes block IDs inserted by the prior eager sweep and have no external references, so existing source markdown stops carrying noise.
- status:: ready
effort:: EFF-2026-05-01-12-00

#### Acceptance Criteria

- Running the orphan-block-id cleanup against the current vault removes the block IDs that no external block reference targets, while preserving those that are referenced.
- The cleanup run produces a reviewable diff and reports counts; nothing is removed without explicit apply confirmation.
- Anchors that are referenced via broken inbound links (likely typos) are retained and surfaced as warnings so the underlying broken link can be fixed separately.
- Post-cleanup, default validation runs cleanly: no `missing_embedded_block_id` issues remain (because the diagnostic is retired), and no orphan-block-id warnings remain in the default check set (because the cleanup check is opt-in).

### US7 - Identifier-backed block IDs and link migration
- id:: ^SPEC-0023-US7
- summary:: Embedded-node identifier fields double as durable Obsidian block targets, and validation/autofix migrates existing links to those targets.
- status:: ready
effort:: EFF-2026-05-01-12-00

#### Acceptance Criteria

- The parser treats a preferred embedded identifier field with a caret-prefixed value as semantic identifier `SPEC-0023-US1` and block ID `SPEC-0023-US1`. ^SPEC-0023-US7-AC1
- Typed projections, ontology queries, alias/identifier validation, and indexes never include the leading caret in identifier values.
- Validation offers an autofix to convert eligible embedded identifiers from non-block form to block-backed form using an Obsidian-safe identifier shape.
- Validation offers rewrite fixes for links that target old generated anchors when the target node now has an identifier-backed block ID.
- Validation offers safe removal of redundant standalone anchors only after link migration proves the identifier-backed target receives all known inbound references.
- Authoring guides and starter templates teach identifier-backed block IDs for externally cited embedded nodes and avoid heading-embedded block IDs.

### US8 - Identifier population policy for embedded nodes

- id:: ^SPEC-0023-US8
- summary:: Embedded identifiers distinguish derivation from markdown population, and embedded nodes without identifiers receive plain locators only on citation — so stories keep stable authored ids while acceptance criteria stay body-first and linkable without `id::` noise.
- status:: ready

#### Acceptance Criteria

- Embedded identifier derivation is explicit.
  `@identifier(preferred: true)` on a field whose containing type is `@node(locator: EMBEDDED)` is treated as derivable-identity by default. An explicit `derivable: false` arg opts out. A `derivedSuffix:` arg supplies the per-type tag used in derived ids, and `populate:` controls whether markdown must carry the identifier.
- Populate on creation requires authored ids.
  `populate: ON_CREATE` means authors must write the block-safe identifier line when creating the node. For section-backed embedded nodes, prefer a metadata bullet such as `- id:: ^SPEC-0023-US1`. Spec-driven `UserStory.id` uses this policy, so stories author the story id immediately and validation treats missing story ids as defects.
- Populate on link defers authored ids.
  `populate: ON_LINK` means the semantic id may stay derived until an external citation requires a durable target. On-demand fix-up writes the identifier-backed line; orphan cleanup may remove it when references drop to zero.
- Identifierless embedded nodes use plain locators.
  Embedded nodes without `@identifier`, including spec-driven `AcceptanceCriterion`, do not have semantic `id::` fields. When one is externally cited or selected into effort scope, `EnsureLinkTargetApply` mints a plain standalone block locator.
- Uncited criteria do not need locators.
  Default validation does not require block locators on uncited identifierless embedded nodes and does not eagerly insert them. Bare acceptance-criterion list items are still under-authored when they lack a clear testable description.
- Authoring guidance teaches the distinction.
  Authoring guides, starter templates, the `rhizome` ontology-authoring and structured-markdown references, and the `specify` / `effort-new` skills teach derive vs populate for identifier fields, authored story ids on creation, and plain on-demand locators for acceptance criteria.

## Open Questions

- Which embedded identifier fields should be converted eagerly by validation autofix, and which should wait until external citation proves the need?
- What similarity heuristic (Levenshtein distance, prefix overlap, token overlap) and threshold should the orphan check use to decide that a broken inbound reference is a near-match for a candidate orphan? Should the threshold be configurable?
- Should the fragile-external check auto-apply block-ID upgrades when both the source link and the target are write-eligible, or always require explicit user confirmation?
- Should `rzm index` keep auto-detecting newly inserted block IDs (yes — read-only) and also auto-detect removals from the cleanup pass for index invalidation, or should the cleanup pass invalidate explicitly through the apply-session?
- Should link targets be stored beside semantic primary chunks immediately, or derived on demand until the new lifecycle has settled?
- Should the agent `node-link` command accept multi-ref input as repeated `--ref` flags only, or also as a stdin/JSON payload for very large batches (and should that payload format reuse the existing `LinkTargetRequest` JSON shape)?
