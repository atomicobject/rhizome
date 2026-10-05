---
type: TechnicalSpec
summary: "Detect and repair external `note#Heading Text` references whose target heading has drifted, and add a proactive `rzm note rename-heading` workflow that rewrites inbound references — preferring identifier-backed block-ID upgrades — when an author renames a heading in place."
id: SPEC-0054
spec-status: active
last-updated: 2026-07-16
aliases:
  - SPEC-0054
  - Heading rename safety and fragile-external link drift
---

# Heading rename safety and fragile-external link drift

## Summary

Heading-fragment external references (`note#Heading Text`) are the most fragile shape of cross-note link Rhizome supports. A heading rename, reflow, or restructure breaks every inbound reference of that shape, and Rhizome currently has no diagnostic that catches the drift and no workflow that proactively rewrites those references when a heading is renamed in place. SPEC-0023 already specifies the durable replacement (identifier-backed `note#^id` block IDs) and lists the **fragile-external** audit as the surfacing channel, but defers both the check and any rename tooling out of EFF-0015's bounded slice.

This spec owns the full mitigation: an opt-in **fragile-external** validation check that flags heading-only external references whose target heading has drifted (and offers a block-ID upgrade as the suggested fix), plus a new `rzm note rename-heading` command that detects an in-place heading rename and rewrites known vault-internal inbound references, preferring the identifier-backed `^id` upgrade so future drift can no longer break them. Together the two paths close the heading-rename hole that today causes silent reference rot.

This spec complements [SPEC-0023 linkable-embedded-node-identifiers](linkable-embedded-node-identifiers.md). SPEC-0023 owns the block-ID lifecycle and the `EnsureLinkTargetApply` contract that converts an embedded identifier into block-ID form. This spec owns the audit that *finds* heading-only references at risk, and the proactive author-driven workflow that rewrites those references when a heading rename actually happens. Both paths converge on the same outcome — heading-only external references either get upgraded to durable block-ID form or get surfaced as drift — but the entry points and UX are distinct.

## Goals

- detect external `note#Heading Text` references whose target heading has drifted or ambiguates against multiple current headings, and surface them through a single opt-in validation check
- offer a reviewable block-ID upgrade fix that routes through `NodeLinkService.EnsureLinkTargetApply` so the durable target is identifier-backed when the target node has a preferred identifier field and falls back to a generated standalone anchor only when no identifier exists
- provide a first-class `rzm note rename-heading` command (and matching MCP tool) that rewrites inbound vault-internal references when an author renames a heading in place, preferring the identifier-backed `^id` upgrade over a fragile new-text rewrite
- keep heading-rename behavior independent from file-rename and file-move: `rzm note rename` and `rzm note move` continue to own filename-portion rewriting and MUST surface a clear pointer at `rzm note rename-heading` when a heading-fragment reference would be left dangling
- expose the same surface through agent CLI and read-write MCP so generators and authoring tools can rewrite heading-only references without hand-constructing the fix

## Non-Goals

- replacing `rzm note rename` or `rzm note move` for file rename/move workflows
- automatically rewriting external (non-vault) references such as web URLs that happen to point at a heading fragment
- guaranteeing that hand-edited heading text changes outside Rhizome's tooling will be detected synchronously — the audit path catches drift after the fact, the proactive command catches it at rename time
- forcing every heading-only external reference to be upgraded to a block-ID; the audit reports drift, but a heading-only link whose target heading still resolves cleanly is not, by itself, a finding
- changing the canonical `NodeRef` definition or the `EnsureLinkTargetMode` contract from SPEC-0023
- adding a new SQLite index for heading-rename history; the audit and the rename command operate on the current vault snapshot
- inferring "this heading was renamed" automatically from filesystem state; the rename command is invoked deliberately by the author and takes the old/new heading text as input

## Requirements

### Fragile-external validation check

The check is opt-in. It runs as `rzm validate fragile-external` and through `rzm agent validate fragile-external`. It MUST never appear in the default check set.

#### Must

- The check MUST walk vault notes that contain external wikilinks of the shape `note#Heading Text` (heading-text fragment, not `^block-id`) and resolve each fragment against the target note's current heading list and structural projection.
- For each heading-fragment reference, the check MUST classify the result as one of:
  - `linkable` — heading text matches exactly (no finding).
  - `heading_drifted` — no heading in the target note matches the fragment's normalized form.
  - `heading_ambiguous` — multiple headings match the fragment's normalized form (for example, two `## Notes` sections under different parents).
