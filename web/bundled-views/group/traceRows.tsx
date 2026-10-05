// Trace's row groups: a lifecycle band of rows, each row's spine cell with its
// nesting controls, and the record chips in its cells.
import { RecordChip, StatusMark, openNote } from "@rhizome/kit";
import { useMemo, useState } from "react";

import { Dash, IssueCount, RecordMark, RecordTitle, openRecord } from "./components.tsx";
import {
  visibleRows,
  type MatrixBand,
  type MatrixColumn,
  type MatrixEntry,
  type MatrixRow,
  type TraceMatrix,
} from "./matrix.ts";
import { fieldLabel, keyText, memberLabel, type GroupModel } from "./model.ts";
import { useRememberedOpen } from "./remembered.ts";

/** Rows shown per band before "Show more". */
export const BAND_ROWS = 50;

/** Records shown per cell before "+N more". */
export const CELL_RECORDS = 6;

/** Indentation per level of nesting, in pixels. */
const INDENT = 16;

/** A label inside a sentence: "Work items" reads "work items", "IP ideas" stays. */
export const inSentence = (label: string) =>
  /^[A-Z][a-z]/.test(label) ? label.charAt(0).toLowerCase() + label.slice(1) : label;

function Entry({ model, entry }: { model: GroupModel; entry: MatrixEntry }) {
  if (entry.kind === "note") {
    return (
      <RecordChip
        recordKey={entry.key}
        mark={<span className="gv-out-dot" aria-hidden="true" />}
        title={entry.note.title}
        onOpen={() => openNote(entry.note.path)}
        wrap
        className="gv-chip"
      >
        {entry.note.title}
      </RecordChip>
    );
  }

  const member = model.memberIndex.get(entry.record.member);

  return (
    <RecordChip
      recordKey={entry.key}
      mark={member && <RecordMark member={member} record={entry.record} />}
      indirect={!entry.direct}
      title={entry.record.title}
      onOpen={() => openRecord(entry.record)}
      wrap
      className="gv-chip"
    >
      {entry.record.title}
    </RecordChip>
  );
}

function Cell({
  model,
  matrix,
  row,
  column,
}: {
  model: GroupModel;
  matrix: TraceMatrix;
  row: MatrixRow;
  column: MatrixColumn;
}) {
  const [all, setAll] = useState(false);
  const entries = row.cells.get(column.id) ?? [];
  const total = row.subtree.get(column.id) ?? 0;
  const rolled = row.children.length > 0 && total > entries.length;

  if (!entries.length && !rolled) return <Dash />;
  const shown = all ? entries : entries.slice(0, CELL_RECORDS);
  const spineLabel = memberLabel(model, matrix.spine, { count: row.descendants });

  return (
    <>
      {shown.map((entry) => (
        <Entry key={entry.key} model={model} entry={entry} />
      ))}
      {shown.length < entries.length && (
        <button type="button" className="gv-trace-more" onClick={() => setAll(true)}>
          +{entries.length - shown.length} more
        </button>
      )}
      {rolled && (
        <div
          className="gv-rollup"
          title={`${total} across ${row.record.title} and the ${row.descendants} ${inSentence(spineLabel)} nested under it`}
        >
          <b>{total}</b> in subtree
        </div>
      )}
    </>
  );
}

