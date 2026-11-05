import { placeLabels, placeEdgeLabels, type Box } from "./map-labels.ts";

export { placeEdgeLabels } from "./map-labels.ts";
// Positions for the Overview map (SPEC-0117 US1). Pure and deterministic: the
// same input always yields the same positions, with no randomness.
//
// Linked members get a small force layout seeded on a golden-angle spiral and
// stretched to the box, every node at least INSET inside it. Members with no
// links at all, inside or outside the group, are left out of the drawing (the side panel lists them),
// unless no member is linked, when they spread over the whole box. Outside
// neighbors sit on an ellipse at the edge, each at the angle of the members it
// links to, spread so none overlap. Each label goes on the first of eight
// sides of its node that clears the nodes, labels, and edges already placed,
// largest nodes first.

export type LayoutNode = { id: string; radius: number; label: string };

export type LayoutEdge = {
  a: string;
  b: string;
  links: number;
  /** Text to draw on the edge, looked up in `edgeLabels` by its key. */
  label?: { key: string; text: string };
};

export type LayoutOutside = { id: string; label: string; byMember: ReadonlyMap<string, number> };

export type LayoutInput = {
  width: number;
  height: number;
  nodes: readonly LayoutNode[];
  edges: readonly LayoutEdge[];
  outside: readonly LayoutOutside[];
};

export type Point = { x: number; y: number };

export function outsideOrigin(
  positions: ReadonlyMap<string, Point>,
  byMember: ReadonlyMap<string, number>,
): Point {
  const total = [...byMember.values()].reduce((sum, count) => sum + count, 0) || 1;

  return [...byMember].reduce(
    (sum, [id, count]) => {
      const point = positions.get(id);

      return point
        ? { x: sum.x + (point.x * count) / total, y: sum.y + (point.y * count) / total }
        : sum;
    },
    { x: 0, y: 0 },
  );
}

export type LabelPlacement = Point & { anchor: "start" | "middle" | "end"; leader?: Point };

export type MapLayout = {
  height: number;
  positions: ReadonlyMap<string, Point>;
  labels: ReadonlyMap<string, LabelPlacement>;
  /** Members with no links to other members, not drawn; empty when no member is linked. */
  loose: readonly string[];
  /** Where each edge label sits; an edge whose label would cover a node or its label has none. */
  edgeLabels: ReadonlyMap<string, Point>;
};

export const OUTSIDE_RADIUS = 6;

/** Room kept outside the ring of outside neighbors for their names. */
const RING_LABEL_GUTTER = 150;

const ITERATIONS = 400;

/** Average label glyph width at the map's 13px font, generous so boxes cover the drawn text. */
const GLYPH = 7.5;

/** The least gap between a node's edge and the box's edge. */
export const INSET = 24;

