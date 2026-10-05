---
type: ReferenceDoc
summary: "Critical review of shipped HTML ownership, missing retrieval and viewer integration, tab routing fixes, and the decisions needed for first-class HTML notes."
reference-kind: analysis
last-verified: 2026-09-11
code-paths:
  - pkg/noteformat/html
  - pkg/app/indexing
  - pkg/ontology/noderead
  - web/src/components/notesRoute.ts
  - web/src/lib/content.ts
tags: [html, note-formats, web, indexing]
---

# HTML note integration review

## Result and scope

HTML has a sound ownership foundation, but the first-class feature has not been implemented. PR #188 merged on 2026-08-09 as `fcc95dc534c8bca135f673e23d128f67c64cf527`. Its final branch tree exactly matches that squash commit. The completed provider foundation effort explicitly excluded extraction, HTML metadata, search, ontology projection, mutation, and viewing. Those are missing delivery slices, not regressions in what that PR promised.

This review began against `main` at `ce69a1fe8150c9a3e42902a0215f2dcdce9a5c7d`. The new `feat/html-note-integration` branch starts there. The old effort remains closed. The user authorized branch integration, clear fixes, verification, and a new PR, while reserving material experience choices for the end of the review.

The immediate corrections preserve HTML file identity through the current tab and link routes, constrain file reads to the vault, reject unsupported HTML mutations, and align startup/live indexing with executable provider support. The proposed viewer contract now follows [[notes-workspace-shell|SPEC-0090]] and explicitly keeps HTML Read and Source content read only. This change does not advertise an HTML projector, renderer, or metadata writer that has not been connected and verified.

The detailed follow-on plan is First-class HTML notes and prototypes. It records the user's subsequent decisions to insert missing metadata on Save, include generated downloads, and support both local and remote access. It specifies the implementation phases, owning modules, contract reviews, and acceptance evidence; these capabilities remain future delivery work.

## Verified current coverage

| Surface | Current behavior | Remaining work |
| --- | --- | --- |
| Ownership and discovery | Explicit `.html`/`.htm` includes select one note owner; broad legacy includes do not silently opt HTML in. Provider extension matching is case insensitive. | Keep this admission rule throughout viewing and writes. |
| Full and incremental lifecycle | Shared transitions retire the prior owner, preserve source identity, and reconcile interrupted work. HTML remains descriptor-only. | Verify the same transitions once HTML publishes actual derived facts. |
| Source reads | The HTML descriptor advertises source reading, but current public note-read adapters require an executable projector and reject or omit HTML. | Add a configured-ownership-checked raw-source route and disclose format/representation consistently. |
| Parsing and metadata | `pkg/noteformat/html/provider.go` contains a descriptor, not a `Projector`. | Implement static title, JSON metadata, aliases, tags, links, fragments, and search evidence. |
| Lexical and semantic search | Current indexing and semantic body assembly still depend on Markdown. Provider `SearchRegions` have no production lexical or semantic search-index consumer. | Connect visible and supplemental provider evidence to the existing note search model. |
| Ontology and typed query | Indexing, hydration, and query snapshot paths explicitly reject or skip non-Markdown. | Project one HTML root from provider facts, with no synthetic Markdown sections. |
| Graph and backlinks | Shared ownership is ready, but no HTML link facts are produced. Relative-link consumers still use Markdown policy and resolution. | Add URI/base/query/fragment semantics to the shared resolver and preserve source locators. |
| Tabs and navigation | The retained-tab shell already has the required state and navigation behavior. Its path normalization incorrectly appended `.md` to HTML filenames. | Fix the path defect now; connect the eventual renderer to this shell. |
| Reading UI | Descriptor-only HTML is rejected before rendering. A future projectable non-Markdown provider would enter a raw-source fallback with a Markdown-shaped response. | Use an explicit format/representation contract and an HTML reading component before enabling projection. |
| Metadata editing | Existing property controls and edit sessions are reusable; no HTML metadata edit adapter exists. | Add precise JSON-span writes and capability gating through the existing staged Save flow. |
| Active document viewing | No dedicated content origin, viewer capability, sandbox response, bridge, or identity handshake exists. | Deliver and browser-test the isolation boundary before enabling authored scripts. |

## Findings and design corrections

### Fix now: preserve explicit HTML paths

`web/src/components/notesRoute.ts` converted `reports/page.html` into `reports/page.html.md`. `useNoteTabs` and persisted tab restoration share that normalizer, so the error affected open, reuse, deep links, and reload. `web/src/lib/content.ts` also appended `.md` in rendered-link fallback. Both boundaries need regression coverage, including uppercase extensions, ordinary Markdown shorthand, dotted Markdown titles, fragment navigation, and Markdown/HTML notes with the same basename.

