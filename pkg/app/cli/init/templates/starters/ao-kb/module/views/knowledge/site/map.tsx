// The topic map (SPEC-0135 R4): one node per concept at the library's deterministic positions.
// On the KB overview every placed name is drawn; the scoped neighbourhood crops the same
// positions to a record's scope, keeps the overview's encoding for in-scope nodes, draws the
// other nodes inside the crop faint and neutral without labels, and draws only lines whose two
// ends it draws.
import { nodeHref, usePageNavigation } from "@rhizome/kit";
import type { ViewNodeRef } from "@rhizome/kit";
import { layout } from "../lib/site.ts";
import { neighbourhood } from "../lib/neighbourhood.ts";
import type { MapNode, Site } from "../lib/site.ts";
import { KIND_GROUPS, cmp, groupOf } from "../lib/kinds.ts";
import { ChartBox, kindColor, useChart } from "./chart.tsx";
import { plural } from "./data.tsx";

const SCOPED_WIDTH = 696;

function Node({ n }: { n: MapNode }) {
  if (n.hollow) return <circle className="ak-node-hollow" cx={n.x} cy={n.y} r={n.r} />;
  if (!n.arcs) return <circle className="ak-node-solid" cx={n.x} cy={n.y} r={n.r} />;
  // Kind-group shares as one ring of arcs, in the legend's order, from twelve o'clock.
  const rr = n.r * 0.66,
    width = n.r * 0.68,
    C = 2 * Math.PI * rr;
  const parts = KIND_GROUPS.filter((g) => n.shares[g.id]);
  const gap = parts.length > 1 ? Math.min(2, C * 0.04) : 0;
  let offset = 0;
  return (
    <g transform={`rotate(-90 ${n.x} ${n.y})`}>
      {parts.map((g) => {
        const len = (C * n.shares[g.id]) / n.members,
          dash = `${Math.max(0.5, len - gap)} ${C - len + gap}`,
          at = -offset;
        offset += len;
        return (
          <circle
            key={g.id}
            cx={n.x}
            cy={n.y}
            r={rr}
            fill="none"
            stroke={kindColor(g.id)}
            strokeWidth={width}
            strokeDasharray={dash}
            strokeDashoffset={at}
          />
        );
      })}
    </g>
  );
}

export const nodeTip = (site: Site, n: MapNode) =>
  `${n.title}\n${n.members ? plural(n.members, "member assertion") : "No member assertions"} · ${plural(site.related.get(n.path)?.length ?? 0, "Related Concept")}`;

