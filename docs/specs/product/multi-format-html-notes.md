---
type: ProductSpec
id: SPEC-0083
summary: "Defines first-class, trusted HTML notes within Rhizome's multi-format note model, including discovery, search, root typing, graph participation, bounded maintenance, and active web viewing."
spec-status: active
last-updated: 2026-09-11
aliases:
  - SPEC-0083
  - multi-format-html-notes
  - Multi-format HTML notes
---

# Multi-format HTML notes

## Summary

Rhizome should treat explicitly configured HTML files as first-class notes without converting them to Markdown. Those notes must be discoverable, searchable, queryable, linkable, and viewable across the same product surfaces as Markdown notes. HTML remains the authored source, while each note format provides the derived text, metadata, links, and capabilities that shared Rhizome services consume.

HTML note support is intentionally read-oriented. Users can view the authored page, search its statically embedded content, use root-level ontology metadata, follow and retarget links, and perform narrow validation or maintenance operations. The HTML body and Source view remain read only even when metadata editing is available. Rhizome does not provide a general HTML editor or project the document's DOM structure into ontology sections in this phase.

The repository owner's extension-explicit note-include configuration is the trust and rollout boundary. A classic-vault default or broad legacy include does not silently activate HTML after an upgrade. An explicitly included HTML note may execute its authored JavaScript and load authored external resources in the web viewer, but it remains isolated from the Rhizome application. Indexing is always static and deterministic: it never executes JavaScript, fetches external resources, or treats runtime-generated DOM as source content.

## Goals

- let repository owners opt trusted `.html` and `.htm` files into note ownership without conversion or duplicate code identity
- provide lexical, semantic, typed-query, file-context, CLI, agent/MCP, graph, validation, and web discoverability for HTML notes
- preserve raw HTML as source authority while exposing deterministic, format-aware projections for search and ontology behavior
- support root-level ontology typing through a canonical plain-JSON metadata block
- render the actual authored HTML, including JavaScript and external resources, without granting it authority over the Rhizome application
- make authored local links behave like Rhizome navigation and graph edges, including safe retargeting during confirmed renames and moves
- provide a narrow, source-preserving maintenance surface for root metadata and authored links
- establish a note-format model that can support additional formats and richer HTML capabilities later without making HTML-specific behavior universal

## Non-Goals

- converting, importing, or mirroring HTML as Markdown
- general-purpose HTML or DOM editing in the web UI
- creating or scaffolding new HTML notes through Rhizome; v1 operates on repository-authored files
- structural ontology projection of headings, sections, elements, or runtime-generated DOM
- executing JavaScript, loading resources, or rendering a browser DOM during indexing
- indexing external script files, remote resources, or asset content as part of an HTML note
- interpreting inline JavaScript as code intelligence, symbols, imports, calls, coderefs, or a second code-owned document
- extracting content from runtime UI state that is not embedded in the authored HTML source
- using JSON-LD, RDFa, microdata, arbitrary `<meta>` elements, or selectors as ontology inputs in this phase
- validating or repairing general HTML conformance, CSS, JavaScript, DOM structure, or external URL availability
- adding per-file active-content prompts or separate scripts/external-resource trust controls
- supporting `.xhtml` in the initial HTML provider
- loading third-party note-format providers at runtime; the initial provider registry is an internal product extension point
- reloading or reindexing a note solely because one of its local or external assets changed

## User Stories

### US1 - Opt trusted HTML files into first-class note ownership

- id:: ^SPEC-0083-US1
- summary:: A repository owner can include HTML paths as notes and receive one consistent note identity across Rhizome.
- status:: ready

#### Acceptance Criteria

