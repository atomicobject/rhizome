// The Overview map (SPEC-0117 US1, US3): one node per member, sized by its
// records, edges counting the record links between members, faded outside
// neighbors on a ring, and members without links listed in the legend. Nodes take
// keyboard focus most records first; hovering or focusing one highlights its
// edges and shows its figures. The Matrix is the text alternative.
import { openCollection, openView } from "@rhizome/kit";
import { useEffect, useMemo, useRef, useState } from "react";

import { layoutMap, OUTSIDE_RADIUS, type Point } from "./map-layout.ts";
import { plural } from "./briefing-parts.tsx";
import {
  edgeKey,
  type DeclaredRelation,
  type LinkModel,
  type MapEdge,
  type OutsideNeighbor,
  type ScopeModel,
  type ScopeNode,
} from "./scope.ts";
import type { TypeSummary } from "./aggregate.ts";

/** Outside neighbors drawn on the ring; the legend counts the rest. */
export const OUTSIDE_RING = 7;

/** The layout width before the canvas is measured, and where it cannot be measured. */
const WIDTH = 760;

/** The map's height, small enough that the Members table starts above the fold. */
const HEIGHT = 280;

/** The canvas's width in CSS pixels, so the map fills its column and labels draw at their real size. */
function useCanvasWidth() {
  const ref = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(WIDTH);

  useEffect(() => {
    const element = ref.current;

    if (!element) return;
    const measure = (next: number) => next > 0 && setWidth(Math.round(next));

    // Without ResizeObserver, measure once so the layout and tooltips match the drawn canvas.
    if (typeof ResizeObserver === "undefined") {
      measure(element.clientWidth);

      return;
    }

    const observer = new ResizeObserver(([entry]) => measure(entry.contentRect.width));
    observer.observe(element);

    return () => observer.disconnect();
  }, []);

  return { ref, width };
}

// Categorical hues for named groups, from the shared type palette. The first
// palette color marks types in no named group; red (risk) and teal
// (connection) never mark a group.
const HUES = [2, 3, 4, 5, 6, 7].map((n) => `var(--type-${n})`);

const fmt = (count: number) => count.toLocaleString("en-US");

/** A node's color: its named group's hue, blue without one, neutral gray for untyped notes. */
export function nodeHue(model: ScopeModel, node: Pick<ScopeNode, "kind" | "group">) {
  if (node.kind === "untyped") return "var(--type-8)";
  const index = node.group === null ? -1 : model.groupNames.indexOf(node.group);

  return index < 0 ? "var(--type-1)" : HUES[index % HUES.length];
}

/** Declared relations no record uses between members with no edge, one hairline per pair. */
export function unusedPairs(links: LinkModel, relations: readonly DeclaredRelation[]) {
  const pairs = new Map<string, { a: string; b: string }>();

  for (const relation of relations) {
    const key = edgeKey(relation.member, relation.target);

    if (relation.count === 0 && !links.edgeIndex.has(key))
      pairs.set(key, { a: relation.member, b: relation.target });
  }

  return [...pairs.values()];
}

type Hot = { kind: "node"; id: string } | { kind: "edge"; key: string } | null;

type MapProps = {
  model: ScopeModel;
  links: LinkModel;
  relations: readonly DeclaredRelation[];
  summaries: ReadonlyMap<string, TypeSummary>;
  /** Named groups expanded at All notes. */
  expanded: ReadonlySet<string>;
  onExpand: (group: string, open: boolean) => void;
};

/** Node area follows record count. */
function nodeRadius(model: ScopeModel) {
  const max = Math.max(1, ...model.nodes.map((node) => node.count));

  return (node: ScopeNode) => 5 + 25 * Math.sqrt(node.count / max);
}

const typeCount = (node: ScopeNode) =>
  node.kind === "group"
    ? ` · ${node.members.length} ${plural(node.members.length, "type", "types")}`
    : "";

const nodeText = (node: ScopeNode) => `${node.label}${typeCount(node)} ${fmt(node.count)}`;

