---
type: TechnicalSpec
summary: "Defines narrow, source-preserving maintenance mutations for format-aware notes: root-metadata edits, local-link retargeting, and governed note rename or move without introducing general HTML editing."
id: SPEC-0087
spec-status: active
last-updated: 2026-09-11
aliases:
  - SPEC-0087
  - Format-aware note maintenance mutations
---

# Format-aware note maintenance mutations

## Summary

Format-aware notes need a small mutation surface so validation findings, broken links, ontology metadata, and note moves can be repaired without turning Rhizome into a general HTML editor. This spec defines three supported mutation capabilities: root-metadata field edits, authored local-note link retargeting, and note-file rename or move. A format provider identifies exact authored spans and the encoding rules needed to replace them. The first delivery slice implements root-metadata editing and the shared transaction boundary. Link retargeting and file moves remain later slices under US2 and US3. Shared mutation infrastructure owns target resolution, candidate ranking, confirmation policy, transaction boundaries, drift detection, and post-apply validation rather than assuming those facilities already exist.

For HTML notes, root metadata is the canonical `rhizome-metadata` JSON block and local links are statically authored `<a href>` values or schema-declared link fields in that metadata. Changes preserve every byte outside the governed edit spans. Structural DOM editing, arbitrary source editing, and attempts to repair or rewrite authored program behavior remain outside this contract.

## Goals

- Give validation, CLI, agent, and MCP surfaces one governed plan/preview/apply contract for supported note-maintenance changes regardless of note format.
- Preserve source outside exact provider-owned edit spans and preserve HTML body bytes during root-metadata changes.
- Establish one target resolver, candidate matcher, move planner, confirmation policy, transaction engine, and revalidation contract, using current Markdown behavior as the compatibility baseline and migrating Markdown callers onto the shared path.
- Let HTML notes participate safely in note rename and move workflows, including inbound reference updates and required outbound relative-note-link adjustments.
- Make unsupported, ambiguous, stale, or structurally unsafe changes explicit blocked outcomes rather than falling back to broad text replacement.

## Non-Goals

- Provide a general-purpose HTML, DOM, rich-text, or source editor in the CLI or web UI.
- Add, remove, reorder, or otherwise structurally edit HTML elements, headings, sections, element IDs, or legacy anchor names.
- Repair HTML conformance, formatting, accessibility, scripts, styles, templates, forms, or external resources.
- Rewrite JavaScript, CSS, inline event handlers, template strings, dynamically constructed URLs, resource URLs, or runtime navigation.
- Check external URL availability or retarget external links.
- Make HTML ontology projection structural; metadata changes remain root-only.
- Introduce format-specific candidate matching, confirmation heuristics, transaction engines, or mutation safety classes.
- Create or scaffold new Markdown or HTML notes; this contract only maintains existing authored sources.

## Mutation Contract

### Provider responsibilities

Each note-format provider declares granular mutation capabilities rather than a binary `editable` flag. For each supported operation, it returns the authored source span, decoded value, encoding context, and a replacement encoder that can produce a valid format-local value. Providers may parse enough surrounding structure to prove ownership and placement, but they do not choose semantic targets, rank candidates, approve actions, write files, or run transactions.

The HTML provider supports these capabilities:

- root-metadata field set, add, delete, and key rename within the one canonical `rhizome-metadata` JSON object
- schema-declared metadata link-field value replacement
- statically authored local-note or fragment target replacement in `<a href>` attributes
- note-file rename and move participation through provider-produced reference edits

If the canonical metadata block is absent, a metadata operation may synthesize the minimum valid `<head>` and direct-child metadata block required by the HTML provider contract. Duplicate or malformed canonical blocks, overlapping spans, ambiguous attribute ownership, or a replacement that cannot be encoded safely block mutation.

### Shared mutation responsibilities

The current implementation has separate Markdown read resolution, validation-only broken-link candidate matching, file-first move flows, and whole-block metadata serialization. Those are migration inputs, not an already complete shared transaction layer. Implementation MUST first define a format-neutral planner and resolver contract, adapt Markdown read/validation/move behavior to it without changing confirmed user-facing heuristics, and only then add HTML provider edits.

