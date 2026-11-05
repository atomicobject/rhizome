import type { TypeSummary } from "./aggregate.ts";
// The Overview's tables (SPEC-0117 US1-US3): the Matrix, a complete text
// alternative to the map, and the member table comparing every member's size,
// progress, completeness, and connection.
import {
  DisclosureIcon,
  RelativeTime,
  openCollection,
  openIssues,
  useTypeColors,
} from "@rhizome/kit";

import { plural } from "./briefing-parts.tsx";
import { Dash } from "./components.tsx";
import type { LifecycleSummary, MemberRow, NodeRow } from "./members.ts";
import { firstSentence } from "./model.ts";
import { nodeHue, compositionArcs, unusedPairs } from "./overview-map-parts.tsx";
import {
  edgeKey,
  type DeclaredRelation,
  type LinkModel,
  type ScopeModel,
  type ScopeNode,
} from "./scope.ts";

const fmt = (count: number) => count.toLocaleString("en-US");

/**
 * Members as rows and columns, most records first: links between two
 * members, links among a member's own records on the diagonal, and on a
 * group an Outside column summing links that leave it.
 */
export function ScopeMatrix({
  model,
  links,
  relations,
}: {
  model: ScopeModel;
  links: LinkModel;
  relations: readonly DeclaredRelation[];
}) {
  const unused = new Set(unusedPairs(links, relations).map((pair) => edgeKey(pair.a, pair.b)));
  const group = model.scope.kind === "group";

  const outside = (id: string) =>
    links.outside.reduce((sum, neighbor) => sum + (neighbor.byMember.get(id) ?? 0), 0);

  return (
    <div className="gv-mx-wrap">
      <table className="gv-mx gv-ov-mx">
        <thead>
          <tr>
            <td />
            {model.nodes.map((node) => (
              <th key={node.id} scope="col">
                <span>{node.label}</span>
              </th>
            ))}
            {group && (
              <th scope="col">
                <span>Outside</span>
              </th>
            )}
          </tr>
        </thead>
        <tbody>
          {model.nodes.map((row) => (
            <tr key={row.id}>
              <th scope="row">{row.label}</th>
              {model.nodes.map((column) => {
                if (row.id === column.id) {
                  const own = links.self.get(row.id) ?? 0;

                  return (
                    <td key={column.id} data-cell="self">
                      {own ? fmt(own) : ""}
                      <span className="sr-only"> among its own records</span>
                    </td>
                  );
                }

                const edge = links.edgeIndex.get(edgeKey(row.id, column.id));

                if (!edge)
                  return unused.has(edgeKey(row.id, column.id)) ? (
                    <td key={column.id} data-cell="unused">
                      <span className="sr-only">declared, unused</span>
                    </td>
                  ) : (
                    <td key={column.id} />
                  );

                return (
                  <td
                    key={column.id}
                    data-cell={edge.relation ? "relation" : "plain"}
                    title={`${edge.relationLinks} relation · ${edge.plainLinks} plain`}
                  >
                    {fmt(edge.links)}
                    <span className="sr-only">
                      {" "}
                      ({edge.relationLinks} relation, {edge.plainLinks} plain)
                    </span>
                  </td>
                );
              })}
              {group && <td data-cell="plain">{outside(row.id) ? fmt(outside(row.id)) : ""}</td>}
            </tr>
          ))}
        </tbody>
      </table>
      <p className="gv-legend">
        Bold teal: mostly relation links. Gray: mostly plain links. Hatched: declared, unused.
        Diagonal: links among a member&apos;s own records.
      </p>
    </div>
  );
}

function LifecycleCell({ lifecycle }: { lifecycle: LifecycleSummary | null | undefined }) {
  if (lifecycle === undefined) return null;

  if (lifecycle === null) return <span className="gv-faint">no lifecycle</span>;

  return (
    <span className="gv-ov-stage">
      {/* A table cell has no room for type-briefing's visible key, so the
          bar carries the same breakdown as its text alternative. */}
      <span
        className="gv-dist gv-ov-bar"
        role="img"
        aria-label={`Stages: ${lifecycle.segments.map((segment) => `${segment.label} ${segment.count}`).join(", ")}`}
      >
        {lifecycle.segments.map((segment) => (
          <i
            key={segment.name}
            data-tone={segment.tone}
            style={{ flexGrow: segment.count }}
            title={`${segment.label} ${segment.count}`}
          />
        ))}
      </span>
      {/* The visible key: every held stage and its count, in lifecycle order. */}
      <span className="gv-ov-stage-key">
        {lifecycle.segments
          .filter((segment) => segment.count > 0)
          .map((segment) => `${segment.label} ${fmt(segment.count)}`)
          .join(" · ")}
      </span>
    </span>
  );
}

function ShareCell({ share }: { share: number | null }) {
  if (share === null) return null;
  const percent = Math.round(share * 100);

  return (
    <span className="gv-ov-share">
      <span className="gv-num">{percent}%</span>
      <span className="gv-ov-mini" aria-hidden="true">
        <i style={{ width: `${percent}%` }} />
      </span>
    </span>
  );
}

