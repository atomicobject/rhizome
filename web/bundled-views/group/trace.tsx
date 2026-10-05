// Trace (SPEC-0111): records of one member (the spine) as rows, with every
// record they connect to across the group as columns. Rows band by lifecycle
// and nest under parents; pointing at a record lights every row it appears in.
import {
  HighlightProvider,
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
import { fieldLabel, memberLabel, type GroupModel, type Member } from "./model.ts";
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

function columnLabel(model: GroupModel, column: MatrixColumn) {
  return column.kind === "member"
    ? memberLabel(model, column.member, { plural: true })
    : fieldLabel(column.link.field);
}

const openColumn = (column: MatrixColumn) =>
  openCollection(column.kind === "member" ? column.member.name : column.link.targetType);

function ColumnHead({
  model,
  matrix,
  column,
}: {
  model: GroupModel;
  matrix: TraceMatrix;
  column: MatrixColumn;
}) {
  const label = columnLabel(model, column);

  if (column.collapsed) {
    const why = column.kind === "member" ? "none recorded" : "no row links one";

    return (
      <th scope="col" className="gv-narrow" title={`${label}: ${why}`}>
        <button type="button" className="gv-member gv-vertical" onClick={() => openColumn(column)}>
          {label} <span className="gv-num">0</span>
          <span className="sr-only">, {why}</span>
        </button>
      </th>
    );
  }

  const where =
    column.kind === "member"
      ? RELATION[column.relation]
      : column.link.targetGroup
        ? `in ${column.link.targetGroup}`
        : "outside the group";

  const thin = column.covered < matrix.rowCount / 3;

  return (
    <th scope="col" className={column.kind === "outside" ? "gv-outcol" : undefined}>
      <div className="gv-head">
        {/* The count sits inside the button so it wraps with the label's last word. */}
        <button type="button" className="gv-member" onClick={() => openColumn(column)}>
          {label}
          {column.kind === "member" && (
            <>
              {"\u00a0"}
              <span className="gv-num">{column.member.count}</span>
            </>
          )}
        </button>
      </div>
      <div className="gv-cov">
        <b className={thin ? "gv-thin" : undefined}>
          {column.covered}/{matrix.rowCount}
        </b>{" "}
        rows · {where}
      </div>
    </th>
  );
}

function SpineHead({ model, matrix }: { model: GroupModel; matrix: TraceMatrix }) {
  const { spine } = matrix;
  const lifecycle = spine.lifecycle;

  const order = lifecycle
    ? `by ${fieldLabel(lifecycle.field).toLowerCase()}, most advanced first`
    : "most connected first";

  return (
    <th scope="col" className="gv-trace-spine">
      <div className="gv-head">
        <MemberHeading model={model} member={spine} />
        {"\u00a0"}
        <span className="gv-num">{spine.count}</span>
      </div>
      <div className="gv-cov">
        {order}
        {matrix.hierarchical && ", nested under parents"}
      </div>
      {matrix.emptyValues.length > 0 && (
        <div className="gv-cov">
          Empty: {matrix.emptyValues.map((value) => value.label ?? value.name).join(", ")}
        </div>
      )}
    </th>
  );
}

function Footer({ model, matrix }: { model: GroupModel; matrix: TraceMatrix }) {
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
      <span>
        Lighter italic titles were reached through another record in the row. Point at or focus a
        record to light every row it appears in.
      </span>
    </footer>
  );
}

/** The matrix's narrowest readable width, in pixels. */
function matrixWidth(matrix: TraceMatrix) {
  const wide = matrix.columns.filter((column) => !column.collapsed).length;

  return 280 + wide * 120 + (matrix.columns.length - wide) * 30;
}

function Matrix({ model, matrix }: { model: GroupModel; matrix: TraceMatrix }) {
  const open = matrix.bands.some((band) => !band.closed);

  return (
    <>
      <HighlightProvider>
        <table className="gv-trace">
          <colgroup>
            <col className="gv-col-spine" />
            {matrix.columns.map((column) => (
              <col key={column.id} className={column.collapsed ? "gv-col-narrow" : undefined} />
            ))}
          </colgroup>
          <thead>
            <tr>
              <SpineHead model={model} matrix={matrix} />
              {matrix.columns.map((column) => (
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
      <Footer model={model} matrix={matrix} />
    </>
  );
}

function SpineSwitcher({
  model,
  graph,
  choices,
  spine,
  onChoose,
}: {
  model: GroupModel;
  graph: LinkGraph;
  choices: readonly string[];
  spine: string;
  onChoose: (spine: string) => void;
}) {
  const fallback = choices[0];
  const member = model.memberIndex.get(fallback);

  const tree =
    (member?.parentField ?? null) !== null && (graph.incoming.get(fallback)?.size ?? 0) > 0;

  return (
    <div className="gv-toolbar">
      <span id="gv-rows-label" className="gv-label">
        Rows
      </span>
      <div className="gv-seg" role="radiogroup" aria-labelledby="gv-rows-label">
        {choices.map((name) => {
          const choice = model.memberIndex.get(name);

          if (!choice) return null;

          return (
            <label key={name}>
              <input
                type="radio"
                name="gv-trace-spine"
                className="sr-only"
                value={name}
                checked={name === spine}
                onChange={() => onChoose(name)}
              />
              {name === fallback && <span className="gv-default-dot" aria-hidden="true" />}
              {memberLabel(model, choice, { plural: true })}
              <span className="gv-num">{choice.records.length}</span>
              {name === fallback && <span className="sr-only">(default)</span>}
            </label>
          );
        })}
      </div>
      <span className="gv-faint">
        <span className="gv-default-dot" aria-hidden="true" /> Default:{" "}
        {tree ? "the tree other types link to" : "the type linked with the most others"}. Columns
        follow link direction.
      </span>
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
      <SpineSwitcher
        model={model}
        graph={graph}
        choices={choices}
        spine={spine}
        onChoose={choose}
      />
      <Matrix key={spine} model={model} matrix={matrix} />
    </div>
  );
}

export default function Trace(_props: ViewModuleProps) {
  const state = useGroupModel();

  return <GroupPage state={state}>{(model) => <TraceBody model={model} />}</GroupPage>;
}
