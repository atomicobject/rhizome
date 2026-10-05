// Shared by every view in this folder: the page frame and the column.
import { embedded } from "@rhizome/kit";
import { Badge, Button, Skeleton } from "@rhizome/ui";
import { ExternalLink } from "lucide-react";
import type { ReactNode } from "react";

export function Page(props: { title: string; summary: string; toolbar?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex h-screen flex-col">
      <header className="flex items-center gap-3 border-b bg-card px-4 py-2">
        <div className="min-w-0 flex-1">
          <h1 className="font-serif text-lg leading-tight font-bold">{props.title}</h1>
          <p className="truncate text-xs text-muted-foreground">{props.summary}</p>
        </div>
        {props.toolbar}
        {embedded && (
          <Button asChild variant="ghost" size="sm" title="Open this view as a standalone page">
            <a href={window.location.href} target="_blank" rel="noreferrer">
              <ExternalLink /> Standalone
            </a>
          </Button>
        )}
      </header>
      <main className="min-h-0 flex-1 overflow-auto p-4">{props.children}</main>
    </div>
  );
}

export function Column(props: { title: string; count: number; tone?: "default" | "secondary"; children: ReactNode }) {
  return (
    <section className="flex w-80 shrink-0 flex-col rounded-lg border bg-card">
      <h2 className="flex items-center justify-between border-b px-3 py-2 text-sm font-semibold">
        {props.title}
        <Badge variant={props.tone ?? "secondary"}>{props.count}</Badge>
      </h2>
      <div className="flex flex-col divide-y">{props.children}</div>
    </section>
  );
}

export function Loading() {
  return (
    <div className="flex gap-4">
      {[0, 1, 2].map((key) => (
        <Skeleton key={key} className="h-64 w-80" />
      ))}
    </div>
  );
}

export function groupBy<T>(items: T[], key: (item: T) => string): [string, T[]][] {
  const groups = new Map<string, T[]>();
  for (const item of items) groups.set(key(item), [...(groups.get(key(item)) ?? []), item]);
  return [...groups.entries()].sort(([a], [b]) => a.localeCompare(b));
}