const memberLinks = (links: LinkModel, id: string) =>
  links.edges.reduce((sum, edge) => sum + (edge.a === id || edge.b === id ? edge.links : 0), 0);

function nodeName(model: ScopeModel, links: LinkModel, node: ScopeNode) {
  const unit =
    node.kind === "untyped"
      ? plural(node.count, "note", "notes")
      : plural(node.count, "record", "records");

  const linked = memberLinks(links, node.id);
  const inGroup = model.scope.kind === "group" ? " to other members" : "";

  const state =
    node.kind === "group"
      ? `, collapsed group of ${node.members.length} ${plural(node.members.length, "type", "types")}`
      : "";

  return `${node.label}, ${fmt(node.count)} ${unit}, ${fmt(linked)} ${plural(linked, "link", "links")}${inGroup}${state}`;
}

function NodeTip({ model, links, node }: { model: ScopeModel; links: LinkModel; node: ScopeNode }) {
  const own = links.self.get(node.id) ?? 0;
  const linked = memberLinks(links, node.id);

  return (
    <>
      <b>{node.label}</b>
      <span>
        {fmt(node.count)}{" "}
        {node.kind === "untyped"
          ? plural(node.count, "note", "notes")
          : plural(node.count, "record", "records")}{" "}
        · {fmt(linked)} {plural(linked, "link", "links")}
        {model.scope.kind === "group" ? " in the group" : ""}
      </span>
      {own > 0 && (
        <span>
          {fmt(own)} {node.kind === "group" ? "links inside the group" : "among themselves"}
        </span>
      )}
      {node.kind === "group" && <span>Activate to expand</span>}
    </>
  );
}

function EdgeTip({
  edge,
  label,
  summaries,
  trace,
}: {
  edge: MapEdge;
  label: (id: string) => string;
  summaries: ReadonlyMap<string, TypeSummary>;
  trace: boolean;
}) {
  return (
    <>
      <b>
        {label(edge.a)} — {label(edge.b)}
      </b>
      <span>
        {fmt(edge.links)} {plural(edge.links, "link", "links")}
      </span>
      {edge.fields.map((field) => (
        <span key={`${field.type}.${field.field}`}>
          {summaries.get(field.type)?.label ?? field.type}.{field.field} {fmt(field.count)}
        </span>
      ))}
      {edge.plainLinks > 0 && <span>plain links {fmt(edge.plainLinks)}</span>}
      {trace && <span>Activate to open Trace</span>}
    </>
  );
}

function OutsideTip({ model, neighbor }: { model: ScopeModel; neighbor: OutsideNeighbor }) {
  return (
    <>
      <b>{neighbor.label}</b>
      <span>
        {fmt(neighbor.links)} {plural(neighbor.links, "link", "links")} to{" "}
        {model.scope.kind === "group" ? model.scope.group : "the scope"} · outside the group
      </span>
    </>
  );
}

