---
type: ExperienceSpec
summary: "Defines the trusted HTML note reading experience in the web workspace: authored HTML, JavaScript, and external HTTPS resources render in an app-isolated iframe while Rhizome-owned navigation opens internal targets in retained note tabs."
id: SPEC-0088
spec-status: active
last-updated: 2026-09-14
aliases:
  - SPEC-0088
  - trusted-html-note-viewer
---

# Trusted HTML note viewer

## Summary

HTML notes are trusted repository content and should appear in the web workspace as the authored documents, including their JavaScript and external HTTPS resources. They are not converted into Markdown or flattened into an extracted-text preview. Trusting the content does not require giving it Rhizome application authority, however. Each HTML note runs in an opaque-origin iframe backed by a dedicated, capability-scoped content origin that can render active content while remaining unable to reach Rhizome cookies, storage, DOM, or authenticated APIs.

Navigation and generated exports are the deliberate seams between the document and the application. Rhizome injects a small, versioned bridge before authored scripts run. The bridge captures ordinary anchor activation and offers a narrow `window.rhizome` API. It sends typed, nonce-scoped messages to the parent, where Rhizome validates the sender, resolves internal targets through the shared note-link infrastructure, and presents bounded generated files for an explicit user download action. Cross-note navigation opens or focuses a retained note tab through [[notes-workspace-shell|SPEC-0090]]. Same-document fragments scroll within the current frame and update the active tab deep link. Opening another note retains the source tab; Cmd/Ctrl activation opens a new target beside it or reuses an existing tab in place, without changing the active tab.

The viewer is a reading surface, not a general-purpose HTML editor. The HTML body remains read only in both Read and Source modes, including when the workspace is in edit mode. Root metadata may be staged in the existing property controls and saved through the governed transaction flow. Generated files leave the frame only through the parent-controlled download flow below.

## Goals

- render configured HTML notes as actual HTML, with authored JavaScript and external HTTPS resources enabled
- isolate rendered content from Rhizome application authority even though vault content is trusted
- make ordinary internal HTML links behave like note navigation elsewhere in the web workspace
- preserve normal in-document fragment navigation without creating duplicate tabs
- provide a deliberately narrow bridge for script-driven navigation
- let prototypes export bounded generated files after an explicit parent-side user action
- support the same isolated viewer from local and remotely accessed Rhizome deployments
- keep viewing, source inspection, errors, and reload behavior understandable within the existing tab model
- preserve accessible browser and keyboard behavior for reading and navigation

## Non-Goals

- providing a general-purpose visual or source editor for HTML
- previewing HTML files that are not configured as notes
- executing HTML or JavaScript during indexing, search, validation, or ontology projection
- indexing a runtime DOM or content fetched by the rendered document
- granting iframe content access to Rhizome queries, mutations, filesystem operations, credentials, cookies, storage, or application DOM
- translating arbitrary authored navigation such as direct `window.location` assignment into Rhizome tab navigation; scripts use the bridge when Rhizome-aware navigation is required, and mismatched frame navigation is contained as described below
- allowing forms, popups, native iframe downloads, or top-level navigation directly from iframe sandbox capabilities; generated exports use the bounded parent bridge
- reloading or reindexing a note merely because one of its local or external assets changed
- displaying an active-content warning badge for configured HTML notes

## Experience Model

### Pane anatomy

An HTML note occupies its own retained file tab in `NotesShell`, reusing the note header, Read/Source controls, and right rail. Its canonical identity is the authored vault-relative path, including its explicit `.html` or `.htm` extension and case. An HTML fragment is a location inside that tab, not an ontology section or a second tab. The pane header remains outside the iframe and stays visible while the document scrolls inside the frame. It provides the note identity, source-view control, open-original control, and any pane-scoped diagnostic. The document itself owns its layout and vertical scroll position.

### Viewer lifecycle and states