function Dot({
  summaries,
  node,
}: {
  summaries: ReadonlyMap<string, TypeSummary>;
  node: ScopeNode;
}) {
  const color = useTypeColors();
  const hue = nodeHue(node, color);
  const arcs = compositionArcs(node, summaries, color);

  if (arcs.length)
    return (
      <i
        className="gv-ov-dot gv-ov-ring"
        aria-hidden="true"
        style={{
          background: `conic-gradient(${arcs.map((arc) => `${arc.color} ${arc.start * 100}% ${(arc.start + arc.share) * 100}%`).join(",")})`,
        }}
      />
    );

  return (
    <i
      className="gv-ov-dot"
      aria-hidden="true"
      style={{ background: node.count === 0 ? "var(--surface)" : hue, borderColor: hue }}
    />
  );
}

function openNode(node: ScopeNode) {
  if (node.kind === "type" || node.kind === "interface") openCollection(node.name);
}

function NameCell({
  summaries,
  row,
  onToggle,
}: {
  summaries: ReadonlyMap<string, TypeSummary>;
  row: NodeRow;
  onToggle: (group: string, open: boolean) => void;
}) {
  const { node } = row;

  if (row.kind === "group")
    return (
      <button
        type="button"
        className="gv-member"
        aria-expanded={row.expanded}
        onClick={() => onToggle(node.name, !row.expanded)}
      >
        <DisclosureIcon open={Boolean(row.expanded)} className="gv-ov-chev" />
        <Dot summaries={summaries} node={node} />
        {node.label}{" "}
        <span className="gv-sub">
          · {plural(node.members.length, "1 type", `${node.members.length} types`)}
        </span>
      </button>
    );

  const name = (
    <>
      <Dot summaries={summaries} node={node} />
      {node.label}
      {node.count === 0 && node.kind !== "untyped" && <span className="gv-faint"> empty</span>}
    </>
  );

  if (node.kind === "untyped")
    return <span className="gv-ov-name gv-ov-no-disclosure">{name}</span>;

  const open = (
    <button type="button" className="gv-record" onClick={() => openNode(node)}>
      {name}
    </button>
  );

  // An interface folds its implementing types under a chevron of its own.
  if (row.toggle === undefined)
    return <span className="gv-ov-name gv-ov-no-disclosure">{open}</span>;
  const { toggle } = row;

  return (
    <span className="gv-ov-name">
      <button
        type="button"
        className="rz-disclosure gv-ov-chev-button"
        aria-expanded={row.expanded}
        aria-label={`${row.expanded ? "Hide" : "Show"} the types implementing ${node.label}`}
        onClick={() => onToggle(toggle, !row.expanded)}
      >
        <DisclosureIcon open={Boolean(row.expanded)} />
      </button>
      {open}
    </span>
  );
}

function IssuesCell({ node }: { node: ScopeNode }) {
  if (node.kind === "untyped") return <Dash />;

  if (node.issueCount === 0) return <span className="gv-num num gv-faint">0</span>;

  if (node.kind === "group") return <span className="gv-issues num">{node.issueCount}</span>;

  return (
    <button
      type="button"
      className="gv-issues num"
      aria-label={`${node.issueCount} ${plural(node.issueCount, "issue", "issues")} in ${node.label}`}
      onClick={() =>
        openIssues({ kind: node.kind === "interface" ? "interface" : "type", key: node.name })
      }
    >
      {node.issueCount}
    </button>
  );
}

function GapsCell({ row }: { row: NodeRow }) {
  if (row.node.count === 0 || row.kind === "implementor" || row.kind === "group") return null;

  if (!row.gaps.length)
    return row.node.stats?.gaps.length ? <span className="gv-faint">none empty</span> : null;

  return (
    <span className="gv-ov-gaps">
      {row.gaps.map((gap) => (
        <span key={gap.field} data-required={gap.required ? "true" : undefined}>
          {gap.label}{" "}
          <span className="gv-num">
            {gap.empty}/{row.node.count}
          </span>
        </span>
      ))}
    </span>
  );
}

/** Which optional columns hold anything in these rows; an empty column hides. */
type Shown = { stages: boolean; gaps: boolean; linked: boolean };

const hasLinked = (row: NodeRow) =>
  row.node.kind !== "untyped" &&
  row.kind !== "implementor" &&
  row.kind !== "group" &&
  row.linkedShare !== null;

const hasStages = (row: NodeRow) =>
  row.node.count > 0 && row.kind !== "implementor" && Boolean(row.lifecycle);

const hasGaps = (row: NodeRow) =>
  row.node.count > 0 &&
  row.kind !== "implementor" &&
  row.kind !== "group" &&
  Boolean(row.gaps.length || row.node.stats?.gaps.length);

