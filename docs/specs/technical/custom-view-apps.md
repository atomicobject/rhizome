---
type: TechnicalSpec
id: SPEC-0105
aliases: [SPEC-0105, custom-view-apps]
summary: "A configured view can be custom code: a view definition names a TSX or HTML entry beside it, rzm serves it same-origin with a prebuilt UI kit and per-file TSX transform, and the same URL works mounted in the Rhizome nav or as a standalone tool."
spec-status: active
last-updated: 2026-10-04
---

# Custom view apps

## Summary

[[view-preferences|SPEC-0114]] owns durable personal settings, concrete invocation identity, reset and migration, and explicit promotion into shared YAML. Its ignored repository-local SQLite store supersedes browser-only preferences and ephemeral expansion choices in this contract.

Configured views today are declarative: a YAML file under `.rhizome/views/` selects a source and a table variant, and the web UI renders it. This spec adds views whose body is code an agent writes. A view definition with `source.kind: custom` names an entry file beside it. `rzm` serves that folder from the main web origin, transforms `.tsx` on request, and provides a prebuilt UI kit (React, TanStack Query, shadcn-style components, Tailwind's browser compiler, a theme mapped from Rhizome's tokens, and a client for the public API). Nothing needs Node, a bundler, or a second process.

The design goal is the agent authoring loop: install Rhizome, read the GraphQL schema, write one `.tsx` file and one `.yaml` file, open a URL. The same URL is the standalone form of the view, so a tool built this way works with or without the Rhizome interface around it.

[[unified-view-contract|SPEC-0110]] extends applicability and invocation to type/interface collections, named or generic display groups, and individual nodes. It owns the shared programming contract, host, selection, and contextual navigation; this spec retains the trusted serving and kit boundary. [[group-views-and-view-platform|SPEC-0111]] adds bundled views served through the same path, CSS side-effect imports, cached transforms with ETag revalidation, host-forwarded freshness, and further kit exports and `validate views` checks; criteria below that it changes cite it.

## Goals

- An agent can create a working custom view with two files and no toolchain.
- A folder can hold several custom views that share components through relative imports.
- A custom view mounts in the Notes rail or compatible type/group/node workspace and also opens full-page at its contextual URL.
- Custom views read through the public GraphQL endpoint and write through the public edit-session routes, using the same client code the first-party UI uses.
- `rzm validate` reports a broken custom view (missing entry, transform error) without a browser.

## Non-Goals