1. **Loading**: The pane identifies the note while its dedicated viewer URL loads. Loading does not expose extracted search text as a substitute document.
2. **Ready**: The iframe displays the authored HTML. Scripts may run and local or external HTTPS resources may load according to browser policy.
3. **Navigating**: Same-document fragments update the current frame. Internal cross-note targets flow through Rhizome and open or focus a retained note tab. External targets require an intentional parent-routed action.
4. **Document error**: Failure to load or run the document appears as a pane-scoped diagnostic with access to source view. The rest of the workspace remains usable.
5. **Resource error**: A failed script, image, stylesheet, or external request is a viewer concern. It does not make the indexed note unavailable or stale.
6. **Updated**: A change to the HTML source reloads the affected viewer while preserving the open tab set when the note identity still resolves. Asset-only changes do not trigger reload.

## User Stories

### US1 - Read an HTML note as the authored interactive document

- id:: ^SPEC-0088-US1
- summary:: Read a configured HTML note in the web workspace with its authored layout, JavaScript, and external HTTPS resources intact.
- status:: ready

#### Acceptance Criteria

- Opening a configured `.html` or `.htm` note creates or activates its retained note tab and renders the actual document in an iframe rather than a Markdown conversion, extracted-text rendering, or browser download. ^SPEC-0088-US1-AC1
  verification:: Open a fixture note with distinctive HTML layout and confirm the pane matches the authored document rather than its indexed text derivative.
- Authored inline and repository-local JavaScript runs in the viewer. ^SPEC-0088-US1-AC2
  verification:: Open a fixture whose inline and local scripts change visible document state; both changes appear.
- Authored external HTTPS scripts, stylesheets, images, fonts, and fetches are permitted, subject to ordinary browser and remote-server policy. A failed external resource does not prevent the pane from showing the remaining document. ^SPEC-0088-US1-AC3
  verification:: Exercise one successful and one intentionally failing external HTTPS resource; the successful resource loads and the failed resource remains isolated to the document/viewer diagnostic surface.
- The iframe has an opaque origin and cannot read Rhizome cookies, storage, application DOM, or authenticated application APIs. ^SPEC-0088-US1-AC4
  verification:: A hostile fixture attempting each access receives no Rhizome authority while remaining able to execute its own JavaScript.
- The pane header remains fixed outside the iframe while the document scrolls inside the iframe. ^SPEC-0088-US1-AC5
  verification:: Scroll a long HTML note and confirm the document moves while tab identity and controls remain available.
- The viewer does not show a routine active-content warning or badge merely because the configured note contains scripts. ^SPEC-0088-US1-AC6
  verification:: Open scripted and script-free HTML fixtures and confirm neither receives an active-content badge.

---

### US2 - Follow HTML links without losing the Rhizome navigation context

- id:: ^SPEC-0088-US2
- summary:: Follow links from an HTML note using the same retained-tab navigation model as other Rhizome notes.
- status:: ready

#### Acceptance Criteria

- Activating a relative or vault-root internal link sends its authored target to the parent, where shared link resolution determines the destination and creates or activates its retained file tab without closing the source tab. ^SPEC-0088-US2-AC1
  verification:: Follow relative and vault-root links to Markdown and HTML notes and confirm each resolves to the shared canonical note identity and opens in its own tab while the source tab remains available.
- When the resolved destination is already open in a note tab, the workspace activates that tab rather than opening a duplicate. ^SPEC-0088-US2-AC2
  verification:: Follow a link back to an already open note and confirm its existing tab is activated without a duplicate or redundant workspace fetch.
- Cmd/Ctrl activation opens a new destination tab beside the source or reuses an existing destination in place while keeping the source tab active; unrelated tabs and each tab's state are retained. ^SPEC-0088-US2-AC3
  verification:: Open three notes, Cmd/Ctrl-activate a link from the first, and confirm all tabs remain open and the first remains active.
