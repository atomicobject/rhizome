# Experiences

An experience is a folder of views that Rhizome runs as one site: a group home, record pages, lists, and an optional panel at the bottom of the Notes sidebar. Put an `experience.yml` in any views folder (`.rhizome/views/<folder>/`, or `.rhizome/modules/<module>/views/<folder>/`) and every view definition in that folder and below it joins the experience. The views stay ordinary view definitions; the experience changes how they are hosted.

Rhizome renders an experience's pages in one long-lived frame per window. Opening a record, another record, and a list, each in its own tab, reuses that frame: the kit and your modules load once, and each tab keeps its own page, scroll position, and component state. Returning to a recent tab shows its page as it was, without rereading data that has not changed.

## Files

```text
.rhizome/modules/team-docs/views/handbook/
├── experience.yml
├── home.yaml       # group page: mount { kind: group, group: Handbook }
├── home.tsx
├── policy.yaml     # record page: mount { kind: node, type: Policy }
├── policy.tsx
├── panel.yaml      # mount { kind: panel }
└── panel.tsx
```

`experience.yml`:

```yaml
title: Handbook          # required
group: Handbook          # a display group the experience presents
types: [Policy, Guide]   # types or interfaces it presents beyond its views' mounts
panel: handbook.panel    # the id of a panel view in this folder
tailwind: false          # true (the default) loads the runtime Tailwind compiler
```

`panel.yaml`:

```yaml
apiVersion: rhizome.view.v1
id: handbook.panel
name: Handbook
source: { kind: custom, entry: panel.tsx }
mount: { kind: panel }
```

`rzm validate views` reports each problem with its file and field. A missing `title` or an unreadable file makes the experience invalid, and its views then work as ordinary views. An unknown field, type, interface, or group is a warning that drops that name, and a panel problem drops only the panel, so one `experience.yml` works across vaults whose schemas differ. An `experience.yml` inside another experience's folder declares nothing.

## Pages

- **Default-export the component.** The experience frame renders a script entry's default export. An HTML entry, or a module without a default export, keeps a frame of its own per subject; validation warns about it.
- **Read through hooks.** A hidden page re-renders with its own subject, so components read the subject, params, and configuration with `useViewContext()`, `useViewParams()`, `useViewLocation()`, and `useViewConfiguration()`, never `getViewInvocation()`. The imperative exports (`openNode`, `setViewParams`, `getViewParams`, `getViewPreference`, `markReady`) act for the page on screen; from components and their async callbacks use `usePageNavigation()` (the `open*` functions and `setViewParams`) and `useMarkReady()`, which act for the page that called them.
- **Stop your own work while hidden.** `useViewActive()` is false while the page is hidden or the frame is suspended. Kit queries and action polling pause on their own; stop your own timers, intervals, and animations when it turns false.
- **Say when the page is ready.** Call `useMarkReady()`'s function (or `markReady()`) once the reads the page needs have resolved; Rhizome's performance diagnostics time data-ready from it.
- **One frame is shared.** Module-level variables and styles a module injects persist across every page in the frame, so keep per-page state in React. State that must survive the page being dropped and rebuilt goes in `useViewPreference`, or `sessionStorage` for state scoped to the window.
- **A new subject is a new page.** A location with a different view, subject, or identity param replaces the page: a new component, fresh state, and a reset error boundary. A UI param change updates the page in place.

## Record pages

Build record pages on `RecordPage` so every page shares one structure and its loading, partial, and error states. It shows the title at once, before any read finishes, so pass the record's title from the subject or a small first read and load whole-scope data beside it:

```tsx
import { RecordPage, sectionStatus, useGraphQL, useViewContext } from "@rhizome/kit";

type Policy = { title: string; summary?: string | null; content?: string | null };

export default function PolicyPage() {
  const context = useViewContext();
  const path = context.kind === "node" ? context.ref.notePath : "";
  // The record's own fields, read alone so the page shows them before any whole-scope read.
  const own = useGraphQL<{ node: Policy | null }>(
    `query Policy($ref: String!) { node(ref: $ref) { title ... on Policy { summary content } } }`,
    { ref: path },
  );
  const policy = own.data?.node;

  return (
    <RecordPage
      kind={<span>Policy</span>}
      title={policy?.title ?? path.split("/").pop()}
      status={sectionStatus(own)}
      what="this policy"
    >
      {() => <article className="whitespace-pre-wrap">{policy?.content}</article>}
    </RecordPage>
  );
}
```

The header also takes `facts` (one `Facts` line), `actions` (controls at the right), and `aside` (a side column). `partial` takes a notice for a body that shows less than the whole scope, from `typedRootCoverage(data)` or `truncation(data)`. `readNodes(refs, selection)` reads any number of refs in batches of 200 and returns `{ nodes, unresolved, errors, truncated, complete }`; use it whenever a record page reads a scope larger than one batch.

## The panel

The panel is a view with `mount: { kind: panel }`, named by the experience's `panel`. It is never a view choice and is not listed among the sidebar's views. The Notes sidebar shows it at its bottom for every subject the experience presents:

1. the experience's standalone views;
2. records whose type is a mount target of its views or is listed in `types`;
3. records of a type that implements an interface it presents;
4. its group (its `group`, or a group its views mount on): the group page, its type collections, and their records.

The most specific level wins, so an experience that lists a type in `types` takes those records from an experience that only presents their group. Within a level, the flat `.rhizome/views` folder wins over modules, modules win by name, and then folder path decides; validation warns when two experiences with panels tie. An embedded or section node whose type no experience presents at levels 1 to 3, and that is in no group, shows its owning note's panel. A note that no experience with a panel presents shows the built-in Folder panel; search, Problems, and Explorer show none.

The panel runs in its own frame and keeps one page: a new subject updates that page in place, so the component stays mounted and its own state (find text, lists, scroll) survives navigation. Read the active subject with `useViewLocation()` or `useViewContext()`, and keep what the panel remembers in `useViewPreference`, which is scoped to the panel's experience rather than to the subject. Draw lines and highlighted marks with `--chrome-link` and text with `--chrome-text` and `--chrome-muted`, since the panel sits on the sidebar's chrome.

An experience panel may declare view params to read matching values from the active workspace URL. Keep these params non-identity to preserve the panel's local state as the active subject changes.

## Tailwind

Every frame loads the kit's precompiled stylesheet, which holds the theme, the kit's components, and every utility the kit and Rhizome's bundled starters use. Set `tailwind: false` when every utility your views use is in that stylesheet or in your own CSS, so the frame skips the runtime compiler and starts faster. Keep the default, `tailwind: true`, when your views use other utilities; with `false` they would not apply.

## Eject a starter and extend it

A bundled starter's experience lives in its module, which `rzm init` refreshes. To change it, eject the starter: `rzm init --eject <starter>` records it under `management.ejected` in `.rhizome/workflows.yml`, and later `rzm init` runs neither refresh nor remove the module's files. The experience, its panel, its scripts, and its bindings keep working from your copy exactly as they did from the bundled one.

Bundled starters set `tailwind: false`. Before adding a utility the precompiled stylesheet lacks, set `tailwind: true` in the ejected `experience.yml`. A page you add to the folder joins the experience and shares its frame; give it a mount, or a standalone mount with `hidden: true` when only the panel or other pages link to it.