- For findings other than `linkable`, the check MUST emit a `fragile_external_link` issue with: source note path, source link byte range, target note path, fragment text, classification, candidate target `NodeRef`s (when resolvable), and any block-ID upgrade fix the link-target service can plan.
- The check MUST offer a reviewable `FixKindUpgradeToBlockID` fix when the candidate target resolves to a node the link-target service can render durably. The fix MUST route through `NodeLinkService.EnsureLinkTargetApply` so identifier-backed conversion is preferred over standalone fallback anchors.
- The check MUST NOT auto-mutate files. Apply MUST go through the existing edit-session / apply-session pipeline, with the same review prompts the orphan-block-id cleanup uses.
- The check MUST treat soft normalization deltas (case differences in fragment vs heading, whitespace folding, trailing punctuation) the same way the wikilink resolver does today; cosmetic mismatches that the resolver still resolves successfully are NOT findings.
- The check MUST be runnable scoped: `--scope-note <path>` for one source note, `--scope-target <path>` for one target note, and `--scope-ref <NodeRef>` for one specific candidate target.

#### Should

- The check should reuse the same per-note snapshot cache the link-target service uses, so a single run over the vault parses each target note at most once.
- The check should expose a configurable similarity heuristic (Levenshtein distance, prefix overlap) to flag near-matches where the fragment plausibly intended a heading whose text was edited slightly. Near-matches surface as `heading_drifted` issues with a `near_match_candidate` data field rather than as a separate classification.
- The check should accept `--auto-upgrade` to apply unambiguous block-ID upgrades in batch, and refuse the flag when the candidate target is `heading_ambiguous` or has multiple plausible upgrade targets.
- The check should surface a "no candidate target" diagnostic when neither the original heading nor any near-match resolves, so authors can decide between deletion, upgrade, or a manual rewrite.
- A future persistent-state extension should classify `heading_moved` when Rhizome can compare the current parent path or sibling ordinal against a previously linkable observation.

### Heading rename command

`rzm note rename-heading <path> "<old>" "<new>" [flags]` rewrites the heading text in `<path>` and rewrites known vault-internal inbound references that target the old heading.

#### Must

- The command MUST require the source note path, the old heading text, and the new heading text. The match against the source heading MUST use the same normalization the wikilink resolver uses, so trailing punctuation or trivial whitespace differences are tolerated.
- The command MUST refuse to run when the old heading does not resolve to exactly one heading in the source note. Multiple matches return a structured "ambiguous heading" diagnostic and exit non-zero. Zero matches return a structured "heading not found" diagnostic.
- The command MUST scan vault-internal references and rewrite each `note#<old>` reference whose target node resolves to the renamed heading. The default rewrite MUST prefer the identifier-backed `^id` form via `NodeLinkService.EnsureLinkTargetApply` when the target node has a preferred identifier field; when no identifier field exists, the rewrite MUST fall back to either a standalone block-ID upgrade or `note#<new>` (configurable via `--fallback heading|block-id`, default `block-id`).
- The command MUST run through the existing edit-session pipeline and produce a reviewable diff before mutation. `--apply` is required to mutate; without it, the command operates as a plan that prints the proposed rewrites and exits.
- The command MUST be idempotent. A second invocation with the same arguments after a successful apply returns "no inbound heading references remain" and exits cleanly.
- The command MUST emit a structured `heading_rename_skipped` diagnostic for each inbound reference it cannot safely rewrite (for example, a near-match that is not a confident rewrite target, or a link inside a code block).
- The command MUST NOT modify references in non-vault paths or in files outside the configured vault scope.

#### Should

- The command should accept `--upgrade-to-block-id=auto|always|never` (default `auto`) so authors can choose whether to upgrade references at the same time as the heading rename. `auto` upgrades when the target has a preferred identifier field; `always` insists on a block-ID anchor (creating a fallback standalone if needed); `never` keeps the rewrite as `note#<new>`.
- The command should report counts after each apply: matched-references, rewritten, upgraded-to-block-id, skipped, and ambiguous.
- The command should integrate with the same conflict-detection that `EditSession` uses today, so a concurrent edit to the heading line aborts the rewrite cleanly.
- When `--upgrade-to-block-id=auto` and the target is a non-embedded section (no schema-modeled identifier field, no useful fallback target), the command should report the references as "kept as heading-text rewrite" rather than failing.

### Surface contracts