- Activating a same-document fragment scrolls the current iframe to the matching `id` or legacy named anchor without opening a new tab, and updates the tab deep link so the location can be restored or shared. ^SPEC-0088-US2-AC4
  verification:: Follow fragment links in the current document and confirm scroll, URL/deep-link state, back/forward behavior, and tab count.
- Activating an internal link with a fragment opens or activates the destination tab and positions that HTML viewer at the resolved fragment. ^SPEC-0088-US2-AC5
  verification:: Follow a cross-note fragment link and confirm both the correct destination tab and target position.
- External targets are handled by an intentional parent-routed action and do not navigate the iframe or Rhizome application in place. ^SPEC-0088-US2-AC6
  verification:: Activate an external HTTPS anchor and confirm the app applies its external-link behavior without replacing the current note pane or top-level Rhizome page.
- Links are keyboard operable and preserve their authored accessible names and focus indication. ^SPEC-0088-US2-AC7
  verification:: Navigate and activate internal, fragment, and external links using only the keyboard and a screen-reader accessibility tree.

---

### US3 - Let authored scripts request Rhizome-aware navigation through a narrow bridge

- id:: ^SPEC-0088-US3
- summary:: Use a small, stable viewer API when an authored script needs to open a note or external destination through Rhizome.
- status:: ready

#### Acceptance Criteria

- The viewer exposes a versioned `window.rhizome` presentation API limited to `open`, `openExternal`, `currentNote`, and `download`. ^SPEC-0088-US3-AC1
  verification:: A fixture can inspect the supported bridge version, read its current note identity, request internal and external navigation, and request a bounded generated download through the documented calls.
- The bridge is installed before authored scripts execute so a script in the document head can use it deterministically. ^SPEC-0088-US3-AC2
  verification:: An inline head script successfully detects and invokes the bridge during initial parsing.
- Bridge events use a typed, versioned message schema and an unguessable per-viewer nonce; the parent accepts an event only when `event.source` is the pane's iframe window and the schema, version, nonce, and payload are valid. ^SPEC-0088-US3-AC3
  verification:: Valid fixture messages navigate; messages from a sibling frame and messages with an invalid source, type, version, nonce, or payload are ignored.
- Internal bridge requests use the same shared resolver and retained-tab behavior as ordinary anchor activation. ^SPEC-0088-US3-AC4
  verification:: Compare an anchor activation and `window.rhizome.open` call for the same authored target and confirm identical resolution and pane behavior.
- The bridge does not expose search, ontology/query access, note mutation, filesystem access, authentication data, application DOM access, or arbitrary parent messaging. ^SPEC-0088-US3-AC5
  verification:: Inspect the public bridge and exercise a fixture that probes for excluded capabilities; none are available.
- Authored scripts that assign `window.location` directly are not promised Rhizome-aware pane navigation; a resulting top-document load must complete the viewer identity handshake for the pane's canonical note or the parent restores the current note with a contained diagnostic. ^SPEC-0088-US3-AC6
  verification:: Navigate directly to another local HTML document and an external document; neither silently changes the pane's canonical note identity, gains application authority, or escapes the frame.

---

### US4 - Inspect source and recover from viewer failures without losing knowledge access

- id:: ^SPEC-0088-US4
- summary:: Switch to source or a safely isolated standalone view and understand viewer failures without making search or note data unavailable.
- status:: ready

#### Acceptance Criteria

- Every HTML note pane offers a source view that displays the raw HTML without executing it and allows returning to the rendered view without changing tab identity. ^SPEC-0088-US4-AC1
  verification:: Toggle between rendered and source views; confirm the exact source is visible, no source-view scripts run, and the pane remains in the same tab position.
- "Open in browser tab" opens the canonical Rhizome note route in a new browser tab in its chrome-free form (`bare=1`), which renders only the document through a new isolated iframe capability. It reuses the normal navigation and generated-download parent controls, offers a way back to the full Rhizome view, and never serves authored HTML unsandboxed on the Rhizome application origin. ^SPEC-0088-US4-AC2
  verification:: Open the note in a second browser tab, close the originating application tab, exercise navigation and a generated export, and rerun the application-authority isolation fixture. Each application page owns and revokes its viewer capability independently.
