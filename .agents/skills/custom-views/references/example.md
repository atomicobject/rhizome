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
import { Count } from "@rhizome/kit";
import type { ReactNode } from "react";

// A board column: a sunk band with a hairline border holding flat white cards.
export function Column(props: { title: string; count: number; children: ReactNode }) {
  return (
    <section className="w-72 shrink-0 rounded-md border bg-sunk" aria-label={props.title}>
      <h2 className="flex items-baseline gap-2 border-b px-2.5 py-1.5 text-sm font-bold">
        {props.title}
        <Count>{props.count}</Count>
      </h2>
      <div className="flex flex-col gap-1.5 p-1.5">{props.children}</div>
    </section>
  );
}
```

`.rhizome/views/team/board.tsx`:

```tsx
import { NoteLink, useGraphQL, useSetField, useViewPreference, type ViewModuleProps } from "@rhizome/kit";
import { Button, Checkbox } from "@rhizome/ui";

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

  if (isPending || showCompleted.loading) return <p className="p-4 text-faint">Loading action items…</p>;
  if (error) return <p role="alert" className="p-4 text-risk">{error.message}</p>;

  const byAssignee = new Map<string, Item[]>();
  for (const item of data.actionItem) {
    if (!showCompleted.value && item.done) continue;
    const key = item.assignee?.title ?? "Unassigned";
    byAssignee.set(key, [...(byAssignee.get(key) ?? []), item]);
  }

  return (
    <main className="space-y-3 p-4">
      <div className="flex items-center gap-3">
        <label className="flex items-center gap-2">
          <Checkbox checked={showCompleted.value} onCheckedChange={(checked) =>
            remember(showCompleted.set(checked === true))
          } />
          Show completed
        </label>
        {(showCompleted.overridden || showCompleted.error) && <Button size="sm" onClick={() => remember(showCompleted.reset())}>Use default</Button>}
        {showCompleted.pending && <span role="status">Saving preference…</span>}
      </div>
      {showCompleted.error && <p role="alert" className="text-risk">
        {showCompleted.error.message}{" "}
        <Button size="sm" onClick={() => remember(showCompleted.retry())}>Retry</Button>
      </p>}
      <div className="flex items-start gap-4">
        {[...byAssignee].map(([assignee, items]) => (
          <Column key={assignee} title={assignee} count={items.length}>
            {items.map((item) => (
              <label key={item.ref.nodeId} className="flex items-start gap-2 rounded-md border bg-surface px-2.5 py-1.5">
                <Checkbox
                  checked={item.done}
                  disabled={setField.isPending}
                  onCheckedChange={(checked) =>
                    setField.mutate({ target: item.ref, field: "done", value: checked === true ? "true" : "false" })
                  }
                />
                <span className="min-w-0 flex-1">
                  {item.title}
                  <NoteLink path={item.ref.notePath} className="block truncate font-mono text-xs" />
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
