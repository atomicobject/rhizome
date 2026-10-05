import type { ViewCatalogEntry } from "../api/types";

export type StandaloneViewGroup = { group: string; views: ViewCatalogEntry[] };

export function groupStandaloneViews(views: ViewCatalogEntry[]): StandaloneViewGroup[] {
  const sorted = views
    .filter((view) => view.mount?.kind === "standalone" && !view.mount.hidden)
    .sort(
      (a, b) =>
        (a.mount?.group || "").localeCompare(b.mount?.group || "") ||
        (a.mount?.order ?? 0) - (b.mount?.order ?? 0) ||
        a.name.localeCompare(b.name),
    );

  const groups: StandaloneViewGroup[] = [];

  for (const view of sorted) {
    const group = view.mount?.group || "Views";
    const current = groups[groups.length - 1];

    if (current?.group === group) current.views.push(view);
    else groups.push({ group, views: [view] });
  }

  return groups;
}
