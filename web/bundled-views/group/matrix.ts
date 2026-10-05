// Trace's matrix (SPEC-0111): one row per record of the spine member, each
// row's cells, per-column coverage, lifecycle bands, and, for a hierarchical
// spine, rows nested under their parents with subtree rollups. Pure.
//
// - traceMatrix(model, graph, spine): the whole matrix.
// - visibleRows(roots, collapsed): rows in reading order, skipping the rows
//   nested under collapsed ones.
import { orderedEnumValues, type EnumValueDoc } from "@rhizome/kit";

import {
  buildHierarchy,
  lifecycleRank,
  traceColumns,
  traceRow,
  type LinkGraph,
  type TraceColumn,
  type TraceColumns,
  type TreeNode,
} from "./graph.ts";
import {
  isOutside,
  lifecycleValue,
  linkTargets,
  type GroupModel,
  type GroupRecord,
  type LinkedNote,
  type Member,
  type MemberLink,
} from "./model.ts";

/** A record in a cell: a group record, or a note an outside-group link points to. */
export type MatrixEntry =
  | {
      kind: "record";
      key: string;
      record: GroupRecord;
      /** Linked with the row's own record rather than reached through another record. */
      direct: boolean;
    }
  | { kind: "note"; key: string; note: LinkedNote };

type ColumnFacts = {
  id: string;
  /** Rows whose own cell holds at least one record. */
  covered: number;
  /** Nothing to show: a member without records, or an outside link no row uses. */
  collapsed: boolean;
};

export type MatrixColumn = ColumnFacts &
  (
    | { kind: "member"; member: Member; relation: TraceColumn["relation"] }
    | { kind: "outside"; link: MemberLink }
  );

export type MatrixRow = {
  record: GroupRecord;
  depth: number;
  /** The row this one is nested under; null at the top level. */
  parent: GroupRecord | null;
  children: readonly MatrixRow[];
  /** Entries by column id. */
  cells: ReadonlyMap<string, readonly MatrixEntry[]>;
  /** Distinct records per column across this row and every row nested under it. */
  subtree: ReadonlyMap<string, number>;
  /** Rows nested under this one at any depth. */
  descendants: number;
  /** Records across the subtree's cells; rows with more come first. */
  connections: number;
};

export type MatrixBand = {
  /** The lifecycle value of the band's top-level rows; null when empty or unbanded. */
  value: string | null;
  /** The value is in the `dropped` stage or collapsed, a closed state the view may fold away. */
  closed: boolean;
  roots: readonly MatrixRow[];
  /** Rows in the band, nested rows included. */
  size: number;
};

export type TraceMatrix = {
  spine: Member;
  /** Columns after the spine: members in Trace column order, then outside links. */
  columns: readonly MatrixColumn[];
  /** Lifecycle bands, most advanced first; one unbanded entry when the spine has no lifecycle. */
  bands: readonly MatrixBand[];
  banded: boolean;
  hierarchical: boolean;
  rowCount: number;
  /** Lifecycle values no spine record holds, in lifecycle order. */
  emptyValues: readonly EnumValueDoc[];
  /** Members with no link path to the spine. */
  unreached: readonly Member[];
  /** Records of member columns that appear in no row, by member. */
  missing: readonly { member: Member; count: number }[];
};

const NO_CELLS: ReadonlyMap<string, readonly MatrixEntry[]> = new Map();

function matrixColumns(model: GroupModel, layout: TraceColumns) {
  const members = layout.members.flatMap((column) => {
    const member = model.memberIndex.get(column.member);

    return member ? [{ id: `member:${member.name}`, member, relation: column.relation }] : [];
  });

  return {
    members,
    outside: layout.outside.map((link) => ({ id: `link:${link.field}:${link.targetType}`, link })),
  };
}

type Grown = { row: MatrixRow; keys: ReadonlyMap<string, ReadonlySet<string>> };

/**
 * The matrix for `spine` rows. Top-level rows are banded by their own
 * lifecycle value, and nested rows stay under their parents whatever their
 * value, so a subtree is never split across bands. Rows and siblings go most
 * advanced first, then most connected, then by title.
 */