Shared note-maintenance infrastructure owns:

- local-note and fragment target resolution
- candidate discovery, normalization, ranking, and deterministic ordering
- safety classification as safe, confirmation-needed, or blocked
- rename and move heuristics equivalent to Markdown behavior
- plan identity, complete affected-path enumeration, source-hash and expected-span preconditions
- overlap and destination-collision detection
- preview, transactional apply, interrupted-transaction recovery, exact-path refresh, and targeted revalidation
- surface-neutral results and structured confirmation questions

The new shared planner consumes provider edits and composes them into the repair transactions defined by [SPEC-0018](validation-fix-plan-and-apply-session.md). A format provider cannot weaken those transaction, lifecycle, or conflict requirements. Existing Markdown move behavior is the confirmation and candidate-heuristic baseline, but its current file-first apply order, incomplete outbound relative-link rebasing, validation-specific candidate code, and whole-block metadata rewrites are not normative architecture and MUST NOT be copied into another provider.

### Source preservation

HTML root-metadata writes serialize the metadata object as stable, pretty-printed JSON and encode any literal sequence that could terminate the containing script element, including case-insensitive `</script` forms. Metadata mutations preserve the HTML body byte-for-byte and preserve every other byte outside the exact metadata span or the bounded insertion needed to synthesize `<head>`.

Link retargeting changes only the provider-identified authored target span and applies the provider's attribute or JSON-string escaping rules. It preserves query and fragment components unless the planned semantic operation explicitly replaces them. It does not normalize unrelated whitespace, quote style, entities, attribute ordering, JSON keys, or document formatting.

## User Stories

### US1 - Maintain root metadata without rewriting HTML content

- id:: ^SPEC-0087-US1
- summary:: A user or automated repair can make an exact root-metadata change to an HTML note while all unrelated authored content remains unchanged.
- status:: ready

#### Acceptance Criteria

- An operation can set or add a root field, delete a root field, or rename a root field key in the canonical JSON metadata object, including fields whose schema declares note links. ^SPEC-0087-US1-AC1
- A metadata preview identifies the old and new structured values, exact affected file, safety class, and source preconditions before apply. ^SPEC-0087-US1-AC2
- Applying a metadata operation produces stable pretty JSON, safely encodes script-terminating text, and leaves the HTML body byte-for-byte unchanged. ^SPEC-0087-US1-AC3
- When metadata is absent and the document shape permits an unambiguous insertion, the provider can synthesize the minimum `<head>` and canonical metadata block; malformed or duplicate metadata and unsafe document shapes are blocked without writing. ^SPEC-0087-US1-AC4
- Root-metadata validation may plan safe fixes for schema defaults whose field, value, and insertion point are uniquely determined; inferred or contentful values remain confirmation-needed or blocked. ^SPEC-0087-US1-AC5

### US2 - Validate and retarget statically authored local links

- id:: ^SPEC-0087-US2
- summary:: A user can diagnose and repair local HTML note links through the same shared candidate and confirmation workflow used for Markdown notes.
- status:: ready

#### Acceptance Criteria

- Validation reports missing or ambiguous internal note targets and fragments from static `<a href>` values and schema-declared metadata link fields while ignoring external and runtime-generated URLs. ^SPEC-0087-US2-AC1
- Candidate matching, normalization, ranking, and confirmation are shared across note formats; the HTML provider contributes only decoded authored values, exact spans, and replacement encoding. ^SPEC-0087-US2-AC2
- An approved retarget changes only the authored `href` or metadata string span, preserving unrelated source bytes plus query and fragment components not selected for replacement. ^SPEC-0087-US2-AC3
- A repair that would require creating or changing an element ID, legacy named anchor, DOM structure, script, stylesheet, template, or external URL is blocked or reported as requiring manual author action. ^SPEC-0087-US2-AC4

### US3 - Rename or move HTML notes with graph-safe reference updates