- A document load, parse, script, or resource failure is shown at the affected pane and does not remove the note from search, typed query results, backlinks, or other derived knowledge surfaces. ^SPEC-0088-US4-AC3
  verification:: Break a viewer-only resource and confirm the pane explains the failure while indexed discovery and graph/query results remain available.
- A pane diagnostic provides enough context to distinguish failure of the HTML source request from failure of a subordinate local or external resource. ^SPEC-0088-US4-AC4
  verification:: Exercise both failure classes and confirm their pane states are distinguishable without requiring developer tools.
- Source view remains available when the rendered document cannot initialize. ^SPEC-0088-US4-AC5
  verification:: Open an intentionally broken viewer fixture and use the pane control to inspect its raw HTML.

---

### US5 - Refresh an open HTML note only when its source changes

- id:: ^SPEC-0088-US5
- summary:: See an open HTML note refresh when the note changes without turning asset watching into a second dependency graph.
- status:: ready

#### Acceptance Criteria

- Creating or modifying the HTML note source causes its open viewer to load the current source once the normal vault update is accepted. ^SPEC-0088-US5-AC1
  verification:: Modify the open fixture HTML and confirm the pane displays the new source-derived behavior.
- Renaming the HTML note preserves or re-establishes its tab identity according to the shared note move behavior and loads it from the new repository path. ^SPEC-0088-US5-AC2
  verification:: Rename an open fixture through the supported move workflow and confirm the pane remains usable at the new identity.
- Changing only a local script, stylesheet, image, or other referenced asset does not cause Rhizome to reload or reindex the HTML note. ^SPEC-0088-US5-AC3
  verification:: Modify each asset class while the note is open and confirm no note-source update or automatic frame reload occurs.
- Refreshing or reopening the pane may naturally retrieve current assets using normal browser cache semantics. ^SPEC-0088-US5-AC4
  verification:: After an asset-only change, manually refresh or reopen and confirm behavior follows the browser/server cache contract rather than a Rhizome asset watcher.

---

### US6 - Download a file generated by an interactive prototype

- id:: ^SPEC-0088-US6
- summary:: Export a bounded file such as a CSV from an HTML prototype through an explicit Rhizome-controlled download action.
- status:: ready

#### Acceptance Criteria

- A prototype can request a generated download through `window.rhizome.download` and through an intercepted same-frame `<a download>` whose target is frame-created Blob or data content. ^SPEC-0088-US6-AC1
  verification:: Export the same CSV through both entry points and assert the exact filename, media type, and bytes received by the parent.
- A request transfers at most 32 MiB, permits one pending export per viewer, validates the request identity and payload before allocation where possible, and returns a useful rejection for malformed, duplicate, stale, revoked, or oversized requests without truncating the file. ^SPEC-0088-US6-AC2
  verification:: Exercise the size boundary, invalid encoding, repeated request ID, second pending request, canceled request, closed tab, and revoked viewer.
- Receiving a frame message never starts a browser download. The parent shows the sanitized filename and size and requires a real user activation before creating the download. ^SPEC-0088-US6-AC3
  verification:: Request an export from a timer and confirm no save begins until the user activates the parent Download action.
- The bridge does not fetch arbitrary URLs, read repository files, open a preview, execute downloaded content, or add iframe `allow-downloads`, popup, form, same-origin, or top-navigation authority. ^SPEC-0088-US6-AC4
  verification:: Inspect the sandbox and bridge, then attempt URL, executable-preview, forged-message, and direct native-download paths; each remains unavailable.
- Pending buffers and object URLs are released after download, cancellation, replacement, tab close, or capability revocation. ^SPEC-0088-US6-AC5
  verification:: Observe each lifecycle transition and confirm no pending action or reusable object URL survives.

## Requirements

