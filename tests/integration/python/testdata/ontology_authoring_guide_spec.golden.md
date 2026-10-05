# Ontology Authoring Guide

Write note markdown to satisfy the ontology selectors and field contracts below. Use `type: <TypeName>` as author intent and disambiguation, but selectors (`paths` / `matches`) still control whether a note belongs to a type.

## Spec

### Resolution

- paths: notes/specs/*.md
- declared type: set `type: Spec` in frontmatter when authoring or disambiguating this note type
- default property case: `kebab`

### Authored Fields

- `name`: required; frontmatter key `name`; single String value
- `requirements`: required; markdown heading; single section body value; heading `## Requirements`; tooling: @contains {"heading":"Requirements","level":"H2","required":true}
- `stories`: optional; markdown heading; single section body value; heading `## Stories`; tooling: @contains {"heading":"Stories","level":"H2","required":false}
- `summary`: optional; frontmatter key `summary`; single String value

### Tooling Semantics

- @node {"locator":"FILE","paths":["notes/specs/*.md"],"propertyCase":"KEBAB"}

### Validation Traps

- the note must still match this type's selectors; `type:` alone is not enough
- missing `name` raises `missing_required_field`
- multiple `name` values raise `field_shape_mismatch`
- missing `requirements` raises `missing_required_section`
- empty `requirements` raises `empty_required_section`
- wrong `requirements` heading level raises `wrong_section_level`
- duplicate `requirements` headings raise `duplicate_section`
- missing `requirements.details` raises `missing_required_section`
- empty `requirements.details` raises `empty_required_section`
- wrong `requirements.details` heading level raises `wrong_section_level`
- duplicate `requirements.details` headings raise `duplicate_section`
- do not author `requirements.decisions`; neighbor fields are derived from existing typed links/backlinks
- duplicate `stories` headings raise `duplicate_section`
- multiple `stories.stories.storyId` values raise `field_shape_mismatch`
- multiple `stories.stories.status` values raise `field_shape_mismatch`
- multiple `stories.stories.spec` values raise `field_shape_mismatch`
- `stories.stories.spec` targets must resolve to notes; unresolved links raise `link_target_missing`
- `stories.stories.spec` targets must resolve to `Spec`; mismatches raise `wrong_target_type`
- multiple `summary` values raise `field_shape_mismatch`

### Related Types

- `RequirementsSection` appears below in the supporting appendix: referenced by `requirements`. Read this type's authoring guide before authoring that embedded structure directly.
- `StoriesSection` appears below in the supporting appendix: referenced by `stories`. Read this type's authoring guide before authoring that embedded structure directly.

### Skeleton

```md
---
type: Spec
name:
summary:
---

## Requirements

### Details

## Stories

###
```

## Supporting Types

These related types are included because they are authored links, contained sections, or shared interfaces needed while drafting this type.

### RequirementsSection

- section type: heading-derived structure used inside parent note bodies
- `details`: required; markdown heading; single section body value; heading `### Details`; tooling: @contains {"heading":"Details","level":"H3","required":true}
### StoriesSection

- section type: heading-derived structure used inside parent note bodies
- `stories`: optional; markdown heading; list of section body values; heading `###`; tooling: @contains {"level":"H3","required":false}
