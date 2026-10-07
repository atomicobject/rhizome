// How a group's members connect, and the structures built on those links:
// Trace's spine, columns, and row cells, record trees, and Sections' order,
// rows, and columns (SPEC-0111). Pure.
//
// - linkGraph(model): member-level and record-level links.
// - defaultSpine(model, graph), spineChoices(model, graph): Trace's rows.
// - traceColumns(model, graph, spine), traceRow(model, graph, columns, record).
// - lifecycleRank(lifecycle, value), compareAdvanced(member): "most advanced first".
// - buildHierarchy(member, records), flattenTree(roots), subtreeRecords(node).
// - sectionOrder(model, graph), sectionRows(member), sectionColumns(member, rows).
import { isTerminalValue, orderedEnumValues } from "@rhizome/kit";

import {
  fieldLabel,
  isEmptyValue,
  lifecycleValue,
  linkTargets,
  recordField,
  type GroupModel,
  type GroupRecord,
  type Lifecycle,
  type Member,
  type MemberField,
  type MemberLink,
} from "./model.ts";

/** One forward link from a record to a record of another member. */
export type RecordEdge = { from: string; to: string; field: string };

export type LinkGraph = {
  /** Members each member declares links to, itself excluded. */
  outgoing: ReadonlyMap<string, ReadonlySet<string>>;
  /** Members declaring links to each member. */
  incoming: ReadonlyMap<string, ReadonlySet<string>>;
  edges: readonly RecordEdge[];
  /** Record keys each record is linked with, in either direction. */
  adjacent: ReadonlyMap<string, ReadonlySet<string>>;
  /** Members that link to no other member; Trace never expands through them. */
  referenceTypes: ReadonlySet<string>;
  /** Some member links to another. */
  linked: boolean;
};

const empty: ReadonlySet<string> = new Set();

function addTo(map: Map<string, Set<string>>, key: string, value: string) {
  const set = map.get(key) ?? new Set<string>();
  set.add(value);
  map.set(key, set);
}

/** Links between members whose declared target is another member, and the record links they carry. */
export function linkGraph(model: GroupModel): LinkGraph {
  const outgoing = new Map<string, Set<string>>();
  const incoming = new Map<string, Set<string>>();
  const adjacent = new Map<string, Set<string>>();
  const edges: RecordEdge[] = [];

  for (const member of model.members) {
    for (const link of member.links) {
      if (link.target === null || link.target === member.name) continue;
      addTo(outgoing, member.name, link.target);
      addTo(incoming, link.target, member.name);

      for (const record of member.records) {
        for (const note of linkTargets(record, link)) {
          const target = model.records.get(note.key);

          if (!target || target.member === member.name) continue;
          edges.push({ from: record.key, to: target.key, field: link.field });
          addTo(adjacent, record.key, target.key);
          addTo(adjacent, target.key, record.key);
        }
      }
    }
  }

  return {
    outgoing,
    incoming,
    edges,
    adjacent,
    referenceTypes: new Set(
      model.members.flatMap((member) => (outgoing.has(member.name) ? [] : [member.name])),
    ),
    linked: outgoing.size > 0,
  };
}

const out = (graph: LinkGraph, member: string) => graph.outgoing.get(member) ?? empty;

const into = (graph: LinkGraph, member: string) => graph.incoming.get(member) ?? empty;

/** Distinct members linked with `member` in either direction. */
export const linkedMembers = (graph: LinkGraph, member: string) =>
  new Set([...out(graph, member), ...into(graph, member)]);

function crossLinks(model: GroupModel, graph: LinkGraph, member: string) {
  const isMember = (key: string) => model.records.get(key)?.member === member;

  return graph.edges.filter((edge) => isMember(edge.from) || isMember(edge.to)).length;
}

/**
 * Trace's default rows: the hierarchical member another member links to, else
 * the member linked with the most other members, ties going to the one with
 * more record links. Only members with records qualify.
 */
