---
aliases:
    - SPEC-0004
id: SPEC-0004
last-updated: 2026-07-16T00:00:00Z
spec-status: archived
summary: Defines the audit types this repo supports and what each one must cover for alignment auditing.
type: ProcessSpec
---
<!-- RZM AGENTIC-ENGINEERING RETIREMENT NOTICE -->
This is a historical record from the retired spec-driven starter, not current guidance. Current team policy lives in `docs/engineering/`. Reconcile any local policy still unique to this document there, then delete it.


# Audits

## Summary

Audits are deterministic reality checks run by `alignment-audit`. Each audit type is small, scoped, and produces the same report shape so follow-up skills can act on the findings mechanically.

## Goals

- keep audits scoped and repeatable
- share one report shape across audit types
- make `backport` trivial to chain after an audit

## Non-Goals

- replacing `rzm agent validate`; audits are a workflow layer on top of it
- running broad `rzm index --rebuild` passes; audits use the live surface

## Requirements

### Must

- `alignment-audit` supports the types listed below.
- Every audit produces the report shape defined in `alignment-audit`'s SKILL.md.
- `spec-alignment` runs against behavior-changing structural refactors before effort closure.

### Should

- At least one domain-specific audit runs alongside `spec-alignment` for efforts that touch multiple subsystems.

## Audit types

### `spec-alignment`

- Verify selected user stories, acceptance criteria, and MUST/SHOULD requirements in the frozen spec set are implemented or explicitly deferred in the effort's deviations.
- Report mismatches, partial matches, and required spec updates.
- Sources: frozen spec set from the effort, `rzm agent validate ontology`, targeted `rzm agent semantic-query --mode docs_for_code`.

### `link-hygiene`

- Scan markdown files in scope for broken or malformed links.
- Sources: `rzm agent validate broken-links` and `rzm agent validate link-hygiene`.
- `broken-links` reports each unresolved wikilink or Markdown note link once; `ontology` does not repeat them. Resolution follows Obsidian: case-insensitive names, attachments and ignored paths resolve, and links in code are skipped. With git history, only links whose target was deleted or renamed away count; `rzm agent validate placeholder-links` (audit only) lists placeholders that never had a target. `validation.brokenLinks.placeholders: strict` counts every unresolved link.

### `documentation`

- Check that code anchors resolve, frontmatter is valid, and coderefs point at real notes.
- Sources: `rzm agent validate code-frontmatter` and `rzm agent validate code-anchors`.
- `code-frontmatter` checks that every note's frontmatter parses, because code-anchor definitions live there, and that code-anchor definitions are valid. It skips an Obsidian template file only when its Templater `<% ... %>` or core `{{...}}` placeholders are what keep the frontmatter from parsing, and lists it in the check's notes; a template that is also malformed otherwise is still reported.
- `code-anchors` is `not_applicable` until at least one code-anchor language that is not in `code.disabledLanguages` has `code.<language>.roots` in `.rhizome/config.yml`. `code.enabled` or top-level `code.scan` alone enables coderefs only, and `rzm index` cannot produce anchors without roots.

### `drift`

- Find code changes in the effort whose anchored notes are now stale.
- Sources: `rzm agent files --include-backlinks` on touched paths, plus anchor match checks.

### `frozen-scope-drift`

- Flag any `planned`/`active` effort whose `Spec Set (Frozen)` references a spec whose `last-updated` is after the effort's `created-at`.
- Run before closing an effort, and any time `specify` lands an edit on a spec that may be referenced by a live effort.
- Each issue carries `{ effortPath, effortId, effortStatus, effortCreatedAt, specPath, specId, specLastUpdated }`. Resolution is either: land a Deviation entry on the affected effort that names what changed, refreeze the scope explicitly, or revert the spec edit.
- Acknowledgement marker: a (effort, spec) pair is treated as resolved when the effort's `## Deviations` section contains BOTH the case-insensitive marker `frozen-scope-drift acknowledged` AND the spec's identifier (e.g. `SPEC-0022`). Suggested entry shape: `(frozen-scope-drift acknowledged via EFF-XXXX) SPEC-YYYY was refreshed; <impact on delivery contract>.` This lets agents close the loop honestly without rewriting timestamps.
- Sources: `rzm agent validate frozen-scope-drift`. Pre-existing drift findings are best handled in a small dedicated backport effort rather than silently fixing.

## Open Questions

- whether this starter should ship quality-gates and transcript audits once the core four stabilize