- A repository owner can explicitly include `.html` and `.htm` paths through extension-constrained note-inclusion configuration, with provider extension matching performed case-insensitively. ^SPEC-0083-US1-AC1
- A configured HTML path is owned as a note across discovery, indexing, graph, query, agent, and web surfaces rather than also appearing as a code document. ^SPEC-0083-US1-AC2
- An HTML path not included as a note retains the ordinary code-file classification and cannot be opened through the active HTML note viewer. ^SPEC-0083-US1-AC3
- Extension-explicit note inclusion is the sole explicit trust and rollout choice for initial HTML support; classic-vault defaults and broad legacy globs preserve their prior ownership set, and users are not asked to maintain separate feature, script, or external-resource flags. ^SPEC-0083-US1-AC4
- HTML remains the canonical authored file in its repository location; enabling note ownership does not create a converted or mirrored Markdown file. ^SPEC-0083-US1-AC5
- Note-oriented product surfaces disclose the source format so users and agents can distinguish HTML from Markdown without inferring it from content. ^SPEC-0083-US1-AC6

### US2 - Find statically embedded HTML content through every note-retrieval surface

- id:: ^SPEC-0083-US2
- summary:: A user or agent can find relevant HTML notes through the same lexical, semantic, and structured retrieval workflows used for Markdown notes.
- status:: ready

#### Acceptance Criteria

- Visible headings, prose, lists, tables, captions, preformatted text, code text, and useful alternative text from the authored HTML are available to lexical and semantic note search. ^SPEC-0083-US2-AC1
- Comments, style content, hidden content, and accessibility-hidden content are excluded from ordinary visible-text search projection. ^SPEC-0083-US2-AC2
- Bodies of inline JavaScript and inline JSON or data blocks are indexed as lower-priority supplemental note content, with the canonical Rhizome metadata block excluded to avoid duplicate metadata evidence. ^SPEC-0083-US2-AC3
- Supplemental script or data text remains owned by the note and is not surfaced as symbols, imports, calls, coderefs, code-search results, or a separate document identity. ^SPEC-0083-US2-AC4
- Indexing never executes scripts, fetches external script or resource content, or includes content produced only in the runtime DOM. ^SPEC-0083-US2-AC5
- HTML notes participate in lexical search, semantic search, typed query, file-context, CLI, agent/MCP, and web search with the same source identity and format disclosure. ^SPEC-0083-US2-AC6
- Search projection may use heading boundaries and breadcrumbs to improve chunks and locators without turning headings into ontology sections or nodes. ^SPEC-0083-US2-AC7
- Supplemental content uses the normal bounded semantic budgets and reports truncation diagnostics rather than expanding indexing work without limit. ^SPEC-0083-US2-AC8

### US3 - Read either the authored source or the representation appropriate to the task

- id:: ^SPEC-0083-US3
- summary:: A user or agent receives raw HTML for source reads and a clearly identified derived representation for search-oriented reads.
- status:: ready

#### Acceptance Criteria

- Direct note read and print operations return the raw authored HTML, not a Markdown conversion or reconstructed DOM. ^SPEC-0083-US3-AC1
- Search, semantic retrieval, file-context, and agent-oriented evidence use the deterministic derived text representation and identify that representation in their response contract. ^SPEC-0083-US3-AC2
- Search evidence retains source locators precise enough to relate visible or supplemental derived text back to the authored HTML. ^SPEC-0083-US3-AC3
- Tolerantly parseable malformed HTML remains readable and searchable rather than being rejected only because a browser would repair its DOM. ^SPEC-0083-US3-AC4
- The display title resolves in this order: root metadata `title`, document `<title>`, first visible `<h1>`, then filename. ^SPEC-0083-US3-AC5

### US4 - Type an HTML note through canonical root metadata

- id:: ^SPEC-0083-US4
- summary:: An author can declare root-level ontology metadata in plain JSON without imposing structural sections on the HTML document.
- status:: ready

#### Acceptance Criteria

- An HTML note may contain exactly one canonical `<script id="rhizome-metadata" type="application/json">` block as a direct child of `<head>`. ^SPEC-0083-US4-AC1
- The metadata payload is a top-level plain-JSON object whose canonical keys, including `type`, have the same ontology meanings as corresponding Markdown root metadata. ^SPEC-0083-US4-AC2
- A valid `type` value projects one typed note root that can be queried and related through the ontology; HTML elements and headings do not become additional ontology nodes. ^SPEC-0083-US4-AC3
- The root metadata property panel exposes parsed fields while source-oriented views continue to expose the authored JSON block in context. ^SPEC-0083-US4-AC4
- A missing metadata block leaves the HTML note untyped but does not prevent ordinary text, link, graph, or viewer behavior. ^SPEC-0083-US4-AC5
- Duplicate or malformed canonical metadata produces a blocking diagnostic for typing and mutation, while static text and link indexing continue and the note remains untyped. ^SPEC-0083-US4-AC6
- JSON-LD, microdata, RDFa, arbitrary `<meta>` elements, and DOM selectors do not contribute ontology fields in this phase. ^SPEC-0083-US4-AC7