export function defaultSpine(model: GroupModel, graph: LinkGraph): string | null {
  const candidates = model.members.filter(
    (member) => member.records.length > 0 && linkedMembers(graph, member.name).size > 0,
  );

  const hierarchical = candidates.find(
    (member) => member.parentField !== null && into(graph, member.name).size > 0,
  );

  if (hierarchical) return hierarchical.name;

  const score = (member: Member) =>
    [linkedMembers(graph, member.name).size, crossLinks(model, graph, member.name)] as const;

  let best: { name: string; score: readonly [number, number] } | null = null;

  for (const member of candidates) {
    const next = score(member);

    if (
      !best ||
      next[0] > best.score[0] ||
      (next[0] === best.score[0] && next[1] > best.score[1])
    ) {
      best = { name: member.name, score: next };
    }
  }

  return best?.name ?? null;
}

/**
 * Members Trace can use as rows: every linked member with records, the default
 * first, then the rest in its column order, then members its rows never reach.
 */
export function spineChoices(model: GroupModel, graph: LinkGraph): string[] {
  const spine = defaultSpine(model, graph);

  if (spine === null) return [];
  const columns = traceColumns(model, graph, spine);
  const names = [spine, ...columns.members.map((column) => column.member), ...columns.unreached];

  return names.filter((name) => {
    const records = model.memberIndex.get(name)?.records.length ?? 0;

    return records > 0 && linkedMembers(graph, name).size > 0;
  });
}

export type TraceColumn = {
  member: string;
  /** Links between this member and the spine, counted in members. */
  distance: number;
  /** The spine links to it, it links to the spine, or it is reached through other columns. */
  relation: "linked-from-rows" | "links-to-rows" | "through";
};

export type TraceColumns = {
  spine: string;
  /** Member columns after the spine, in display order. */
  members: readonly TraceColumn[];
  /** The spine's link fields whose declared target is outside the group. */
  outside: readonly MemberLink[];
  /** Members with no link path to the spine. */
  unreached: readonly string[];
};

/**
 * The longest chain of outgoing member links from each member, never revisiting
 * a member on the chain.
 */
function depths(model: GroupModel, graph: LinkGraph) {
  // ponytail: exhaustive search without a memo, so link cycles cannot make the
  // result depend on member order; fine for the handful of members a group
  // has, memoize per (member, visited set) if groups grow large.
  const depth = (name: string, seen: ReadonlySet<string>): number => {
    let longest = 0;

    for (const next of out(graph, name)) {
      if (!seen.has(next)) longest = Math.max(longest, 1 + depth(next, new Set([...seen, next])));
    }

    return longest;
  };

  return new Map(
    model.members.map((member) => [member.name, depth(member.name, new Set([member.name]))]),
  );
}

/**
 * Trace's columns for `spine`: the members the spine links to in the order it
 * declares those fields, then the other members by link distance and then
 * depth, then the spine's links to types outside the group. Distance never
 * passes through a reference type, since rows never expand through one; a
 * member reachable only that way is unreached.
 */
export function traceColumns(model: GroupModel, graph: LinkGraph, spine: string): TraceColumns {
  const distance = new Map([[spine, 0]]);
  const queue = [spine];

  for (let name = queue.shift(); name !== undefined; name = queue.shift()) {
    const reached = distance.get(name) ?? 0;

    // A row never expands through a reference type, so neither do columns.
    if (name !== spine && graph.referenceTypes.has(name)) continue;

    for (const next of linkedMembers(graph, name)) {
      if (distance.has(next)) continue;
      distance.set(next, reached + 1);
      queue.push(next);
    }
  }

  const spineMember = model.memberIndex.get(spine);

  const declared = [
    ...new Set(
      (spineMember?.links ?? []).flatMap((link) =>
        link.target !== null && link.target !== spine ? [link.target] : [],
      ),
    ),
  ];

  const depth = depths(model, graph);

  const rank = (name: string) =>
    declared.includes(name) ? declared.indexOf(name) : declared.length + (depth.get(name) ?? 0);

  const reached = model.members.flatMap((member) =>
    member.name !== spine && distance.has(member.name) ? [member.name] : [],
  );

  reached.sort((a, b) => (distance.get(a) ?? 0) - (distance.get(b) ?? 0) || rank(a) - rank(b));

  return {
    spine,
    members: reached.map((name) => ({
      member: name,
      distance: distance.get(name) ?? 0,
      relation: out(graph, spine).has(name)
        ? "linked-from-rows"
        : into(graph, spine).has(name)
          ? "links-to-rows"
          : "through",
    })),
    outside: (spineMember?.links ?? []).filter((link) => link.target === null),
    unreached: model.members.flatMap((member) => (distance.has(member.name) ? [] : [member.name])),
  };
}

