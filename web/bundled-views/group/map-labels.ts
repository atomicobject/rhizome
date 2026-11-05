import type { Point, LabelPlacement, LayoutEdge } from "./map-layout.ts";

const GLYPH = 7.5;

export type Box = { x: number; y: number; w: number; h: number };

const overlaps = (a: Box, b: Box) =>
  a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y;

/** Whether the segment from `a` to `b` passes through the box. */
function crosses(box: Box, a: Point, b: Point) {
  // Liang–Barsky clipping: the segment meets the box when some t in [0, 1] lies inside it.
  let t0 = 0;
  let t1 = 1;
  const dx = b.x - a.x;
  const dy = b.y - a.y;

  for (const [p, q] of [
    [-dx, a.x - box.x],
    [dx, box.x + box.w - a.x],
    [-dy, a.y - box.y],
    [dy, box.y + box.h - a.y],
  ]) {
    if (p === 0) {
      if (q < 0) return false;
      continue;
    }

    const t = q / p;

    if (p < 0) t0 = Math.max(t0, t);
    else t1 = Math.min(t1, t);

    if (t0 > t1) return false;
  }

  return true;
}

/** The least gap between a label and any other node or label. */
const LABEL_CLEARANCE = 4;

const grow = (box: Box, by: number): Box => ({
  x: box.x - by,
  y: box.y - by,
  w: box.w + 2 * by,
  h: box.h + 2 * by,
});

/** Label text height plus its halo. */
const LABEL_HEIGHT = 34;

type LabelItem = { id: string; radius: number; label: string; outward?: Point };

export function placeLabels(
  positions: ReadonlyMap<string, Point>,
  items: readonly LabelItem[],
  segments: readonly [Point, Point][],
  width: number,
  height: number,
  reserved: readonly Box[] = [],
) {
  const boxes: Box[] = items.flatMap((item) => {
    const point = positions.get(item.id);

    return point
      ? [
          {
            x: point.x - item.radius,
            y: point.y - item.radius,
            w: item.radius * 2,
            h: item.radius * 2,
          },
        ]
      : [];
  });

  boxes.push(...reserved);
  const labels = new Map<string, LabelPlacement>();
  const center = { x: width / 2, y: height / 2 };

  const distance = (point: Point, box: Box) =>
    Math.hypot(
      point.x - Math.max(box.x, Math.min(point.x, box.x + box.w)),
      point.y - Math.max(box.y, Math.min(point.y, box.y + box.h)),
    );

  for (const item of [...items].sort((a, b) => b.radius - a.radius || a.id.localeCompare(b.id))) {
    const point = positions.get(item.id);

    if (!point) continue;
    const w = Math.max(...item.label.split("\n").map((line) => line.length)) * GLYPH;
    const h = LABEL_HEIGHT;
    const angle = Math.atan2(point.y - center.y, point.x - center.x);

    const candidates = [10, 16, 24, 36, 52, 72].flatMap((gap) =>
      [0, -0.4, 0.4, -0.8, 0.8, -1.2, 1.2, -1.57, 1.57, -2, 2, -2.4, 2.4, Math.PI].map((turn) => {
        const dx = Math.cos(angle + turn),
          dy = Math.sin(angle + turn);

        const reach = item.radius + gap;
        const x = point.x + dx * reach - (dx < -0.25 ? w : dx > 0.25 ? 0 : w / 2);
        const y = point.y + dy * reach - (dy < -0.25 ? h : dy > 0.25 ? 0 : h / 2);
        const box = { x, y, w, h };
        const own = distance(point, box);

        const clear =
          x >= 4 &&
          y >= 4 &&
          x + w <= width - 4 &&
          y + h <= height - 4 &&
          own >= item.radius + LABEL_CLEARANCE &&
          !boxes.some((other) => overlaps(other, grow(box, LABEL_CLEARANCE))) &&
          items.every((other) => {
            const at = positions.get(other.id);

            return other.id === item.id || !at || distance(at, box) >= own;
          });

        return {
          box,
          gap,
          turn,
          clear,
          crossings: segments.filter(([a, b]) => crosses(grow(box, 2), a, b)).length,
        };
      }),
    );

    const open = candidates.filter((candidate) => candidate.clear);
    const near = open.filter((candidate) => distance(point, candidate.box) <= item.radius + 12);
    const chosen = near.find((candidate) => candidate.crossings === 0) ?? near[0] ?? open[0];

    if (!chosen) continue;
    const { box, turn } = chosen;
    boxes.push(box);

    const leader =
      distance(point, box) > item.radius + 12 || turn !== 0
        ? {
            x: Math.max(box.x, Math.min(point.x, box.x + box.w)),
            y: Math.max(box.y, Math.min(point.y, box.y + box.h)),
          }
        : undefined;

    labels.set(item.id, { x: box.x, y: box.y + 12, anchor: "start", leader });
  }

  return { labels, boxes: boxes.filter((box) => !reserved.includes(box)) };
}

/** Where along an edge its label may sit, nearest the middle first. */
const EDGE_STOPS = [0.5, 0.4, 0.6, 0.3, 0.7];

/**
 * Each labeled edge's label at the first stop along it whose box clears
 * `occupied` (node circles and labels) and the labels placed before it,
 * preferring one no other segment crosses, heaviest edges first; an edge
 * with no clear stop gets no label.
 */
export function placeEdgeLabels(
  positions: ReadonlyMap<string, Point>,
  edges: readonly LayoutEdge[],
  occupied: readonly Box[],
  segments: readonly [Point, Point][] = [],
) {
  const boxes = [...occupied];
  const placed = new Map<string, Point>();
  const labeled = edges.filter((edge) => edge.label);

  for (const edge of labeled.sort((a, b) => b.links - a.links)) {
    const a = positions.get(edge.a);
    const b = positions.get(edge.b);

    if (!a || !b || !edge.label) continue;
    // The count's box covers the pill overview-map.tsx draws, centered on the edge.
    const w = edge.label.text.length * GLYPH + 10;

    const spots = EDGE_STOPS.flatMap((t) => {
      const x = a.x + (b.x - a.x) * t;
      const y = a.y + (b.y - a.y) * t;
      const box = { x: x - w / 2, y: y - 9, w, h: 18 };

      return boxes.some((other) => overlaps(other, box)) ? [] : [{ x, y, box }];
    });

    // Prefer a stop no other edge passes through, so a count reads as its own edge's.
    const others = segments.filter(([from, to]) => !(from === a && to === b));

    const spot =
      spots.find((candidate) => !others.some(([from, to]) => crosses(candidate.box, from, to))) ??
      undefined;

    if (!spot) continue;
    boxes.push(spot.box);
    placed.set(edge.label.key, { x: spot.x, y: spot.y });
  }

  return placed;
}