- Sandboxing custom view code. See [Trust model](#trust-model).
- A build pipeline inside `rzm`: no bundling, no `node_modules` resolution, no type checking.
- Cross-origin browser access to the API for separately served tools. They use a dev proxy or server-side calls, as `web/` does today.
- Publishing the kit to npm, and a Vite-built proof of concept. The serving model admits built folders (an `.html` entry in a `dist/` folder); proving that path is follow-up work.
- Custom code variants inside a native declarative definition; separate custom definitions provide additional presentations through the shared registration model.
- Replacing the first-party UI's styling with the kit.

## Trust model

Custom view code is repository configuration, trusted like the repository's build scripts. It runs same-origin with the Rhizome web UI and can call every route that UI can call, including writes and agent routes. Opening a custom view therefore runs the repository's code with the user's local Rhizome authority. This is a deliberate choice (Drew, 2026-09-19: local, in-repo code should not meet rigid permissions). The isolated viewer host remains the mechanism for HTML notes, whose content is not assumed trusted.

Two limits keep the choice contained: custom view code runs only when a user or agent opens that view, never on index, validate, or serve startup; and only files under `.rhizome/views/` are served by these routes.

## User Stories

### US1 - Author a custom view with two files

- id:: ^SPEC-0105-US1
- summary:: An agent or developer adds a definition and an entry script under `.rhizome/views/<folder>/` and the view appears in Rhizome.
- status:: ready

#### Acceptance Criteria

- A view definition with `source.kind: custom` and a relative `source.entry` loads through the existing view loader and appears in the view catalog with its mount.
  verification:: loader and validator unit test; catalog checked over HTTP against this repository's vault.
- `source.entry` must stay inside the definition's folder, must exist, and must end in `.tsx`, `.jsx`, `.ts`, `.js`, or `.html`; otherwise validation reports a fatal issue naming the file.
  verification:: validator unit tests for escape, absolute path, extension, and missing file.
- A custom view supports standalone, type, interface, group, and concrete node mounts as defined by [[unified-view-contract|SPEC-0110]], while rejecting native execution `defaults`, `variants`, and `filterPresets`. Shared custom configuration and invocation context do not imply native source execution.
  verification:: validator unit tests.
- The loader skips `node_modules` directories under `.rhizome/views/`.
  verification:: loader unit test with a definition inside `node_modules`.

### US2 - Serve and transform

- id:: ^SPEC-0105-US2
- summary:: An author opens `/views/<id>` and the view runs with the kit available, with no toolchain on the machine.
- status:: ready

#### Acceptance Criteria

- `GET /views/<id>` for a script entry returns an HTML shell that loads the kit's boot script and a module script for the entry. For an `.html` entry it redirects to the file URL and keeps the query string.
  verification:: handler unit tests.
- `GET /views/_files/<path>` serves files under `.rhizome/views/` only, rejects traversal and symlink escapes, and answers with an ETag and `Cache-Control: no-cache`, so an unchanged file revalidates to 304 ([[group-views-and-view-platform|SPEC-0111]]; originally `no-store`). It never serves a view definition (any `.yaml` or `.yml` file there), a dot-prefixed file or folder, or anything under `node_modules`; a custom view's entry may not live in such a folder either.
  verification:: handler unit tests with traversal paths, a symlink to a file outside the folder, a definition, `.env`, `.git/`, and `node_modules/`; validator test for an entry in a dot-prefixed folder.
- `GET /views/_stamp/<folder>` and `GET /views/_check/<folder>` read the folder through `os.Root`, skip private folders, and never follow a symlink, so neither can hash or transform a file outside `.rhizome/views/`.
  verification:: handler unit test with a symlinked folder at the views root, a symlinked folder inside a view folder, and a symlinked script.
- `.tsx`, `.jsx`, and `.ts` files are transformed to ES modules per request with the automatic JSX runtime. Relative imports with those extensions resolve through the same route.
  verification:: transform unit test; browser run of a view that imports a sibling component.
- A transform failure returns HTTP 422 with `file:line:column: message` text. When a view fails to load in a browser, the page shows the folder's diagnostics from `GET /views/_check/<folder>`, because a browser reports a broken import only as a missing export.
  verification:: handler unit tests; browser run that breaks a shared component and reads the message on the page.
- The entry module's default export is rendered as a React component inside the kit's providers; a module with no default export may mount itself.
  verification:: browser run of both proof-of-concept views.
- Script and HTML entries receive the shared invocation context/configuration and services defined by [[unified-view-contract|SPEC-0110]]; direct and embedded launches preserve canonical subject identity.
  verification:: contextual launch and two-node browser checks.

### US3 - Kit

- id:: ^SPEC-0105-US3
- summary:: An author imports well-known components and Rhizome hooks without installing anything.
- status:: ready

#### Acceptance Criteria

- The kit is embedded in the binary and served at `/kit/v1/`. One classic script, `boot.js`, installs an import map exposing `react`, `react/jsx-runtime`, `react-dom`, `react-dom/client`, `@tanstack/react-query`, `lucide-react`, `@rhizome/ui`, and `@rhizome/kit`, sharing one React instance, plus the theme and Tailwind's browser compiler.
  verification:: kit build output; browser run with no console errors.
- `@rhizome/ui` exports shadcn-compatible components under their conventional names and props, so shadcn code an agent already knows works unchanged apart from the import path.
  verification:: proof-of-concept views written in ordinary shadcn idiom.
- `@rhizome/kit` exports a GraphQL query hook over `/api/v1/graphql`, a field write over the edit-session routes, and navigation helpers that open a note in Rhizome when embedded and navigate the page when standalone.
  verification:: browser run that reads, writes a checkbox into a note, and opens a note from the embedded view.
- Kit queries refetch when the vault announces `index.changed` or `node.changed`, because the index catches up with a write asynchronously, and after every event-stream reconnect, because missed events are not replayed. [[group-views-and-view-platform|SPEC-0111]] adds schema and validation event classes and delivers events to a hosted frame from the workspace instead of a stream of its own.
  verification:: browser run asserts the board updates after a write without a manual refresh.
- The theme maps Rhizome's design tokens onto shadcn's CSS variable names from one source, the `:root` block of `web/src/base.css`.
  verification:: screenshots of the embedded view beside the first-party UI.
- The `/kit/v1/` module names and exports are a versioned public contract; a breaking change ships as `/kit/v2/`.
  verification:: recorded in `web/CONTEXT.md`.

### US4 - Mount and standalone

- id:: ^SPEC-0105-US4
- summary:: A user opens a custom view from the Notes rail, or directly by URL as a standalone tool.
- status:: ready

#### Acceptance Criteria

- A custom view with a standalone mount appears in its rail group and opens in a view tab that frames `/views/<id>`.
  verification:: web unit test for the frame; browser run.
- The same URL opened directly renders the view full-page with no Rhizome chrome.
  verification:: browser run and screenshot.
- A note link activated inside an embedded view opens that note in the surrounding Rhizome workspace. The host accepts that message only from its own origin and from the frame it created.
  verification:: web unit test for origin and source checks; browser run.
- An embedded view takes part in the workspace edit session. By default its writes stage in that session through the frame, its queries read through the session, and the workspace saves or discards those edits with the rest. A standalone view, or a view that wraps its writes in `<EditSession mode="immediate">`, commits each write instead. The host answers the view's session requests only from the frame it created, and relays each staging result back.
  verification:: web unit test for the frame's session bridge; browser run that stages a checkbox from the embedded view, sees it in the view and the workspace, and saves it.
- While a view is open, saving a file in its folder reloads it without a manual refresh, including an edit made while the page was still loading. A hosted frame reloads on the `views.changed` event; bundled views never reload ([[group-views-and-view-platform|SPEC-0111]]).
  verification:: handler unit test for the folder stamp; browser run that edits a shared component.

### US5 - Agent authoring loop

- id:: ^SPEC-0105-US5
- summary:: An agent can discover the schema, try queries, write the view, and check it, from the shell.
- status:: ready

#### Acceptance Criteria

- `rzm init` installs a `custom-views` skill that teaches the loop: read the schema, prove the query from the CLI, write the definition and entry, run `rzm validate views`, open `/views/<id>`.
  verification:: init package tests; after `rzm init`, `rzm init --check` reports no changes.
- The skill documents the kit's exports and carries one complete working example.
  verification:: skill references `kit-api.md` and `example.md`.
- The `views` validation check transforms every script in each custom view's folder and reports transform errors with file and line, once per folder. [[group-views-and-view-platform|SPEC-0111]] adds unresolved-import, unmapped-import, and literal GraphQL checks.
  verification:: validation unit test with two views sharing a broken component.

## Requirements

- The view loader, validator, and catalog stay the single owners of view definitions; custom views add a source kind, not a parallel manifest.
- The transform uses esbuild's Go API, vendored. It performs syntax transform only: no bundling, no resolution, no type checking.
- The kit MUST expose `useViewPreference` and subscribed `getViewPreference` for validated personal choices scoped to the current concrete invocation, including stable authored widget slots, reset, pending state, visible errors, and retry ([[view-preferences|SPEC-0114]]). Authored `configuration` supplies defaults; mounting MUST NOT persist them.
- Kit source lives in `web/kit/` and builds with the web UI into the embedded assets. A `NO_WEB=1` build has no kit, and `/views/<id>` says so.
- The OpenAPI document and generated web types carry the new source kind and entry field.
- Executing a custom view through `POST /api/v1/views/{id}/execute` or `rzm view run` fails with a clear unsupported-kind error.
- A standalone page reloads by polling a content stamp of the view's folder against a baseline the server writes into the shell. A hosted frame reloads on the `views.changed` event, which one server poller emits while an event stream is open ([[group-views-and-view-platform|SPEC-0111]]). Neither extends the vault watcher, which excludes `.rhizome/` by design.

## Open questions

- Should separately served browser tools get opt-in cross-origin access (an allowed-origins serve flag)? Recommendation: yes, opt-in and loopback-only, as its own change once a real tool needs it.
- Should the kit publish to npm for Vite-built view folders? Deferred until a built view is attempted.

## Documentation plan

- Subsystem ownership: `pkg/ontology/viewconfig/CONTEXT.md` gains the custom source kind; `web/CONTEXT.md` gains the kit and the `/views/` routes.
- The skill template is the agent-facing documentation.
