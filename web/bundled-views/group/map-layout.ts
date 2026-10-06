// Positions for the Overview map (SPEC-0117 US1). Pure and deterministic: the
// same input always yields the same positions, with no randomness.
//
// Linked members get a small force layout seeded on a golden-angle spiral and
// stretched to the box. Members with no links to other members sit in labeled
// rows along the bottom. Outside neighbors sit on an ellipse at the edge, each
// at the angle of the members it links to, spread so none overlap. Each label
// goes below, above, right, or left of its node, whichever first clears the
// labels and nodes already placed, largest nodes first.

export type LayoutNode = { id: string; radius: number; label: string };

export type LayoutEdge = { a: string; b: string; links: number };

export type LayoutOutside = { id: string; label: string; byMember: ReadonlyMap<string, number> };

export type LayoutInput = {
  width: number;
  height: number;
  nodes: readonly LayoutNode[];
  edges: readonly LayoutEdge[];
  outside: readonly LayoutOutside[];
};

export type Point = { x: number; y: number };

export type LabelPlacement = Point & { anchor: "start" | "middle" | "end" };

export type MapLayout = {
  positions: ReadonlyMap<string, Point>;
  labels: ReadonlyMap<string, LabelPlacement>;
  /** Members with no links to other members, in the bottom rows. */
  loose: readonly string[];
  /** The top of the bottom rows, or null when every member is linked. */
  looseTop: number | null;
};

export const OUTSIDE_RADIUS = 6;

const ITERATIONS = 400;

const ROW_HEIGHT = 30;

/** The row label's width, before the first loose member. */
const ROW_START = 104;

/** Average label glyph width at the map's font size. */
const GLYPH = 6;

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

  const sx = (width - 2 * padX) / Math.max(maxX - minX, 1);
  const sy = (height - 2 * padY) / Math.max(maxY - minY, 1);
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

/** Loose members left to right in rows from the bottom up; returns the rows used. */
function placeLoose(
  positions: Map<string, Point>,
  loose: readonly LayoutNode[],
  width: number,
  height: number,
) {
  let row = 0;
  let x = ROW_START;

  for (const node of loose) {
    const span = 2 * node.radius + 10 + node.label.length * GLYPH + 24;

    if (x > ROW_START && x + span > width) {
      row += 1;
      x = ROW_START;
    }

    positions.set(node.id, { x: x + node.radius, y: height - ROW_HEIGHT / 2 - row * ROW_HEIGHT });
    x += span;
  }

  return loose.length ? row + 1 : 0;
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

  for (const { id, angle } of angles) {
    positions.set(id, {
      x: cx + (width / 2 - 40) * Math.cos(angle),
      y: cy + (height / 2 - 16) * Math.sin(angle),
    });
  }
}

type Box = { x: number; y: number; w: number; h: number };

const overlaps = (a: Box, b: Box) =>
  a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y;

function placeLabels(
  positions: ReadonlyMap<string, Point>,
  items: readonly { id: string; radius: number; label: string; loose: boolean }[],
  width: number,
  height: number,
) {
  const boxes: Box[] = items.flatMap((item) => {
    const point = positions.get(item.id);

    return point
      ? [
          {
            x: point.x - item.radius - 3,
            y: point.y - item.radius - 3,
            w: 2 * item.radius + 6,
            h: 2 * item.radius + 6,
          },
        ]
      : [];
  });

  const labels = new Map<string, LabelPlacement>();

  for (const item of [...items].sort((a, b) => b.radius - a.radius || a.id.localeCompare(b.id))) {
    const point = positions.get(item.id);

    if (!point) continue;
    const { x, y } = point;
    const r = item.radius;
    const w = item.label.length * GLYPH;
    const h = 13;

    const candidates = [
      { x, y: y + r + 12, anchor: "middle" as const, box: { x: x - w / 2, y: y + r + 2, w, h } },
      { x, y: y - r - 5, anchor: "middle" as const, box: { x: x - w / 2, y: y - r - 16, w, h } },
      { x: x + r + 5, y: y + 4, anchor: "start" as const, box: { x: x + r + 4, y: y - 7, w, h } },
      { x: x - r - 5, y: y + 4, anchor: "end" as const, box: { x: x - r - 4 - w, y: y - 7, w, h } },
    ].filter(
      ({ box }) =>
        box.x > 2 && box.x + box.w < width - 2 && box.y > 2 && box.y + box.h < height - 2,
    );

    // A loose member's label reads along its row.
    if (item.loose)
      candidates.sort((a, b) => Number(b.anchor === "start") - Number(a.anchor === "start"));

    const chosen = candidates.find(
      (candidate) => !boxes.some((box) => overlaps(box, candidate.box)),
    ) ??
      candidates[0] ?? { x, y: y + r + 12, anchor: "middle" as const, box: { x, y, w: 0, h: 0 } };

    boxes.push(chosen.box);
    labels.set(item.id, { x: chosen.x, y: chosen.y, anchor: chosen.anchor });
  }

  return labels;
}

export function layoutMap(input: LayoutInput): MapLayout {
  const { width, height } = input;
  const linked = new Set(input.edges.flatMap((edge) => [edge.a, edge.b]));
  const core = input.nodes.filter((node) => linked.has(node.id));
  const loose = input.nodes.filter((node) => !linked.has(node.id));
  const looseOnly = new Map<string, Point>();
  const rows = placeLoose(looseOnly, loose, width, height);
  const top = height - rows * ROW_HEIGHT;
  const positions = forceLayout(core, input.edges, width, top);
  const ring = input.outside.length > 0;

  if (core.length) fit(positions, width, top, ring ? 190 : 80, ring ? 70 : 30);

  for (const [id, point] of looseOnly) positions.set(id, point);

  if (ring) placeRing(positions, input.outside, width, top);

  const labels = placeLabels(
    positions,
    [
      ...input.nodes.map((node) => ({ ...node, loose: !linked.has(node.id) })),
      ...input.outside.map((neighbor) => ({
        id: neighbor.id,
        radius: OUTSIDE_RADIUS,
        label: neighbor.label,
        loose: false,
      })),
    ],
    width,
    height,
  );

  return { positions, labels, loose: loose.map((node) => node.id), looseTop: rows ? top : null };
}
