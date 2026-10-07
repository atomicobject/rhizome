// The Overview's tables (SPEC-0117 US1-US3): the Matrix, a complete text
// alternative to the map, and the member table comparing every member's size,
// progress, completeness, and connection.
import { RelativeTime, openCollection, openIssues } from "@rhizome/kit";

import { plural } from "./briefing-parts.tsx";
import { Dash } from "./components.tsx";
import type { LifecycleSummary, MemberRow, NodeRow } from "./members.ts";
import { firstSentence } from "./model.ts";
import { nodeHue, unusedPairs } from "./overview-map.tsx";
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
      <span className="gv-dist gv-ov-bar">
        {lifecycle.segments.map((segment) => (
          <i
            key={segment.name}
            data-tone={segment.tone}
            style={{ flexGrow: segment.count }}
            title={`${segment.label} ${segment.count}`}
          />
        ))}
      </span>
      {lifecycle.active > 0 && (
        <span className="gv-num">
          <b>{lifecycle.active}</b> {lifecycle.activeLabel}
        </span>
      )}
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

function Dot({ model, node }: { model: ScopeModel; node: ScopeNode }) {
  const hue = nodeHue(model, node);

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
  model,
  row,
  onToggle,
}: {
  model: ScopeModel;
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
        <span className="gv-ov-chev" aria-hidden="true">
          {row.expanded ? "▾" : "▸"}
        </span>
        <Dot model={model} node={node} />
        {node.label}{" "}
        <span className="gv-sub">
          · {plural(node.members.length, "1 type", `${node.members.length} types`)}
        </span>
      </button>
    );

  const name = (
    <>
      <Dot model={model} node={node} />
      {node.label}
      {node.count === 0 && node.kind !== "untyped" && <span className="gv-faint"> empty</span>}
    </>
  );

  if (node.kind === "untyped") return <span className="gv-ov-name">{name}</span>;

  return (
    <button type="button" className="gv-record" onClick={() => openNode(node)}>
      {name}
    </button>
  );
}

function IssuesCell({ node }: { node: ScopeNode }) {
  if (node.kind === "untyped") return <Dash />;

  if (node.issueCount === 0) return <span className="gv-num gv-faint">0</span>;

  if (node.kind === "group") return <span className="gv-issues">{node.issueCount}</span>;

  return (
    <button
      type="button"
      className="gv-issues"
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

function DataRow({
  model,
  row,
  onToggle,
}: {
  model: ScopeModel;
  row: NodeRow;
  onToggle: (group: string, open: boolean) => void;
}) {
  const { node } = row;
  const workspace = model.scope.kind === "workspace";
  const description = node.description ? firstSentence(node.description) : undefined;
  const brief = row.kind === "implementor";

  return (
    <tr data-kind={row.kind} title={description}>
      <td style={{ paddingLeft: `${20 + row.depth * 16}px` }}>
        <NameCell model={model} row={row} onToggle={onToggle} />
      </td>
      <td className="gv-right gv-num">{fmt(node.count)}</td>
      {node.kind === "untyped" ? (
        <td colSpan={3} className="gv-faint">
          Not records: no lifecycle or gaps by type.
        </td>
      ) : (
        <>
          <td>{node.count > 0 && !brief && <LifecycleCell lifecycle={row.lifecycle} />}</td>
          <td>
            <GapsCell row={row} />
          </td>
          <td>{!brief && row.kind !== "group" && <ShareCell share={row.linkedShare} />}</td>
        </>
      )}
      <td className="gv-right gv-num">{brief ? "" : fmt(row.links)}</td>
      {!workspace && <td className="gv-right gv-num">{brief ? "" : fmt(row.outside ?? 0)}</td>}
      <td className="gv-right">{!brief && <IssuesCell node={node} />}</td>
      <td className="gv-right gv-num">
        {node.lastChanged !== null && !brief ? <RelativeTime value={node.lastChanged} /> : ""}
      </td>
    </tr>
  );
}

/** One row per member, an interface's implementors nested under it, named groups expanding at All notes. */
export function MemberTable({
  model,
  rows,
  onToggle,
}: {
  model: ScopeModel;
  rows: readonly MemberRow[];
  onToggle: (group: string, open: boolean) => void;
}) {
  const workspace = model.scope.kind === "workspace";
  const columns = workspace ? 8 : 9;

  return (
    <table className="gv-table gv-ov-table">
      <thead>
        <tr>
          <th scope="col">{workspace ? "Group · type" : "Type"}</th>
          <th scope="col" className="gv-right">
            Records
          </th>
          <th scope="col">Stages</th>
          <th scope="col">Gaps (empty / records)</th>
          <th scope="col">{workspace ? "Linked" : "Linked in group"}</th>
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
              row={row}
              onToggle={onToggle}
            />
          ),
        )}
      </tbody>
    </table>
  );
}