export type TraceCellEntry = {
  record: GroupRecord;
  /** Linked with the row's own record rather than reached through another record. */
  direct: boolean;
};

const byTitle = (a: GroupRecord, b: GroupRecord) => a.title.localeCompare(b.title);

/**
 * One Trace row's cells by member: the records of each column linked with
 * anything already placed in the row, repeated until nothing changes. A column
 * fills only through the row and columns no farther from the spine, so a
 * record never arrives back through a farther column. Records of reference
 * types are placed but never expanded through.
 */
export function traceRow(
  model: GroupModel,
  graph: LinkGraph,
  columns: TraceColumns,
  row: GroupRecord,
): Map<string, TraceCellEntry[]> {
  /** Placed records to expand from, with the distance of the column holding them. */
  const placed = new Map([[row.key, 0]]);
  const direct = graph.adjacent.get(row.key) ?? empty;
  const found = new Map(columns.members.map((column) => [column.member, new Set<string>()]));

  for (let grew = true; grew;) {
    grew = false;

    for (const { member, distance } of columns.members) {
      const cell = found.get(member) ?? new Set<string>();

      for (const [key, from] of placed) {
        if (from > distance) continue;

        for (const next of graph.adjacent.get(key) ?? empty) {
          if (cell.has(next) || model.records.get(next)?.member !== member) continue;
          cell.add(next);
          grew = true;

          if (!graph.referenceTypes.has(member)) placed.set(next, distance);
        }
      }
    }
  }

  const cells = new Map<string, TraceCellEntry[]>();

  for (const [member, keys] of found) {
    const entries = [...keys].flatMap((key) => {
      const record = model.records.get(key);

      return record ? [{ record, direct: direct.has(key) }] : [];
    });

    entries.sort((a, b) => Number(b.direct) - Number(a.direct) || byTitle(a.record, b.record));
    cells.set(member, entries);
  }

  return cells;
}

/**
 * A value's place in "most advanced first" order: non-terminal values from the
 * last to the first, then terminal values in order, then empty or unknown.
 */
export function lifecycleRank(lifecycle: Lifecycle | null, value: string | null) {
  return rankOf(advancedOrder(lifecycle), value);
}

/** Value names in "most advanced first" order. */
function advancedOrder(lifecycle: Lifecycle | null) {
  if (!lifecycle) return [];
  const ordered = orderedEnumValues(lifecycle.values);
  const active = ordered.filter((entry) => !isTerminalValue(entry)).reverse();

  return [...active, ...ordered.filter(isTerminalValue)].map((entry) => entry.name);
}

function rankOf(sequence: readonly string[], value: string | null) {
  const index = value === null ? -1 : sequence.indexOf(value);

  return index < 0 ? sequence.length : index;
}

/** Most advanced first, then most recently changed, then by title. */
export function compareAdvanced(member: Member) {
  const sequence = advancedOrder(member.lifecycle);
  const rank = (record: GroupRecord) => rankOf(sequence, lifecycleValue(member, record));

  return (a: GroupRecord, b: GroupRecord) =>
    rank(a) - rank(b) || (b.updatedAt ?? 0) - (a.updatedAt ?? 0) || byTitle(a, b);
}

export type TreeNode = {
  record: GroupRecord;
  depth: number;
  /** The record this one is nested under; null for a root. */
  parent: GroupRecord | null;
  children: TreeNode[];
};

/**
 * Records nested under their parents through the member's PARENT-role field,
 * siblings keeping the order of `records`. A record whose parent is missing
 * is a root. A parent cycle has no root above it, so the walk starts it from
 * a record on the cycle, keeping any record that hangs off the cycle beneath.
 */
