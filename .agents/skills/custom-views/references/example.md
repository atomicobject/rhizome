# Example: a board that reads and writes

`.rhizome/views/team/board.yaml`:

```yaml
apiVersion: rhizome.view.v1
id: team.board
name: Action Board
source:
  kind: custom
  entry: board.tsx
mount:
  kind: standalone
  group: Custom
configuration:
  showCompleted: true
```

`.rhizome/views/team/components/Column.tsx`, shared by every view in the folder:

```tsx
import { Badge } from "@rhizome/ui";
import type { ReactNode } from "react";

export function Column(props: { title: string; count: number; children: ReactNode }) {
  return (
    <section className="w-80 shrink-0 rounded-lg border bg-card">
      <h2 className="flex items-center justify-between border-b px-3 py-2 text-sm font-semibold">
        {props.title}
        <Badge variant="secondary">{props.count}</Badge>
      </h2>
      <div className="flex flex-col divide-y">{props.children}</div>
    </section>
  );
}
```

`.rhizome/views/team/board.tsx`:

```tsx
import { NoteLink, useGraphQL, useSetField, useViewPreference, type ViewModuleProps } from "@rhizome/kit";
import { Checkbox } from "@rhizome/ui";

import { Column } from "./components/Column.tsx";

type Item = {
  ref: { notePath: string; fragment: string | null; nodeId: string };
  title: string;
  done: boolean;
  assignee: { title: string } | null;
};

const QUERY = `{
  actionItem(first: 500) { ref { notePath fragment nodeId } title done assignee { title } }
}`;

const isBoolean = (value: unknown): value is boolean => typeof value === "boolean";

export default function Board({ configuration }: ViewModuleProps) {
  const showCompleted = useViewPreference("showCompleted", {
    defaultValue: isBoolean(configuration?.showCompleted) ? configuration.showCompleted : true,
    validate: isBoolean,
  });
  // The preference exposes failures in its snapshot; prevent an unhandled rejection.
  const remember = (write: Promise<void>) => { void write.catch(() => {}); };
  const { data, error, isPending } = useGraphQL<{ actionItem: Item[] }>(QUERY);
  const setField = useSetField();

  if (isPending || showCompleted.loading) return <p className="p-4 text-muted-foreground">Loading…</p>;
  if (error) return <p className="p-4 text-destructive">{error.message}</p>;

  const byAssignee = new Map<string, Item[]>();
  for (const item of data.actionItem) {
    if (!showCompleted.value && item.done) continue;
    const key = item.assignee?.title ?? "Unassigned";
    byAssignee.set(key, [...(byAssignee.get(key) ?? []), item]);
  }

  return (
    <main className="space-y-3 p-4">
      <div className="flex items-center gap-3 text-sm">
        <label className="flex items-center gap-2">
          <Checkbox checked={showCompleted.value} onCheckedChange={(checked) =>
            remember(showCompleted.set(checked === true))
          } />
          Show completed
        </label>
        {(showCompleted.overridden || showCompleted.error) && <button onClick={() => remember(showCompleted.reset())}>Use default</button>}
        {showCompleted.pending && <span role="status">Saving preference…</span>}
      </div>
      {showCompleted.error && <p role="alert" className="text-destructive">
        {showCompleted.error.message}{" "}
        <button onClick={() => remember(showCompleted.retry())}>Retry</button>
      </p>}
      <div className="flex items-start gap-4">
        {[...byAssignee].map(([assignee, items]) => (
          <Column key={assignee} title={assignee} count={items.length}>
            {items.map((item) => (
              <label key={item.ref.nodeId} className="flex items-start gap-2 px-3 py-2 text-sm">
                <Checkbox
                  checked={item.done}
                  disabled={setField.isPending}
                  onCheckedChange={(checked) =>
                    setField.mutate({ target: item.ref, field: "done", value: checked === true ? "true" : "false" })
                  }
                />
                <span className="min-w-0 flex-1">
                  {item.title}
                  <NoteLink path={item.ref.notePath} className="block truncate text-xs text-muted-foreground" />
                </span>
              </label>
            ))}
          </Column>
        ))}
      </div>
    </main>
  );
}
```

The completed-record filter is a personal display preference. The item checkboxes still stage content edits through `useSetField`; they are not view preferences. The configuration supplies the shared initial choice, and mounting the board never writes that default. Reset returns to the current configured choice. For two intentionally independent boards in one invocation, pass distinct stable authored `slot` values to the hook.

Check it with `rzm validate views`, then open `/views/team.board`. Change Show completed and reload; reset it and confirm the YAML default applies. Verify a failed preference write stays visible and Retry completes it. A generic type, group, or node mount should also be tested on two concrete subjects to confirm isolation.
