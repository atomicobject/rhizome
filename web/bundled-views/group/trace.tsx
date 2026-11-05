// Trace (SPEC-0111): records of one member (the spine) as rows, with every
// record they connect to across the group as columns. Rows band by lifecycle
// and nest under parents; pointing at a record lights every row it appears in.
import {
  Segmented,
  HighlightProvider,
  StatusMark,
  orderedEnumValues,
  openCollection,
  useViewPreference,
  type ViewModuleProps,
} from "@rhizome/kit";
import { useMemo } from "react";

import { isString } from "./api.ts";
import { GroupFacts, GroupPage, MemberHeading } from "./components.tsx";
import { linkGraph, spineChoices, type LinkGraph } from "./graph.ts";
import { useGroupModel } from "./load.ts";
import { traceMatrix, type MatrixColumn, type TraceMatrix } from "./matrix.ts";
import { fieldLabel, lifecycleValue, memberLabel, type GroupModel, type Member } from "./model.ts";
import { Band, inSentence } from "./traceRows.tsx";

/** A closed band with more rows than this starts folded while other bands are open. */
const FOLD_CLOSED = 3;

const DEFAULT_SPINE: string | null = null;

const isSpine = (value: unknown): value is string | null => value === null || isString(value);

const SPINE = {
  defaultValue: DEFAULT_SPINE,
  validate: isSpine,
};

const RELATION: Record<Extract<MatrixColumn, { kind: "member" }>["relation"], string> = {
  "linked-from-rows": "linked from rows",
  "links-to-rows": "link to rows",
  through: "through other columns",
};

const list = new Intl.ListFormat("en", { type: "conjunction" });

function columnLabel(model: GroupModel, column: MatrixColumn, joining = 1) {
  if (column.kind !== "member") return fieldLabel(column.link.field);

  const label = memberLabel(model, column.member, { plural: true });

  // Records joined by several fields (plans and work logs) are related, not one kind.
  return joining > 1 ? `Related ${inSentence(label)}` : label;
}

const openColumn = (column: MatrixColumn) =>
  openCollection(column.kind === "member" ? column.member.name : column.link.targetType);

/**
 * The link field that joins a column to the rows, such as "Governing specs":
 * the rows' own field for a column they link to, the column's field for one
 * that links to them. A column reached through others has none.
 */
function relationFields(matrix: TraceMatrix, column: MatrixColumn) {
  if (column.kind === "outside") return [];
  const { spine } = matrix;

  const links =
    column.relation === "linked-from-rows"
      ? spine.links.filter((item) => item.target === column.member.name)
      : column.relation === "links-to-rows"
        ? column.member.links.filter((item) => item.target === spine.name)
        : [];

  return [...new Set(links.map((link) => fieldLabel(link.field)))];
}

/** Lifecycle values the column's records hold, so the key names only marks on screen. */
function heldValues(matrix: TraceMatrix, column: MatrixColumn & { kind: "member" }) {
  const held = new Set<string>();

  const visit = (row: TraceMatrix["bands"][number]["roots"][number]) => {
    for (const entry of row.cells.get(column.id) ?? []) {
      const value = entry.kind === "record" ? lifecycleValue(column.member, entry.record) : null;

      if (value) held.add(value);
    }

    row.children.forEach(visit);
  };

  for (const band of matrix.bands) band.roots.forEach(visit);

  return held;
}

function ColumnHead({
  model,
  matrix,
  column,
}: {
  model: GroupModel;
  matrix: TraceMatrix;
  column: MatrixColumn;
}) {
  const fields = relationFields(matrix, column);
  const label = columnLabel(model, column, fields.length);

  const where =
    column.kind === "member"
      ? fields.length
        ? fields.join(" · ")
        : RELATION[column.relation]
      : column.link.targetGroup
        ? `in ${column.link.targetGroup}`
        : "outside the group";

  const values = column.kind === "member" ? (column.member.lifecycle?.values ?? []) : [];
  const held = column.kind === "member" ? heldValues(matrix, column) : new Set<string>();
  const shown = orderedEnumValues(values).filter((value) => held.has(value.name));
  const metadata = [where, ...shown.map((value) => value.label ?? value.name)].join(" · ");

  return (
    <th
      scope="col"
      className={
        [
          column.kind === "outside" && "gv-outcol",
          fields.length <= 1 && sparse(matrix, column) && "gv-col-sparse",
          fields.length > 1 && "gv-trace-related",
        ]
          .filter(Boolean)
          .join(" ") || undefined
      }
    >
      <div className="gv-head">
        <button
          type="button"
          className="gv-member"
          title={label}
          onClick={() => openColumn(column)}
        >
          {label}
        </button>
        {column.kind === "member" && <span className="gv-num">{column.member.count}</span>}
        <span
          className="gv-cov-count"
          title={`${column.covered} of ${matrix.rowCount} rows hold one`}
        >
          <b>{column.covered}</b> of {matrix.rowCount} linked
        </span>
      </div>
      <div className="gv-cov" title={metadata}>
        {where}
        {shown.length > 0 && (
          <span className="gv-col-legend" aria-label={`${label} status marks`}>
            {" · "}
            {shown.map((value, index) => (
              <span key={value.name}>
                {index > 0 && " · "}
                <StatusMark value={value.name} values={values} />
              </span>
            ))}
          </span>
        )}
      </div>
    </th>
  );
}