### US5 - View trusted HTML as the authored interactive page without giving it Rhizome authority

- id:: ^SPEC-0083-US5
- summary:: A user can open the actual trusted HTML page in its own retained note tab, including its scripts and external resources, while Rhizome remains isolated and in control of navigation.
- status:: ready

#### Acceptance Criteria

- The web workspace renders the actual HTML document in an isolated frame rather than replacing it with a derived Markdown or plain-text preview. ^SPEC-0083-US5-AC1
- Authored JavaScript may execute and authored external resources may load because note inclusion establishes repository trust. ^SPEC-0083-US5-AC2
- The framed document has an opaque origin and no access to Rhizome cookies, storage, DOM, authenticated APIs, or filesystem authority. ^SPEC-0083-US5-AC3
- A narrow, versioned navigation bridge lets authored content request note, fragment, or external navigation without exposing query, mutation, filesystem, or authentication capabilities. ^SPEC-0083-US5-AC4
- Same-document fragments scroll within the existing HTML tab, cross-note links create or activate retained note tabs under [[notes-workspace-shell|SPEC-0090]], and external links are allowed through an intentional parent-controlled action. ^SPEC-0083-US5-AC5
- View-source and open-original actions remain available, and open-original preserves the same application isolation rather than serving trusted HTML with Rhizome's origin authority. ^SPEC-0083-US5-AC6
- Viewer or external-resource failures produce pane-scoped diagnostics and do not make the indexed note unavailable to search, graph, query, or agent surfaces. ^SPEC-0083-US5-AC7
- The product does not add a persistent active-content badge or prompt to every included HTML note. ^SPEC-0083-US5-AC8
- A prototype can export a bounded generated file through a parent-controlled download action without receiving native iframe download, popup, application, or filesystem authority. ^SPEC-0083-US5-AC9

### US6 - Navigate and understand authored HTML links as part of the vault graph

- id:: ^SPEC-0083-US6
- summary:: A user can follow local HTML links and fragments through Rhizome while backlinks and graph views reflect the same resolved destinations.
- status:: ready

#### Acceptance Criteria

- Local anchor `href` values contribute navigation, backlink, and graph relationships when their destination resolves within the vault. ^SPEC-0083-US6-AC1
- Relative targets resolve from the source file's directory, vault-root targets resolve from the vault root, and an authored `<base>` element is honored; a base outside the vault makes relative targets external. ^SPEC-0083-US6-AC2
- Query strings are preserved for navigation but excluded from note identity, while fragments remain separate locators within the resolved note. ^SPEC-0083-US6-AC3
- Fragment resolution recognizes element `id` values and legacy anchor `name` values as navigation and search locators without projecting them as ontology nodes. ^SPEC-0083-US6-AC4
- External schemes remain external and do not create internal graph edges or trigger external-link availability checks. ^SPEC-0083-US6-AC5
- Broken internal paths and fragments are available to validation with diagnostics that identify the authored source location and candidate destination when one exists. ^SPEC-0083-US6-AC6
- Link resolution, candidate matching, confirmation heuristics, and rename or move policy are shared across formats; an HTML provider supplies authored targets, source ranges, and replacement encoding without inventing a competing workflow. ^SPEC-0083-US6-AC7
- Explicit-extension links select that note format, while extensionless links and aliases use a shared cross-format candidate set; same-basename collisions remain ambiguous, and v1 does not infer HTML extensions or directory `index.html` targets. ^SPEC-0083-US6-AC8

### US7 - Perform narrow, source-preserving HTML note maintenance

- id:: ^SPEC-0083-US7
- summary:: A user or agent can repair root metadata and authored links or move an HTML note through the same reviewable safeguards used for Markdown maintenance.
- status:: ready