export function ScopeMap({ model, links, relations, summaries, expanded, onExpand }: MapProps) {
  const [hot, setHot] = useState<Hot>(null);
  const { ref: canvas, width } = useCanvasWidth();
  const group = model.scope.kind === "group" ? model.scope.group : null;
  const ring = links.outside.slice(0, OUTSIDE_RING);
  const unused = useMemo(() => unusedPairs(links, relations), [links, relations]);
  const radius = nodeRadius(model);

  const maxLinks = Math.max(
    1,
    ...links.edges.map((edge) => edge.links),
    ...ring.map((neighbor) => neighbor.links),
  );

  const layout = useMemo(() => {
    const size = nodeRadius(model);
    // A group labels every edge; All notes only edges carrying a visible share of links.
    const showCount = (edge: MapEdge) => group !== null || edge.links >= maxLinks * 0.03;

    return layoutMap({
      width,
      height: HEIGHT,
      nodes: model.nodes.map((node) => ({
        id: node.id,
        radius: size(node),
        label: nodeText(node),
      })),
      edges: links.edges.map((edge) => ({
        ...edge,
        label: showCount(edge) ? { key: edge.key, text: fmt(edge.links) } : undefined,
      })),
      outside: links.outside.slice(0, OUTSIDE_RING).map((neighbor) => ({
        ...neighbor,
        label: `${neighbor.label} ${fmt(neighbor.links)} links`,
      })),
    });
  }, [model, links, group, maxLinks, width]);

  const at = (id: string): Point => layout.positions.get(id) ?? { x: 0, y: 0 };

  const stroke = (count: number) => 1 + 7 * Math.sqrt(count / maxLinks);
  const label = (id: string) => model.nodeIndex.get(id)?.label ?? id;
  const traceable = (edge: MapEdge) => group !== null && edge.relationLinks > 0;

  // What the highlight keeps lit: the hot node and its edges and neighbors, or the hot edge and its ends.
  const lit = new Set<string>();

  if (hot?.kind === "node") {
    lit.add(hot.id);

    for (const edge of links.edges) {
      if (edge.a !== hot.id && edge.b !== hot.id) continue;
      lit.add(edge.key);
      lit.add(edge.a);
      lit.add(edge.b);
    }

    for (const neighbor of ring) {
      if (neighbor.id !== hot.id && !neighbor.byMember.has(hot.id)) continue;
      lit.add(neighbor.id);

      for (const member of neighbor.byMember.keys()) {
        if (neighbor.id === hot.id || member === hot.id) {
          lit.add(member);
          lit.add(`out:${member}:${neighbor.id}`);
        }
      }
    }
  } else if (hot?.kind === "edge") {
    const edge = links.edgeIndex.get(hot.key);
    lit.add(hot.key);

    if (edge) {
      lit.add(edge.a);
      lit.add(edge.b);
    }
  }

  const on = (id: string) => (hot && lit.has(id) ? "true" : undefined);
  const hotEdge = hot?.kind === "edge" ? links.edgeIndex.get(hot.key) : undefined;
  const hotNode = hot?.kind === "node" ? model.nodeIndex.get(hot.id) : undefined;

  const hotOutside =
    hot?.kind === "node" ? ring.find((neighbor) => neighbor.id === hot.id) : undefined;

  const tipAt = hotEdge
    ? { x: (at(hotEdge.a).x + at(hotEdge.b).x) / 2, y: (at(hotEdge.a).y + at(hotEdge.b).y) / 2 }
    : hot?.kind === "node"
      ? at(hot.id)
      : null;

  const activate = (node: ScopeNode) => {
    if (node.kind === "group") onExpand(node.name, true);
    else if (node.kind !== "untyped") openCollection(node.name);
  };

  const looseIds = new Set(layout.loose);

  // A declared relation to a member listed in the legend has no line to draw.
  const drawnUnused = unused.filter(
    (pair) => layout.positions.has(pair.a) && layout.positions.has(pair.b),
  );

  const ordered = [...model.nodes]
    .filter((node) => !looseIds.has(node.id))
    .sort((a, b) => b.count - a.count || a.label.localeCompare(b.label));

  const loose = model.nodes.filter((node) => looseIds.has(node.id));
  const countAt = (edge: MapEdge) => layout.edgeLabels.get(edge.key);

  return (
    <div className="gv-map-wrap">
      <div className="gv-map-canvas" ref={canvas} style={{ height: `${layout.height}px` }}>
        <svg
          className="gv-map"
          viewBox={`0 0 ${width} ${layout.height}`}
          role="group"
          aria-label={`${group ?? "All notes"} map`}
          data-dim={hot ? "true" : undefined}
        >
          {drawnUnused.map((pair) => (
            <line
              key={`unused:${edgeKey(pair.a, pair.b)}`}
              className="gv-map-unused"
              x1={at(pair.a).x}
              y1={at(pair.a).y}
              x2={at(pair.b).x}
              y2={at(pair.b).y}
            />
          ))}
          {ring.flatMap((neighbor) =>
            [...neighbor.byMember].map(([member, count]) => (
              <line
                key={`out:${member}:${neighbor.id}`}
                className="gv-map-spoke"
                data-hot={on(`out:${member}:${neighbor.id}`)}
                x1={at(member).x}
                y1={at(member).y}
                x2={at(neighbor.id).x}
                y2={at(neighbor.id).y}
                strokeWidth={Math.max(1.5, stroke(count) * 0.55)}
              />
            )),
          )}
          {links.edges.map((edge) => (
            <g
              key={edge.key}
              className="gv-map-edge"
              data-kind={edge.relation ? "relation" : "plain"}
              data-hot={on(edge.key)}
            >
              <line
                x1={at(edge.a).x}
                y1={at(edge.a).y}
                x2={at(edge.b).x}
                y2={at(edge.b).y}
                strokeWidth={stroke(edge.links)}
              />
              <line
                className="gv-map-hit"
                data-action={traceable(edge) ? "trace" : undefined}
                x1={at(edge.a).x}
                y1={at(edge.a).y}
                x2={at(edge.b).x}
                y2={at(edge.b).y}
                onMouseEnter={() => setHot({ kind: "edge", key: edge.key })}
                onMouseLeave={() => setHot(null)}
                onClick={() => {
                  if (group !== null && traceable(edge))
                    openView("group.trace", { kind: "group", group });
                }}
              />
            </g>
          ))}
          {ring.map((neighbor) => {
            const point = at(neighbor.id);

            return (
              <g
                key={neighbor.id}
                className="gv-map-node"
                data-outside="true"
                data-hot={on(neighbor.id)}
                onMouseEnter={() => setHot({ kind: "node", id: neighbor.id })}
                onMouseLeave={() => setHot(null)}
              >
                <title>{`${neighbor.label}, ${fmt(neighbor.links)} links, outside the group`}</title>
                <circle cx={point.x} cy={point.y} r={OUTSIDE_RADIUS} />
              </g>
            );
          })}
          {ordered.map((node) => {
            const point = at(node.id);
            const r = radius(node);
            const hue = nodeHue(model, node);

            return (
              <g
                key={node.id}
                className="gv-map-node"
                data-node={node.id}
                data-kind={node.kind}
                data-hot={on(node.id)}
                tabIndex={0}
                role={node.kind === "untyped" ? "img" : "button"}
                aria-label={nodeName(model, links, node)}
                onMouseEnter={() => setHot({ kind: "node", id: node.id })}
                onMouseLeave={() => setHot(null)}
                onFocus={() => setHot({ kind: "node", id: node.id })}
                onBlur={() => setHot(null)}
                onClick={() => activate(node)}
                onKeyDown={(event) => {
                  if (event.key !== "Enter" && event.key !== " ") return;
                  event.preventDefault();
                  activate(node);
                }}
              >
                {node.kind === "group" && (
                  <circle
                    className="gv-map-ring"
                    cx={point.x}
                    cy={point.y}
                    r={r + 4}
                    stroke={hue}
                  />
                )}
                <circle
                  cx={point.x}
                  cy={point.y}
                  r={r}
                  fill={node.count === 0 ? "var(--surface)" : hue}
                  stroke={node.count === 0 ? hue : "var(--surface)"}
                />
              </g>
            );
          })}
          {/* Text sits above every node and edge, on a halo of the surface color. */}
          <g className="gv-map-labels" aria-hidden="true">
            {links.edges.map((edge) => {
              const spot = countAt(edge);

              return (
                spot && (
                  <text
                    key={edge.key}
                    className="gv-map-count"
                    data-kind={edge.relation ? "relation" : "plain"}
                    data-hot={on(edge.key)}
                    x={spot.x}
                    y={spot.y}
                  >
                    {fmt(edge.links)}
                  </text>
                )
              );
            })}
            {ring.map((neighbor) => {
              const place = layout.labels.get(neighbor.id);

              return (
                place && (
                  <text
                    key={neighbor.id}
                    data-outside="true"
                    data-hot={on(neighbor.id)}
                    x={place.x}
                    y={place.y}
                    textAnchor={place.anchor}
                  >
                    {neighbor.label} <tspan className="gv-map-n">{fmt(neighbor.links)} links</tspan>
                  </text>
                )
              );
            })}
            {ordered.map((node) => {
              const place = layout.labels.get(node.id);

              return (
                place && (
                  <text
                    key={node.id}
                    data-hot={on(node.id)}
                    data-group={node.kind === "group" ? "true" : undefined}
                    x={place.x}
                    y={place.y}
                    textAnchor={place.anchor}
                  >
                    {node.label}
                    {node.kind === "group" && (
                      <tspan className="gv-map-n">{typeCount(node)}</tspan>
                    )}{" "}
                    <tspan className="gv-map-n">{fmt(node.count)}</tspan>
                  </text>
                )
              );
            })}
          </g>
        </svg>
        {tipAt && (hotNode || hotEdge || hotOutside) && (
          <div
            className="gv-map-tip"
            role="tooltip"
            style={{ left: `${tipAt.x}px`, top: `${tipAt.y}px` }}
          >
            {hotNode && <NodeTip model={model} links={links} node={hotNode} />}
            {hotEdge && (
              <EdgeTip
                edge={hotEdge}
                label={label}
                summaries={summaries}
                trace={traceable(hotEdge)}
              />
            )}
            {hotOutside && <OutsideTip model={model} neighbor={hotOutside} />}
          </div>
        )}
      </div>
      <MapLegend
        model={model}
        links={links}
        loose={loose}
        unused={drawnUnused.length > 0}
        expanded={expanded}
        onExpand={onExpand}
        onOpen={activate}
      />
    </div>
  );
}

