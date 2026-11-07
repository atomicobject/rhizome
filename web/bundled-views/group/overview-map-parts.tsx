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

export const OUTSIDE_RING = 7;

const fmt = (count: number) => count.toLocaleString("en-US");

/** The neutral: interfaces, collapsed groups, and untyped notes. */
const NEUTRAL = "var(--type-8)";

/** A type's palette color, from the kit's `useTypeColors`. */
export type TypeColor = (name: string) => string;

/**
 * A node's color. A type keeps the hue it has in the Atlas, Explorer, and
 * type pages; interfaces, collapsed groups, and untyped notes are neutral, and
 * the map rings an interface or group in its member types' hues.
 */
export function nodeHue(node: Pick<ScopeNode, "kind" | "name">, color: TypeColor) {
  return node.kind === "type" || node.kind === "interface" ? color(node.name) : NEUTRAL;
}

/** The ring around a node that stands for several types: one arc per type, sized by records. */
export function compositionArcs(
  node: ScopeNode,
  summaries: ReadonlyMap<string, TypeSummary>,
  color: TypeColor,
  radius = 30,
) {
  if (node.kind !== "group" && node.kind !== "interface") return [];

  const parts = node.types
    .map((name) => ({ name, count: summaries.get(name)?.count ?? 0 }))
    .filter((part) => part.count > 0);

  const total = parts.reduce((sum, part) => sum + part.count, 0);
  const circumference = 2 * Math.PI * (radius - Math.max(4, radius * 0.32) / 2);
  const minimum = Math.min(4 / circumference, 1 / parts.length);
  let remaining = 1;
  let weight = total;
  const shares = new Map<string, number>();

  for (const part of [...parts].sort((a, b) => a.count - b.count)) {
    const share = Math.max(minimum, (remaining * part.count) / weight);
    shares.set(part.name, share);
    remaining -= share;
    weight -= part.count;
  }

  let start = 0;

  return parts.map((part) => {
    const arc = {
      name: part.name,
      color: color(part.name),
      start,
      share: shares.get(part.name) ?? 0,
    };

    start += arc.share;

    return arc;
  });
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

export type Hot = { kind: "node"; id: string } | { kind: "edge"; key: string } | null;

export type MapProps = {
  model: ScopeModel;
  links: LinkModel;
  relations: readonly DeclaredRelation[];
  summaries: ReadonlyMap<string, TypeSummary>;
  /** Named groups expanded at All notes. */
  expanded: ReadonlySet<string>;
  onExpand: (group: string, open: boolean) => void;
};

/** Node area follows record count. */
export function nodeRadius(model: ScopeModel) {
  const max = Math.max(1, ...model.nodes.map((node) => node.count));

  return (node: ScopeNode) => 5 + 25 * Math.sqrt(node.count / max);
}

export const nodeText = (node: ScopeNode) =>
  `${node.label}\n${fmt(node.count)} ${plural(node.count, "note", "notes")}${node.kind === "group" ? ` · ${node.members.length} member ${plural(node.members.length, "type", "types")}` : ""}`;

const memberLinks = (links: LinkModel, id: string) =>
  links.edges.reduce((sum, edge) => sum + (edge.a === id || edge.b === id ? edge.links : 0), 0);

export function nodeName(model: ScopeModel, links: LinkModel, node: ScopeNode) {
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

export function NodeTip({
  model,
  links,
  node,
}: {
  model: ScopeModel;
  links: LinkModel;
  node: ScopeNode;
}) {
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

export function EdgeTip({
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
      {edge.plainLinks > 0 && <span>Plain links {fmt(edge.plainLinks)}</span>}
      {edge.plainLinks > 0 && edge.relationLinks > 0 && (
        <span>Mostly {edge.relation ? "relations" : "plain links"}</span>
      )}
      {trace && <span>Activate to open Trace</span>}
    </>
  );
}

export function OutsideTip({ model, neighbor }: { model: ScopeModel; neighbor: OutsideNeighbor }) {
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

export function MapLegend({
  model,
  links,
  drawn,
  summaries,
  color,
  unused,
  expanded,
  onExpand,
}: {
  model: ScopeModel;
  links: LinkModel;
  /** The members drawn on the map; the legend keys only what they show. */
  drawn: readonly ScopeNode[];
  summaries: ReadonlyMap<string, TypeSummary>;
  color: TypeColor;
  unused: boolean;
  expanded: ReadonlySet<string>;
  onExpand: (group: string, open: boolean) => void;
}) {
  const shown = Math.min(OUTSIDE_RING, links.outside.length);
  // An interface's ring names its segments; a collapsed group's ring stays one key.
  const rings = drawn.filter((node) => node.kind === "interface" || node.kind === "group");
  const label = (name: string) => summaries.get(name)?.label ?? name;

  const familyLabel = (name: string, family: ScopeNode) => {
    const suffix = label(name).split(" ").at(-1);

    return family.kind === "interface" &&
      family.types.length > 1 &&
      suffix &&
      family.types.every((type) => label(type).endsWith(` ${suffix}`))
      ? label(name).slice(0, -(suffix.length + 1))
      : label(name);
  };

  const types = drawn
    .filter((node) => node.kind !== "group" && node.kind !== "interface" && node.kind !== "untyped")
    .map((node) => (
      <span key={node.id}>
        <i style={{ background: nodeHue(node, color) }} />
        {node.label} {fmt(node.count)}
      </span>
    ));

  const kinds = [
    links.edges.some((edge) => edge.relation) && (
      <span key="relation">
        <s data-kind="relation" />
        Relations
      </span>
    ),
    links.edges.some((edge) => !edge.relation) && (
      <span key="plain">
        <s data-kind="plain" />
        Plain links
      </span>
    ),
    unused && (
      <span key="unused">
        <s data-kind="unused" />
        declared, unused
      </span>
    ),
  ].filter((item) => item !== false);

  if (links.outside.length > 0)
    kinds.push(
      <span key="outside">
        <s data-kind="outside" />
        Outside links
        {links.outside.length > shown && ` (top ${shown} of ${links.outside.length})`}
      </span>,
    );

  const groups = [
    ...rings.map((node) => ({
      title: node.label,
      // Types with no records draw no segment; Coverage already names them.
      items: node.types
        .filter((name) => (summaries.get(name)?.count ?? 0) > 0)
        .map((name) => (
          <span key={name}>
            <i style={{ background: color(name) }} />
            {familyLabel(
              name,
              node.kind === "group"
                ? (node.members.find((member) => member.types.includes(name)) ?? node)
                : node,
            )}{" "}
            {fmt(summaries.get(name)?.count ?? 0)}
          </span>
        )),
    })),
    { title: "Types", items: types },
    {
      title: "Untyped",
      items: drawn.some((node) => node.kind === "untyped")
        ? [
            <span key="untyped">
              <i className="gv-legend-untyped" />
              {model.untyped?.label ?? "Untyped notes"}
            </span>,
          ]
        : [],
    },
    ...model.groupNames
      .filter((name) => expanded.has(name))
      .map((name) => ({
        title: name,
        items: [
          <button
            key={name}
            type="button"
            className="gv-link"
            onClick={() => onExpand(name, false)}
          >
            Collapse
          </button>,
        ],
      })),
  ].filter((group) => group.items.length > 0);

  if (kinds.length) groups.push({ title: "Lines", items: kinds });

  return (
    <>
      <dl className="gv-legend gv-map-legend">
        {groups.map((group) => (
          <div key={group.title} className="gv-map-legend-row">
            <dt>{group.title}</dt>
            <dd>{group.items}</dd>
          </div>
        ))}
      </dl>
      {model.scope.kind === "group" && links.edges.length === 0 && (
        <p className="gv-quiet">No member records link to each other.</p>
      )}
    </>
  );
}
