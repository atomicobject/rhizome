---
type: TechnicalSpec
summary: "Defines how Rhizome classifies nested typed notes without treating freeform sibling notes as specs, efforts, or plans when the schema requires an identifier."
id: SPEC-0055
spec-status: active
last-updated: 2026-07-11
aliases:
  - SPEC-0055
  - Identifier-gated typed note classification
---

# Identifier-gated typed note classification

## Summary

Rhizome uses ontology `@node(paths: [...])` declarations to discover candidate note types. The spec-driven starter now allows nested spec and effort directories, for example `docs/specs/technical/search/SPEC-0042-foo.md`. That wider layout is useful, but path-only classification is too confident for important note families: freeform sibling notes such as `plan.md` or `notes.md` can sit under a typed directory without being specs or efforts.

Classification therefore has two gates. Explicit `type:` frontmatter remains the strongest author signal when it matches the schema selector. For undeclared notes, a path glob may classify a note by itself only when the candidate type does not declare a required `@identifier` field. If the candidate type has a required identifier field, the note must actually present that field before the path match becomes a classification.

## Goals

- allow nested spec, effort, reference, and plan directories without false hard validation against freeform sibling notes
- keep `type:` frontmatter as explicit author intent while preserving existing `declared_type_mismatch` behavior when selectors do not match
- make the schema's required `@identifier` field the contract for whether path alone is enough to classify a note
- preserve path-only classification for identifier-less document families such as `DocumentationHub`
- add a first-class `Plan` note family for intentionally typed nested plan artifacts

## Non-Goals

- adding filename-pattern selectors such as `**/SPEC-*.md`
- adding path-negation glob support
- migrating existing freeform `plan.md` / `plan-v2.md` files to typed `Plan` notes
- changing how declared type mismatch validation works
- adding a new warning surface for every untyped note in a typed directory in this slice

## Requirements

### Candidate classification

#### Must

- `matchCandidateTypes` MUST continue to ask the schema whether a note matches each candidate type's selectors.
- When a note has no explicit `type:` frontmatter and a candidate type declares one or more required fields marked `@identifier`, Rhizome MUST keep that candidate only when at least one required identifier field is present on the note.
- The identifier gate MUST apply before ambiguity resolution so `mostSpecificCandidate` sees only candidates that are allowed to classify the note.
- When a note has explicit `type:` frontmatter and that declared type matches the candidate selectors, Rhizome MUST keep the resolved type even if required fields are missing. Missing required fields are then surfaced as normal validation issues.
- When a note has explicit `type:` frontmatter but the declared type does not match candidate selectors, Rhizome MUST keep the existing `declared_type_mismatch` behavior.
- Candidate types without required `@identifier` fields MUST continue to classify by path or selector alone.
- A gated-out candidate MUST NOT emit that type's `missing_required_field` or `missing_required_section` issues, because the note was not classified as that type.

#### Should

- The implementation should use the existing field-presence logic rather than duplicating frontmatter or inline parsing rules.
- The implementation should keep the gate local to assessment/candidate resolution so projection, indexing, validation, and GraphQL read paths share the same resolved-type truth.
- The implementation should leave diagnostic warning design to a follow-on unless validation shows authors have lost necessary feedback.

### Nested spec directories

#### Must

- Spec-driven spec families MUST allow nested notes under their family root:
  - `docs/specs/process/**/*.md`
  - `docs/specs/product/**/*.md`
  - `docs/specs/technical/**/*.md`
  - `docs/specs/experience/**/*.md`
  - `docs/specs/operations/**/*.md`
- A nested note such as `docs/specs/technical/search/SPEC-0042-foo.md` with a valid required `id:` field MUST classify as its spec family when its path matches that family.
- A nested freeform note such as `docs/specs/technical/search/plan.md` without `id:` and without `type:` MUST remain untyped instead of being validated as `TechnicalSpec`.
- Specs organization guidance MUST say subdirectories are allowed and that classification is by explicit `type:` or required identifier presence, not filename precision alone.

### Plan note family

#### Must

