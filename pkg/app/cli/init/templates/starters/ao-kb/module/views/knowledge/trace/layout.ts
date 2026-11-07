import { edgeKey, type Trace, type TraceEdge, type Visit } from "./model.ts";

export const COLUMN_CAP = 6;
export type Column = {
  key: string;
  title: string;
  hop: number;
  all: Visit[];
  shown: Visit[];
  hidden: Visit[];
  orphan: number;
};
export function columns(trace: Trace, up: number, down: number, expanded: Set<string>) {
  const definitions: Column[] = [];
  for (let hop = up; hop >= 1; hop--)
    definitions.push({
      key: `u${hop}`,
      title: `Upstream · hop ${hop}`,
      hop,
      all: [],
      shown: [],
      hidden: [],
      orphan: 0,
    });
  definitions.push({
    key: "f",
    title: "Focus and related",
    hop: 0,
    all: [],
    shown: [],
    hidden: [],
    orphan: 0,
  });
  for (let hop = 1; hop <= down; hop++)
    definitions.push({
      key: `d${hop}`,
      title: `Downstream · hop ${hop}`,
      hop,
      all: [],
      shown: [],
      hidden: [],
      orphan: 0,
    });
  const shown = new Set([trace.focus.path]);
  const rank = new Map([[trace.focus.path, 0]]);
  for (const column of [...definitions].sort((a, b) => a.hop - b.hop)) {
    column.all = [...trace.nodes.values()].filter(
      (n) =>
        n.lane !== "focus" &&
        (n.lane === "related" ? "f" : `${n.lane === "up" ? "u" : "d"}${n.hop}`) === column.key,
    );
    const reachable = column.all.filter((n) => n.parent && shown.has(n.parent));
    column.orphan = column.all.length - reachable.length;
    reachable.sort((a, b) => (rank.get(a.parent ?? "") ?? 0) - (rank.get(b.parent ?? "") ?? 0));
    const parents = new Map<string, Visit[]>();
    for (const n of reachable) {
      const key = n.parent ?? "";
      parents.set(key, [...(parents.get(key) ?? []), n]);
    }
    const picked: Visit[] = [];
    for (let i = 0; picked.length < reachable.length; i++) {
      for (const group of parents.values()) if (group[i]) picked.push(group[i]);
    }
    const chosen = new Set(expanded.has(column.key) ? picked : picked.slice(0, COLUMN_CAP));
    column.shown = reachable.filter((n) => chosen.has(n));
    column.hidden = reachable.filter((n) => !chosen.has(n));
    column.shown.forEach((n, i) => {
      shown.add(n.record.path);
      rank.set(n.record.path, i);
    });
  }
  return { columns: definitions.filter((c) => c.hop <= 1 || c.all.length), shown };
}
export function edgeRole(edge: TraceEdge, trace: Trace, shown: Set<string>) {
  if (!shown.has(edge.from) || !shown.has(edge.to)) return "Record behind Show more";
  const visit = [...trace.nodes.values()].find((n) => n.edge && edgeKey(n.edge) === edgeKey(edge));
  if (!visit) return "Second path";
  return visit.lane === "related"
    ? "Related to the focus"
    : `${visit.lane === "up" ? "Upstream" : "Downstream"}, hop ${visit.hop}`;
}
export function hiddenTypes(hidden: Visit[]) {
  const counts = new Map<string, number>();
  for (const n of hidden)
    counts.set(
      n.record.__typename.toLowerCase(),
      (counts.get(n.record.__typename.toLowerCase()) ?? 0) + 1,
    );
  return [...counts].map(([type, n]) => `${n} ${type}${n === 1 ? "" : "s"}`).join(", ");
}
