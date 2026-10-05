# Example: a group dashboard opens a node presentation

This example uses the Agentic Engineering `Delivery` group and concrete `EffortNote` type. Discover those names and the schema in the target vault first. The group organizes navigation; the explicit native definition below supplies the effort collection. It does not infer ontology membership from the group.

All files live in `.rhizome/views/delivery/`. Add a small native source definition, or reuse an existing compatible catalog definition:

```yaml
# efforts.yaml
apiVersion: rhizome.view.v1
id: delivery.efforts
name: Efforts
source:
  kind: ontology_type
  type: EffortNote
mount:
  kind: type
  type: EffortNote
variants:
  table:
    columns:
      - field: title
      - field: status
```

Register two custom entries independently:

```yaml
# dashboard.yaml
apiVersion: rhizome.view.v1
id: delivery.dashboard
name: Delivery dashboard
source:
  kind: custom
  entry: dashboard.tsx
mount:
  kind: group
  group: Delivery
  default: true
configuration:
  nativeView: delivery.efforts
  detailView: delivery.effort-detail
```

```yaml
# detail.yaml
apiVersion: rhizome.view.v1
id: delivery.effort-detail
name: Effort detail
source:
  kind: custom
  entry: detail.tsx
mount:
  kind: node
  type: EffortNote
```

The dashboard reuses native execution and canonical navigation:

```tsx
// dashboard.tsx
import { nodeHref, openNode, useViewRows, type ViewModuleProps } from "@rhizome/kit";

export default function Dashboard({ context, configuration }: ViewModuleProps) {
  const source = String(configuration?.nativeView ?? "delivery.efforts");
  const detail = String(configuration?.detailView ?? "delivery.effort-detail");
  const rows = useViewRows(source, { variant: "table" });
  if (context.kind !== "group") return <p>Select a display group to open this page.</p>;
  if (rows.isPending) return <p>Loading efforts…</p>;
  if (rows.error) return <p role="alert">{rows.error.message}</p>;
  return (
    <main className="space-y-3 p-4">
      <h1 className="font-serif text-xl">{context.group}</h1>
      <ul>
        {rows.data.rows.map((row) => (
          <li key={row.ref.nodeId ?? row.ref.notePath}>
            <a href={nodeHref(row.ref, { view: detail })} onClick={(event) => {
              if (event.button !== 0 || event.shiftKey || event.altKey) return;
              event.preventDefault();
              openNode(row.ref, { view: detail, beside: event.metaKey || event.ctrlKey });
            }}>{row.title}</a>
          </li>
        ))}
      </ul>
    </main>
  );
}
```

The node page derives its read/write target from context. Its GraphQL query is specific to the discovered type, while the focused node can differ on every invocation. GraphQL's `node(ref:)` takes a `path#fragment` locator string; writes and navigation take the context's `ref` object unchanged:

```tsx
// detail.tsx
import { useGraphQL, useSetField, type ViewContext, type ViewModuleProps } from "@rhizome/kit";
import { Button } from "@rhizome/ui";

const QUERY = `query Detail($ref: String!) {
  node(ref: $ref) { title ... on EffortNote { summary status } }
}`;

function Detail({ context }: { context: Extract<ViewContext, { kind: "node" }> }) {
  const ref = context.ref;
  const locator = ref.notePath + (ref.fragment ? `#${ref.fragment}` : "");
  const result = useGraphQL<{ node: { title: string; summary: string; status: string } | null }>(
    QUERY, { ref: locator },
  );
  const edit = useSetField();
  if (result.isPending) return <p>Loading effort…</p>;
  if (result.error) return <p role="alert">{result.error.message}</p>;
  if (!result.data.node) return <p>This effort is unavailable.</p>;
  return (
    <main className="space-y-3 p-4">
      <h1 className="font-serif text-xl">{result.data.node.title}</h1>
      <p>{result.data.node.summary}</p>
      <p>Status: {result.data.node.status}</p>
      <Button disabled={edit.isPending} onClick={() =>
        edit.mutate({ target: ref, field: "status", value: "active" })
      }>Set active</Button>
      {edit.error && <p role="alert">{edit.error.message}</p>}
    </main>
  );
}