- id:: ^SPEC-0087-US3
- summary:: A user can rename or move an HTML note under the same governed heuristics and confirmation boundary as a Markdown note.
- status:: ready

#### Acceptance Criteria

- A rename or move plan includes the file operation and every recognized inbound note reference that must be retargeted across supported note formats. ^SPEC-0087-US3-AC1
- Moving a note across directories also plans required changes to statically authored outbound relative note links and metadata link fields so they continue to resolve to the same note identities. ^SPEC-0087-US3-AC2
- Existing Markdown move heuristics and confirmation policy classify equivalent HTML cases the same way after both formats route through the new shared planner; format-specific logic cannot independently approve, reject, or rank a destination. ^SPEC-0087-US3-AC3
- Destination collisions, ambiguous targets, unsupported authored locations, incomplete affected-path enumeration, overlapping edits, or source-hash drift block the connected transaction before any partial rename or rewrite is committed. ^SPEC-0087-US3-AC4

### US4 - Use one maintenance contract across non-editor surfaces

- id:: ^SPEC-0087-US4
- summary:: Operators and agents receive equivalent maintenance plans and outcomes without exposing a general web editor.
- status:: ready

#### Acceptance Criteria

- CLI, agent, MCP, and validation surfaces expose equivalent supported operations, safety classifications, affected paths, preconditions, preview data, and post-apply results. ^SPEC-0087-US4-AC1
- Human-interactive CLI flows may request confirmation for confirmation-needed actions; agent and MCP flows never prompt and instead return stable candidate evidence and structured questions. ^SPEC-0087-US4-AC2
- Web integration may stage and preview root-metadata operations through the existing edit-session model and commit them through the journaled repair transaction adapter. Other HTML maintenance findings, plans, diffs, and results remain read only until their governing stories are delivered; the web never exposes free-form HTML, source, narrative, or structural editing. ^SPEC-0087-US4-AC3
- Successful apply refreshes exactly the changed, renamed, and deleted paths, reruns the targeted validation checks, and reports any remaining findings without implying full-vault freshness. ^SPEC-0087-US4-AC4

## Requirements

### Must

- The format registry MUST advertise root-metadata edit, metadata-link retarget, authored-link retarget, and file-move participation as separate provider capabilities.
- The HTML provider MUST restrict root-metadata edits to the canonical direct-child `rhizome-metadata` JSON block in `<head>` and MUST reject duplicate or malformed block ownership.
- Root-metadata mutations MUST support set/add, delete, and key rename operations without silently coercing values outside the schema and JSON contracts.
- Validation MUST be able to report root-metadata parse and ontology findings with exact authored ranges, and it MUST limit safe default fixes to values and insertion points uniquely determined by the active schema.
- Metadata serialization MUST be deterministic, pretty-printed JSON and MUST prevent a metadata value from terminating the containing script element.
- A metadata-only edit MUST preserve the HTML body byte-for-byte and MUST preserve all other source bytes except the exact metadata replacement or bounded `<head>` insertion.
- The HTML provider MUST expose exact source spans, decoded authored targets, source encoding context, and replacement encoders for supported references; it MUST NOT choose semantic destinations or write files.
- Shared infrastructure MUST own path and fragment resolution, candidate matching, ranking, confirmation, lifecycle policy, plan identity, transaction grouping, drift detection, collision handling, recovery, refresh, and revalidation for every note format.
- Implementation MUST extract and reconcile the current Markdown read resolver, validation broken-link candidate matcher, and move/rename orchestration into that shared infrastructure before HTML mutation depends on it.
- Markdown and HTML connected move plans MUST enumerate outbound relative-link rebases and MUST commit file operations plus reference edits transactionally or recoverably; the current file-first Markdown sequence is not the target contract.
- Safety classification MUST mirror the existing Markdown policy: deterministic explicitly targeted operations may be safe, heuristic target selection requires confirmation, and ambiguity or unmet safety preconditions block the action.
- Validation MUST distinguish missing note targets from missing fragments and MUST preserve deterministic candidate ordering and evidence across CLI, agent, MCP, and web result rendering.
- Link repair MUST be limited to static `<a href>` values and schema-declared root-metadata link fields whose spans the provider can prove; it MUST NOT scan and replace arbitrary matching text.
- HTML link and move planning MUST use the shared resolved target identity after the HTML provider's `<base>`, vault-root, relative-path, percent-encoding, query, and fragment semantics are applied; it MUST rewrite only an eligible authored target and MUST NOT rewrite `<base>` itself.
- Rename and move planning MUST enumerate all recognized inbound note references and, for cross-directory moves, all supported outbound relative note references whose authored value must change to preserve identity.
- Rewrites MUST NOT modify element IDs, legacy anchor names, JavaScript, CSS, inline event handlers, template or data expressions outside the canonical metadata block, resource URLs, external URLs, or runtime-created navigation.
- Every operation MUST use the preview and transaction invariants in [SPEC-0018](validation-fix-plan-and-apply-session.md), including source hashes, expected spans or text, atomic connected transactions, recoverable apply, and post-apply targeted checks.
- CLI, agent, MCP, and validation adapters MUST use one shared operation model and MUST NOT implement format-specific confirmation or file-write paths.
- Web adapters MAY originate root-metadata Save after edit-session preview and conflict replay. Apply MUST use the shared journaled repair transaction engine as the sole disk writer, preserve modes, recover interrupted transactions, and refresh exact paths. Other HTML maintenance operations remain read only until their selected delivery slice is complete. No web adapter may expose a general HTML source or structural editor.

