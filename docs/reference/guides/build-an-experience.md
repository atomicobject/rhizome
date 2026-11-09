---
type: ReferenceDoc
summary: "How to build an experience in a team module: experience.yml, a group home, a record page on RecordPage, a sidebar panel, how Rhizome chooses the panel, and ejecting a bundled starter to extend its experience."
reference-kind: guide
last-verified: 2026-11-09
status: active
---

# Build an experience

An experience is a folder of custom views that Rhizome runs as one site: a group home, record pages, lists, and a panel at the bottom of the Notes sidebar ([[experiences|SPEC-0136]]). This guide builds a small one in a team module. The same files work in the flat `.rhizome/views` folder, in a bundled starter's module, and in an ejected copy of one; nothing in Rhizome treats them differently.

The agent-facing guide is the `custom-views` skill and its `references/experiences.md`; this page is the same material for people, with a complete example.

## What you get

- **One frame per window.** Every page of the experience renders in one long-lived frame. Opening a record in a new tab, another record, and a list reuses it, so the kit and your modules load once.
- **A page per tab.** Each tab keeps its own page, scroll position, and component state. Up to eight recently shown pages stay mounted but hidden, so returning to one is instant and rereads nothing unless the index or the edit session changed.
- **A sidebar panel.** The experience's panel shows at the bottom of the Notes sidebar for every subject the experience presents, and stays the same component as the subject changes.
- **Shared reads.** Public GraphQL and view reads are cached per index generation on the server, so the panel and a page running the same query execute it once.

## The example

A team module `team-docs` ships a `Policy` type in a "Handbook" display group, and an experience with a group home, a policy page, and a panel listing every policy:

```text
.rhizome/modules/team-docs/
├── ontology/
│   └── handbook.graphql
└── views/
    └── handbook/
        ├── experience.yml
        ├── home.yaml
        ├── home.tsx
        ├── policy.yaml
        ├── policy.tsx
        ├── panel.yaml
        └── panel.tsx
```

`ontology/handbook.graphql`:

```graphql
type Policy @node(paths: ["handbook/policies/*.md"]) @display(group: "Handbook") {
  summary: String
}
```

`views/handbook/experience.yml`:

```yaml
title: Handbook
group: Handbook
panel: handbook.panel
```