export function buildHierarchy(member: Member, records: readonly GroupRecord[]): TreeNode[] {
  const byKey = new Map(records.map((record) => [record.key, record]));
  const children = new Map<string, GroupRecord[]>();

  const parentOf = (record: GroupRecord) => {
    if (member.parentField === null) return null;
    const key = record.links.get(member.parentField)?.[0]?.key;
    const parent = key === undefined ? undefined : byKey.get(key);

    return parent && parent.key !== record.key ? parent : null;
  };

  for (const record of records) {
    const parent = parentOf(record);

    if (parent) children.set(parent.key, [...(children.get(parent.key) ?? []), record]);
  }

  const visited = new Set<string>();

  const grow = (record: GroupRecord, depth: number, parent: GroupRecord | null): TreeNode => {
    visited.add(record.key);

    const kids = (children.get(record.key) ?? []).flatMap((child) =>
      visited.has(child.key) ? [] : [grow(child, depth + 1, record)],
    );

    return { record, depth, parent, children: kids };
  };

  const roots = records.flatMap((record) => (parentOf(record) ? [] : [grow(record, 0, null)]));

  for (const record of records) {
    if (visited.has(record.key)) continue;
    // Climb until the chain repeats; the record that closes it is on the cycle.
    const climbed = new Set([record.key]);
    let start = record;

    for (
      let parent = parentOf(start);
      parent && !visited.has(parent.key);
      parent = parentOf(start)
    ) {
      start = parent;

      if (climbed.has(parent.key)) break;
      climbed.add(parent.key);
    }

    roots.push(grow(start, 0, null));
  }

  return roots;
}

/** Tree nodes in reading order: each parent, then its children. */
export function flattenTree(roots: readonly TreeNode[]): TreeNode[] {
  return roots.flatMap((node) => [node, ...flattenTree(node.children)]);
}

/** A node's record and every record below it. */
export function subtreeRecords(node: TreeNode): GroupRecord[] {
  return [node.record, ...node.children.flatMap(subtreeRecords)];
}

/** Sections' member order: like Trace's columns when the group has links, otherwise by record count. */
export function sectionOrder(model: GroupModel, graph: LinkGraph): Member[] {
  const spine = graph.linked ? defaultSpine(model, graph) : null;

  if (spine === null) return [...model.members].sort((a, b) => b.count - a.count);
  const columns = traceColumns(model, graph, spine);
  const names = [spine, ...columns.members.map((column) => column.member), ...columns.unreached];

  return names.flatMap((name) => model.memberIndex.get(name) ?? []);
}

/** A member's rows: most advanced first, then newest, nested under parents for a hierarchical member. */
export function sectionRows(member: Member): TreeNode[] {
  const sorted = [...member.records].sort(compareAdvanced(member));

  return member.parentField === null
    ? sorted.map((record) => ({ record, depth: 0, parent: null, children: [] }))
    : flattenTree(buildHierarchy(member, sorted));
}

export type SectionColumn =
  | { kind: "type" }
  | { kind: "lifecycle"; field: string }
  | {
      kind: "field";
      field: MemberField;
      /** Unique within the member even when implementors declare one name differently. */
      id: string;
      label: string;
    };

const COLUMN_KINDS: ReadonlySet<MemberField["kind"]> = new Set(["scalar", "enum", "link"]);

/**
 * A member's table columns after the title: the implementing type for an
 * interface, the lifecycle, and KEY fields. A column empty on every shown row
 * is omitted, except the lifecycle.
 */
export function sectionColumns(member: Member, shown: readonly GroupRecord[]): SectionColumn[] {
  const columns: SectionColumn[] = member.kind === "interface" ? [{ kind: "type" }] : [];

  if (member.lifecycle) columns.push({ kind: "lifecycle", field: member.lifecycle.field });

  const fields = member.fields.filter(
    (field) =>
      field.key &&
      COLUMN_KINDS.has(field.kind) &&
      field.name !== member.lifecycle?.field &&
      field.name !== member.summaryField &&
      shown.some((record) => !isEmptyValue(recordField(record, field))),
  );

  for (const field of fields) {
    // Implementors declaring one name differently get one column each, named by type.
    const shared = fields.filter((other) => other.name === field.name).length > 1;
    const label = fieldLabel(field.name);

    columns.push({
      kind: "field",
      field,
      id: `field:${field.name}:${field.declaredBy.join(",")}`,
      label: shared ? `${label} (${field.declaredBy.map(fieldLabel).join(", ")})` : label,
    });
  }

  return columns;
}