/** The map. With `scope`, the cropped neighbourhood of a record page; `mark` is ringed in ink. */
export function TopicMap({
  site,
  scope,
  mark,
  label,
}: {
  site: Site;
  scope?: Set<string>;
  mark?: string;
  label: string;
}) {
  const l = layout(site);
  const { openNode } = usePageNavigation();
  const order = scope ? l.order.filter((i) => scope.has(l.nodes[i].path)) : l.order;
  const chart = useChart(order.length);
  const hood = scope ? neighbourhood(l, scope) : null;
  const box = hood?.box;
  const scale = box ? box.w / SCOPED_WIDTH : 1;
  const hotNode = chart.hot === null ? null : l.nodes[order[chart.hot]];
  const near = hotNode ? new Set([hotNode.path, ...(site.related.get(hotNode.path) ?? [])]) : null;
  const inScope = (path: string) => !scope || scope.has(path);
  const refOf = (n: MapNode) => site.byPath.get(n.path)?.ref as ViewNodeRef | undefined;
  const open = (n: MapNode) => {
    const ref = refOf(n);
    if (ref) openNode(ref, { view: "ao-kb.concept" });
  };
  return (
    <ChartBox
      chart={chart}
      className={`ak-map${scope ? " ak-map-scoped" : ""}`}
      label={`${label}. Arrow keys move between concepts by member count; Enter opens one.`}
    >
      <svg
        viewBox={box ? `${box.x} ${box.y} ${box.w} ${box.h}` : `0 0 ${l.width} ${l.height}`}
        style={{ maxWidth: scope ? SCOPED_WIDTH : l.width }}
        data-hover={near ? "" : undefined}
      >
        {(hood?.edges ?? l.edges).map(([a, b]) => {
          const pa = l.nodes[a],
            pb = l.nodes[b];
          const on = inScope(pa.path) && inScope(pb.path);
          const lit = near
            ? near.has(pa.path) &&
              near.has(pb.path) &&
              (pa.path === hotNode!.path || pb.path === hotNode!.path)
            : false;
          return (
            <line
              key={`${a}-${b}`}
              className={`ak-edge${on ? "" : " ak-faint"}${lit ? " ak-lit" : ""}`}
              x1={pa.x}
              y1={pa.y}
              x2={pb.x}
              y2={pb.y}
              strokeWidth={1.25 * scale}
            />
          );
        })}
        {l.order.map((i) => {
          const n = l.nodes[i],
            k = order.indexOf(i),
            on = inScope(n.path);
          if (hood && !hood.drawn.has(n.path)) return null;
          const dim = near && !near.has(n.path);
          const body = on ? (
            <Node n={n} />
          ) : (
            <circle className="ak-node-faint" cx={n.x} cy={n.y} r={Math.max(n.r, 4)} />
          );
          if (k < 0)
            return (
              <g key={n.path} aria-hidden="true">
                {body}
              </g>
            );
          return (
            <a
              key={n.path}
              className={`ak-mark${dim ? " ak-dim" : ""}`}
              href={refOf(n) ? nodeHref(refOf(n)!, { view: "ao-kb.concept" }) : undefined}
              {...chart.item(k, nodeTip(site, n), () => open(n))}
            >
              <circle className="ak-hit" cx={n.x} cy={n.y} r={n.r + 7} />
              {body}
              {mark === n.path && (
                <circle
                  className="ak-ring"
                  cx={n.x}
                  cy={n.y}
                  r={n.r + 4.5}
                  strokeWidth={2.5 * scale}
                />
              )}
              <circle
                className="ak-focus-ring"
                cx={n.x}
                cy={n.y}
                r={n.r + 6.5}
                strokeWidth={2.5 * scale}
              />
            </a>
          );
        })}
        <g className="ak-labels" aria-hidden="true">
          {l.nodes.map((n) => {
            if (scope ? !scope.has(n.path) : n.label.hidden) return null;
            const x =
              n.label.anchor === "start"
                ? n.label.x
                : n.label.anchor === "end"
                  ? n.label.x + n.label.w
                  : n.label.x + n.label.w / 2;
            return (
              <text
                key={n.path}
                className={near && !near.has(n.path) ? "ak-dim" : undefined}
                x={x}
                y={n.label.y + 12.5}
                textAnchor={n.label.anchor}
                style={{ fontSize: 13 * scale, strokeWidth: 4 * scale }}
              >
                {n.label.text}
              </text>
            );
          })}
        </g>
      </svg>
    </ChartBox>
  );
}

/** The kind groups with the raw kinds this KB records in each, and their counts (R3). */
export function KindLegend({ site }: { site: Site }) {
  const counts = new Map<string, number>();
  for (const a of site.assertions) if (a.kind) counts.set(a.kind, (counts.get(a.kind) ?? 0) + 1);
  return (
    <div className="ak-legend">
      {KIND_GROUPS.map((g) => {
        const kinds = [...counts].filter(([k]) => groupOf(k) === g.id);
        if (!kinds.length) return null;
        return (
          <span key={g.id}>
            <i className="ak-swatch" style={{ background: kindColor(g.id) }} aria-hidden="true" />
            <b>{g.label}</b>{" "}
            {kinds
              .sort((x, y) => y[1] - x[1] || cmp(x[0], y[0]))
              .map(([k, n]) => `${k} ${n}`)
              .join(", ")}
          </span>
        );
      })}
    </div>
  );
}