#### Acceptance Criteria

- Capability reporting distinguishes root-metadata edits, link retargeting, file moves, and structural edits rather than labeling an entire format simply editable or read-only. ^SPEC-0083-US7-AC1
- HTML notes support setting, adding, deleting, and renaming root metadata fields, including metadata fields whose values are ontology links. ^SPEC-0083-US7-AC2
- Metadata mutation preserves the document body byte-for-byte, writes stable pretty JSON, safely represents a literal `</script>` sequence, and may synthesize the minimum valid `<head>` container when needed. ^SPEC-0083-US7-AC3
- Link retargeting changes only the intended authored anchor or metadata-link value and does not rewrite JavaScript, CSS, templates, runtime URLs, or unrelated HTML. ^SPEC-0083-US7-AC4
- A confirmed file move can update inbound references and the moved note's outbound relative links using the same candidate matching, confirmation, and unsafe-move blocking policy as Markdown. ^SPEC-0083-US7-AC5
- Metadata and link operations use shared preview, transaction, source-hash drift, revalidation, safe-versus-confirm, and apply-result behavior across CLI, validation, agent/MCP, and supporting web result surfaces. ^SPEC-0083-US7-AC6
- Automated fixes are limited to deterministic metadata defaults, unambiguous retargets, and known rename or move operations; they do not repair general HTML conformance, element identifiers, DOM structure, or external URLs. ^SPEC-0083-US7-AC7
- The web UI may stage, preview, and save root metadata through the existing property and edit-session experience. HTML body, Source, narrative, structural, link-retarget, and move operations remain read only until their governing delivery slices are complete. ^SPEC-0083-US7-AC8

### US8 - Keep HTML note state convergent across configuration and source changes

- id:: ^SPEC-0083-US8
- summary:: A repository owner receives consistent full and incremental behavior when HTML notes are created, changed, moved, deleted, or reclassified.
- status:: ready

#### Acceptance Criteria

- Initial indexing and incremental create, modify, rename, move, and delete processing produce equivalent note, search, graph, ontology, and diagnostic state. ^SPEC-0083-US8-AC1
- When configuration gives an HTML path note ownership, Rhizome atomically removes any code or coderef representation before publishing the note-owned projection. ^SPEC-0083-US8-AC2
- When configuration removes note ownership, Rhizome removes the note's search, graph, metadata, ontology, and viewer state and allows the ordinary code classifier to reclaim the path. ^SPEC-0083-US8-AC3
- Format-provider or parser version changes and relevant configuration changes invalidate affected derived data without requiring users to discover stale projections manually. ^SPEC-0083-US8-AC4
- An unreadable or fatally unprojectable HTML source retains file identity and a diagnostic but does not leave stale search, graph, or ontology claims active. ^SPEC-0083-US8-AC5
- A change to the HTML source reloads the open viewer and reindexes the note; changes to referenced assets alone do not trigger note reload or reindex. ^SPEC-0083-US8-AC6
- A representative acceptance fixture proves discovery, lexical and semantic search, typed query, graph/backlinks, validation and fixes, CLI and agent/MCP reads, watcher updates, web rendering, link navigation, rename or move handling, and source preservation. ^SPEC-0083-US8-AC7

## Requirements

### Must