function forceLayout(
  nodes: readonly LayoutNode[],
  edges: readonly LayoutEdge[],
  width: number,
  height: number,
) {
  const positions = new Map<string, Point>();
  const golden = Math.PI * (3 - Math.sqrt(5));

  nodes.forEach((node, index) => {
    const r = 40 * Math.sqrt(index + 0.5);
    positions.set(node.id, {
      x: width / 2 + r * Math.cos(index * golden),
      y: height / 2 + r * Math.sin(index * golden),
    });
  });

  const k = Math.sqrt((width * height) / Math.max(nodes.length, 1)) * 0.8;
  const at = (id: string) => positions.get(id) ?? { x: 0, y: 0 };

  for (let step = 0; step < ITERATIONS; step++) {
    const limit = 18 * (1 - step / ITERATIONS) + 0.4;
    const shift = new Map(nodes.map((node) => [node.id, { x: 0, y: 0 }]));
    const move = (id: string) => shift.get(id) ?? { x: 0, y: 0 };

    for (let i = 0; i < nodes.length; i++) {
      for (let j = i + 1; j < nodes.length; j++) {
        const a = at(nodes[i].id);
        const b = at(nodes[j].id);
        const distance = Math.hypot(a.x - b.x, a.y - b.y) || 0.01;
        const gap = nodes[i].radius + nodes[j].radius + 26;
        const force = (k * k) / distance + (distance < gap ? (gap - distance) * 4 : 0);
        const dx = ((a.x - b.x) / distance) * force;
        const dy = ((a.y - b.y) / distance) * force;
        move(nodes[i].id).x += dx;
        move(nodes[i].id).y += dy;
        move(nodes[j].id).x -= dx;
        move(nodes[j].id).y -= dy;
      }
    }

    for (const edge of edges) {
      const a = at(edge.a);
      const b = at(edge.b);
      const distance = Math.hypot(a.x - b.x, a.y - b.y) || 0.01;
      const force = ((distance * distance) / k) * (0.2 + Math.log10(edge.links + 1) / 4) * 0.35;
      const dx = ((a.x - b.x) / distance) * force;
      const dy = ((a.y - b.y) / distance) * force;
      move(edge.a).x -= dx;
      move(edge.a).y -= dy;
      move(edge.b).x += dx;
      move(edge.b).y += dy;
    }

    for (const node of nodes) {
      const point = at(node.id);
      const delta = move(node.id);
      delta.x += (width / 2 - point.x) * 0.2;
      delta.y += (height / 2 - point.y) * 0.2;
      const length = Math.hypot(delta.x, delta.y) || 1;
      point.x += (delta.x / length) * Math.min(length, limit);
      point.y += (delta.y / length) * Math.min(length, limit);
    }
  }

  return positions;
}

/** Stretch the points to the box, keeping the aspect within a factor of two. */
function fit(
  positions: Map<string, Point>,
  width: number,
  height: number,
  padX: number,
  padY: number,
) {
  const points = [...positions.values()];

  if (!points.length) return;
  const xs = points.map((point) => point.x);
  const ys = points.map((point) => point.y);

  const [minX, maxX, minY, maxY] = [
    Math.min(...xs),
    Math.max(...xs),
    Math.min(...ys),
    Math.max(...ys),
  ];

  // Padding never takes more than 70% of the box, so a narrow canvas still spreads its members.
  const sx = Math.max(width - 2 * padX, width * 0.3) / Math.max(maxX - minX, 1);
  const sy = Math.max(height - 2 * padY, height * 0.3) / Math.max(maxY - minY, 1);
  const scale = Math.min(sx, sy, 3);
  const tx = Math.min(sx, scale * 2, 4);
  const ty = Math.min(sy, scale * 2, 4);
  const cx = (minX + maxX) / 2;
  const cy = (minY + maxY) / 2;

  for (const point of points) {
    point.x = width / 2 + (point.x - cx) * tx;
    point.y = height / 2 + (point.y - cy) * ty;
  }
}

/** Outside neighbors on an ellipse, each toward the members it links to, at least 0.6 rad apart. */
function placeRing(
  positions: Map<string, Point>,
  outside: readonly LayoutOutside[],
  width: number,
  height: number,
) {
  const cx = width / 2;
  const cy = height / 2;

  const angles = outside
    .map((neighbor) => {
      let x = 0;
      let y = 0;

      for (const [member, links] of neighbor.byMember) {
        const point = positions.get(member);

        if (!point) continue;
        x += (point.x - cx) * links;
        y += (point.y - cy) * links;
      }

      return { id: neighbor.id, angle: Math.atan2(y || 0.001, x || 0.001) };
    })
    .sort((a, b) => a.angle - b.angle || a.id.localeCompare(b.id));

  for (let pass = 0; pass < 20 && angles.length > 1; pass++) {
    for (let i = 0; i < angles.length; i++) {
      const current = angles[i];
      const next = angles[(i + 1) % angles.length];
      const gap = next.angle - current.angle + (i === angles.length - 1 ? 2 * Math.PI : 0);

      if (gap < 0.6) {
        current.angle -= (0.6 - gap) / 2;
        next.angle += (0.6 - gap) / 2;
      }
    }
  }

  // The ring leaves a label gutter outside it, so a neighbor's name can sit
  // beyond its node, clear of the links running in to it.
  const rx = Math.max(width / 2 - INSET - RING_LABEL_GUTTER, width * 0.3);

  for (const { id, angle } of angles) {
    positions.set(id, {
      x: cx + rx * Math.cos(angle),
      y: cy + (height / 2 - INSET - OUTSIDE_RADIUS) * Math.sin(angle),
    });
  }
}

