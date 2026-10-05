---
type: ExperienceSpec
summary: "Hovering a link to a note in the web workspace opens an index-backed preview card showing the target's type, identifier, summary, and interesting properties, shaped by a field-level @display annotation that schema authors use to mark properties as key, detail, or hidden from previews. The same importance orders the properties panel and picks configured-view default columns."
id: SPEC-0106
spec-status: proposed
last-updated: 2026-09-19
aliases:
  - SPEC-0106
---

# Note link hover preview

## Summary

A link to a note tells the reader nothing about its target until they open it. Obsidian answers this with a page preview of the target's raw content. Rhizome knows more than the raw content: the target's type, its identifier, its summary, and its typed properties are already in the index. This spec defines a hover preview card for note links in the web workspace that presents that structure, and the schema annotation that decides which properties are worth presenting.

The card is served from indexed metadata only, so it returns in a few milliseconds and never parses a note. Schema authors shape it with a field-level `@display` directive. The same annotation is the source of property prominence elsewhere: the note properties panel leads with key properties and folds detail properties away, and configured views without an authored field list choose their default columns by importance.

## Goals

- let a reader learn what a linked note is, and whether it is worth opening, without leaving the note they are reading
- present structured notes by their structure: type, identifier, summary, and interesting properties, rather than a slice of raw text
- keep the preview fast enough to feel instant, by reading only indexed metadata
- stay out of the reader's way: no request for a pointer that merely passes over a link, a card placed clear of the pointer, and a card the pointer can enter and scroll
- give schema authors one annotation that says which properties are key, which are detail, and which should stay out of previews, with sensible behavior when nothing is annotated
- let a reader follow link-valued properties from inside a card, including previewing them

## Non-Goals

- rendering arbitrary body excerpts or linked section contents; a declared summary section is projected into indexed metadata for the card, and a plain note previews as its title, path, and date
- hover previews inside the Markdown source editor, the graph views, or HTML note iframes
- reflecting unsaved edit-session changes in the card
- pinned, draggable, or resizable cards
- applying `@display` importance to agent context packs or search results
- a friendly-label argument on `@display`; labels derive from field names through the shared humanizer
- touch and coarse-pointer devices

## User Stories

### US1 - Preview a linked note by hovering its link

- id:: ^SPEC-0106-US1
- summary:: As a reader, I hover a link to a note and see a card describing the target, so I can decide whether to open it.
- status:: ready

#### Acceptance Criteria

- **Typed target**: The card for a typed note shows the type label, the preferred identifier when the type has one, key properties as chips, the title, the summary as lead text, the remaining previewable properties with humanized labels. Typed previews omit duplicate tag chips and the file path/date footer; the title carries the path as secondary hover text.
  verification:: Hover a link to a product spec; the card shows `Product spec`, its `SPEC-` identifier, a status chip, the title, and the summary.
- **Untyped target**: The card for an untyped note shows the title, every frontmatter property with a humanized label, tags, path, and date. A `summary` frontmatter property renders as the lead text.
- **HTML target**: The card for an HTML note shows the same metadata as any other note of its type and never renders the HTML.
- **Issues**: A target with validation issues shows an issues indicator.
- **Heading and block links**: A link to `note#Heading` or `note#^block` previews the note and shows the linked heading text, or block id, under the title when the index knows that fragment. The card shows no section content.
- **Unresolved fragment**: A link whose fragment matches neither an indexed node nor an indexed fragment previews the note and states that the heading was not found.
- **Unresolvable target**: A link whose target cannot be resolved shows a quiet unavailable state rather than an empty card.
- **Coverage**: The card opens from internal note links rendered from Markdown in narrative body blocks and the ontology note pane, from entries in the related-notes rail, and from relation values in configured-view tables, cards, and boards.
- **Rail and view triggers keep their behavior**: Previewing a rail entry or a relation value changes nothing about how clicking, dragging, or keyboard-activating it works. Rail cards open to the left of the rail.

### US2 - A preview that stays out of the way

- id:: ^SPEC-0106-US2
- summary:: As a reader, I get previews only when I pause on a link, positioned clear of my pointer, and I can move into the card to scroll it or follow its links.
- status:: ready

#### Acceptance Criteria

- **No request on pass-over**: A pointer that leaves a link within 100ms causes no network request.
- **Open timing**: The request starts after a 100ms dwell. The card opens 300ms after the pointer entered when the data has arrived, and otherwise when the data arrives. After 700ms without data the card opens in a loading state.
- **Placement**: The card anchors to the line of the link under the pointer, including for a link wrapped across lines, opens below it, flips above when there is no room, and stays inside the viewport.
- **Pointer travel**: Moving the pointer from the link into the card keeps it open. The card scrolls when its content is taller than its maximum height. Leaving both closes it after a 150ms grace period.
- **Dismissal**: Escape closes the topmost card. Clicking the link closes the card and opens the note with the existing click semantics (plain click stacks, meta or ctrl click opens beside, other modified clicks keep browser behavior).
- **Keyboard**: Focusing a link with the keyboard opens the card on the same timing, and blur closes it unless focus moved into the card.
- **Coarse pointers**: Devices without hover show no card.

