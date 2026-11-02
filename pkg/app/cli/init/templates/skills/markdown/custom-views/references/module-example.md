# A module with actions and parameters

The compounding add-on's workspace is the reference for a view that does what the browser cannot. Its module ships a script, one [action](#actions) per operation of that script, and custom views that call the actions and take [parameters](#parameters). Install it with `rzm init --add-addon compounding` and read the files under `.rhizome/modules/compounding/`:

```text
.rhizome/modules/compounding/
  scripts/workspace.mjs           one entry: workspace.mjs <op>, JSON on stdin, JSON on stdout
  actions/recommendations.yaml    one declaration per op
  actions/dismiss.yaml
  ...
  views/workspace/workspace.yaml  the Recommendations, Health, and Memory tabs
  views/workspace/workspace.tsx
  views/workspace/compounding-item.yaml
  views/workspace/item.tsx
  views/workspace/parts.tsx       components both views share
```

## Actions

An action is `.rhizome/modules/<module>/actions/<name>.yaml`, with id `<module>/<name>`. A command reads its input object from stdin and prints one JSON value. Exit 2 refuses, with the last stderr line as the reason the view shows; any other failure is an error.

```yaml
# actions/dismiss.yaml
description: Dismiss a compounding proposal locally, with a reason
inputs:
  cluster: { kind: string, required: true }
  reason: { kind: string, required: true }
run:
  command: [node, "{module}/scripts/workspace.mjs", "dismiss"]
timeout: 30s
```

Write the script so a refusal a person can act on, such as a lock another run holds, exits 2 with one plain sentence. Check each action from the terminal before writing UI: `rzm compounding recommendations`, or `rzm compounding item --ref proposal:<cluster>`.

## Parameters

A view declares the parameters it takes. A UI parameter, such as the open tab, changes without remounting the view. An identity parameter, such as the item one page shows, is part of the invocation, so changing it remounts.

```yaml
# views/workspace/workspace.yaml
apiVersion: rhizome.view.v1
id: workspace
name: Compounding
source:
  kind: custom
  entry: workspace.tsx
mount:
  kind: standalone
  group: Knowledge
  order: 29
params:
  - name: tab
    kind: string
    default: recommendations
  - name: item
    kind: string
    identity: true
```

A page that only another view opens, like `compounding-item`, sets `mount.hidden: true` so the sidebar does not list it.

## The view

Read with `useActionQuery`, which refreshes while the page is visible, and change with `useActionCommand`, which refreshes every action query once it succeeds. Both reject with the action's `reason` when it refuses, so show that reason beside the control.

```tsx
import { Section, sectionStatus, setViewParams, useActionCommand, useActionQuery, useViewParams } from "@rhizome/kit";
import { Button, Tabs, TabsList, TabsTrigger } from "@rhizome/ui";

export default function Workspace() {
  const { tab } = useViewParams() as { tab: string };
  const query = useActionQuery("compounding/recommendations", {});
  const dismiss = useActionCommand("compounding/dismiss");
  const items = (query.data as { items: { ref: string; title: string; cluster?: string }[] } | undefined)?.items ?? [];
  return (
    <main className="px-5 py-3">
      <Tabs value={tab} onValueChange={(next) => setViewParams({ tab: next })}>
        <TabsList>
          <TabsTrigger value="recommendations">Recommendations</TabsTrigger>
          <TabsTrigger value="health">Health</TabsTrigger>
        </TabsList>
      </Tabs>
      <Section title="Recommendations" count={items.length} status={sectionStatus(query)} what="recommendations">
        {() => (
          <ul>
            {items.map((x) => (
              <li key={x.ref} className="flex items-baseline gap-2 py-1">
                {x.title}
                {x.cluster ? (
                  <Button size="sm" variant="ghost" disabled={dismiss.isPending} onClick={() => dismiss.mutateAsync({ cluster: x.cluster, reason: "not relevant" })}>
                    Remove
                  </Button>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </Section>
      {dismiss.error ? <p className="text-sm text-risk">{(dismiss.error as { reason?: string }).reason}</p> : null}
    </main>
  );
}
```

Open another view with its parameters through `openView("compounding-item", { kind: "standalone" }, { item: ref })`.

## Check it

1. `rzm validate actions` reports declarations the runner would reject: a bad input kind, a missing script, a timeout over five minutes, or a name shared with a recipe.
2. `rzm validate views` checks the definitions, their parameters, and the TSX.
3. Run each action from the terminal, then open the view and exercise every control, including a refusal: compounding's changes refuse while a mine run holds its lock.

Actions run only in a trusted repository; an untrusted one refuses every action with a reason naming `rzm trust`.