- The fragile-external check MUST be reachable through `rzm validate fragile-external` and `rzm agent validate fragile-external`. The agent surface follows the same opt-in posture as `frozen-scope-drift` and `orphan-block-ids`.
- The rename command MUST be reachable through `rzm note rename-heading`, an `rzm agent note-rename-heading` thin wrapper that emits structured JSON, and a write-capable MCP `note_rename_heading` tool. The agent CLI and read-only MCP MUST refuse `--apply` with the same `apply_requires_read_write` diagnostic SPEC-0023 specifies for the link-target service.
- The agent surface for rename MUST return per-rewrite results with: source note, source link byte range, target node `NodeRef`, rewrite kind (`block_id_upgrade | heading_text_rewrite | skipped`), and any diagnostics.
- Generated coderefs and agent citation emitters that already route through `NodeLinkService` MUST NOT need changes for this spec — the rename command upgrades existing inbound links so future emissions go through the durable target.

### Validation diagnostics

- `fragile_external_link` issues MUST include the same fix-safety classification convention as other ontology fixes (`safe`, `confirm`, `unsafe`).
- `heading_rename_skipped` diagnostics MUST include a structured `reason` enum (`code_block`, `ambiguous_target`, `unsafe_rewrite_range`, `cross_vault_reference`).
- The fragile-external check MUST keep its diagnostic codes distinct from `broken_link` and `fragment_broken_link` so the audit dashboard can show the three concerns separately.

### Performance and safety

- A single fragile-external run over a vault of N notes with M heading-fragment references MUST parse each target note at most once and MUST avoid reparsing the source notes more than once per scope.
- `rzm note rename-heading` MUST batch all rewrites in a single edit-session apply so partial-apply failures roll back cleanly.
- The rename command MUST refuse to run while another apply-session is holding the same notes, mirroring existing edit-session locking.

### Testing requirements

- Unit tests MUST cover the shipped classification outcomes (`linkable`, `heading_drifted`, `heading_ambiguous`) of the fragile-external check. `heading_moved` is deferred until persistent linkability observations exist.
- Unit tests MUST cover the block-ID upgrade fix path through identifier-backed conversion (target has identifier field) and the standalone-anchor fallback (target has no identifier field).
- Unit tests MUST cover the rename command's three flag values (`--upgrade-to-block-id auto|always|never`) and the `--fallback heading|block-id` default.
- Unit tests MUST cover idempotent re-invocation of the rename command and the ambiguous-old-heading refusal path.
- Integration tests MUST cover end-to-end `rzm note rename-heading` against a fixture vault that mixes vault-internal heading-fragment references, identifier-backed references (already durable), code-block references (skipped), and references with near-match heading text (skipped with diagnostic).
- Validation tests MUST prove the fragile-external check is not in the default check set and emits zero issues unless explicitly requested.

### Migration strategy

The check and the command can land in either order; they don't depend on each other.

1. Land the fragile-external check as the positional `fragile-external` selector (opt-in) with the upgrade-to-block-id fix routed through `NodeLinkService`.
2. Run the check against this repo's `docs/` once and apply the safe upgrades to absorb today's accumulated heading-only references that already point at nodes with identifier fields.
3. Land `rzm note rename-heading` with the three-flag rewrite contract and the agent/MCP surface.
4. Update agent skill templates, the rhizome-md authoring guidance, and `rzm note rename` / `rzm note move` help text so the rename-heading workflow is discoverable.
5. Document the contract in the agent-workflow / authoring guidance: heading renames go through `rzm note rename-heading`; heading-only references that survive belong to fragile-external's audit surface.

## User Stories

### US1 - Drifted heading-only references audit

- id:: ^SPEC-0054-US1
- summary:: An opt-in validation check that flags external `note#Heading Text` references whose target heading has drifted, so I can review and upgrade them to durable block-ID form before drift breaks anything.
- status:: ready

#### Acceptance Criteria