function SpineHead({ model, matrix }: { model: GroupModel; matrix: TraceMatrix }) {
  const { spine } = matrix;
  const lifecycle = spine.lifecycle;

  const empty = new Intl.ListFormat("en", { type: "disjunction" }).format(
    matrix.emptyValues.map((value) => (value.label ?? value.name).toLowerCase()),
  );

  const metadata = [
    !matrix.banded && "Most connected first",
    matrix.hierarchical && "Nested under parents",
    lifecycle && empty && `No ${empty} ${inSentence(memberLabel(model, spine, { plural: true }))}`,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <th scope="col" className="gv-trace-spine">
      <div className="gv-head">
        <MemberHeading model={model} member={spine} />
        <span className="gv-num">{spine.count}</span>
      </div>
      <div className="gv-cov" title={metadata}>
        {metadata}
      </div>
    </th>
  );
}

/** Columns with nothing to show, named once in the footer instead of as empty columns. */
function EmptyColumns({ model, matrix }: { model: GroupModel; matrix: TraceMatrix }) {
  const empty = matrix.columns.filter((column) => column.collapsed);

  if (!empty.length) return null;

  return (
    <span className="gv-faint">
      No links:{" "}
      {empty.map((column, index) => (
        <span key={column.id}>
          {index > 0 && ", "}
          <button
            type="button"
            className="gv-inline gv-link"
            title={column.kind === "member" ? "None recorded" : "No row links one"}
            onClick={() => openColumn(column)}
          >
            {columnLabel(model, column)}
          </button>
        </span>
      ))}
    </span>
  );
}

function Footer({
  model,
  matrix,
  defaultNote,
}: {
  model: GroupModel;
  matrix: TraceMatrix;
  /** Why the row choice is the default. */
  defaultNote?: string;
}) {
  const label = (member: Member, count?: number) =>
    inSentence(memberLabel(model, member, count === undefined ? { plural: true } : { count }));

  const missing = matrix.missing.reduce((sum, entry) => sum + entry.count, 0);

  return (
    <footer className="gv-trace-foot">
      {missing === 0 ? (
        <span className="gv-ok">✓ Every linked record appears in at least one row.</span>
      ) : (
        <span>
          {missing} {missing === 1 ? "record appears" : "records appear"} in no row:{" "}
          {list.format(
            matrix.missing.map(({ member, count }) => `${count} ${label(member, count)}`),
          )}
          .
        </span>
      )}
      {matrix.unreached.length > 0 && (
        <span>
          Not linked to {label(matrix.spine)}:{" "}
          {list.format(matrix.unreached.map((member) => label(member)))}.
        </span>
      )}
      <EmptyColumns model={model} matrix={matrix} />
      {defaultNote && (
        <span>
          <span className="gv-default-dot" aria-hidden="true" /> {defaultNote}
        </span>
      )}
      <span>
        Lighter italic titles were reached through another record in the row. Point at or focus a
        record to light every row it appears in.
      </span>
    </footer>
  );
}

/** Columns that show; an empty column is named in the toolbar instead. */
const shownColumns = (matrix: TraceMatrix) => matrix.columns.filter((column) => !column.collapsed);

/** Keep related materials readable alongside the spine and other linked columns. */
function matrixWidth(matrix: TraceMatrix) {
  return (
    340 +
    shownColumns(matrix).reduce(
      (width, column) => width + (relationFields(matrix, column).length > 1 ? 300 : 220),
      0,
    )
  );
}