This is an identity correction, not permission to open arbitrary files as notes. The server remains responsible for configured ownership and supported capabilities.

### Fix now: enforce the existing file and capability boundaries

The existing tree, raw-file, and rendered-note APIs joined user paths to the vault root without checking resolved containment. Traversal and a symlink pointing outside the vault could reach unrelated files. All three now share a confined resolver that also checks ignore rules against aliases and resolved targets; legitimate vault files and internal symlinks remain usable. This correction does not turn those APIs into the future isolated HTML content server.

`RenameNote`, `MoveNotes`, and `RenameHeading` accepted HTML paths despite the descriptor lacking move and structural mutation support. A Markdown-looking line inside HTML could be rewritten as a heading. The fix checks the caller-composed format runtime and the exact mutation capability before parsing or writing, checks both ends of a move, and preflights every batch member before the first rename. Ordinary attachment moves retain their existing behavior. Neither a filename change nor a metadata capability authorizes format conversion or body editing.

Agent startup classified descriptor-only HTML as a rich read using `SourceReading` alone, while the executing adapters require `CanProject`. Startup must report the same capability as execution. Likewise, two live semantic rebuild paths used every metadata path, including stale descriptor-only HTML, while full indexing uses `ontology.ProjectableMetadataPaths`. Reuse the full-index selection for live rebuilds so HTML cannot enter the Markdown semantic parser.

### Critical integration gap: a projector alone would publish misleading capabilities

The relevant boundaries are concrete:

- `pkg/app/indexing/ownership_run.go` and `unified.go` feed Markdown candidates into the current note ingestion lane. `pkg/app/codeintel/ingest.go` and `pkg/anchors/batch.go` enforce Markdown parsing there.
- `pkg/ontology/index.go` and `sync.go` explicitly select Markdown sources and build Markdown document snapshots.
- `pkg/ontology/noderead/records.go` and `pkg/ontology/query/note_path_cache.go`, `loaders.go`, and `execute.go` retain Markdown-only hydration assumptions.
- `pkg/search/semantic/ontology_node_syncer.go` and `ontology_node_body.go` build evidence from ontology/Markdown bodies rather than consuming provider search regions.

Registering an HTML projector would make some readiness and metadata paths consider the note projectable while those consumers still omit it or parse it incorrectly. Enable each advertised capability only when its production read and lifecycle paths work. Retain one source identity and the existing publication/barrier model.

### Critical integration gap: source, search text, and rendered content need distinct contracts

The public GraphQL `NodeContent` contract and TypeScript adapter currently carry a `markdown` representation. `pkg/app/web/ontology.go` currently rejects descriptor-only HTML. Its fallback for a future projectable non-Markdown provider would put raw source into `Content` and `Rendered`; `OntologyNotePane` then selects Markdown rendering. Thus enabling projection alone would expose the next incorrect boundary.

Add a format and representation discriminator at the canonical read boundary. Preserve raw source separately from provider-derived search text and from the viewer descriptor. Update GraphQL, OpenAPI where applicable, generated types, the TypeScript adapter, agent/CLI output, and format presentation together. Source bytes must never select a parser heuristically.

### Important integration gap: diagnostics and links are incomplete contracts

`pkg/notemeta/projection_rows.go` persists the first blocking diagnostic for a fatal projection; it drops nonblocking diagnostics from a current projection. HTML with malformed or duplicate metadata must remain searchable and viewable while showing its metadata error. Choose the smallest durable diagnostic representation or deterministic current-source recomputation; do not assume a new generic table is necessary.

The current relative-link facts and consumers do not express the full HTML contract. `projection_rows.go` and `source_snapshot_adapt.go` gate relative links through `SupportsMarkdownLinks` and resolve them using `ResolveMdLink`. HTML needs decoded URI values, `<base>` semantics, exact attribute ranges, query preservation, fragments, and the correct replacement encoding. Keep that syntax work inside the provider and use one canonical target resolver across navigation, graph, validation, and moves.

### Important design correction: use the current tab workspace

SPEC-0088 described opening to the right and truncating deeper panes. Those rules conflict with the delivered tab shell. The revised contract uses one retained tab per canonical file; ordinary activation opens or focuses it, Cmd/Ctrl activation opens new targets beside or reuses existing tabs in place without changing the active tab, and fragments remain inside the same HTML document. Notes with the same basename and different formats retain distinct identities.

