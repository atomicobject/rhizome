---
type: ProductSpec
id: SPEC-0065
summary: "Defines the user-facing ontology-aware search experience: CLI results grouped by note type with frontmatter fields surfaced, type and frontmatter filtering, clear command routing so developers know which search command to use, and remediation of the confusing rzm agent semantic-query surface."
spec-status: proposed
last-updated: 2026-07-11
aliases:
  - SPEC-0065
  - ontology-aware-search
---

# Ontology-Aware Search

## Summary

Rhizome's search commands return file-list-and-snippet output that does not reflect the typed, ontology-backed knowledge the tool manages. Demo feedback from the JIS engagement surfaced two concrete pain points: search results are too sparse to be useful for domain-specific skills (filtering by `system`, `court-type`, or `spec-status` is not possible), and developers cannot determine which search command to use for their goal — `rzm search`, `rzm agent semantic-query`, and other retrieval commands expose overlapping, inconsistently documented surfaces.

This spec defines the user-facing search experience on top of the underlying SPEC-0032 (primary semantic chunks and NodeRef search) engine: results grouped by note type, frontmatter fields visible in output, type and field filtering at the CLI, a consistent web UI search experience, and clear command-routing guidance so developers find the right search command without trial and error.

This spec governs the user-facing contract. Implementation rests on SPEC-0032 (primary chunks, NodeRef), SPEC-0035 (unified search answer architecture), and SPEC-0042 (GraphQL query contract).

## Goals

- Search results group by note type so a developer searching for "court processing" sees Requirement, DomainProcess, and Spec results in distinct buckets
- Relevant frontmatter fields (id, type, summary, spec-status, and any domain-specific field) are visible in CLI and web search output without requiring a follow-up read
- A developer can filter search results by note type and frontmatter values from the CLI
- A developer can determine which search command to use for their goal without consulting the docs
- `rzm agent semantic-query` either provides a clear, accurate experience or is replaced by a routed alternative with explicit deprecation guidance

## Non-Goals

- Changing the underlying retrieval engine or ranking (SPEC-0032 and SPEC-0035 own that)
- Building a full Docusaurus documentation site (separate initiative)
- Providing full-text filtering across all possible frontmatter keys on arbitrary note families
- Real-time search-as-you-type in the web UI (out of scope for v1)
- Changing the `rzm agent start` / `rzm agent file-context` / `rzm agent files` surfaces (those are agent retrieval, not developer search)

## User Stories

### US1 - Search and receive results organized by note type

- id:: ^SPEC-0065-US1
- summary:: A developer runs a search and receives results grouped by note type with key frontmatter fields visible, not a flat file list with snippets.
- status:: ready

The current output is a flat ranked list. Typed grouping makes the result set navigable and reflects the ontology's structure: a developer can immediately see that their query matched 3 Requirements, 1 TechnicalSpec, and 2 ReferenceDoc notes, and read the relevant fields without opening each file.

#### Acceptance Criteria

- CLI search output groups results by resolved note type.
  - `rzm search <query>` output groups matched notes under their resolved ontology type heading (e.g., `## Requirement (3)`, `## TechnicalSpec (1)`). Notes whose type cannot be resolved appear under `## Untyped`.
- Each result displays id, summary, and spec-status (when present) alongside path and snippet.
  - For typed notes, the CLI output shows the note's `id` field value, `summary` frontmatter, and `spec-status` (or equivalent status field) when present. Snippet text remains available but is secondary to the structured fields.
- Ungrouped mode remains available via a flag for backwards-compatible scripting.
  - A `--flat` flag (or equivalent) produces the original flat-list output. Default switches to grouped.

---

### US2 - Filter search results by note type and frontmatter values

- id:: ^SPEC-0065-US2
- summary:: A developer can narrow search results to a specific note type or frontmatter value, such as only active Requirements or only Specs with a specific domain tag.
- status:: ready

The JIS engagement needed to filter search by `system` and `court-type` frontmatter fields — domain-specific values that differ per project. The filtering mechanism should be generic enough to handle any frontmatter key present in the vault.

#### Acceptance Criteria

- CLI supports --type flag to restrict results to a single ontology type.
  - `rzm search <query> --type Requirement` returns only notes resolved as the named type. The flag accepts any ontology type name present in the vault's schema.
- CLI supports --filter flag for frontmatter key=value filtering.
  - `rzm search <query> --filter spec-status=active` restricts results to notes where the named frontmatter key matches the given value. Multiple `--filter` flags AND together. Unknown keys produce a warning but do not abort the search.