/** A column few rows fill is held to a fifth of the width, so dense columns get the room. */
const sparse = (matrix: TraceMatrix, column: MatrixColumn) => column.covered < matrix.rowCount / 4;

function Matrix({
  model,
  matrix,
  defaultNote,
}: {
  model: GroupModel;
  matrix: TraceMatrix;
  defaultNote?: string;
}) {
  const open = matrix.bands.some((band) => !band.closed);

  return (
    <>
      <HighlightProvider>
        <table className="gv-trace">
          <colgroup>
            <col className="gv-col-spine" />
            {shownColumns(matrix).map((column) => (
              <col
                key={column.id}
                className={
                  relationFields(matrix, column).length > 1
                    ? "gv-trace-related"
                    : sparse(matrix, column)
                      ? "gv-col-sparse"
                      : undefined
                }
              />
            ))}
          </colgroup>
          <thead>
            <tr>
              <SpineHead model={model} matrix={matrix} />
              {shownColumns(matrix).map((column) => (
                <ColumnHead key={column.id} model={model} matrix={matrix} column={column} />
              ))}
            </tr>
          </thead>
          {matrix.bands.map((band) => (
            <Band
              key={band.value ?? ""}
              model={model}
              matrix={matrix}
              band={band}
              folded={band.closed && open && band.size > FOLD_CLOSED}
            />
          ))}
        </table>
      </HighlightProvider>
      <Footer model={model} matrix={matrix} defaultNote={defaultNote} />
    </>
  );
}

/** Why the first row choice is the default, for the footer. */
function defaultRowsNote(model: GroupModel, graph: LinkGraph, choices: readonly string[]) {
  const fallback = choices[0];
  const member = model.memberIndex.get(fallback);

  if (!member || choices.length < 2) return undefined;

  const tree =
    (member.parentField ?? null) !== null && (graph.incoming.get(fallback)?.size ?? 0) > 0;

  return `${memberLabel(model, member, { plural: true })} are the default rows: ${tree ? "the tree other types link to" : "the type linked with the most others"}. Columns follow link direction.`;
}

function SpineSwitcher({
  model,
  choices,
  spine,
  onChoose,
}: {
  model: GroupModel;
  choices: readonly string[];
  spine: string;
  onChoose: (spine: string) => void;
}) {
  const fallback = choices[0];

  return (
    <div className="gv-toolbar">
      <Segmented
        label="Rows"
        showLabel
        value={spine}
        onChange={onChoose}
        options={choices.flatMap((name) => {
          const choice = model.memberIndex.get(name);

          if (!choice) return [];

          return [
            {
              value: name,
              label: memberLabel(model, choice, { plural: true }),
              count: choice.records.length,
              description: name === fallback ? "(default)" : undefined,
            },
          ];
        })}
      />
    </div>
  );
}

function TraceBody({ model }: { model: GroupModel }) {
  const graph = useMemo(() => linkGraph(model), [model]);
  const choices = useMemo(() => spineChoices(model, graph), [model, graph]);
  const remembered = useViewPreference("trace.spine", SPINE);
  const chosen = remembered.value;
  const spine = chosen !== null && choices.includes(chosen) ? chosen : (choices[0] ?? null);

  const matrix = useMemo(
    () => (graph.linked && spine !== null ? traceMatrix(model, graph, spine) : null),
    [model, graph, spine],
  );

  const choose = (name: string) =>
    void remembered.set(name === choices[0] ? null : name).catch(() => {});

  if (!graph.linked || spine === null || matrix === null) {
    return (
      <>
        <GroupFacts model={model} />
        <p className="gv-state" role="status">
          {graph.linked
            ? `The linked types in ${model.name} have no records yet, so Trace has no rows to show.`
            : `Trace needs member types that link to each other, and ${model.name} has none.`}
        </p>
      </>
    );
  }

  // The page grows to the matrix's width and scrolls sideways, so the header
  // can stick to the top of the frame; the strips above stay at frame width.
  return (
    <div className="gv-trace-page" style={{ minWidth: matrixWidth(matrix) }}>
      <GroupFacts model={model} />
      <SpineSwitcher model={model} choices={choices} spine={spine} onChoose={choose} />
      <Matrix
        key={spine}
        model={model}
        matrix={matrix}
        defaultNote={defaultRowsNote(model, graph, choices)}
      />
    </div>
  );
}

export default function Trace(_props: ViewModuleProps) {
  const state = useGroupModel();

  return <GroupPage state={state}>{(model) => <TraceBody model={model} />}</GroupPage>;
}