export function traceMatrix(model: GroupModel, graph: LinkGraph, spineName: string): TraceMatrix {
  const spine = model.memberIndex.get(spineName);

  if (!spine) throw new Error(`traceMatrix: no member ${spineName}`);
  const layout = traceColumns(model, graph, spineName);
  const { members, outside } = matrixColumns(model, layout);
  const ids = [...members, ...outside].map((column) => column.id);

  const cellsOf = (record: GroupRecord) => {
    const row = traceRow(model, graph, layout, record);
    const cells = new Map<string, MatrixEntry[]>();

    for (const column of members) {
      cells.set(
        column.id,
        (row.get(column.member.name) ?? []).map((entry) => ({
          kind: "record",
          key: entry.record.key,
          record: entry.record,
          direct: entry.direct,
        })),
      );
    }

    for (const column of outside) {
      cells.set(
        column.id,
        // A field typed by a broad interface such as Note can still point at a group record.
        linkTargets(record, column.link).flatMap((note) =>
          isOutside(model, note) ? [{ kind: "note" as const, key: note.key, note }] : [],
        ),
      );
    }

    return cells;
  };

  const cells = new Map(spine.records.map((record) => [record.key, cellsOf(record)]));

  const grow = (node: TreeNode): Grown => {
    const grown = node.children.map(grow);
    const own = cells.get(node.record.key) ?? NO_CELLS;

    const keys = new Map(
      ids.map((id) => [id, new Set((own.get(id) ?? []).map((entry) => entry.key))]),
    );

    for (const child of grown) {
      for (const [id, childKeys] of child.keys) for (const key of childKeys) keys.get(id)?.add(key);
    }

    const subtree = new Map([...keys].map(([id, set]) => [id, set.size]));
    const children = grown.map((child) => child.row);

    return {
      keys,
      row: {
        record: node.record,
        depth: node.depth,
        parent: node.parent,
        children,
        cells: own,
        subtree,
        descendants: children.reduce((count, child) => count + 1 + child.descendants, 0),
        connections: [...subtree.values()].reduce((sum, count) => sum + count, 0),
      },
    };
  };

  const hierarchical = spine.parentField !== null;

  const nodes = hierarchical
    ? buildHierarchy(spine, spine.records)
    : spine.records.map((record) => ({ record, depth: 0, parent: null, children: [] }));

  const ranks = new Map(
    spine.records.map((record) => [
      record.key,
      lifecycleRank(spine.lifecycle, lifecycleValue(spine, record)),
    ]),
  );

  const rank = (row: MatrixRow) => ranks.get(row.record.key) ?? 0;

  const order = (rows: readonly MatrixRow[]): MatrixRow[] =>
    rows
      .map((row) => ({ ...row, children: order(row.children) }))
      .sort(
        (a, b) =>
          rank(a) - rank(b) ||
          b.connections - a.connections ||
          a.record.title.localeCompare(b.record.title),
      );

  const roots = order(nodes.map((node) => grow(node).row));

  const covered = (id: string) =>
    spine.records.filter((record) => (cells.get(record.key)?.get(id)?.length ?? 0) > 0).length;

  const placed = new Set(
    [...cells.values()].flatMap((row) => [...row.values()].flat().map((entry) => entry.key)),
  );

  return {
    spine,
    columns: [
      ...members.map((column): MatrixColumn => ({
        ...column,
        kind: "member",
        covered: covered(column.id),
        collapsed: column.member.records.length === 0,
      })),
      ...outside.map((column): MatrixColumn => {
        const count = covered(column.id);

        return { ...column, kind: "outside", covered: count, collapsed: count === 0 };
      }),
    ],
    bands: spine.lifecycle ? band(spine, roots) : [unbanded(roots)],
    banded: spine.lifecycle !== null,
    hierarchical,
    rowCount: spine.records.length,
    emptyValues: spine.lifecycle
      ? orderedEnumValues(spine.lifecycle.values).filter(
          (value) => !spine.records.some((record) => lifecycleValue(spine, record) === value.name),
        )
      : [],
    unreached: layout.unreached.flatMap((name) => model.memberIndex.get(name) ?? []),
    missing: members.flatMap(({ member }) => {
      const count = member.records.filter((record) => !placed.has(record.key)).length;

      return count > 0 ? [{ member, count }] : [];
    }),
  };
}

const sizeOf = (roots: readonly MatrixRow[]) =>
  roots.reduce((count, row) => count + 1 + row.descendants, 0);

const unbanded = (roots: readonly MatrixRow[]): MatrixBand => ({
  value: null,
  closed: false,
  roots,
  size: sizeOf(roots),
});

/** Group already-ordered top-level rows into bands by their lifecycle value. */
function band(spine: Member, roots: readonly MatrixRow[]): MatrixBand[] {
  const byValue = new Map<string | null, MatrixRow[]>();

  for (const row of roots) {
    const value = lifecycleValue(spine, row.record);
    const rows = byValue.get(value) ?? [];
    rows.push(row);
    byValue.set(value, rows);
  }

  return [...byValue].map(([value, rows]) => {
    const doc = spine.lifecycle?.values.find((entry) => entry.name === value);

    const closed = doc ? doc.stage === "dropped" || doc.collapsed === true : false;

    return { value, closed, roots: rows, size: sizeOf(rows) };
  });
}

/** Rows in reading order, each parent before its children, skipping rows under collapsed keys. */
export function visibleRows(
  roots: readonly MatrixRow[],
  collapsed: ReadonlySet<string>,
): MatrixRow[] {
  return roots.flatMap((row) => [
    row,
    ...(collapsed.has(row.record.key) ? [] : visibleRows(row.children, collapsed)),
  ]);
}