### Rendering and isolation

- HTML notes MUST load from a dedicated content origin, separate from the Rhizome application origin, whose URL path mirrors the repository-relative note path and whose URL root maps to the vault root so relative, vault-root, and `<base>` URL semantics remain meaningful.
- Local and remotely accessed Rhizome deployments MUST provide that dedicated origin. Remote browsers MUST receive a browser-reachable content host and MUST NOT receive a loopback URL. If the configured remote host or TLS boundary is unavailable, the viewer MUST report a configuration error rather than falling back to application-origin HTML.
- Remote content hosting SHOULD use a separate registrable site. A same-site content host is supported only when application credentials are host-only and never forwarded to the content service, state-changing application requests reject the content or opaque origin, and reverse-proxy access and error logs redact capability-bearing hosts and URLs.
- The content origin MUST require an unguessable viewer-scoped capability that grants only read access to eligible static content. That capability is not Rhizome application authentication and MUST expose no application API authority.
- Eligible local content is limited to regular files whose fully resolved paths remain inside the vault root, are not ignored, and are not inside Rhizome, VCS, or other configured control directories. Symlinks, encoded traversal, path normalization, and case behavior MUST NOT permit escape from that scope.
- The content origin MAY serve any eligible local asset requested by the trusted document because runtime JavaScript can construct asset URLs; it MUST NOT serve files outside that bounded vault scope or expose directory listing, mutation, query, search, or metadata APIs.
- Content responses MUST preserve raw bytes, use deterministic MIME handling, define cache/revalidation behavior, and provide only the narrowly required cross-origin headers for opaque-origin classic scripts, module scripts, styles, fonts, media, and fetches. The existing general file-view API MUST NOT be reused without this stricter confinement and response contract.
- The iframe MUST use `sandbox="allow-scripts"` without `allow-same-origin`, `allow-forms`, `allow-popups`, `allow-downloads`, or top-navigation capabilities.
- Viewer responses MUST apply a compatible Content Security Policy sandbox so direct/open-original rendering remains opaque-origin and script-capable without relying on an enclosing iframe attribute; resource directives MUST still permit the authored external HTTPS behavior in this spec.
- Authored JavaScript MUST be allowed for configured HTML notes, including inline and repository-local scripts.
- External HTTPS resources and requests MUST be allowed as authored. Mixed-content HTTP behavior follows the browser and MUST NOT be weakened by Rhizome.
- Forms, popups, downloads, external navigation, and top-level navigation MUST NOT escape directly through iframe sandbox capabilities. Any supported action MUST be intentionally routed and enforced by the parent application.
- The rendered iframe MUST NOT receive Rhizome authentication material or application authority.
- Indexing and ontology availability MUST NOT depend on the viewer, script execution, network access, or successful resource loading.

### Navigation bridge

- Rhizome MUST inject the bridge before authored scripts run while preserving the authored document as the source of displayed content.
- Parent/iframe messages MUST be typed and versioned, scoped by a per-viewer nonce, and accepted only after validating `event.source` and the full payload.
- Ordinary anchor activation and bridge navigation MUST converge on the same shared internal-link resolver and note-tab coordinator.
- The public `window.rhizome` surface MUST remain limited to `open`, `openExternal`, `currentNote`, `download`, and their versioned presentation contract. Download accepts generated bytes and bounded metadata; it does not fetch URLs or read files.
- Same-document fragments MUST remain in the current iframe; cross-note destinations MUST use the Rhizome note-tab coordinator.
- The injected bridge MUST complete a parent-validated startup handshake containing the nonce and canonical note identity. After any top-document load, a missing or mismatched handshake MUST NOT change tab identity; the parent restores the canonical document or shows a contained viewer diagnostic.
- A direct authored navigation mechanism that bypasses anchor interception or the bridge carries no pane-navigation compatibility guarantee and MUST remain contained to the iframe even when the browser permits the frame load.

### Viewer controls and updates

