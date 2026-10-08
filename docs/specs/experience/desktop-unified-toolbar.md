---
type: ExperienceSpec
id: SPEC-0119
summary: "In Rhizome Desktop, the native toolbar carries the workspace's section tabs, project search, issue badge, and shortcuts, and the web UI hides its own header, so a window has one top bar."
spec-status: proposed
last-updated: 2026-10-07
aliases:
  - SPEC-0119
---

# Desktop unified toolbar

## Summary

A [[rhizome-desktop|Rhizome Desktop]] window today stacks two top bars. The native toolbar holds the sidebar toggle, back, forward, reload, the worktree selector, and Settings. Directly below it, the web UI's own header repeats the vault name and path and adds the section tabs (Notes, Ontology, Explorer, Agent, GraphQL), project search, the typed and type counts, the validation issue badge, and the keyboard shortcuts button.

This spec merges them into the native toolbar. The toolbar gains the section tabs, project search, the issue badge, and the shortcuts button. The web UI, when it runs inside the desktop app, hides its header. The vault name and path are already in the worktree selector, and the typed and type counts are dropped from the desktop toolbar. The web UI in a browser keeps its header unchanged.

The two webviews stay separated as [[rhizome-desktop|SPEC-0113]] requires: the content webview gains no native command permissions. The page reports its state through its document title, which the native layer reads, checks, and passes to the shell. The native layer sends commands to the page as fixed DOM events, as Close Tab already does.

## Goals

- One top bar in a desktop window that shows a workspace page.
- The toolbar's section tabs, search, issue badge, and shortcuts behave like the web header's and always reflect the page's current state.
- No new native permissions for runtime content.
- An older runtime that does not support this still shows a working window, with its own header and no duplicate tabs.

## Non-Goals

- Moving the Notes tab strip, rails, or any other page chrome into native UI.
- Showing search results in native UI. Results render in the page, as today.
- Relocating the typed and type counts. The desktop toolbar drops them; the browser header keeps them.
- Changing the web header in a browser, including Open in Browser from the desktop app.
- Windows and Linux desktop builds.

## User Stories

### US1 - Use one top bar in the desktop app

- id:: ^SPEC-0119-US1
- summary:: A person using a workspace in Rhizome Desktop switches sections, searches, checks validation, and finds shortcuts from the native toolbar, with no second header below it.
- status:: draft

#### Acceptance Criteria

- While the content area shows a workspace page from a runtime that supports this spec, the window shows one top bar, the native toolbar, and the page draws no header of its own.
- After the worktree selector, the toolbar shows the section tabs Notes, Ontology, Explorer, Agent, and GraphQL, with the page's current section marked active, then a search field, the issue badge while the section is Notes, and a keyboard shortcuts button. Settings stays last. Neither the vault name and path block nor the typed and type counts appear.
- Choosing a tab switches the page's section without reloading it, so open note tabs and unsaved page state survive. The active tab follows every section change, including links inside the page and back and forward.
- Submitting the search field runs the same project search as the web header, and the field shows the current page's search text. ⌘K focuses the field from anywhere in the window.
- The issue badge shows the same count and health as the web header and opens the validation issues view.
- The shortcuts button opens the page's keyboard shortcuts dialog; the `?` key inside the page still opens it.
- The page controls are hidden while the shell draws over the content area (Settings, setup, What gets indexed, and open status screens) and while the content view shows anything other than a workspace page that has reported its state.
- At the minimum window width, every toolbar control stays reachable without overlapping another; the worktree selector and search field shrink first.
- A runtime that does not support this spec keeps its own header, and the toolbar shows no page controls.
  verification:: Open a worktree whose pinned runtime predates this spec.

## Requirements

- The content webview MUST NOT gain native command permissions. The page MUST report state only through its document title, and the native layer MUST send it commands only as DOM events with fixed names whose details it serializes.
- The native layer MUST accept a report only while the content view's top-level page is on the verified runtime origin. It MUST check the report (known section names, bounded search text, a non-negative issue count, known health values) and ignore anything else. A page load, a navigation away from the runtime origin, or clearing the content view MUST clear the shell's page state.
- The native layer MUST send page commands only while the content view shows a workspace page on the verified runtime origin.
- The web UI MUST hide its header and report state only when the desktop app marks the content view. In a browser, it MUST behave as it does today.
- Shells MUST continue to receive no runtime addresses or tokens.
- Toolbar commands MUST reuse the web UI's own navigation and search code paths, so the two surfaces cannot diverge in behavior.

## Open questions

None. Drew settled the shape on 2026-10-07: the shell owns the bar, the typed and type counts are dropped, and the work runs as an effort.

## Documentation plan

- `desktop/README.md`: the toolbar's page controls, the title report, the page events, and the ⌘K menu item.
- `web/CONTEXT.md`: `AppShell`'s desktop mode.
- [[rhizome-desktop|SPEC-0113]] already requires a toolbar with the worktree selector and a separate content webview without permissions; this spec adds to both without changing them, so SPEC-0113 needs no edit.
