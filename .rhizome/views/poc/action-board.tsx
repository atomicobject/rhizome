import { NoteLink, embedded, useGraphQL, useSetField } from "@rhizome/kit";
import { Badge, Checkbox, Input, Tabs, TabsList, TabsTrigger } from "@rhizome/ui";
import { useState } from "react";

import { Column, Loading, Page, groupBy } from "./components/Page.tsx";

type ActionItem = {
  ref: { notePath: string; fragment: string | null; nodeId: string };
  title: string;
  done: boolean;
  due: string | null;
  assignee: { title: string } | null;
};

const QUERY = `{
  actionItem(first: 500, sort: [{ field: "due", direction: asc }]) {
    ref { notePath fragment nodeId } title done due assignee { title }
  }
}`;

const today = new Date().toISOString().slice(0, 10);

export default function ActionBoard() {
  const { data, error, isPending } = useGraphQL<{ actionItem: ActionItem[] }>(QUERY);
  const setField = useSetField();
  const [show, setShow] = useState("open");
  const [search, setSearch] = useState("");

  const items = (data?.actionItem ?? []).filter(
    (item) =>
      (show === "all" || item.done === (show === "done")) && item.title.toLowerCase().includes(search.toLowerCase()),
  );

  return (
    <Page
      title="Action Board"
      summary={`${items.length} items by assignee. ${embedded ? "Checking one stages the change for Save." : "Checking one writes the checkbox in its note."}`}
      toolbar={
        <>
          <Input className="w-48" placeholder="Filter…" value={search} onChange={(event) => setSearch(event.target.value)} />
          <Tabs value={show} onValueChange={setShow}>
            <TabsList>
              <TabsTrigger value="open">Open</TabsTrigger>
              <TabsTrigger value="done">Done</TabsTrigger>
              <TabsTrigger value="all">All</TabsTrigger>
            </TabsList>
          </Tabs>
        </>
      }
    >
      {isPending && <Loading />}
      {error && <p className="text-destructive">{error.message}</p>}
      {setField.error && <p className="mb-3 text-destructive">Write failed: {setField.error.message}</p>}
      <div className="flex items-start gap-4">
        {groupBy(items, (item) => item.assignee?.title ?? "Unassigned").map(([assignee, group]) => (
          <Column key={assignee} title={assignee} count={group.length}>
            {group.map((item) => (
              <label key={item.ref.nodeId} className="flex items-start gap-2 px-3 py-2 text-sm hover:bg-muted/50">
                <Checkbox
                  className="mt-0.5"
                  checked={item.done}
                  disabled={setField.isPending}
                  onCheckedChange={(checked) =>
                    setField.mutate({ target: item.ref, field: "done", value: checked === true ? "true" : "false" })
                  }
                />
                <span className="min-w-0 flex-1">
                  <span className={item.done ? "text-muted-foreground line-through" : ""}>{item.title}</span>
                  <NoteLink path={item.ref.notePath} className="block truncate text-xs text-muted-foreground hover:underline" />
                </span>
                {item.due && (
                  <Badge variant={!item.done && item.due < today ? "destructive" : "outline"}>{item.due}</Badge>
                )}
              </label>
            ))}
          </Column>
        ))}
      </div>
    </Page>
  );
}
