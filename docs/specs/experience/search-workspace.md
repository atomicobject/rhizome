---
type: ExperienceSpec
id: SPEC-0098
summary: "Defines project-wide ranked search in a retained workspace tab, with note/code, type, and folder filters, no note sidebars, and stable note-context navigation when returning to a note."
spec-status: active
last-updated: 2026-09-11
aliases:
  - SPEC-0098
---

# Search workspace

## Summary

Rhizome users need to find relevant notes and code without confusing project search with filtering a note list. Today the top-bar search control focuses a second search input in the Notes sidebar, while a third input filters the list below. The server-side sidebar query produces alphabetized note matches; it does not provide the ranked project search experience.

The global header becomes the entry point for Rhizome's existing retrieval engine. Submitting a query opens a retained search-results tab. Search occupies the full workspace below the tab strip, without a left sidebar, right sidebar, or collapsed sidebar strips. Results have useful excerpts and explicit scope filters. The note browser retains one local note-list filter. Returning to a note restores its context, whose Info, Outline, and Graph tabs keep the same position.

This spec extends [Notes workspace shell](notes-workspace-shell.md) and consumes [Frontend data lifecycle](../technical/frontend-data-lifecycle.md). Once approved, it replaces SPEC-0090's requirement for a separate sidebar search and makes search tabs an explicit exception to its every-tab left-rail rule. It does not supersede the rest of the Notes-shell contract. Search behavior follows [Search subsystem guidance](../../reference/subsystems/search.md).

## Goals

- Distinguish searching the project from narrowing the current note list.
- Give ranked note and code evidence enough room to be useful.
- Let users refine and revisit a search without losing their reading context.
- Keep query, filters, results, and displayed counts consistent.
- Preserve Rhizome's visual identity and remove movement and duplicate titles in note-context navigation.

## Non-Goals

- A new retrieval engine, relevance-weight tuning, embedding-provider setup, or indexer redesign.
- An AI answer/chat surface, command palette, advanced query builder, saved searches, or search history manager.
- A new code editor or a new family of code workspace tabs; code results use the existing source/Explorer surface.
- Redesigning note Home, note-body rendering, ontology navigation, configured views, or graph layout.
- Removing sidebars from existing note-reading tabs or changing their established collapse preferences. The requested full-width exception applies to search.
- Alternate result sorting in the first delivery. Results remain ordered by relevance; an unsupported sort menu is not shown.
- Phone layouts, dark-theme work, or replacing the established design system.

## User Stories

### US1 - Submit a project search into a full-width results tab

- id:: ^SPEC-0098-US1
- summary:: Search notes and code from the global header and receive results in a retained tab without note-navigation sidebars.
- status:: satisfied

#### Acceptance Criteria

- The global header contains an editable field labelled "Search this project" with placeholder "Search this project…". Cmd/Ctrl+K focuses and selects its text from the app's existing routes.
- Enter submits a nonempty trimmed query. Typing alone does not run the full engine, and whitespace-only submission creates no tab or request.
- A new submitted query opens and activates a search tab labelled with that query, preserving Home and existing note/search tabs. Repeating the same normalized query with its initial filters reuses the matching tab.
- While a search tab is active, results occupy the workspace below the global header and tab strip. Neither note sidebar nor its collapsed strip, reserved column, note-edit toolbar, or note-context title is present. Home remains reachable in the tab strip.
- The header is the only editable query field on the search page. The result heading and submitted query are read-only text. Submitting another query from the header starts from All, Any type, and Anywhere, independently of the current note collection.

### US2 - Read ranked evidence and open its source

- id:: ^SPEC-0098-US2
- summary:: Scan relevant excerpts from notes and code, then open the identified source without losing the search.
- status:: satisfied

#### Acceptance Criteria

- Results use the existing unified Rhizome search engine and retain its relevance order. Each row identifies a note or code result, its title or symbol, its vault-relative path, and available type and section/location metadata.
- Each result provides an available source-backed excerpt, with unobtrusive literal query-term highlighting. Missing or omitted excerpts are represented honestly; generated prose, filenames, and titles are not presented as matching source excerpts.
- Clicking a note result creates or activates its existing file tab. A section or embedded-node match opens the canonical target in that tab. Cmd/Ctrl-click opens or reuses the note tab without activating it. The search tab remains available.
- Clicking a code result opens the existing source/Explorer surface for that file and its available location. The UI does not claim to jump to a symbol or line when that location is absent; returning to search restores its state.
- "Load more results" appends the next page for the same query and filters, preserving relevance order and earlier rows. End-of-results, retryable pagination failure, and a continuation that needs a fresh search are distinguishable.

### US3 - Refine the searched scope with explicit filters

- id:: ^SPEC-0098-US3
- summary:: Narrow project search by result kind, note type, and folder while trusting that filters apply to the searched result set.
- status:: satisfied

#### Acceptance Criteria

- The results toolbar offers All, Notes, and Code, plus "Type: Any" and "Folder: Anywhere" controls. Relevance is the default ordering. Clearing filters restores the full project scope for the current query.
- Selecting a concrete note type selects Notes scope. Selecting Code clears the note-type filter. Available note types come from the live ontology, and schema-less vaults remain searchable without a type control.
- A folder selection constrains results to that vault-relative directory and its descendants. Directory boundaries are respected, so selecting `docs/specs` does not match `docs/specs-old`.
- Filters apply on the server before counting and pagination across eligible search candidates. A match outside the first unfiltered page can still appear in a filtered search. Filtering only the already-loaded browser rows does not satisfy this story.
- A filter change updates the current search tab, resets its continuation and scroll to the first page, and presents the new state as loading. Counts describe the effective search scope and any engine cap; exact totals and per-scope badges appear only when the backend can support their meaning.