### US3 - Follow and preview links inside a card

- id:: ^SPEC-0106-US3
- summary:: As a reader, I see link-valued properties in a card as links by target title, and I can hover them to preview their targets.
- status:: ready

#### Acceptance Criteria

- **Link values are links**: A link-valued property renders each resolved value as a link labelled with the target's title. An unresolved value renders as plain text.
- **Nested preview**: Hovering a link inside a card opens a nested card while the parent stays open. Leaving the nested card for the parent closes only the nested card.
- **Nested placement**: A nested card opens beside its parent rather than over the parent's other links, falling back to another side when the viewport requires it.

### US4 - Shape previews from the schema

- id:: ^SPEC-0106-US4
- summary:: As a schema author, I annotate fields with `@display` to mark which properties are key, which are detail, and which stay out of previews.
- status:: ready

#### Acceptance Criteria

- **Default**: A field with no annotation that has a value appears in the card as a normal property.
- **Key**: `@display(importance: KEY)` renders the property as a chip ahead of the other properties.
- **Detail**: `@display(importance: DETAIL)` keeps the property out of the card.
- **Hover opt-out**: `@display(hover: false)` keeps the property out of the card and affects no other surface.
- **Summary role**: `@display(role: SUMMARY)` names the field rendered as lead text. A field named `summary` holds the role when no field on the type declares it.
- **Identifier**: The preferred `@identifier` field renders as the identifier without any `@display` annotation, and never as an ordinary property.
- **Never previewed**: Section fields other than a singular declared SUMMARY, neighbor fields, empty values, and a field whose only value repeats the title do not appear.
- **Invalid annotations fail schema compilation**: more than one SUMMARY role on a type, a SUMMARY role on a field that is neither a single text value nor a singular section, field arguments on a type, and type arguments on a field are compile errors with a source position.
- **Starters**: The core, action-items, complex-domain, and agentic-engineering starter ontologies ship with `@display` annotations on their fields.

### US5 - Importance shapes the properties panel and view defaults

- id:: ^SPEC-0106-US5
- summary:: As a reader, I see a note's key properties first and its detail properties folded away, and a configured view I have not customized shows the columns that matter.
- status:: ready

#### Acceptance Criteria

- **Panel order**: The properties panel lists KEY properties first, then NORMAL, keeping schema order within each group.
- **Panel detail fold**: DETAIL properties sit behind a keyboard-accessible disclosure that is collapsed by default in read mode.
- **Never hidden**: A property with a validation issue, a missing required property, and every property in edit mode are shown regardless of importance.
- **Unannotated types unchanged**: A type with no `@display` annotations renders its properties exactly as before.
- **View defaults**: A configured view with no authored field list shows title, identifier, KEY fields in schema order, summary when room remains, then Issues and Updated, which always keep their columns. NORMAL fields fill only leftover room under the existing cap. DETAIL fields are never default-visible and stay available in the column picker, which lists KEY first and DETAIL last. Generated column labels are humanized.
- **Authored views unchanged**: A view with an authored field list executes exactly as before.

## Requirements

- The preview endpoint `GET /api/v1/nodes/preview` MUST be served from the summary hydrate profile of the indexed read model and MUST NOT read or parse note files.
- The endpoint MUST resolve the requested ref once, with at most one fallback resolve for an unresolved fragment, and MUST resolve all link values of all fields in one batched resolve.
- The endpoint is part of the public REST contract: it MUST appear in `openapi.yaml`, the capabilities document, and `docs/api/public-api.md`, and MUST use the public error envelope.
- A fragment is matched against the index's recorded fragment targets for the note, using the index's own normalization, and never by reading the note.
- The response MUST cap the summary at 600 characters, each field at 12 values with a count of the remainder, and the card at 24 fields.
- Dates MUST render as `YYYY-MM-DD` and date-times as RFC 3339.
- Field labels MUST come from one shared humanizer that handles camel, kebab, and snake case and common acronyms, and that is idempotent. Configured-view labels MUST use the same humanizer.
- The card MUST use an accessible non-modal pattern for interactive content and MUST be reachable and dismissible by keyboard.
- The web client MUST cache previews per target for a short period and MUST NOT prefetch previews for links the reader has not paused on.
- The feature MUST add no runtime dependency to the web client.

## Open Questions

- **Section content for fragment links.** The card names the linked heading but shows none of its content. The index stores section text, so a bounded excerpt is possible without parsing. Proposed: leave it out until the named heading proves insufficient in use.
- **Importance elsewhere.** Agent context packs and search result cards could use the same annotation. Proposed: separate efforts per surface.
