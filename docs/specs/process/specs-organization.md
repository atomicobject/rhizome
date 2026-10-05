---
aliases:
    - SPEC-0005
id: SPEC-0005
last-updated: 2026-07-16T00:00:00Z
spec-status: archived
summary: Explains how docs/specs/ is organized, including nested spec-family directories and ontology classification rules.
type: ProcessSpec
---
<!-- RZM AGENTIC-ENGINEERING RETIREMENT NOTICE -->
This is a historical record from the retired spec-driven starter, not current guidance. Current team policy lives in `docs/engineering/`. Reconcile any local policy still unique to this document there, then delete it.


# Specs organization

## Summary

This repo's `docs/specs/` tree is an enforced taxonomy, not a suggestion. Each spec-family root maps to an ontology type declared under `.rhizome/ontology/*.graphql`, and the schema's `@node(paths: [...])` directives make the mapping load-bearing. Subdirectories are allowed under each family root for organization, but classification is not based on filename precision alone: explicit `type:` frontmatter or required identifier presence determines whether a note is actually a spec.

## Goals

- give authors a single rule for where a new spec goes
- keep the ontology and the filesystem in lock-step without forcing every sibling note under a spec directory to become a spec
- make `alignment-audit` findings actionable by folder

## Non-Goals

- documenting the full schema; see `.rhizome/ontology/*.graphql`
- prescribing folder structure for `docs/reference/` or `docs/efforts/` (those have their own hubs)

## Folder → type mapping

| Folder | Ontology type | What belongs there |
|---|---|---|
| `docs/specs/product/**` | `ProductSpec` | User-visible behavior, scope, product intent |
| `docs/specs/technical/**` | `TechnicalSpec` | Engineering contracts, architecture boundaries |
| `docs/specs/process/**` | `ProcessSpec` | How work flows: development loop, audits, skills |
| `docs/specs/experience/**` | `ExperienceSpec` | Interaction design, flows, content behavior |
| `docs/specs/operations/**` | `OperationsSpec` | Deployment, support, migration, operational readiness |

Each type uses a nested `@node(paths: ["docs/specs/<folder>/**/*.md"])` directive, so specs can be organized by area, for example `docs/specs/technical/search/SPEC-0042-foo.md`. For important top-level families with a required `@identifier` field, path is only the discovery default: an undeclared note under the path must also present the required identifier field before Rhizome classifies it as that type. A `ProductSpec` placed under `docs/specs/technical/` still fails validation rather than silently mistyping, because explicit `type:` must agree with selectors.

Freeform sibling notes are allowed under spec directories when they are not intended to be typed specs. A `plan.md` or `notes.md` file with no `type:` and no required `id:` stays untyped instead of inheriting the parent directory's spec type. Use the first-class `Plan` type only when the planning artifact should be queryable and durable.

## Requirements

### Must

- New specs pick the folder whose type matches the intent. If no type fits, propose a schema change before filing the note.
- Spec frontmatter includes `type`, `summary`, `id`, `spec-status`, and `last-updated`. `last-updated` is the sole versioning marker; bump it on substantive edits and treat it as the read-time freshness signal that downstream skills (alignment-audit, frozen-scope-drift) consume.
- Specs may live in subdirectories under their family root. Do not rely on filenames such as `SPEC-*.md` for classification; required identifier presence is the gate for undeclared notes in identifier-backed families.
- Top-level spec ids use the `SPEC-XXXX` format defined in `id-allocation.md`.
- Specs that participate in delivery work usually include a `User Stories` section with descriptive `USn - Outcome` story titles and required authored, block-safe story id metadata bullets. Write `- id::` with a leading caret before the story id, such as `- id:: ^SPEC-XXXX-US1`.
- `TechnicalSpec` may omit `User Stories` when requirements-only structure is a better fit for the subsystem contract.
- `alignment-audit` with `audit_type: documentation` catches folder/type mismatches via `rzm agent validate ontology`.

### Should

- Use folder `README.md` as a `DocumentationHub` explaining reading order for that folder.
- Keep scratch planning and working notes visibly freeform unless they should become typed `Plan` notes with `PLAN-XXXX` identifiers.

## Open Questions

- none currently; use `docs/specs/technical/` for normative technical targets and `docs/reference/` for descriptive supporting architecture material