### US4 - Resume searches and trust asynchronous results

- id:: ^SPEC-0098-US4
- summary:: Switch tabs, reload, follow a search URL, or retry a request without mixing search identities or losing context.
- status:: satisfied

#### Acceptance Criteria

- Switching away and back preserves a search tab's query, filters, loaded result pages, and scroll position. Each search tab retains its own state; new input in the header does not relabel older results before submission.
- A shareable URL represents the active search and effective filters. Reload restores the vault-scoped tab set and active search, re-executing against the current index as needed. Browser Back/Forward restores the corresponding search state without duplicating tabs.
- Existing note URLs, open note tabs, fragments, dirty-state protection, and the Home collection selection survive the introduction of search tabs and any tab-storage migration. Closing a search tab activates an adjacent retained tab or Home.
- Initial loading, no matches, retryable failure, loading another page, and background refresh are distinct states. Failed requests retain the query and filters; an empty result offers a clear way to remove restrictive filters.
- A late response cannot replace results for a newer query/filter identity or reopen a closed tab. Same-query cached results may remain during refresh with a refresh indicator; different-query results cannot masquerade as the new response.
- When semantic retrieval is unavailable or the index is incomplete, the page presents any usable fallback results with a concise capability/availability message. It does not turn a search failure or incomplete search into an unqualified "No results" message.

### US5 - Keep local note filtering separate from project search

- id:: ^SPEC-0098-US5
- summary:: Narrow the current note collection with one clearly local filter when using the note browser.
- status:: satisfied

#### Acceptance Criteria

- The obsolete upper-sidebar "Search notes…" field is removed. On note-browser surfaces the sidebar has collection navigation and its note list, with one "Filter notes…" field beside the list controls.
- The filter immediately narrows that list by title, path, or resolved type and shows the matching count against the current collection. It does not run project search or replace the collection with search results.
- Note-collection selection and local filtering never constrain a global search implicitly. Returning from a search tab restores the prior note-browser selection and local filter.
- Moving between a search tab and a note/Home tab restores the appropriate existing note rails and collapse/context-tab preferences without resetting the note's reading mode, fragment, or drill position.

### US6 - Navigate note context without shifting tabs or duplicate titles

- id:: ^SPEC-0098-US6
- summary:: Switch between Info, Outline, and Graph in a note's right sidebar while its navigation stays fixed and its outline exposes useful sections.
- status:: satisfied

#### Acceptance Criteria

- Info, Outline, and Graph share a single tab row at the same top coordinate and height. Switching tabs, loading graph data, or encountering an error does not insert a note title above that row or move it vertically.
- The Outline and Graph panels omit the redundant note-title headers currently placed above the tabs. Any useful note identity within Info stays below the shared tabs.
- The outline omits a redundant note-container wrapper and matching top-level H1 when they merely repeat the current note's identity, promoting their actual sections. It preserves distinct headings, embedded nodes, meaningful hierarchy, and repeated titles that identify different targets.
- Selecting a promoted outline item still navigates to its correct rendered section or node in Read and Source modes. The fix does not rewrite authored Markdown or change canonical node/anchor identity.

## Requirements

- MUST use the existing global header, tab strip, location adapter, server-state lifecycle, and note-open contract. Search is a tab kind, not a separate application shell or replacement routing framework.
- MUST keep eligibility, rank order, counts, continuations, canonical targets, and capability warnings consistent across the engine, HTTP adapter, and UI. Folder and type restrictions cannot be simulated by post-filtering a bounded HTTP page.
- MUST preserve vault include/exclude rules, path normalization, request cancellation, safe rendering, and existing node/edit-session boundaries. Display source excerpts as text; a search read does not mutate notes or repair anchors.
- MUST scope cached results by vault, submitted query, effective filters, and page identity. Preserve the index-invalidation contract in SPEC-0074.
- MUST remain usable at desktop widths of 1280px and 1440px without horizontal page overflow. Results use the available workspace width with readable excerpt lengths and aligned compact metadata.
- MUST use the existing `DESIGN.md` typography and color tokens: dark warm-ink result titles, coral emphasis for hover/focus/selection, and mono paths. Use an underline or other clear interaction cue; saturated blue result links from the concept image are not part of the approved direction.
- MUST support keyboard submission, tab navigation, filter controls, source links, pagination, and retries with accessible names and visible focus. Status and match highlighting cannot depend on color alone.
- SHOULD use dense rows separated by hairlines, a compact scope toolbar, and short excerpts. Do not add decorative cards or make raw ranking scores part of the normal user flow.

## Open Questions

No product question blocks this planning draft. Proposed implementation defaults are explicit above: new submissions create/reuse a search tab; filters refine the current tab; selecting a type chooses Notes; code results use Explorer; relevance is the sole initial order. Plan approval confirms these defaults and the bounded note-context fixes.

The HTTP result/filter contract and exact code-location navigation need a foundation review during implementation because the current endpoint omits data and controls. That review must preserve this experience contract; a proposed scope reduction returns for a product decision.

## Design reference

The [search workspace concept](assets/search-workspace-concept.png) is the generated image reviewed in this conversation. It records the accepted result-row and filter direction, with two explicit corrections from the user: remove both sidebars from search and replace the blue links with Rhizome's established colors. Its counts, excerpts, and code signature are illustrative, not execution evidence or API contracts.

## Documentation plan

During implementation, reconcile SPEC-0090's search placement and rail-visibility rules without rewriting closed effort history. Update `web/CONTEXT.md` for tab and rail ownership, `pkg/app/web/CONTEXT.md` and the OpenAPI contract for search inputs/results, and any owning subsystem guidance whose invariant changes. Keep the implementation plan, decisions, validation evidence, and delivery status in the linked effort rather than appending execution history to this spec.
