// The Overview's member table, folder block, and coverage line (SPEC-0117
// US2-US4). Pure.
//
// - memberRows(model, input): the table's rows in display order, with record
//   counts, lifecycle segments, gaps, linked share, links, and issues.
// - lifecycleSummary(stats, values): a lifecycle's segments in value order and
//   its active count.
// - folderLines(rows, summaries): top-level folders holding untyped notes.
// - coverage(members, pairs, summaries): types the map does not explain.
import { orderedEnumValues, type EnumValueDoc, type TypeDoc } from "@rhizome/kit";

import { fieldLabel } from "./model.ts";
import { byCount, type ScopeModel, type ScopeNode } from "./scope.ts";
import {
  UNTYPED,
  type FolderRow,
  type LinkPair,
  type MemberFigures,
  type MemberStats,
  type TypeSummary,
} from "./aggregate.ts";

export type LifecycleSegment = {
  name: string;
  label: string;
  tone: string;
  stage: string | null;
  count: number;
};

export type LifecycleSummary = {
  segments: readonly LifecycleSegment[];
  /** Records in an active-stage value. */
  active: number;
  /** The active value's label, or "active" when several values are active. */
  activeLabel: string;
};

export type GapCell = { field: string; label: string; empty: number; required: boolean };

/** The heading over the ungrouped types at All notes. */
export type HeadingRow = { kind: "heading"; label: string; types: number };

export type MemberRow = HeadingRow | NodeRow;

export type NodeRow = {
  /** `group`: a named group at All notes; `implementor`: an interface's implementing type. */
  kind: "member" | "group" | "implementor";
  node: ScopeNode;
  depth: number;
  /** Records with a link to another member, as a fraction; null without records or for untyped notes. */
  linkedShare: number | null;
  /** Links to other members on a group, every link to another type at All notes. */
  links: number;
  /** Links leaving the group; null at All notes. */
  outside: number | null;
  /** Null when the member has no lifecycle; undefined for rows that show none. */
  lifecycle?: LifecycleSummary | null;
  gaps: readonly GapCell[];
  /** A group row's expansion. */
  expanded?: boolean;
};

export type RowInput = {
  members: MemberFigures;
  pairs: readonly LinkPair[];
  docs: Readonly<Record<string, TypeDoc>>;
  summaries: ReadonlyMap<string, TypeSummary>;
  expanded: ReadonlySet<string>;
};

/** The enum values a member's documentation declares for its lifecycle field. */
function lifecycleValues(node: ScopeNode, docs: Readonly<Record<string, TypeDoc>>, field: string) {
  const owners = node.kind === "interface" ? [node.name] : node.types;

  for (const owner of owners) {
    const declared = docs[owner]?.fields.find(
      (entry) => entry.name === field && entry.kind === "enum",
    );

    if (declared?.enum) return declared.enum.values;
  }

  return [];
}

export function lifecycleSummary(
  stats: MemberStats,
  values: readonly EnumValueDoc[],
): LifecycleSummary | null {
  if (!stats.lifecycle) return null;
  const counts = new Map(stats.lifecycle.values.map((value) => [value.name, value.count]));
  const known = orderedEnumValues(values);

  const docs = [
    ...known,
    ...stats.lifecycle.values
      .filter((value) => !known.some((entry) => entry.name === value.name))
      .map((value): EnumValueDoc => ({ name: value.name })),
  ];

  const segments = docs.flatMap((value) => {
    const count = counts.get(value.name) ?? 0;

    return count
      ? [
          {
            name: value.name,
            label: value.label ?? value.name,
            tone: value.tone ?? "neutral",
            stage: value.stage ?? null,
            count,
          },
        ]
      : [];
  });

  const active = segments.filter((segment) => segment.stage === "active");

  return {
    segments,
    active: active.reduce((sum, segment) => sum + segment.count, 0),
    activeLabel: active.length === 1 ? active[0].label.toLowerCase() : "active",
  };
}