- Read and Source MUST share one canonical retained file tab and preserve its identity, mode, and location across tab switches and reloads.
- HTML body and source MUST remain read only, independent of workspace edit mode. Metadata mutation capability MUST NOT enable narrative, whole-source, section, or DOM edits.
- The API MUST disclose authored format and representation so the client selects the HTML reading view without treating raw HTML as Markdown. Provider search text MUST NOT substitute for the authored document.
- Source view MUST render raw HTML inertly.
- Open-in-browser-tab MUST open the canonical application note route in a second browser tab, in its chrome-free form, and use a separately issued isolated iframe capability. It MUST NOT expose a bare bearer URL as the public control target or create a second tab-management model.
- Viewer and subordinate-resource failures MUST be represented as pane-scoped diagnostics and MUST NOT affect derived knowledge surfaces.
- The HTML note source changing MUST trigger viewer refresh through the normal note-update path.
- Referenced asset changes MUST NOT independently trigger note reload or reindex.
- The viewer MUST NOT add an active-content badge or per-file execution prompt for HTML already admitted by note include configuration.

### Accessibility

- Pane controls MUST be keyboard reachable, have accessible names, and expose loading, ready, and error states without relying only on color.
- Focus MUST move predictably when link activation opens or activates a tab, consistent with SPEC-0090. Cmd/Ctrl activation retains focus in the source tab.
- Rhizome MUST NOT suppress accessible semantics authored inside the frame; the iframe itself MUST have an accessible title derived from the note title.
- Source/rendered toggles MUST expose selected state and retain a logical focus target when switching modes.

## Related Specs

- [[notes-workspace-shell]] (`SPEC-0090`) owns retained file tabs, modifier activation, per-tab state, deep links, and reload restoration. It supersedes the earlier cross-note pane-stack behavior in [[ontology-browser-navigation-model]] (`SPEC-0030`).
- [[note-node-indexing-architecture]] (`SPEC-0076`) defines the format-neutral note source and derived read-model boundary; viewer execution never becomes an indexing input.
- [[multi-format-html-notes]] (`SPEC-0083`) defines when an HTML file is admitted as a trusted note and the user-visible parity expected across surfaces.
- [[note-format-provider-registry]] (`SPEC-0084`) defines HTML note ownership and the capabilities consumed by the viewer.
- [[html-note-format-provider]] (`SPEC-0085`) defines HTML root metadata, extraction, and authored-link contracts.
- [[format-aware-root-ontology-projection]] (`SPEC-0086`) keeps runtime rendering outside the ontology projection boundary.
- [[format-aware-note-maintenance-mutations]] (`SPEC-0087`) defines the source-preserving maintenance operations available alongside this read-only experience.

## Documentation Plan

- Add an HTML note viewing guide covering rendered/source modes, allowed active content, external resources, navigation behavior, refresh behavior, and recovery from viewer failures.
- Document the versioned `window.rhizome` API with minimal examples for `open`, `openExternal`, `currentNote`, and `download`, plus the parent download action and an explicit list of capabilities it does not provide.
- Document the trust model: note include configuration admits HTML for active viewing, while opaque-origin isolation protects Rhizome application authority.
- Document the content-origin asset scope, denied control paths, viewer capability, vault-root URL behavior, MIME/cache rules, and why it is not a general repository file server.
- Add troubleshooting guidance for blocked mixed content, remote CORS/CSP failures, direct `window.location` behavior, missing repository-local resources, and the asset-change manual-refresh expectation.
- Update web workspace documentation to describe how HTML links reuse retained-tab navigation and how same-document fragments affect deep links.
- Keep indexing/search documentation explicit that runtime DOM and fetched resources are not indexed.

## Open Questions

None. The approved delivery retains authored JavaScript and external HTTPS resources, includes bounded root-metadata Save, serves eligible vault-contained assets, supports local and remote access, opens the canonical application note route in another browser tab, and includes parent-controlled generated downloads.