/** The share of the box the drawing, labels included, should fill. */
const FILL = 0.85;

function union(boxes: readonly Box[]): Box | undefined {
  if (!boxes.length) return undefined;
  const x = Math.min(...boxes.map((box) => box.x));
  const y = Math.min(...boxes.map((box) => box.y));
  const right = Math.max(...boxes.map((box) => box.x + box.w));
  const bottom = Math.max(...boxes.map((box) => box.y + box.h));

  return { x, y, w: right - x, h: bottom - y };
}

/** Keep every node at least INSET inside the box. */
function inset(
  positions: Map<string, Point>,
  nodes: readonly LayoutNode[],
  width: number,
  height: number,
) {
  for (const node of nodes) {
    const point = positions.get(node.id);

    if (!point) continue;
    const pad = node.radius + INSET;
    point.x = Math.min(Math.max(point.x, pad), Math.max(pad, width - pad));
    const padY = node.radius + 58;
    point.y = Math.min(Math.max(point.y, padY), Math.max(padY, height - padY));
  }
}

function layoutAtSize(input: LayoutInput): MapLayout {
  const { width, height } = input;

  // A member with only outside links still needs a position for its spokes.
  const linked = new Set([
    ...input.edges.flatMap((edge) => [edge.a, edge.b]),
    ...input.outside.flatMap((neighbor) => [...neighbor.byMember.keys()]),
  ]);

  const core = input.nodes.filter((node) => linked.has(node.id));
  // With no linked members there is nothing to set the unlinked ones apart from.
  const spread = core.length === 0;
  const drawn = spread ? input.nodes : core;
  const loose = spread ? [] : input.nodes.filter((node) => !linked.has(node.id));
  const positions = forceLayout(drawn, input.edges, width, height);
  const ring = input.outside.length > 0;

  if (positions.size)
    fit(positions, width, height, ring ? 140 : spread ? 140 : 80, ring ? 70 : spread ? 60 : 30);

  inset(positions, drawn, width, height);

  if (ring) placeRing(positions, input.outside, width, height);

  const segments = () => {
    const list: [Point, Point][] = [];

    const segment = (a: string, b: string) => {
      const from = positions.get(a);
      const to = positions.get(b);

      if (from && to) list.push([from, to]);
    };

    for (const edge of input.edges) segment(edge.a, edge.b);

    for (const neighbor of input.outside) {
      const target = positions.get(neighbor.id);

      if (target) list.push([outsideOrigin(positions, neighbor.byMember), target]);
    }

    return list;
  };

  const place = () => {
    for (let pass = 0; pass < 10; pass++) {
      for (const edge of input.edges) {
        const a = positions.get(edge.a);
        const b = positions.get(edge.b);

        if (!a || !b || !edge.label || edge.a === edge.b) continue;

        const radius = Math.max(
          ...drawn
            .filter((node) => node.id === edge.a || node.id === edge.b)
            .map((node) => node.radius),
        );

        const distance = Math.hypot(b.x - a.x, b.y - a.y) || 1;
        const extra = (2 * radius + edge.label.text.length * GLYPH + 24 - distance) / 2;

        if (extra <= 0) continue;

        const dx = ((b.x - a.x) / distance) * extra;
        const dy = ((b.y - a.y) / distance) * extra;
        a.x -= dx;
        a.y -= dy;
        b.x += dx;
        b.y += dy;
      }
    }

    // Outside marks need room for member labels as well as their circles.
    for (let pass = 0; pass < 8; pass++) {
      for (const neighbor of input.outside) {
        const point = positions.get(neighbor.id);

        if (!point) continue;

        for (const node of drawn) {
          const member = positions.get(node.id);

          if (!member) continue;

          const dx = point.x - member.x,
            dy = point.y - member.y;

          const distance = Math.hypot(dx, dy) || 1;
          const gap = node.radius + OUTSIDE_RADIUS + 90;

          if (distance >= gap) continue;
          point.x += (dx / distance) * (gap - distance);
          point.y += (dy / distance) * (gap - distance);
          point.x = Math.max(INSET, Math.min(width - INSET, point.x));
          point.y = Math.max(INSET, Math.min(height - INSET, point.y));
        }
      }
    }

    return placeLabels(
      positions,
      [
        ...drawn,
        ...input.outside.map((neighbor) => {
          const point = positions.get(neighbor.id) ?? { x: width / 2, y: height / 2 };

          return {
            id: neighbor.id,
            radius: OUTSIDE_RADIUS,
            label: neighbor.label,
            outward: { x: point.x - width / 2, y: point.y - height / 2 },
          };
        }),
      ],
      segments(),
      width,
      height,
      input.edges.flatMap((edge) => {
        const a = positions.get(edge.a);
        const b = positions.get(edge.b);

        if (!a || !b || !edge.label) return [];
        const w = edge.label.text.length * GLYPH + 4;

        return [{ x: (a.x + b.x - w) / 2, y: (a.y + b.y) / 2 - 9, w, h: 18 }];
      }),
    );
  };

  // The drawing, labels included, fills about FILL of the box: spread the
  // positions until it does, re-placing labels each pass.
  let placed = place();

  for (let pass = 0; pass < 4; pass++) {
    const bounds = union(placed.boxes);

    if (!bounds) break;
    let sx = Math.min((width - 2 * INSET) / Math.max(bounds.w, 1), 2.5);
    let sy = Math.min((FILL * height) / Math.max(bounds.h, 1), 2.5);
    [sx, sy] = [Math.min(sx, sy * 3), Math.min(sy, sx * 3)];

    if (Math.abs(sx - 1) < 0.03 && Math.abs(sy - 1) < 0.03) break;
    const cx = bounds.x + bounds.w / 2;
    const cy = bounds.y + bounds.h / 2;

    for (const point of positions.values()) {
      point.x = width / 2 + (point.x - cx) * sx;
      point.y = height / 2 + (point.y - cy) * sy;
    }

    inset(positions, drawn, width, height);
    placed = place();
  }

  // Centre the complete drawing, including labels, rather than only its nodes.
  const bounds = union(placed.boxes);
  const dx = bounds ? (width - bounds.w) / 2 - bounds.x : 0;

  for (const point of positions.values()) point.x += dx;

  for (const label of placed.labels.values()) {
    label.x += dx;

    if (label.leader) label.leader.x += dx;
  }

  const labels = placed.labels;
  const boxes = placed.boxes.map((box) => ({ ...box, x: box.x + dx }));

  return {
    height,
    positions,
    labels,
    loose: loose.map((node) => node.id),
    edgeLabels: placeEdgeLabels(positions, input.edges, boxes, segments()),
  };
}

/** Add room for crowded expansions rather than silently dropping their labels. */
export function layoutMap(input: LayoutInput): MapLayout {
  let result = layoutAtSize(input);

  for (
    let attempt = 1;
    result.labels.size < result.positions.size && attempt <= result.positions.size;
    attempt++
  ) {
    result = layoutAtSize({ ...input, height: input.height + attempt * 60 });
  }

  return result;
}