function gapCells(node: ScopeNode, docs: Readonly<Record<string, TypeDoc>>): GapCell[] {
  const owners = node.kind === "interface" ? [node.name] : node.types;

  const required = (field: string) =>
    owners.some((owner) =>
      docs[owner]?.fields.some((entry) => entry.name === field && entry.required === true),
    );

  return (node.stats?.gaps ?? [])
    .filter((gap) => gap.empty > 0)
    .map((gap) => ({
      field: gap.field,
      label: fieldLabel(gap.field),
      empty: gap.empty,
      required: required(gap.field),
    }));
}

/**
 * Records of `node` with a link to another member, by the target sets of its
 * types. `isOther(source, target)` says whether a link from a record of
 * `source` to `target` counts.
 */
function linkedRecords(
  node: ScopeNode,
  members: MemberFigures,
  isOther: (source: string, target: string) => boolean,
) {
  let linked = 0;

  for (const type of node.types) {
    for (const set of members.types.get(type)?.targetSets ?? []) {
      if (set.types.some((target) => isOther(type, target))) linked += set.records;
    }
  }

  return linked;
}

/** Links from `types` to inside and outside `scope`, each pair once, own links left out. */
function linkTotals(
  types: ReadonlySet<string>,
  scope: ReadonlySet<string>,
  pairs: readonly LinkPair[],
) {
  let inside = 0;
  let outside = 0;

  for (const pair of pairs) {
    const fromA = types.has(pair.a);

    if (fromA === types.has(pair.b)) continue;
    const other = fromA ? pair.b : pair.a;

    if (scope.has(other)) inside += pair.links;
    else outside += pair.links;
  }

  return { inside, outside };
}

function row(
  model: ScopeModel,
  node: ScopeNode,
  input: RowInput,
  kind: NodeRow["kind"],
  depth: number,
): NodeRow {
  const own = new Set(node.types);
  const workspace = model.scope.kind === "workspace";

  // At All notes every other note type counts, even one the same member holds.
  const isOther = (source: string, target: string) =>
    workspace
      ? target !== source && !input.summaries.get(target)?.embedded
      : !own.has(target) && model.types.has(target);

  const scope = workspace
    ? new Set([...model.types, UNTYPED].filter((type) => !own.has(type)))
    : model.types;

  const totals = linkTotals(own, scope, input.pairs);

  const records = node.types.reduce(
    (sum, type) => sum + (input.members.types.get(type)?.count ?? 0),
    0,
  );

  return {
    kind,
    node,
    depth,
    linkedShare:
      node.kind === "untyped" || records === 0
        ? null
        : linkedRecords(node, input.members, isOther) / records,
    links:
      node.kind === "untyped"
        ? input.members.untyped.links
        : workspace
          ? totals.inside + totals.outside
          : totals.inside,
    outside: workspace ? null : totals.outside,
    lifecycle:
      node.stats && kind !== "implementor"
        ? lifecycleSummary(
            node.stats,
            lifecycleValues(node, input.docs, node.stats.lifecycle?.field ?? ""),
          )
        : undefined,
    gaps: kind === "implementor" ? [] : gapCells(node, input.docs),
  };
}

/** An interface member's implementing types, most records first. */
function implementorRows(model: ScopeModel, node: ScopeNode, input: RowInput, depth: number) {
  if (node.kind !== "interface") return [];

  return node.types
    .map((type): ScopeNode => ({
      ...node,
      id: `${node.id}/${type}`,
      kind: "type",
      name: type,
      label: input.summaries.get(type)?.pluralLabel ?? type,
      types: [type],
      count: input.members.types.get(type)?.count ?? 0,
      stats: input.members.types.get(type) ?? null,
    }))
    .sort(byCount)
    .map((type) => row(model, type, input, "implementor", depth));
}

const memberWithImplementors = (
  model: ScopeModel,
  node: ScopeNode,
  input: RowInput,
  depth: number,
) => [row(model, node, input, "member", depth), ...implementorRows(model, node, input, depth + 1)];