Reuse `NotesShell`, `NoteTab`, the query cache, node events, and the right rail. An HTML heading or element ID can supply an outline/scroll target without becoming an ontology section or drill-down node. The title/type/properties/relations remain Rhizome chrome outside the frame; authored HTML owns the document layout.

### Important design correction: read-only body and editable metadata are separate

Keep Read and Source available, with inert source inspection. Global edit mode and metadata capability must never enable narrative, whole-source, heading, section, or DOM changes for HTML. The browser controls and every server mutation ingress must enforce this distinction.

The recommended metadata experience reuses `OntologyIdentityStrip`, `OntologyPropertyPanel`, and the existing edit session. Edit typed fields in the right rail, preview the changes, then use the normal Save/Discard controls. Show the dirty marker on the HTML tab. Preserve body bytes and all bytes outside the metadata span; reject duplicate/invalid JSON and stale source hashes; escape script terminators during serialization. Source-side changes must participate in the same conflict handling and refresh paths as other staged edits.

There is a real spec conflict to resolve: [[format-aware-note-maintenance-mutations|SPEC-0087.US4.AC3]] currently prohibits web-originated mutation commits. Metadata editing needs a bounded amendment to that rule. It does not require turning HTML into a general editor, introducing a JSON editing screen, or moving metadata into a sidecar file.

### Foundation decision: active HTML viewing needs a complete isolation design

The existing spec intends authored scripts and external HTTPS resources to work. Preserve that target if HTML is meant to include interactive reports and presentations. A static-only preview is a different product scope and should be chosen explicitly.

