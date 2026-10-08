---
type: EffortNote
id: EFF-2026-10-07-17-15
aliases: [EFF-2026-10-07-17-15]
name: Desktop unified toolbar
created-at: 2026-10-07T21:15:16Z
status: active
summary: "Move the web UI's section tabs, project search, issue badge, and shortcuts into Rhizome Desktop's native toolbar and hide the web header inside the app, using a title-based state report and fixed page events."
governing-specs:
  - "[[desktop-unified-toolbar]]"
---

# Desktop unified toolbar

## Scope

Deliver [[desktop-unified-toolbar|SPEC-0119]]: one top bar in a desktop window, with the page controls in the native toolbar, the web header hidden only inside the app, and the page-to-app state report and app-to-page events they depend on. Open a pull request after integration and verification.

Excluded: the non-goals SPEC-0119 lists. No release is published. [[rhizome-desktop|SPEC-0113]] constrains the work (the content webview keeps no native permissions; shells receive no runtime addresses) and is not edited.

## Spec Set (Frozen)

- [[desktop-unified-toolbar|SPEC-0119]], all requirements in the 2026-10-07 revision.

## Stories In Scope (Frozen)

- [[desktop-unified-toolbar#US1 - Use one top bar in the desktop app|SPEC-0119 US1]]

## Spec Coverage Checklist

- [ ] Web desktop mode: header hidden, state report in the title, page events handled through the existing navigation and search code. Browser unchanged.
- [ ] Native relay: desktop marker, title report checked against the verified origin and relayed to the shell, page commands as fixed events, state cleared on loads and navigation, ⌘K menu item.
- [ ] Shell toolbar: tabs, search, issue badge, shortcuts; hidden while covered or unreported; usable at the minimum width.
- [ ] Older runtime keeps its own header with no page controls in the toolbar.
- [ ] Tests in each layer; gates; native verification; documentation; independent review; pull request.

## Plan

### Current gap

`AppShell` (`web/src/components/AppShell.tsx`) always renders its header: brand and vault path, the section tabs (`navigateTo`), `GlobalSearch` (its own ⌘K handler and a submit that pushes `?search=`), `NotesNavTools` (counts and `ValidationIssueBadge`), and `KeyboardShortcuts` (a header button plus a `?` key handler and the dialog). The desktop shell toolbar (`desktop/src/App.tsx`) knows nothing about the page. The native layer can already send the page a fixed event (`rhizome:close-tab` in `desktop/src-tauri/src/menu.rs`), but nothing travels from the page to the app, by design. The content webview is built in `desktop/src-tauri/src/windows.rs`; Tauri 2.12's `WebviewBuilder::on_document_title_changed` and `initialization_script` are available there.

### Decisions

Settled with Drew on 2026-10-07: the shell owns the bar, the typed and type counts are dropped from it, and the work runs as this effort. The following are routine choices recorded here so later batches share one contract:

1. **Marker.** An initialization script on the content webview sets `window.__RHIZOME_DESKTOP__ = true`. The web UI checks it once at startup.
2. **Report.** In desktop mode the page sets `document.title` to `rhizome-desktop:` followed by JSON `{"v":1,"section":…,"search":…,"issues":…,"health":…}`, where `issues` and `health` are present only on Notes once known. The native layer accepts a title only while the content view's URL is on the verified runtime origin, decodes it into a typed struct (known sections and health values, search at most 500 characters, issues a non-negative integer), and sends `{"type":"page","state":…}` to the shell. Any other title, and every page load start, sends `state: null`.
3. **Commands.** The shell calls `request("page", …)` with one of `section`, `search` (with text), `issues`, or `shortcuts`. Rust decodes it into an enum, checks that the pane is visible and its URL is on the runtime origin, and evaluates `window.dispatchEvent(new CustomEvent('rhizome:desktop', {detail}))` with `detail` serialized by `serde_json`. The web UI routes each command through the same functions its header uses today.
4. **⌘K.** A native menu item, Edit → Search This Project (⌘K), focuses the shell webview and tells the shell to focus its search field. In desktop mode, the web UI does not register its own ⌘K handler.

### Batches

| Batch | Outcome | Work | Exit evidence |
| --- | --- | --- | --- |
| 1. Web desktop mode | Inside the app, the page hides its header, reports its state, and obeys page events; a browser sees no change. | `AppShell.tsx`: read the marker; factor the search submit out of `GlobalSearch` and the section switch so the header and the event handler share them; skip the header, the vault title, and the page ⌘K handler in desktop mode; add a small reporter that writes the title from route, search text, and the validation query on Notes. `KeyboardShortcuts.tsx`: render without its trigger button and open on the `shortcuts` event. Update `web/CONTEXT.md`. | `AppShell.test.tsx` cases for desktop mode (no header, title report, each event) and browser mode (unchanged). `npm test`, lint, and typecheck in `web/`. |
| 2. Native relay | The app reads checked page state and sends page commands, with no new content permissions. | `windows.rs`: marker script, `on_document_title_changed` handler, clear on page load start. A small `page.rs` with the report decoder and command enum (pure, unit tested). `commands.rs`: the `page` request. `menu.rs`: the ⌘K item and `focus-search` shell command. | `cargo test` for the decoder (valid report, wrong origin, unknown section, oversized search, negative count, non-report title) and command serialization. `npm run test:native`. |
| 3. Shell toolbar | The toolbar shows and drives the page controls as SPEC-0119 US1 requires. | New `desktop/src/PageControls.tsx` (tabs, search field, issue badge, shortcuts button); `App.tsx` holds page state from `page` messages, clears it on covered or non-ready states, and handles `focus-search`; `api.ts` types; `style.css` for the dense toolbar and narrow widths. Load `impeccable` for the layout pass. | `App.test.tsx` cases: controls appear only after a report, the active tab follows reports, clicks and submit send the right `page` requests, hidden while covered. Browser pass of the shell at 760, 1280, and 1440 widths with screenshots. |
| 4. Integration and verification | The packaged app shows one top bar against a current runtime and the old two-bar layout against an older one. | `make check`, `make desktop-check`, `make desktop-build`. Native run with `RHIZOME_DESKTOP_DATA_DIR` set to a temporary directory against the test fixture vault on this build, and against a worktree pinned to an older runtime. Update `desktop/README.md`. Independent review, accepted fixes, `./scripts/rzm validate`, and the frozen-scope-drift check. Open and link a pull request. | Gate results, native evidence (screenshots where the session allows, otherwise a recorded gap for Drew's click-through), the review outcome, and the pull request link. |

Batch 1 and Batch 2 share only the report and command shapes in the decisions above, so they can be built in either order; Batch 3 needs Batch 2's message types.

Escalation: Drew decides any change to what the toolbar shows, or any new native permission for runtime content. Fitting the toolbar at narrow widths and matching badge styling are routine.

## Plan Approval

Drew approved the plan in chat on 2026-10-07 (2026-10-07T21:24:07Z): "Approved", in reply to the request to approve the four-batch plan. The current-user configuration is absent, so `plan-approved-by` is omitted rather than inferred.

## Original Intended Delivery

A Rhizome Desktop window that shows a workspace page has one top bar. The native toolbar carries the section tabs, project search, issue badge, and shortcuts button, and they act on and follow the page. The web UI in a browser is unchanged.

## Actual Delivered

Not started.

## Execution Notes

- 2026-10-07T21:15:16Z: Effort opened after Drew chose the shell-owned bar, dropped the typed and type counts, and asked for an effort. Recon: the web header lives in `AppShell.tsx`; the only existing app-to-page message is `rhizome:close-tab`; Tauri 2.12 exposes `on_document_title_changed` on child webviews.
- 2026-10-07: Batch 1 done. `web/src/components/desktopHost.ts` holds the marker check, the title format, and a `rhizome:desktop` command parser built on `api/parse` guards (the anti-slop lint rejects `Reflect.get` and bare type assertions). `AppShell` factors `runProjectSearch` and `openIssues` out of the header, renders `DesktopReporter` and a trigger-less `KeyboardShortcuts` instead of the header in desktop mode, and skips the vault title there. `web/CONTEXT.md` updated. Evidence: AppShell and KeyboardShortcuts tests (14) passed, including the unchanged browser cases; typecheck, oxlint, and oxfmt clean.
- 2026-10-07: Batch 2 done. `desktop/src-tauri/src/page.rs` holds the marker, the report decoder, and the command enum; `commands.rs` adds `Action::Page`; `windows.rs` adds the marker script, the title handler, and `page_command`; `menu.rs` adds Edit → Search This Project (⌘K). Serde's `deny_unknown_fields` does not apply to unit variants of an internally tagged enum, so `{"command":"issues","extra":1}` decodes; the extra field is ignored, which is harmless. Evidence: `cargo test --locked` (56 passed) and clippy clean.
- 2026-10-07: Batch 3 done. `desktop/src/PageControls.tsx` (`PageSections` left of the drag spacer, `PageTools` right of it), page state in `App.tsx`, shown only while the shell does not cover the content and the open step is ready. A browser pass with a temporary harness that fakes the native layer, composited with the real web UI in desktop mode served by `rzm serve` on the fixture vault, at 760, 1280, and 1440 widths. It found the tabs overlapping the worktree selector and the Settings button clipped at 760; fixed by truncating the selector, keeping icon buttons from shrinking, hiding the branch and ⌘K hint below 960 pixels, and a 72-pixel search minimum. Evidence: `npm run check` in `desktop/` (39 tests) passed.
- 2026-10-07: While integrating, the title handler's call to `webview.url()` was replaced: on the main thread that call runs inline against the runtime's window table, which a title event fired during another webview operation could find borrowed. The pane now records each page load's URL (`Pane::document`), and the title handler and page commands check that against the verified origin.
- 2026-10-07: Gates passed before review fixes: `make desktop-check` and `make check` (web 1509 tests), then `make desktop-build`. Native run of the packaged app with `RHIZOME_DESKTOP_DATA_DIR` in a temporary directory, the global executable set to this build, and a copy of the fixture vault added through `rzm desktop`. This session could capture the window (`screencapture -l`) and post mouse events after activating the app. Captured: one top bar with the web header gone and Notes active from the page's report; clicking Ontology switched the page to the Ontology Atlas and moved the active tab. Keystrokes could not be delivered reliably because the app would not stay frontmost, so ⌘K and typed search rest on unit tests and Drew's click-through.
- 2026-10-07: Independent review (Claude Fable advisor): "ship after small fixes", no correctness or security defect. Applied: a pane-level Rust test for `loading`, `titled`, and `on_runtime`; `.busy` truncates instead of crowding the search field; web tests for the report following history and for `?` opening the dialog without a page button. The reviewer's back-forward-cache concern (a restored page may not emit a title change) stays a click-through item.
- 2026-10-07: Drew asked to center the section tabs. The toolbar is now a three-column grid (`minmax(min-content, 1fr) auto minmax(min-content, 1fr)`), with the traffic-light inset moved into the start group so the middle column is centered in the window (measured exact at 1280 and 1440). The worktree selector uses fixed width caps per breakpoint, since a percentage did not limit the width the grid reserved for its name; at 760 the tabs shift right of center and every control fits. Drew also asked whether the toolbar can use native controls; that is pending his decision.

## Deviations

None.

## Closure Checklist

- [ ] Required quality gates pass.
- [ ] Alignment and actual outcomes are verified.
- [ ] Specs and documentation are reconciled.
- [ ] Follow-ups are triaged.

## Compounding Follow-ups

None yet.

## Status

Active. Batches 1 to 3 implemented; Batch 4 gates, native verification, and independent review in progress.