- Rhizome MUST model note format as a capability-bearing provider contract while preserving one canonical note identity per configured source path.
- Configured HTML notes MUST remain authored HTML and MUST NOT acquire a competing code identity.
- Initial HTML support MUST recognize `.html` and `.htm` case-insensitively and MUST use extension-explicit note-inclusion configuration as its sole trust boundary without changing classic-vault or broad-glob ownership on upgrade.
- HTML notes MUST participate in lexical search, semantic search, typed query, file-context, graph, validation, CLI, agent/MCP, incremental indexing, and web browsing.
- Indexing MUST be static and deterministic: it MUST NOT execute scripts, fetch resources, or treat runtime DOM state as authored content.
- Inline script and data bodies MUST be eligible as supplemental note-owned search content and MUST NOT enter code-intelligence models.
- Raw-note reads MUST return authored HTML; derived reads MUST disclose their representation and preserve source provenance.
- HTML root typing MUST use one canonical plain-JSON metadata block and MUST remain root-only in the initial release.
- The web viewer MUST render authored HTML with scripts and external resources allowed while isolating the frame from Rhizome application authority.
- The isolated viewer MUST work for local and remotely accessed Rhizome deployments and MUST fail closed when a separate browser-reachable content origin is not configured.
- Interactive prototypes MUST be able to export bounded generated files through an explicit parent-controlled user action without granting direct iframe download authority.
- Authored local links MUST use shared resolution, graph, navigation, validation, candidate-matching, confirmation, and rename or move infrastructure.
- Cross-format addressing MUST preserve explicit extensions, treat extensionless and alias collisions as ambiguity rather than format precedence, and require explicit filenames instead of inferring HTML extensions or directory indexes in v1.
- HTML maintenance MUST remain limited to source-preserving root-metadata operations, authored-link retargeting, and file moves.
- Full and incremental pipelines MUST converge across ownership, source, parser-version, rename, move, and deletion changes.
- Lexical and semantic ranking MUST apply visible-versus-supplemental weighting before each retrieval lane truncates candidates, so supplemental content remains discoverable but does not routinely outrank comparable visible prose.

### Should

- Malformed but tolerantly parseable HTML SHOULD retain read, search, link, and viewer behavior whenever a deterministic projection is possible.
- Every surface SHOULD expose capabilities explicitly so callers do not infer that HTML supports Markdown structural operations.
- Diagnostics SHOULD distinguish source parse, metadata, link, viewer, and freshness failures so one failed projection does not hide healthy representations.
- The provider model SHOULD make a later format or richer HTML projection additive rather than requiring format checks throughout shared product surfaces.

### May

- Future HTML support MAY add structural ontology projections, richer safe editing, or embedded-code intelligence under separate specifications.
- Future format providers MAY use metadata syntaxes other than Markdown YAML or HTML JSON when they project the same canonical root contract.
- The viewer bridge MAY gain additional presentation-only affordances only when they preserve its closed, versioned contract and do not expose repository, mutation, query, filesystem, or authentication authority.

## Related Contracts

- [SPEC-0076 — Note-node indexing architecture](../technical/note-node-indexing-architecture.md) owns the format-neutral authored-source, projection, indexing-ownership, and semantic-evidence foundation.
- [SPEC-0084 — Note format provider registry](../technical/note-format-provider-registry.md) owns provider registration, note/code classification, capabilities, and lifecycle invalidation.
- [SPEC-0085 — HTML note format provider](../technical/html-note-format-provider.md) owns HTML metadata, static extraction, links, fragments, and search regions.
- [SPEC-0086 — Format-aware root ontology projection](../technical/format-aware-root-ontology-projection.md) owns typed and fallback root semantics.
- [SPEC-0087 — Format-aware note maintenance mutations](../technical/format-aware-note-maintenance-mutations.md) owns source-preserving metadata, link, rename, and move operations.
- [SPEC-0088 — Trusted HTML note viewer](../experience/trusted-html-note-viewer.md) owns the isolated active iframe, pane behavior, and navigation bridge.
- Existing search, graph, query, validation, agent, and workspace contracts remain authoritative for their cross-format surface behavior.

## Documentation Plan

- Add a user guide for enabling trusted HTML notes through note-inclusion configuration, including the trust consequences of active scripts and external resources.
- Add an HTML authoring guide covering canonical JSON root metadata, supported values, title precedence, local link behavior, and root-only ontology scope.
- Update search and agent guidance to distinguish raw authored HTML from visible and supplemental derived search representations.
- Update validation and mutation guidance with supported HTML fixes, preview and confirmation behavior, source-preservation guarantees, and explicit unsupported structural edits.
- Document the web viewer's isolation model, navigation bridge, external-resource behavior, and open-original safety boundary.
- Update subsystem references for discovery, indexing, note metadata, ontology, search, validation, graph/navigation, MCP/agent reads, watcher invalidation, and web workspaces as implementation lands.
- Add an end-to-end fixture guide that demonstrates parity across full and incremental pipelines and all required product surfaces.

## Open Questions

- None.