The browser boundary requires both the frame sandbox and response-level protection for standalone viewing. Without `allow-same-origin`, sandboxed content receives an opaque origin; response CSP sandboxing also applies when the document is opened outside its original iframe. The CSP sandbox directive cannot be supplied through a meta element. See [MDN's iframe reference](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/iframe) and [CSP sandbox reference](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy/sandbox).

Before building the renderer, settle how its capability is carried to relative and vault-root asset requests without granting application API authority. A token on the initial document's query string alone does not authorize subsequent relative asset URLs. The plan must cover the listener/origin lifecycle, development proxy and deployed origins, traversal and symlink confinement, ignored/control files, MIME handling, cache policy, CORS for opaque-origin modules/fetches, capability revocation, and `Referrer-Policy`. Keep this as one content-serving boundary with browser tests rather than a collection of frontend URL rewrites.

The bridge accepts only versioned navigation messages from that frame, for its current nonce and canonical note. It must reject sibling-frame, malformed, stale, and replayed messages. Parent-controlled external navigation requires a concrete user action; authored scripts must not turn the bridge into unrestricted popup or navigation authority.

## Recommendations requiring product judgment

| Decision | Recommendation | Consequence |
| --- | --- | --- |
| Active content in the first viewer | Keep the existing isolated active-HTML target. | Interactive reports work; the dedicated origin and browser isolation tests are required before release. |
| Web metadata editing | Include typed root fields through existing property controls and staged Save. | Amend SPEC-0087 narrowly; keep HTML body and Source read only. Decide whether initially absent metadata can be inserted automatically after preview. |
| Local asset access | Start with vault-contained, eligible static files under the existing spec's exclusions; make that scope explicit. | Supports self-contained report folders. If that scope is too broad, narrow it before designing the capability. |
| Inactive scripted tabs | Preserve iframe state on tab switches, as other retained tabs preserve state; revoke viewer access when a tab closes or loses note ownership. | Scripts can continue running in inactive tabs. `inert` affects interaction, not script execution; suspension would need a separate lifecycle design and may reset report state. |
| Default reading experience | Render the authored document in Read, offer inert Source, show metadata/relations/problems in the current right rail. | No separate HTML workspace or always-visible warning badge. Consider standalone viewing as a follow-on control using the same safe renderer. |
| Supplemental script/data search | Keep script/data evidence lower priority, clearly labeled, and bounded; exclude canonical metadata from duplicate body evidence. | Preserves useful embedded report data while reducing search noise. Validate ranking with actual documents before choosing a fixed weight. |
| Moving HTML reports | Avoid promising arbitrary cross-directory moves until local assets and `<base>` behavior are accounted for. | SPEC-0087 excludes asset-URL rewrites, so a move can preserve note links yet break a report's scripts/images. Prefer a clear blocked result to a partially working move. |

## Proposed implementation sequence

These are future delivery slices, not claims that their contracts are implemented or approved by this review.

1. **Provider and canonical read foundation.** Freeze the relevant parts of SPEC-0085/0086 and SPEC-0076 into a fresh effort. Define the raw/derived/viewer contract, root-only projection, diagnostics, link semantics, and actual search-region consumer. Reuse the existing provider runtime, ownership transitions, note metadata store, query keys, and publication barriers. Review these interfaces before downstream work depends on them.
2. **One end-to-end HTML read slice.** Implement tolerant static extraction and wire it through full/live indexing, lexical and semantic retrieval, root typing, GraphQL, graph/backlinks, CLI, and agent output. Missing or malformed metadata leaves an untyped root with a diagnostic; source text remains available. Keep structural parsing and HTML code identity disabled.
3. **Isolated viewer in retained tabs.** Add the bounded content server and navigation bridge, then connect an HTML reading component to the existing tab/header/right-rail contracts. Prove direct links, fragments, external links, restore, source toggles, refresh, and isolation in a real browser. Implement the agreed inactive-tab policy.
4. **Metadata through edit sessions.** Add exact JSON-span operations behind root metadata capability and amend the web maintenance contract. Reuse conflict detection, staged preview, Save/Discard, invalidation, and recovery. Keep body/structural operations rejected at server ingress.
5. **Governed maintenance.** Extend validation, link repair, and moves only after the shared URI resolver and complete source-range transaction model exist. Decide report-asset move behavior explicitly. Do not make all Markdown maintenance refactoring a prerequisite for a read-only HTML release.

## Acceptance and verification matrix

| Boundary | Required observable proof |
| --- | --- |
| Admission | Explicit includes own `.html`/`.htm` exactly once; exclusion and config removal revoke note and viewer access; code ownership remains exclusive. |
| Static extraction | Title precedence, malformed HTML, metadata errors, hidden content, tables/alt text, script/data limits, stable source ranges; no JavaScript execution or network work. |
| Retrieval | The same fixture appears in lexical search, semantic evidence, typed/untyped GraphQL, CLI and agent reads, graph and backlinks with one identity and disclosed representation. |
| Root ontology | Metadata supplies a typed root; DOM headings/IDs produce no structural nodes; damaged metadata retains readable untyped content and visible diagnostics. |
| Lifecycle | Full/live parity for create, modify, rename, delete, ignore/config changes, provider-version changes, fatal recovery, and interrupted publication. |
| Tabs | HTML and Markdown paths remain distinct; open/reuse, Cmd/Ctrl, fragments, back/forward, reload, close, and per-tab mode/scroll behave consistently. |
| Viewer | Scripts and selected assets work; attempts to read app DOM/storage/APIs, escape paths, forge bridge messages, or redirect the top document fail; standalone rendering keeps isolation. |
| Metadata writes | Preview and Save alter only canonical metadata bytes; invalid/duplicate JSON, overlapping edits, and source drift reject safely; every HTML body-edit endpoint rejects; reload preserves metadata changes. |
| Moves | Note references and applicable asset semantics stay intact, or the operation blocks before changing files. |

Verification for this review and its immediate fixes is recorded below. The existing ownership fixtures prove the shipped foundation; they intentionally assert absent HTML projections and cannot be cited as full HTML acceptance coverage.

## Verification of this change

- `make check`: passed, including Go race and integration tests, vet, formatting, benchmark contracts, generated API checks, web type checks, and 497 web tests.
- `make web-e2e`: all 30 browser workflows passed against the rebuilt application.
- `make build`: passed with the current web bundle embedded in the binary.
- `./scripts/rzm validate`: zero issues, including ontology, 471 identifier nodes, and broken links.
- `./scripts/rzm validate frozen-scope-drift`: zero issues.
- Focused regression tests failed before the fixes for HTML route identity, unsupported mutation writes, raw/tree traversal, descriptor-only startup, and live semantic publication. They pass with the fixes. File-read tests also cover rendered notes, external and internal symlinks, ignored targets, and empty vault roots.
- Independent diff review covered path normalization, file containment, mutation callsites, and report accuracy. Post-publication Codex review identified that tree entries also needed individual symlink resolution: target checks on only the parent directory still exposed ignored aliases. A regression reproduced the gap; tree entries and their child indicators now share target containment, complete target ignore checks, and resolved directory classification.
- An earlier `make check` ran concurrently with the browser build and failed because that build temporarily replaced embedded web assets. Running the complete gates serially passed; no assertion was weakened to accommodate the collision.

These checks verify the existing application and this corrective change. They do not verify the unimplemented HTML extractor, retrieval integration, isolated viewer, or metadata writer. Those need the acceptance evidence above when delivered.