function MapLegend({
  model,
  links,
  loose,
  unused,
  expanded,
  onExpand,
  onOpen,
}: {
  model: ScopeModel;
  links: LinkModel;
  loose: readonly ScopeNode[];
  unused: boolean;
  expanded: ReadonlySet<string>;
  onExpand: (group: string, open: boolean) => void;
  onOpen: (node: ScopeNode) => void;
}) {
  const shown = Math.min(OUTSIDE_RING, links.outside.length);
  const collapsed = model.groupNames.filter((name) => !expanded.has(name));

  return (
    <>
      <p className="gv-legend">
        {model.scope.kind === "workspace" && (
          <>
            {model.groupNames.map((name) => (
              <span key={name}>
                <i style={{ background: nodeHue(model, { kind: "group", group: name }) }} />
                {name}
                {expanded.has(name) && (
                  <>
                    {" "}
                    <button type="button" className="gv-link" onClick={() => onExpand(name, false)}>
                      collapse
                    </button>
                  </>
                )}
              </span>
            ))}
            <span>
              <i style={{ background: nodeHue(model, { kind: "type", group: null }) }} />
              No group
            </span>
            <span>
              <i style={{ background: nodeHue(model, { kind: "untyped", group: null }) }} />
              Untyped
            </span>
          </>
        )}
        <span>
          <s data-kind="relation" />
          mostly relation links
        </span>
        <span>
          <s data-kind="plain" />
          mostly plain links
        </span>
        {unused && (
          <span>
            <s data-kind="unused" />
            declared, unused
          </span>
        )}
        {links.outside.length > 0 && (
          <span>
            <i className="gv-legend-outside" />
            outside the group
            {links.outside.length > shown ? ` (top ${shown} of ${links.outside.length})` : ""}
          </span>
        )}
        {loose.length > 0 && (
          <span className="gv-legend-loose">
            No member links:
            {loose.map((node) => (
              <button
                key={node.id}
                type="button"
                className="gv-link"
                aria-label={nodeName(model, links, node)}
                onClick={() => onOpen(node)}
              >
                {node.label} <span className="gv-map-n">{fmt(node.count)}</span>
              </button>
            ))}
          </span>
        )}
        {model.scope.kind === "workspace" && collapsed.length > 0 && (
          <span className="gv-faint">Activate a group to expand it.</span>
        )}
      </p>
      {model.scope.kind === "group" && links.edges.length === 0 && (
        <p className="gv-quiet">No member records link to each other.</p>
      )}
    </>
  );
}
