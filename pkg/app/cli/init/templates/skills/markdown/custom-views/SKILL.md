---
name: custom-views
description: Build or fix Rhizome HTML/TSX views for type collections, display groups, individual nodes, or standalone tools. For declarative native layouts and defaults, use the Rhizome views reference.
---

# Custom views

A custom view is a definition and an HTML or script entry in `.rhizome/views/<folder>/`. `rzm` serves it with the kit and per-file TSX transform; no install, bundler, or second server is needed. Overview, Table, Cards, Kanban, and custom entries share registration, context, configuration, lifecycle, navigation, and workspace edit services. The host handles React versus frame rendering. Rhizome's own Briefing, Trace, and Sections group pages and its type Briefing are custom views built on the same public kit and APIs; see [Start from a bundled view](#start-from-a-bundled-view).

The code runs with the user's full Rhizome authority, like a build script in the repository. Write it accordingly.

## Loop

1. **Find the target and data.** Load `rhizome` and take its "Configure view mounts, defaults, or native layouts" route for `rzm agent view list`, actual targets, existing choices/defaults, and mount rules. The GraphQL schema differs per vault: discover the relevant type and query arguments rather than guessing them. A display group organizes navigation; its dashboard must query its chosen types explicitly.
2. **Prove the query** before writing UI: `rzm agent ontology-query --query '{ actionItem(first: 3) { title done } }'`. Errors name the exact field or enum value.
3. **Write** the definition and entry (see [Files](#files)). Read `references/kit-api.md` for context, native collection execution, schema and membership hooks, components, navigation, freshness, and editing. For a group dashboard linking to node presentations, read `references/mounted-example.md`.
4. **Check** with `rzm validate views`. It reports definition problems and every transform error in the folder as `file:line:column: message`, plus relative imports that resolve to no file, bare imports outside the import map, and GraphQL documents that fail against the vault's schema. Only a document written inline as the first argument of `graphql` or `useGraphQL`, without `${}` interpolation, is checked; a query held in a constant is not.
5. **Run it.** `rzm serve --port 8787` if no server is up. A standalone view opens at `http://127.0.0.1:8787/views/<id>`; for a mounted page's direct launch, use `customViewHref(id, context)` with its concrete subject. Node and generic group mounts need that context. Saving a file in the definition's folder reloads a script-entry view; an HTML entry needs a manual reload. Data refreshes without a reload when the vault, schema, or validation changes. Load failures show diagnostics; `/views/_files/<folder>/<file>.tsx` returns the transformed module or HTTP 422 with the error.
6. **Verify the mounted UI** when a browser is available: click the type/group or open the node, clear any remembered override by selecting the view the switcher marks as the default, switch presentations, and exercise contextual links and staged writes. Also verify its direct contextual URL. For remembered controls, reload, open another concrete subject using the same definition, reset to the authored default, and exercise a failed write and retry. Report any unavailable browser check.

## Files

`board.yaml`:

```yaml
apiVersion: rhizome.view.v1
id: team.board            # unique; becomes /views/team.board
name: Board
source:
  kind: custom
  entry: board.tsx        # relative, inside this folder; .tsx .jsx .ts .js or .html
mount:
  kind: standalone        # or type, interface, group, node
  group: Custom           # rail group heading in the Notes workspace
  order: 10
```

`defaults`, `variants`, and `filterPresets` are native execution fields and are rejected on custom views. Supply page settings through top-level `configuration`. Several definitions can share a folder and components. Mount one on a named group with `kind: group, group: Delivery`; use `group: "*"` for a reusable group page. Type and node mounts name a concrete `type`; interface mounts name an `interface`. A page about one type or interface belongs on that type or interface mount, where it joins the collection's view list; reserve `standalone` for tools spanning several types or a sidebar entry the user explicitly asks for. `mount.default: true` selects the configured default, with exact group defaults preceding generic ones.

`board.tsx` default-exports a component receiving `{ view: { id, name }, context, configuration }`. The context is standalone, type, interface, group, or node; node context includes the canonical ref. It renders in kit providers and an error boundary. Use `useViewContext()` in React or `getViewContext()` in HTML/self-mounted code. Reuse canonical refs unchanged and use `openNode(ref, { view })` to select a node presentation. Never hardcode one node into a parameterized page.

## Remember personal choices

Use `useViewPreference` for meaningful display choices such as filters, selected row type, and stable expansion controls. Supply defaults from the definition's `configuration`, validate values, and write only in response to interaction. The kit scopes preferences to the current view and concrete type, interface, group, node, or standalone context, with an optional stable authored widget `slot` nested within the host instance. A widget slot supplements the inherited host identity; it never replaces it. Reusing one generic view does not merge its subjects' settings. Do not construct browser-storage keys or persist drag state, menus, focus, scroll, temporary search, or camera motion.

Personal overrides live in ignored repository-local SQLite, survive reloads and server restarts, and remain separate from shared YAML and staged Markdown edits. Reset removes overrides so current authored defaults apply. Show loading, pending writes, errors, and retry; a failed write is not saved. Read [Personal view preferences](references/kit-api.md#personal-view-preferences) for the React and subscribed HTML APIs and the board example for an interaction using them.

## Rules the transform imposes

- Relative imports need the file extension: `import { Card } from "./components/Card.tsx"`.
- Relative URLs in `fetch` resolve against the page (`/views/<id>`), not the view's folder. Load a sibling file with `fetch(new URL("./data.json", import.meta.url))`. YAML files are never served, so keep data in JSON.
- Bare imports resolve only through the import map: `react`, `react/jsx-runtime`, `react-dom`, `react-dom/client`, `@tanstack/react-query`, `lucide-react`, `@rhizome/ui`, `@rhizome/kit`. Anything else must be a full URL (for example `https://esm.sh/d3`), which needs network access and must not bundle its own React.
- A side-effect import of a sibling stylesheet applies it: `import "./board.css";`. The view renders after the stylesheet loads.
- Each file is transformed once per content and revalidated by ETag, so a saved file is served fresh with no cache busting.
- TypeScript types are stripped, not checked. Keep types for readability; do not rely on them to catch mistakes.
- Style with Tailwind v4 utilities and the theme tokens (`bg-background`, `bg-card`, `text-muted-foreground`, `border`, `text-destructive`, `bg-primary`, `font-serif` for headings). The root font size is 14px because Rhizome's UI is dense; prefer compact spacing.

Read `references/example.md` for a standalone board with a shared component and staged write. The mounted example covers a specific group dashboard, parameterized node page, and HTML accessor.

## An HTML entry or a standalone tool

Set `entry: index.html` to own the whole document. Put `<script src="/kit/v1/boot.js"></script>` in `<head>` before any module script. Launch contextual pages through `/views/<id>` or `customViewHref(id, context)` so registration/configuration/context reach the entry. Direct file URLs are also available for context-free tools. YAML files, dot-prefixed files and folders, and `node_modules` are never served. A built folder works the same way: point `entry` at its built HTML.

## Start from a bundled view

The group views Briefing (`group.briefing`), Trace (`group.trace`), and Sections (`group.sections`), and the type Briefing (`type.briefing` and `interface.briefing`, mounted on `type: "*"` and `interface: "*"`), ship inside `rzm` in one folder, since they share modules. To change one, copy that folder into the repository:

```bash
rzm view eject group.briefing
```

Eject writes the folder's views and shared modules to `.rhizome/views/group/`, refuses to overwrite any existing file, and makes the copies the views those ids resolve to. Default selection, Trace applicability, and the type Briefing's place in the switcher follow the ids, so the copies keep the bundled behavior. Develop the copy like any repository view; a hosted frame reloads when a file in its folder changes. Rhizome upgrades do not change an ejected copy, and deleting the folder restores the bundled views. A copy ejected by an earlier release, before the type Briefing joined the folder, makes a new eject refuse: its modules must match each other, so move the earlier `.rhizome/views/group/` out of `.rhizome/views`, eject again, and reapply your changes to the new copy. To add a variant beside the bundled views instead, give the copied definitions new ids and set `mount.default` if one should open by default.

`RHIZOME_BUNDLED_VIEWS_DIR` serves `web/bundled-views` from disk only while developing Rhizome itself. The kit test harness those views use is internal to Rhizome for now.