### Should

- Provider diagnostics SHOULD explain which capability or ownership proof failed and identify the exact manual edit needed when a mutation is blocked.
- Preview output SHOULD distinguish semantic values from their encoded source representation so reviewers can verify both intent and byte-level scope.
- Regression fixtures SHOULD cover HTML with and without `<head>`, JSON strings containing script terminators, single- and double-quoted attributes, percent-encoded targets, query and fragment combinations, metadata link arrays, malformed and duplicate metadata, cross-directory moves, and stale multi-file plans.

### May

- Additional statically registered note formats MAY implement the same granular capabilities without changing the shared operation or transaction model.
- Later delivery slices MAY add governed web apply controls for link retargeting or file moves over the same plan payload.

## Related Contracts

- [SPEC-0018 - Validation fix plan and apply session](validation-fix-plan-and-apply-session.md) owns repair safety, transaction, replay, and revalidation semantics.
- [SPEC-0076 - Note/node indexing architecture](note-node-indexing-architecture.md) owns canonical note source facts, node identity, source locators, and source-preserving edit ownership.
- [SPEC-0083 - Multi-format HTML notes](../product/multi-format-html-notes.md) defines the user-visible capability boundary; this spec supplies only its narrow maintenance mutation mechanics.
- [SPEC-0084 - Note-format provider registry](note-format-provider-registry.md) owns capability discovery and the shared provider interface.
- [SPEC-0085 - HTML note-format provider](html-note-format-provider.md) owns canonical HTML metadata, local-link extraction, and source-span encoding.
- [SPEC-0086 - Format-aware root ontology projection](format-aware-root-ontology-projection.md) owns root metadata interpretation and schema-declared link-field semantics.
- [SPEC-0088 - Trusted HTML note viewer](../experience/trusted-html-note-viewer.md) owns rendering and pane navigation and does not broaden this mutation surface.

## Documentation Plan

- Update the validation subsystem reference with format-neutral repair operations, safety classes, provider boundaries, and HTML blocked cases.
- Update note mutation, rename, and move guidance to describe provider capabilities and cross-format reference planning while keeping Markdown and HTML confirmation behavior aligned.
- Document canonical HTML root-metadata edit behavior, stable JSON serialization, script-terminator encoding, and source-preservation guarantees.
- Document CLI, agent, and MCP payload parity plus the bounded web root-metadata Save boundary and the read-only state of undelivered maintenance operations.
- Add code-adjacent subsystem invariants where provider mutation spans, shared candidate matching, and repair transactions are implemented.

## Open Questions

None for this increment.