function DataRow({
  model,
  summaries,
  row,
  shown,
  onToggle,
}: {
  model: ScopeModel;
  summaries: ReadonlyMap<string, TypeSummary>;
  row: NodeRow;
  shown: Shown;
  onToggle: (group: string, open: boolean) => void;
}) {
  const { node } = row;
  const workspace = model.scope.kind === "workspace";
  const description = node.description ? firstSentence(node.description) : undefined;
  const brief = row.kind === "implementor";

  return (
    <tr data-kind={row.kind} title={description}>
      {/* A nested row starts 16px past its parent's name, not its dot. */}
      <td style={{ paddingLeft: `${22 + row.depth * 24}px` }}>
        <NameCell summaries={summaries} row={row} onToggle={onToggle} />
      </td>
      <td className="gv-right gv-num num">{fmt(node.count)}</td>
      {node.kind === "untyped" ? (
        // Untyped notes are not records: no lifecycle, gaps, or linked share.
        <>
          {shown.stages && (
            <td>
              <Dash />
            </td>
          )}
          {shown.gaps && (
            <td>
              <Dash />
            </td>
          )}
          {shown.linked && (
            <td>
              <Dash />
            </td>
          )}
        </>
      ) : (
        <>
          {shown.stages && (
            <td>
              {node.count > 0 && !brief && row.lifecycle !== undefined ? (
                <LifecycleCell lifecycle={row.lifecycle} />
              ) : (
                <Dash />
              )}
            </td>
          )}
          {shown.gaps && (
            <td>
              <GapsCell row={row} />
            </td>
          )}
          {shown.linked && (
            <td>
              {brief || row.kind === "group" || row.linkedShare === null ? (
                <Dash />
              ) : (
                <ShareCell share={row.linkedShare} />
              )}
            </td>
          )}
        </>
      )}
      {/* A nested implementor's figures roll up into its interface: a dash, not a blank. */}
      <td className="gv-right gv-num num">{brief ? <Dash /> : fmt(row.links)}</td>
      {!workspace && (
        <td className="gv-right gv-num num">{brief ? <Dash /> : fmt(row.outside ?? 0)}</td>
      )}
      <td className="gv-right gv-num num">{brief ? <Dash /> : <IssuesCell node={node} />}</td>
      <td className="gv-right gv-num num">
        {node.lastChanged !== null && !brief ? <RelativeTime value={node.lastChanged} /> : <Dash />}
      </td>
    </tr>
  );
}

/** One row per member, an interface's implementors nested under it, named groups expanding at All notes. */
export function MemberTable({
  model,
  summaries,
  rows,
  onToggle,
}: {
  model: ScopeModel;
  summaries: ReadonlyMap<string, TypeSummary>;
  rows: readonly MemberRow[];
  onToggle: (group: string, open: boolean) => void;
}) {
  const workspace = model.scope.kind === "workspace";
  const dataRows = rows.flatMap((row) => (row.kind === "heading" ? [] : [row]));

  const shown = {
    stages: dataRows.some(hasStages),
    gaps: dataRows.some(hasGaps),
    linked: dataRows.some(hasLinked),
  };

  const columns =
    (workspace ? 5 : 6) + Number(shown.stages) + Number(shown.gaps) + Number(shown.linked);

  return (
    <table className="gv-table gv-ov-table">
      <colgroup>
        <col />
        <col className="gv-col-number" />
        {shown.stages && <col className="gv-col-stages" />}
        {shown.gaps && <col className="gv-col-gaps" />}
        {shown.linked && <col className="gv-col-linked" />}
        <col className="gv-col-number" />
        {!workspace && <col className="gv-col-number" />}
        <col className="gv-col-number" />
        <col className="gv-col-number" />
      </colgroup>
      <thead>
        <tr>
          <th scope="col">{workspace ? "Group · type" : "Type"}</th>
          <th scope="col" className="gv-right">
            Records
          </th>
          {shown.stages && <th scope="col">Stages</th>}
          {shown.gaps && <th scope="col">Gaps (empty / records)</th>}
          {shown.linked && <th scope="col">{workspace ? "Linked" : "Linked in group"}</th>}
          <th scope="col" className="gv-right">
            {workspace ? "Links" : "Links in group"}
          </th>
          {!workspace && (
            <th scope="col" className="gv-right">
              Outside
            </th>
          )}
          <th scope="col" className="gv-right">
            Issues
          </th>
          <th scope="col" className="gv-right">
            Changed
          </th>
        </tr>
      </thead>
      <tbody>
        {rows.map((row) =>
          row.kind === "heading" ? (
            <tr key="heading" data-kind="heading">
              <td colSpan={columns}>
                {row.label}{" "}
                <span className="gv-sub">
                  · {plural(row.types, "1 type", `${row.types} types`)}
                </span>
              </td>
            </tr>
          ) : (
            <DataRow
              key={`${row.kind}:${row.node.id}`}
              model={model}
              summaries={summaries}
              row={row}
              shown={shown}
              onToggle={onToggle}
            />
          ),
        )}
      </tbody>
    </table>
  );
}