- The spec-driven schema MUST define a top-level `Plan` note type for intentional plan artifacts under spec and effort directories.
- `Plan` MUST match `docs/efforts/**/plan*.md` and `docs/specs/**/plan*.md`.
- `Plan` MUST require an `id` field marked `@identifier(preferred: true, prefix: "PLAN")`.
- `Plan` MUST have minimal required prose sections for `Goal` and `Approach`.
- A plan file with `id: PLAN-0001` under a matching path MUST classify as `Plan`.
- A plan file under the same path without `id:` and without `type:` MUST stay untyped and MUST NOT receive `Plan` missing-field errors.

#### Should

- `Plan` should have a short `summary` field so primary semantic chunks and query recipes can distinguish plan notes from their parent effort or spec.
- Existing freeform plans should remain valid as freeform notes until a separate migration deliberately promotes them to `Plan`.

### Schema and docs mirrors

#### Must

- The live schema and the spec-driven starter schema MUST stay aligned.
- Rhizome ontology example schemas bundled for agent skills MUST stay aligned with the starter schema for the fields and path globs this spec changes.
- SPEC-0005 and SPEC-0025 MUST be updated so future agents learn the classification rule before editing schemas or spec directories.

## User Stories

### US1 - Identifier gate protects nested freeform notes

- id:: ^SPEC-0055-US1
- summary:: Nested freeform notes under spec and effort directories stay untyped unless they declare the required identifier, so organizing work by subdirectory does not create false validation failures.
- status:: ready

#### Acceptance Criteria

- A nested undeclared note under a required-identifier path glob without `id:` remains untyped and does not emit missing-field issues for that candidate type. ^SPEC-0055-US1-AC1
- A nested undeclared note under the same path with the required `id:` field classifies as the matched type. ^SPEC-0055-US1-AC2
- A candidate type without a required `@identifier` field, such as `DocumentationHub`, still classifies by path alone. ^SPEC-0055-US1-AC3
- Explicit `type:` frontmatter continues to resolve when selectors match, and missing required fields surface as normal validation issues. ^SPEC-0055-US1-AC4
- Explicit `type:` frontmatter continues to emit `declared_type_mismatch` when selectors do not match; this spec does not loosen path/type mismatch validation. ^SPEC-0055-US1-AC5

### US2 - Plan notes have a first-class typed home

- id:: ^SPEC-0055-US2
- summary:: Intentionally typed nested plan artifacts use a `Plan` note family, so real plans can be queried without forcing every `plan.md` scratch file to become an effort or spec.
- status:: ready

#### Acceptance Criteria

- The spec-driven schema defines `Plan` with matching paths for `docs/efforts/**/plan*.md` and `docs/specs/**/plan*.md`. ^SPEC-0055-US2-AC1
- `Plan` requires `id: PLAN-...` through a preferred identifier field and requires `Goal` and `Approach` sections. ^SPEC-0055-US2-AC2
- A matching plan file with `id: PLAN-0001` classifies as `Plan`. ^SPEC-0055-US2-AC3
- A matching plan file without `id:` and without `type:` remains untyped and does not emit missing `Plan` fields or sections. ^SPEC-0055-US2-AC4

### US3 - Starter docs explain nested classification

- id:: ^SPEC-0055-US3
- summary:: The schema, process docs, and examples describe the same nested classification rule, so generated repos inherit the behavior without rediscovering it.
- status:: ready

#### Acceptance Criteria

- SPEC-0005 says subdirectories under each spec family are allowed and that `type:` or required identifier presence, not filename precision, determines classification for important typed families. ^SPEC-0055-US3-AC1
- SPEC-0025 documents required `@identifier` fields as the schema-level switch between path-only classification and identifier-gated classification. ^SPEC-0055-US3-AC2
- Live schema, starter schema, and bundled ontology example schemas agree on nested globs and the `Plan` type. ^SPEC-0055-US3-AC3

## Open Questions

- Should a future `unclassified_in_typed_dir` warning distinguish likely-scratch files (`plan.md`, `notes.md`) from likely-forgotten typed notes (`SPEC-0042-draft.md`)?
- Should `Plan` eventually gain lifecycle fields, or should it remain a deliberately lightweight typed note for implementation planning artifacts?
