---
type: ExperienceSpec
id: SPEC-0127
aliases: [SPEC-0127, Workbench sidebar]
summary: "One persistent, user-toggled sidebar across Notes, Ontology, Explorer, and Agent: the same themed frame with section-specific contents, one remembered collapsed state and width, a toggle in the browser top bar and in Rhizome Desktop's toolbar, and ⌃⌘S or ⌘\\ to toggle it."
spec-status: active
last-updated: 2026-10-26
---

# Workbench sidebar

## Summary

After [[workbench-redesign|SPEC-0126]] the Notes sidebar reads well, but it vanishes whenever a search tab is active, its toggle sits in the document tab strip, and the other sections each draw their own left column: Explorer a fixed 260px tree on the surface colour, Agent a session rail on `--surface-alt` that only collapses below 980px, and Ontology nothing at all. On 2026-10-26 Drew asked for one sidebar that behaves the same everywhere: it stays where the user left it, a single toggle shows or hides it, and each section only swaps what is inside it.

This spec governs the left sidebar of the workbench in the browser and in Rhizome Desktop. It amends [[notes-workspace-shell|SPEC-0090]] and [[search-workspace|SPEC-0098]] (search tabs no longer drop the sidebar), [[workbench-redesign|SPEC-0126]] (US5's sidebar toggle and persistence), and [[desktop-unified-toolbar|SPEC-0119]] and [[rhizome-desktop|SPEC-0113]] (a sidebar toggle returns to the desktop toolbar, this time for the workbench sidebar rather than a repository list).

## Goals

- The sidebar is shown or hidden by the user alone. Opening a search, switching sections, or reloading never changes it.
- Notes, Ontology, Explorer, and Agent share one sidebar frame: the same chrome colour, header, width, resize handle, and collapse behaviour. Only the contents differ.
- One toggle, in the same place in every section, controls it: in the browser top bar beside the section switcher, and in Rhizome Desktop's toolbar.

## Non-Goals

- A sidebar for GraphQL. GraphiQL keeps its own docs and history panel.
- Per-section collapsed state or per-section widths.
- New narrow-window behaviour for the sidebar. The existing cap of 45% of the window stays.
- Changes to the right pane (SPEC-0126 US6) and its `]` shortcut.
- Changing what the Notes sidebar holds (SPEC-0126 US8).

## User Stories

### US1 - Keep the sidebar where I put it

- id:: ^SPEC-0127-US1
- summary:: Hide or show the sidebar once and have it stay that way across searches, sections, reloads, and windows.
- status:: ready

#### Acceptance Criteria

- Opening a project search keeps the sidebar visible if it was visible. The search results fill the content panel beside it.
- Switching among Notes, Ontology, Explorer, and Agent keeps the sidebar's collapsed state and width.
- The collapsed state and width survive a reload. In the browser they are remembered per origin and follow changes made in another tab of the same origin. In Rhizome Desktop they apply to every window and repository and survive restarts.
- In GraphQL the content fills the window. Returning to another section restores the sidebar as it was.

### US2 - Toggle the sidebar from one place

- id:: ^SPEC-0127-US2
- summary:: Show or hide the sidebar with one control that sits in the same place in every section, or with a shortcut.
- status:: ready

#### Acceptance Criteria

- In the browser, a sidebar toggle sits in the top bar immediately before the section switcher. It uses the panel-left icon, is labelled "Hide sidebar" or "Show sidebar", and reports its state with `aria-expanded`. The document tab strip no longer has a sidebar toggle.
- In Rhizome Desktop the page draws no toggle of its own. The toolbar's first control after the window buttons is the sidebar toggle, drawn like the other toolbar icon buttons and pressed while the sidebar shows. The View menu has Show Sidebar or Hide Sidebar with ⌃⌘S.
- ⌃⌘S and ⌘\\ toggle the sidebar in the browser and in Rhizome Desktop (Ctrl+Alt+S and Ctrl+\\ off macOS), including while focus is in the content page, but not while typing in a text field or editor.
- In GraphQL the toggle is disabled and its tooltip says the section has no sidebar.

### US3 - Use the same sidebar in every section

- id:: ^SPEC-0127-US3
- summary:: See the same sidebar frame in Notes, Ontology, Explorer, and Agent, holding what each section needs.
- status:: ready

#### Acceptance Criteria

- Every section's sidebar sits on the theme's chrome colour beside the inset content panel, with the same header row naming the vault or repository, the same width range (200–400px, 264px default), and the same drag handle.
- Notes holds Views, the scopes, and the type tree, as today.
- Ontology holds an Atlas row followed by the display groups with their types and interfaces, like the Notes type tree. Choosing a type or interface opens its Ontology detail, and the current one is selected.
- Explorer holds the vault tree, with the current folder or note selected.
- Agent holds the session list. Its narrow-window drawer and hamburger button are replaced by the shared toggle.
- Selected rows, hover, focus, and counts look the same in every section.

## Requirements

### Shell and state

- MUST keep one collapsed flag and one width for the whole workbench. A user's existing Notes sidebar width and collapsed state carry over on first load.
- MUST keep the sidebar mounted per section only while that section is shown; collapsing it MUST NOT discard section state such as expanded groups or the Explorer tree's expansion.
- MUST keep the sidebar hidden and inert to assistive technology while collapsed.

### Desktop

- MUST keep SPEC-0119's protocol: the shell sends the page a validated, fixed-name command carrying the collapsed state, and the page reports its state only through its document title. The page gains no new capability.
- MUST store the collapsed state in Rhizome Desktop's app-wide preferences and apply it to a page when it loads, so every window and repository agrees.
- MUST disable the toolbar toggle while the page shows GraphQL or the page is not a verified runtime page.

### Accessibility and verification

- The toggle MUST be reachable by keyboard in the browser top bar and the desktop toolbar, and its focus indicator MUST meet AA on every chrome theme in both appearances.
- Unit tests MUST cover the shared state (persistence, carry-over from the old key, cross-tab updates, desktop commands) and the shortcuts' text-field exclusion. E2E tests MUST cover the sidebar staying visible on a search tab, keeping its state across sections and a reload, and the GraphQL exception. Desktop tests MUST cover the new command, report field, preference, and menu item.

### Documentation

- MUST amend SPEC-0090, SPEC-0098, SPEC-0113, SPEC-0119, and SPEC-0126 where they omit the sidebar from search tabs, place its toggle in the tab strip, or describe the desktop toolbar without it.
- MUST update `DESIGN.md` (shell, top bar, sidebar, desktop toolbar), `web/CONTEXT.md`, `desktop/README.md` (menus), and `CHANGELOG.md`.

## Open Questions

None. Drew settled the open choices on 2026-10-26: one shared collapsed state, an Ontology type tree, no GraphQL sidebar, and both ⌃⌘S and ⌘\\.