- `rzm validate fragile-external` (and the matching agent/MCP path) is opt-in and never runs in the default check set. ^SPEC-0054-US1-AC1
- The check classifies each heading-fragment external reference as `linkable | heading_drifted | heading_ambiguous` and emits `fragile_external_link` issues only for the non-`linkable` cases. ^SPEC-0054-US1-AC2
- Each finding includes source note path, source link byte range, target note path, fragment text, classification, candidate target `NodeRef`(s), and any block-ID upgrade fix the link-target service can plan. ^SPEC-0054-US1-AC3
- The check offers a reviewable `FixKindUpgradeToBlockID` fix that routes through `NodeLinkService.EnsureLinkTargetApply` so identifier-backed conversion is preferred over standalone fallback anchors. ^SPEC-0054-US1-AC4
- The check is runnable scoped (`--scope-note`, `--scope-target`, `--scope-ref`) and returns a fast result for a single seed without parsing the rest of the vault. ^SPEC-0054-US1-AC5
- A single run over a vault of N notes parses each target note at most once and reuses the link-target service's per-note snapshot cache. ^SPEC-0054-US1-AC6

### US2 - Heading rename rewrites inbound references

- id:: ^SPEC-0054-US2
- summary:: A `rzm note rename-heading` command that rewrites known vault-internal inbound references — preferring identifier-backed `^id` upgrades — so the rename does not silently break links.
- status:: ready

#### Acceptance Criteria

- `rzm note rename-heading <path> "<old>" "<new>"` matches the old heading using the wikilink resolver's normalization, refuses on zero or multiple matches with structured diagnostics, and exits non-zero on those failures. ^SPEC-0054-US2-AC1
- The command produces a reviewable plan by default and only mutates files when invoked with `--apply`. ^SPEC-0054-US2-AC2
- `--upgrade-to-block-id=auto` (default) upgrades inbound references to identifier-backed `^id` form when the target node has a preferred identifier field, and falls back to `note#<new>` text rewrite otherwise. ^SPEC-0054-US2-AC3
- `--upgrade-to-block-id=always` forces a block-ID anchor on the target (creating a fallback standalone if no identifier field exists), and `--upgrade-to-block-id=never` keeps the rewrite as a heading-text rewrite. ^SPEC-0054-US2-AC4
- A second invocation with the same arguments after a successful apply returns "no inbound heading references remain" and exits cleanly (idempotent). ^SPEC-0054-US2-AC5
- Each skipped inbound reference returns a `heading_rename_skipped` diagnostic with a structured `reason` enum (`code_block | ambiguous_target | unsafe_rewrite_range | cross_vault_reference`). ^SPEC-0054-US2-AC6
- The apply runs in a single edit-session batch so partial-apply failures roll back cleanly. ^SPEC-0054-US2-AC7

### US3 - Agent and MCP surface for heading rename

- id:: ^SPEC-0054-US3
- summary:: Agents and write-capable tools can invoke the fragile-external check and heading-rename rewrite via the agent CLI and MCP so heading drift can be surfaced and repaired without escaping the agent surface.
- status:: ready

#### Acceptance Criteria

- `rzm agent validate fragile-external` exposes the opt-in check with the same JSON payload shape as other agent validate checks. ^SPEC-0054-US3-AC1
- `rzm agent note-rename-heading` and the MCP `note_rename_heading` tool expose the rewrite contract, with `--apply` gated by the same read-write capability posture used by apply-capable link-target flows. ^SPEC-0054-US3-AC2
- The rename agent surface returns per-rewrite results with source note, source link byte range, target `NodeRef`, rewrite kind (`block_id_upgrade | heading_text_rewrite | skipped`), and diagnostics. ^SPEC-0054-US3-AC3
- `rzm note rename` and `rzm note move` surface a structured pointer at `rzm note rename-heading` when their normal path-portion rewrite would leave a heading-fragment reference dangling, rather than silently rewriting only the path portion. ^SPEC-0054-US3-AC4

## Open Questions

- Should the fragile-external check auto-apply block-ID upgrades when both the source link and the target are write-eligible (and the candidate is unambiguous), or always require explicit `--auto-upgrade`?
- What similarity heuristic and threshold should the check use to surface near-match heading edits as `heading_drifted` near-match candidates? Should the threshold be configurable, and should it match the orphan-block-id check's existing Levenshtein-2 default?
- Should `rzm note rename-heading` also offer to add an alias-style heading marker (for example, an HTML comment naming the old heading) so external editors that don't know about Rhizome can still discover the rename, or is that out-of-scope for this spec?
- Should `rzm note rename` and `rzm note move` block on dangling heading-fragment references and refuse to apply until the author runs `rzm note rename-heading`, or should they apply with a warning and rely on the audit to surface the drift afterward?
- Should the `--upgrade-to-block-id=always` mode be allowed to insert a fallback standalone anchor on the target heading, or should it be restricted to identifier-backed targets only (and treat "no identifier field" as a refusal rather than a fallback)?