- Web UI search exposes type-filter controls alongside results.
  - The web UI search surface (Explorer or search panel) shows a facet sidebar by resolved note type with counts. Selecting a type filters the displayed results without re-querying. Frontmatter field filters follow in a subsequent iteration.

---

### US3 - Know which search command to use for your goal

- id:: ^SPEC-0065-US3
- summary:: A developer can determine which Rhizome search command fits their goal without reading multiple man pages or asking for help.
- status:: ready

The JIS demo revealed that developers could not distinguish `rzm search`, `rzm agent semantic-query`, `rzm agent files`, and related retrieval commands. This confusion is a discoverability failure: the commands have different strengths but no clear user-facing differentiation at the point of invocation.

#### Acceptance Criteria

- `rzm search --help` includes a one-line description distinguishing it from `rzm agent semantic-query` and `rzm agent files`.
  - The help text states clearly when to use each command. Example: "`rzm search` is for keyword and semantic search across vault notes. Use `rzm agent semantic-query` for AI-shaped answer packets. Use `rzm agent files` for exact note lookup by path, tag, or frontmatter."
- A command-routing reference exists in docs/reference/guides/search-command-guide.md.
  - A short ReferenceDoc maps developer goals to the right command: "I want to find all Requirements about X" → `rzm search --type Requirement X`. "I want context for an agent working on a feature" → `rzm agent semantic-query`. "I want to find notes with a specific tag" → `rzm agent files --tag X`.
- The command-routing reference is linked from the starter decision guide and relevant playbooks.
  - Developers arriving via the onboarding path encounter the routing guide at the point where they need to choose a search command.

---

### US4 - Use rzm agent semantic-query with accurate expectations

- id:: ^SPEC-0065-US4
- summary:: A developer using rzm agent semantic-query receives a clear, predictable experience — or is routed to a better alternative with an actionable message.
- status:: ready

Demo feedback indicates `rzm agent semantic-query` output is confusing enough that demonstrators tell users to avoid it. Either the command's output and documentation must be brought to a state where it can be recommended without qualification, or it should produce a clear routing message pointing to the right alternative.

#### Acceptance Criteria

- An audit of rzm agent semantic-query documents its current behavior, known gaps, and the delta from its documented contract.
  - A tech note or section in the search command guide records: what the command currently returns, which parts of the documented contract are met vs. degraded, and why users find it confusing.
- Outcome is one of: (a) output improved to match documented contract, (b) routing message added pointing to better alternatives, or (c) command deprecated with a migration path.
  - The spec does not prescribe the outcome — that depends on the audit. Any of the three outcomes is acceptable if it eliminates the "tell users not to use it" situation.
- Updated guidance for rzm agent semantic-query is reflected in all playbooks and the command routing guide.
  - Whichever outcome is chosen, the playbooks (SPEC-0064) and the command routing guide reference current, accurate information about when and how to use the command.

## Requirements

- `rzm search` MUST group CLI output by resolved ontology type by default
- `rzm search` MUST surface `id`, `summary`, and `spec-status` (when present) per result without requiring a follow-up file read
- `rzm search --flat` MUST produce backwards-compatible flat output
- `rzm search --type <TypeName>` MUST filter results to the named ontology type
- `rzm search --filter <key>=<value>` MUST filter results by frontmatter key/value; unknown keys MUST warn but MUST NOT abort
- Web UI search MUST expose type-based filtering via a facet sidebar with counts
- `rzm search --help` MUST distinguish `rzm search` from `rzm agent semantic-query` and `rzm agent files`
- A `docs/reference/guides/search-command-guide.md` ReferenceDoc MUST exist mapping goals to commands
- The `rzm agent semantic-query` audit outcome MUST be one of: improved output, routing message, or explicit deprecation with migration path
- All playbooks (SPEC-0064) MUST reference current, accurate search command guidance after the audit completes

## Open Questions

`--filter` flag: exact values only for v1 (proposal accepted). Web UI type filter: facet sidebar confirmed.

## Documentation Plan

- `docs/reference/guides/search-command-guide.md` — new command routing guide (ReferenceDoc)
- `rzm search --help` text — updated to distinguish command purposes
- Playbooks from SPEC-0064 — updated after audit to reference accurate search command guidance
- `docs/specs/technical/` — audit note or addendum documenting semantic-query current state and chosen remediation path