`title` is required. `group` makes the experience present the Handbook group, its type collections, and their records. `types` (not used here) lists further types or interfaces it presents. `panel` names the panel view. `tailwind` is left at its default, `true`, which loads the runtime Tailwind compiler; see [Tailwind](#tailwind).

### The group home

`home.yaml`:

```yaml
apiVersion: rhizome.view.v1
id: handbook.home
name: Handbook
source: { kind: custom, entry: home.tsx }
mount: { kind: group, group: Handbook, default: true }
```

`home.tsx`:

```tsx
import { NodeLink, Section, sectionStatus, useGraphQL, useMarkReady } from "@rhizome/kit";
import { useEffect } from "react";

type Policy = { ref: { notePath: string; kind: string }; title: string; summary?: string | null };

export const POLICIES = `{ policy(first: 500) { ref { notePath kind fragment nodeId } title summary } }`;

export default function Home() {
  const query = useGraphQL<{ policy: Policy[] }>(POLICIES);
  const policies = query.data?.policy ?? [];
  const markReady = useMarkReady();

  useEffect(() => {
    if (query.data) markReady();
  }, [query.data, markReady]);

  return (
    <main className="px-5 py-3">
      <Section title="Policies" count={policies.length} status={sectionStatus(query)} what="policies"
        empty={policies.length ? undefined : "No policies yet."}>
        {() => (
          <ul>
            {policies.map((policy) => (
              <li key={policy.ref.notePath} className="py-1">
                <NodeLink node={policy.ref}>{policy.title}</NodeLink>
              </li>
            ))}
          </ul>
        )}
      </Section>
    </main>
  );
}
```

The component is the module's default export: the experience frame renders default exports. An HTML entry, or a module without one, still works but keeps a frame of its own, and validation warns about it.

### A record page

`policy.yaml`:

```yaml
apiVersion: rhizome.view.v1
id: handbook.policy
name: Policy
source: { kind: custom, entry: policy.tsx }
mount: { kind: node, type: Policy, default: true }
```

`policy.tsx` builds on `RecordPage`, the kit's record-page shell. It shows the title as soon as the record's own read answers, and gives every record page the same loading, partial, and error states:

```tsx
import { RecordPage, sectionStatus, useGraphQL, useMarkReady, useViewContext } from "@rhizome/kit";
import { useEffect } from "react";

type Policy = { title: string; summary?: string | null; content?: string | null };

export default function PolicyPage() {
  const context = useViewContext();
  const path = context.kind === "node" ? context.ref.notePath : "";
  const own = useGraphQL<{ node: Policy | null }>(
    `query Policy($ref: String!) { node(ref: $ref) { title ... on Policy { summary content } } }`,
    { ref: path },
  );
  const markReady = useMarkReady();
  const policy = own.data?.node;

  useEffect(() => {
    if (own.data) markReady();
  }, [own.data, markReady]);

  return (
    <main className="px-5 py-3">
      <RecordPage
        kind={<span>Policy</span>}
        title={policy?.title ?? path.split("/").pop()}
        status={sectionStatus(own)}
        what="this policy"
      >
        {() => <article className="whitespace-pre-wrap">{policy?.content}</article>}
      </RecordPage>
    </main>
  );
}
```

Two rules matter because pages share a frame:

- Read the subject, params, and configuration through hooks (`useViewContext`, `useViewParams`, `useViewLocation`, `useViewConfiguration`). A hidden page re-renders with its own subject, while the imperative accessors (`getViewInvocation`, `getViewParams`) and exports (`openNode`, `setViewParams`, `markReady`) act for the page on screen. Async callbacks use `usePageNavigation()` and `useMarkReady()`, which stay bound to their page.
- Module-level variables and injected styles persist across every page in the frame. Keep per-page state in React, and state that must survive a dropped page in `useViewPreference` (or `sessionStorage` for state scoped to the window). Stop your own timers while `useViewActive()` is false.

### The panel

`panel.yaml`:

```yaml
apiVersion: rhizome.view.v1
id: handbook.panel
name: Handbook
source: { kind: custom, entry: panel.tsx }
mount: { kind: panel }
```

`panel.tsx` lists every policy and marks the one open in the active tab. It reads the same query as the group home, so the server executes it once per index generation for both:

```tsx
import { NodeLink, useGraphQL, useViewContext } from "@rhizome/kit";
import { POLICIES } from "./home.tsx";

export default function Panel() {
  const context = useViewContext();
  const current = context.kind === "node" ? context.ref.notePath : null;
  const query = useGraphQL<{ policy: { ref: { notePath: string; kind: string }; title: string }[] }>(POLICIES);

  return (
    <nav aria-label="Policies" className="px-3 py-2 text-[var(--chrome-text)]">
      {(query.data?.policy ?? []).map((policy) => (
        <div key={policy.ref.notePath} aria-current={policy.ref.notePath === current ? "page" : undefined}
          className={policy.ref.notePath === current ? "font-semibold" : "text-[var(--chrome-muted)]"}>
          <NodeLink node={policy.ref}>{policy.title}</NodeLink>
        </div>
      ))}
    </nav>
  );
}
```

The panel keeps one page whose subject changes in place, so its component and state survive navigation; `useViewContext()` gives the active subject. Its `useViewPreference` settings belong to the experience, not the subject. On the sidebar's chrome, text uses `--chrome-text` and `--chrome-muted`, and lines or highlighted marks use `--chrome-link`, never as text.

Check the folder with `rzm validate views`, which reports every `experience.yml` problem with its file and field.

## How Rhizome chooses the panel

The server gives every view target one panel, most specific first ([[experiences|SPEC-0136]] R6):

1. a standalone view's own experience;
2. an experience with a panel whose views mount on the subject's concrete type, or that lists it in `types`;
3. one that presents an interface of that type;
4. one that presents the subject through a group: its `group`, or a group one of its views mounts on, covering the group page, the group's type collections, and their records;
5. the built-in Folder panel, for any other subject backed by a note;
6. none, for search, Problems, Explorer, and subjects without a note.

Within a level, the flat `.rhizome/views` folder comes first, then modules by name, then bundled views, and then folder path decides; validation warns when two experiences with panels tie. An embedded or section node whose type is in no group and that no experience presents at levels 1 to 3 takes its owning note's panel. The panel depends only on the subject, never on which view the switcher shows.

Display groups are shared: no module owns a group name, so an experience that presents a group also shows its panel for other modules' types in that group. A module that wants its own panel for its types declares an experience that lists them in `types`, which outranks the group.

Folder shows the active note's folder, its ancestors, and its siblings. It starts minimised in each vault until someone opens it.

## Tailwind

Every experience frame loads the kit's precompiled stylesheet: the theme, the kit's components, and every utility the kit and Rhizome's bundled starters use (`web/kitStylesheet.ts` builds it). With `tailwind: true`, the default, the frame also loads the runtime compiler, so any utility works. Setting `tailwind: false` skips the compiler and starts frames faster, but only when every utility the views use is in the precompiled sheet or in a stylesheet beside the views; any other utility silently does not apply. Bundled starters set `false` because the sheet is built from their views.

## Eject a starter and extend it

A bundled starter's experience, such as the AO KB Knowledge site, lives in its module, which `rzm init` keeps current. To change it:

1. Eject the starter: `rzm init --eject <starter>`. Init records it under `management.ejected` in `.rhizome/workflows.yml`, and later runs neither refresh nor remove the module's files ([[init-starter-workflow]]).
2. Set `tailwind: true` in the ejected `experience.yml` before adding a utility the precompiled stylesheet lacks; bundled starters ship `tailwind: false`.
3. Add a page to the experience's folder. It joins the experience and shares its frame. Give it a mount, or `mount: { kind: standalone, hidden: true }` when only the panel or other pages link to it.
4. Change the panel view in place, or point `panel` at a new panel view in the folder.

The experience, its panel, its scripts, and its bindings keep working from your copy exactly as from the bundled module. `rzm init --restore <starter>` resumes updates.