export default function EffortPage({ context }: ViewModuleProps) {
  return context.kind === "node"
    ? <Detail context={context} />
    : <p>Open an effort to use this presentation.</p>;
}
```

In an embedded workspace, the button stages an edit for review/save; it does not commit immediately. In standalone mode it commits. Test writes only on a disposable note and restore its original state.

## Reusable groups and HTML

To make a page available for every group, register `mount: { kind: group, group: "*" }`. Keep actual subject access as `context.group`. Choose explicit queries or a configurable native source appropriate to the page. To adapt to whatever group opens it, read the group's members with `useDisplayGroup()` and their fields and lifecycles with `useTypeDocs(names)` rather than naming types. Rhizome's bundled Briefing, Trace, and Sections pages work this way; `rzm view eject group.briefing` copies them for study or change. An exact group default overrides the generic default, and either overrides the bundled default; all compatible choices remain available.

HTML uses the same accessors. Point a definition at this entry and launch through its registered URL so the invocation reaches the page:

```html
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Group page</title>
  <script src="/kit/v1/boot.js"></script>
</head>
<body><h1 id="title"></h1>
<script type="module">
  import { getViewContext, getViewConfiguration, executeView } from "@rhizome/kit";
  const context = getViewContext();
  document.getElementById("title").textContent = context.kind === "group" ? context.group : "View";
  const source = getViewConfiguration()?.nativeView;
  if (typeof source === "string") {
    try { console.log(await executeView(source, { variant: "table" })); }
    catch (error) { document.body.append(String(error)); }
  }
</script></body></html>
```

Promise data helpers read committed state. Use React hooks for staged workspace reads and subscribed data refresh. Personal preferences support subscriptions outside React too. Add a display toggle to the HTML entry with this module code:

```js
import { getViewConfiguration, getViewPreference } from "@rhizome/kit";

const configured = getViewConfiguration()?.showDetails;
const details = getViewPreference("showDetails", {
  defaultValue: typeof configured === "boolean" ? configured : true,
  validate: (value) => typeof value === "boolean",
  slot: "summary", // A stable authored widget identity, if the page has several widgets.
});
const toggle = document.createElement("button");
const reset = document.createElement("button");
const retry = document.createElement("button");
const status = document.createElement("p");
reset.textContent = "Use default";
retry.textContent = "Retry";
status.setAttribute("role", "status");
document.body.append(toggle, reset, retry, status);
const remember = (write) => { void write.catch(() => {}); };
toggle.onclick = () => remember(details.update((value) => !value));
reset.onclick = () => remember(details.reset());
retry.onclick = () => remember(details.retry());
function render() {
  const state = details.getSnapshot();
  toggle.textContent = state.value ? "Hide details" : "Show details";
  toggle.disabled = state.loading;
  reset.hidden = !state.overridden && !state.error;
  retry.hidden = !state.error;
  status.textContent = state.error?.message ?? (state.loading ? "Loading preference…" : state.pending ? "Saving preference…" : "");
  // Render your detail panel from state.value here.
}
const unsubscribe = details.subscribe(render);
render();
window.addEventListener("pagehide", unsubscribe, { once: true });
```

The invocation already contains the concrete group or node; do not append a browser key or reuse the wildcard mount as identity. The accessor handles persistence and notifies matching mounted consumers. This accessor's reset removes this key in the `summary` widget only. The host's custom-view reset also clears persisted preferences for sibling widgets, including unmounted ones, within this host instance. The public `slot` supplements the inherited host identity. Put `showDetails` in YAML `configuration` when the repository should author its default; keep the interaction personal.

## Verify

Run `rzm validate views`, then click the Delivery label and select the view the switcher marks as the default; picking it clears any remembered choice. Open an effort from the dashboard, switch to its built-in presentation and back, and confirm pending edits survive. Open two efforts using this definition and verify their identities/data differ. Construct direct launches with `customViewHref("delivery.dashboard", { kind: "group", group: "Delivery" })` and a canonical node context for the detail page; verify those launches too. Keep the node detail page optional unless `mount.default: true` is intentionally wanted.