function Row({
  model,
  matrix,
  row,
  collapsed,
  onToggle,
}: {
  model: GroupModel;
  matrix: TraceMatrix;
  row: MatrixRow;
  collapsed: boolean;
  onToggle: (key: string) => void;
}) {
  const { record, depth, parent } = row;
  const { spine } = matrix;
  const key = keyText(spine, record);
  const offset = depth * INDENT + (matrix.hierarchical ? INDENT : 0) + 18;

  return (
    <tr data-record-key={record.key} data-depth={depth || undefined}>
      <th scope="row" className="gv-trace-spine">
        <div className="gv-title" style={{ paddingLeft: depth * INDENT }}>
          {row.children.length > 0 ? (
            <button
              type="button"
              className="gv-twisty"
              aria-expanded={!collapsed}
              aria-label={`Rows under ${record.title}`}
              onClick={() => onToggle(record.key)}
            >
              <span aria-hidden="true">{collapsed ? "▸" : "▾"}</span>
            </button>
          ) : (
            matrix.hierarchical && <span className="gv-twisty-slot" aria-hidden="true" />
          )}
          <RecordMark member={spine} record={record} />
          <RecordTitle record={record} />
          {parent && <span className="sr-only">, under {parent.title}</span>}
          {collapsed && row.descendants > 0 && (
            <span className="gv-nested">{row.descendants} nested</span>
          )}
          <IssueCount record={record} />
        </div>
        {record.summary && (
          <div className="gv-summary" style={{ marginLeft: offset }}>
            {record.summary}
          </div>
        )}
        {key && (
          <div className="gv-kv" style={{ marginLeft: offset }}>
            <em>{key.label}</em>
            <span>{key.text}</span>
          </div>
        )}
      </th>
      {matrix.columns.map((column) => (
        <td key={column.id} className={column.kind === "outside" ? "gv-outcol" : undefined}>
          {!column.collapsed && <Cell model={model} matrix={matrix} row={row} column={column} />}
        </td>
      ))}
    </tr>
  );
}

function BandLabel({ matrix, band }: { matrix: TraceMatrix; band: MatrixBand }) {
  const lifecycle = matrix.spine.lifecycle;

  if (!lifecycle) return null;

  if (band.value === null) return <span>No {fieldLabel(lifecycle.field).toLowerCase()}</span>;

  return <StatusMark value={band.value} values={lifecycle.values} />;
}

export function Band({
  model,
  matrix,
  band,
  folded,
}: {
  model: GroupModel;
  matrix: TraceMatrix;
  band: MatrixBand;
  folded: boolean;
}) {
  const bands = useRememberedOpen("trace.bands", matrix.spine.name);
  const tree = useRememberedOpen("trace.rows", matrix.spine.name);
  const bandKey = band.value ?? "";
  const open = bands.isOpen(bandKey, !folded);
  const setOpen = (next: boolean) => bands.setOpen(bandKey, next);
  const [limit, setLimit] = useState(BAND_ROWS);

  const collapsed = useMemo<ReadonlySet<string>>(
    () => new Set(Object.keys(tree.choices).filter((key) => !tree.choices[key])),
    [tree.choices],
  );

  const rows = useMemo(() => visibleRows(band.roots, collapsed), [band.roots, collapsed]);
  const span = matrix.columns.length + 1;
  const hidden = rows.length - limit;
  const toggle = (key: string) => tree.setOpen(key, collapsed.has(key));

  return (
    <tbody>
      {matrix.banded && (
        <tr className="gv-band">
          <th scope="rowgroup" colSpan={span}>
            <button
              type="button"
              className="gv-band-toggle"
              aria-expanded={open}
              onClick={() => setOpen(!open)}
            >
              <span className="gv-caret" aria-hidden="true">
                {open ? "▾" : "▸"}
              </span>
              <BandLabel matrix={matrix} band={band} />
              <span className="gv-num">{band.size}</span>
            </button>
          </th>
        </tr>
      )}
      {open &&
        rows
          .slice(0, limit)
          .map((row) => (
            <Row
              key={row.record.key}
              model={model}
              matrix={matrix}
              row={row}
              collapsed={collapsed.has(row.record.key)}
              onToggle={toggle}
            />
          ))}
      {open && hidden > 0 && (
        <tr className="gv-more-row">
          <td colSpan={span}>
            <button type="button" className="gv-link" onClick={() => setLimit(limit + BAND_ROWS)}>
              Show {Math.min(hidden, BAND_ROWS)} more
            </button>
            {hidden > BAND_ROWS && <span className="gv-faint"> · {hidden} not shown</span>}
          </td>
        </tr>
      )}
    </tbody>
  );
}
