---
name: requirements-ingest
description: Use when turning source documents, transcripts, workshops, spreadsheets, recordings, or stakeholder notes into reviewable complex-domain requirement candidates with provenance.
---

# Requirements Ingest

Create atomic candidate requirements from source material. Extraction does not accept a requirement or add it to delivery scope.

Load the shared retrieval and evidence guidance through the `complex-domain` router. Use the installed Rhizome guidance for the session, exact source reads, live authoring guides for `RequirementSource` and `Requirement`, safe edits, and focused validation.

## Retrieve

For a known source note, run `domain-context-pack`. For a new or uncertain source, inventory `RequirementSource` and `Requirement` with `domain-inventory-pack`, using stable identity such as title, locator, date, or version when available. Only then use `domain-topic-survey` for unresolved semantic duplicate candidates. Inspect likely matches with the anchored packs.

## Produce

- Reuse one `RequirementSource` for the same artifact. Preserve source identity, locator, provenance, version/date, review state, extraction date, and exact source locations.
- Preserve prior cited evidence and version history when refreshing a source. Add current evidence without overwriting what justified an earlier accepted requirement. If prior content is unavailable, record that gap.
- Create one `candidate` Requirement per distinct obligation. Keep provisional confidence/review state explicit.
- Reuse a matching candidate when an unchanged extraction is rerun. Do not duplicate the source or requirement.
- Link candidates only to sources and domain/spec context supported by the evidence.
- Record duplicates, conflicts, unclear wording, missing definitions, or decisions as relationships or `ActionItem` entries for curation. Do not merge conflicting candidates silently or invent an assignee.

Report the source identity and version, cited locations, created or reused candidates, conflicts, affected existing notes, retrieval qualifications, and decisions still needed. Apply validation proportionate to the notes changed.