/**
 * The member table's rows. On a group: each member, most records first, an
 * interface followed by its implementing types. At All notes: each named
 * group, expanded into its members when open, then the ungrouped types under
 * a heading, then untyped notes.
 */
export function memberRows(model: ScopeModel, input: RowInput): MemberRow[] {
  if (model.scope.kind === "group")
    return model.nodes.flatMap((node) => memberWithImplementors(model, node, input, 0));

  const rows: MemberRow[] = [];

  for (const section of model.sections) {
    if (section.group) {
      const expanded = input.expanded.has(section.group.name);
      rows.push({ ...row(model, section.group, input, "group", 0), expanded });

      if (expanded)
        rows.push(
          ...section.members.flatMap((node) => memberWithImplementors(model, node, input, 1)),
        );
      continue;
    }

    rows.push({ kind: "heading", label: "No group", types: section.members.length });
    rows.push(...section.members.flatMap((node) => memberWithImplementors(model, node, input, 1)));
  }

  if (model.untyped) rows.push(row(model, model.untyped, input, "member", 0));

  return rows;
}

export type FolderLine = {
  folder: string;
  untyped: number;
  total: number;
  /** Untyped notes as a fraction of the folder. */
  share: number;
  /** The two types its untyped notes link to most. */
  linksTo: readonly { type: string; label: string; count: number }[];
};

/** Top-level folders with untyped notes, most untyped first. */
export function folderLines(
  rows: readonly FolderRow[],
  summaries: ReadonlyMap<string, TypeSummary>,
): FolderLine[] {
  const isNoteType = (type: string) => type !== UNTYPED && !summaries.get(type)?.embedded;

  return rows
    .flatMap((entry) =>
      entry.untyped > 0
        ? [
            {
              folder: entry.folder,
              untyped: entry.untyped,
              total: entry.total,
              share: entry.total ? entry.untyped / entry.total : 0,
              linksTo: [...entry.untypedLinksTo]
                .flatMap(([type, count]) =>
                  isNoteType(type)
                    ? [{ type, label: summaries.get(type)?.pluralLabel ?? type, count }]
                    : [],
                )
                .sort((a, b) => b.count - a.count || a.type.localeCompare(b.type))
                .slice(0, 2),
            },
          ]
        : [],
    )
    .sort((a, b) => b.untyped - a.untyped || a.folder.localeCompare(b.folder));
}

export type CoverageEntry = { name: string; label: string; count: number };

export type Coverage = {
  /** Note types with no records. */
  empty: readonly CoverageEntry[];
  /** Note types with records and no links at all. */
  unlinked: readonly CoverageEntry[];
  /** Embedded types, which the map does not draw. */
  embedded: readonly CoverageEntry[];
};

/** What the map leaves out: empty and unlinked note types, and embedded types. */
export function coverage(
  members: MemberFigures,
  pairs: readonly LinkPair[],
  summaries: ReadonlyMap<string, TypeSummary>,
): Coverage {
  const linked = new Set(pairs.flatMap((pair) => (pair.links > 0 ? [pair.a, pair.b] : [])));

  const entry = (name: string, count: number) => [
    { name, label: summaries.get(name)?.pluralLabel ?? name, count },
  ];

  const types = [...members.types.values()].filter((type) => !summaries.get(type.name)?.embedded);
  const byLabel = (a: CoverageEntry, b: CoverageEntry) => a.label.localeCompare(b.label);

  return {
    empty: types.flatMap((type) => (type.count === 0 ? entry(type.name, 0) : [])).sort(byLabel),
    unlinked: types
      .flatMap((type) =>
        type.count > 0 && !linked.has(type.name) ? entry(type.name, type.count) : [],
      )
      .sort(byLabel),
    embedded: [...summaries.values()]
      .flatMap((summary) => (summary.embedded ? entry(summary.name, summary.count) : []))
      .sort(byLabel),
  };
}
