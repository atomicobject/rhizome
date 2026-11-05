// The Overview map (SPEC-0117 US1, US3): one node per member, sized by its
// records, edges counting the record links between members, faded outside
// neighbors on a ring, and members without links listed in the side panel. Nodes take
// keyboard focus most records first; hovering or focusing one highlights its
// edges and shows its figures. The Matrix is the text alternative.
import { openCollection, openView, useTypeColors } from "@rhizome/kit";
import { useEffect, useMemo, useRef, useState } from "react";

import { layoutMap, outsideOrigin, OUTSIDE_RADIUS, type Point } from "./map-layout.ts";
import { plural } from "./briefing-parts.tsx";
import { edgeKey, type MapEdge, type ScopeNode } from "./scope.ts";

/** The layout width before the canvas is measured, and where it cannot be measured. */
const WIDTH = 760;

/** The map's height: with its title and separate line-style legend the block stays within about 360px. */
const HEIGHT = 260;

/** A count's pill: 5px of surface beside its digits, on the edge it counts. */
const COUNT_PAD = 4;

const COUNT_DIGIT = 7;

const COUNT_HEIGHT = 17;

/** Edge strokes run from 1px to this many, by link count, so no edge outweighs the nodes it joins. */
const MAX_STROKE = 2.75;

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

import {
  OUTSIDE_RING,
  compositionArcs,
  nodeHue,
  unusedPairs,
  nodeRadius,
  nodeText,
  nodeName,
  NodeTip,
  EdgeTip,
  OutsideTip,
  MapLegend,
  type Hot,
  type MapProps,
} from "./overview-map-parts.tsx";

export { nodeHue, unusedPairs, OUTSIDE_RING } from "./overview-map-parts.tsx";

const fmt = (count: number) => count.toLocaleString("en-US");

export function ScopeMap({ model, links, relations, summaries, expanded, onExpand }: MapProps) {
  const [hot, setHot] = useState<Hot>(null);
  const color = useTypeColors();
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

    return layoutMap({
      width,
      height: HEIGHT,
      nodes: model.nodes.map((node) => ({
        id: node.id,
        radius: size(node),
        label: nodeText(node),
        alignToNode: node.kind === "group",
      })),
      edges: links.edges.map((edge) => ({
        ...edge,
        label: { key: edge.key, text: fmt(edge.links) },
      })),
      outside: links.outside.slice(0, OUTSIDE_RING).map((neighbor) => ({
        ...neighbor,
        label: `${neighbor.label}\n${fmt(neighbor.links)} links`,
      })),
    });
  }, [model, links, group, maxLinks, width]);

  const at = (id: string): Point => layout.positions.get(id) ?? { x: 0, y: 0 };

  const stroke = (count: number) => 1 + (MAX_STROKE - 1) * Math.sqrt(count / maxLinks);
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
          {ring.map((neighbor) => {
            const members = [...neighbor.byMember];
            const from = outsideOrigin(layout.positions, neighbor.byMember);

            const details =
              hot?.kind === "node" && (hot.id === neighbor.id || neighbor.byMember.has(hot.id));

            return (
              <g key={neighbor.id}>
                {details ? (
                  members.map(([member, count]) => (
                    <line
                      key={member}
                      className="gv-map-spoke"
                      data-hot="true"
                      x1={at(member).x}
                      y1={at(member).y}
                      x2={at(neighbor.id).x}
                      y2={at(neighbor.id).y}
                      strokeWidth={Math.max(1.25, stroke(count) * 0.6)}
                    />
                  ))
                ) : (
                  <line
                    className="gv-map-spoke"
                    x1={from.x}
                    y1={from.y}
                    x2={at(neighbor.id).x}
                    y2={at(neighbor.id).y}
                    strokeWidth={Math.max(1.25, stroke(neighbor.links) * 0.6)}
                  >
                    <title>
                      {neighbor.links} links to {neighbor.label}
                    </title>
                  </line>
                )}
              </g>
            );
          })}
          {[...layout.labels].map(
            ([id, place]) =>
              place.leader && (
                <line
                  key={`leader:${id}`}
                  className="gv-map-leader"
                  x1={at(id).x}
                  y1={at(id).y}
                  x2={place.leader.x}
                  y2={place.leader.y}
                />
              ),
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
              <path
                className="gv-map-hit"
                data-action={traceable(edge) ? "trace" : undefined}
                d={`M${at(edge.a).x},${at(edge.a).y} L${at(edge.b).x},${at(edge.b).y}`}
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
                <circle cx={point.x} cy={point.y} r={OUTSIDE_RADIUS} strokeDasharray="3 3" />
              </g>
            );
          })}
          {ordered.map((node) => {
            const point = at(node.id);
            const r = radius(node);
            const hue = nodeHue(node, color);
            const arcs = compositionArcs(node, summaries, color, r);

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
                <circle
                  cx={point.x}
                  cy={point.y}
                  r={r}
                  data-composite={arcs.length > 0 ? "true" : undefined}
                  fill={node.count === 0 ? "var(--surface)" : hue}
                  stroke={node.count === 0 ? hue : "var(--surface)"}
                />
                {arcs.map((arc) => {
                  // A donut inside the node's edge: each member type's share of the records.
                  const band = Math.max(4, r * 0.32);

                  return (
                    <circle
                      key={arc.name}
                      className="gv-map-arc"
                      cx={point.x}
                      cy={point.y}
                      r={r - band / 2}
                      stroke={arc.color}
                      strokeWidth={band}
                      pathLength={100}
                      strokeDasharray={`${arc.share * 100} ${100 - arc.share * 100}`}
                      strokeDashoffset={25 - arc.start * 100}
                    />
                  );
                })}
              </g>
            );
          })}
          {/* Text sits above every node and edge, on a halo of the surface color. */}
          <g className="gv-map-labels" aria-hidden="true">
            {links.edges.map((edge) => {
              const spot = countAt(edge);

              const text = fmt(edge.links);
              const w = text.length * COUNT_DIGIT + 2 * COUNT_PAD;

              return (
                spot && (
                  <g
                    key={edge.key}
                    data-hot={on(edge.key)}
                    data-kind={edge.relation ? "relation" : "plain"}
                    className="gv-map-count-wrap"
                  >
                    <rect
                      className="gv-map-count-halo"
                      x={spot.x - w / 2}
                      y={spot.y - COUNT_HEIGHT / 2}
                      width={w}
                      height={COUNT_HEIGHT}
                      rx={COUNT_HEIGHT / 2}
                    />
                    <text className="gv-map-count" x={spot.x} y={spot.y + 4}>
                      {text}
                    </text>
                  </g>
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
                    {neighbor.label}{" "}
                    <tspan className="gv-map-n" x={place.x} dy={17}>
                      {fmt(neighbor.links)} links
                    </tspan>
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
                    {node.label}{" "}
                    <tspan className="gv-map-n" x={place.x} dy={17}>
                      {fmt(node.count)} {plural(node.count, "note", "notes")}
                      {node.kind === "group"
                        ? ` · ${node.members.length} member ${plural(node.members.length, "type", "types")}`
                        : ""}
                    </tspan>
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
        drawn={ordered}
        summaries={summaries}
        color={color}
        unused={drawnUnused.length > 0}
        expanded={expanded}
        onExpand={onExpand}
      />
    </div>
  );
}
