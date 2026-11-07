import type { OntologyEdge, OntologyGraphModel, OntologyNode } from "./ontologyGraph";

type Rect = { x: number; y: number; w: number; h: number };

/** ELK's padding around the drawing, in card units. */
const LAYOUT_PADDING = 20;

/** The least gap between disconnected groups of types after packing, in card units. */
const COMPONENT_GAP = 40;

/** The packing grid, in card units. */
const PACK_STEP = 10;

/** The canvas's usual width over height; packing keeps the drawing near it, so it fits large. */
const VIEW_ASPECT = 1.6;

function boundsOf(rects: readonly Rect[]): Rect {
  const x = Math.min(...rects.map((r) => r.x));
  const y = Math.min(...rects.map((r) => r.y));

  return {
    x,
    y,
    w: Math.max(...rects.map((r) => r.x + r.w)) - x,
    h: Math.max(...rects.map((r) => r.y + r.h)) - y,
  };
}

/**
 * ELK stacks disconnected groups of types in a row, which can leave a void
 * beside the largest one. This moves each smaller group with more than one
 * type, largest first, to the free spot at least COMPONENT_GAP clear of every
 * card and line already placed that keeps the drawing smallest at the
 * canvas's aspect. Lone types stay in ELK's packed grid.
 */
export function packComponents(graph: OntologyGraphModel): OntologyGraphModel {
  const root = new Map(graph.nodes.map((node) => [node.id, node.id]));

  const find = (id: string): string => {
    const parent = root.get(id) ?? id;

    return parent === id ? id : find(parent);
  };

  for (const edge of graph.edges)
    if (root.has(edge.source) && root.has(edge.target))
      root.set(find(edge.source), find(edge.target));

  const groups = new Map<string, OntologyNode[]>();

  for (const node of graph.nodes)
    groups.set(find(node.id), [...(groups.get(find(node.id)) ?? []), node]);

  const ordered = [...groups.values()].sort((a, b) => b.length - a.length);
  const moving = ordered.slice(1).filter((group) => group.length > 1);

  if (moving.length === 0) return graph;
  const movingIds = new Set(moving.flat().map((node) => node.id));
  const rect = (node: OntologyNode): Rect => ({ ...node.position, w: node.width, h: node.height });

  const segments = (edge: OntologyEdge): Rect[] =>
    (edge.points ?? []).slice(1).map((point, index) => {
      const from = (edge.points ?? [])[index];

      return {
        x: Math.min(from.x, point.x),
        y: Math.min(from.y, point.y),
        w: Math.abs(point.x - from.x),
        h: Math.abs(point.y - from.y),
      };
    });

  const obstacles: Rect[] = [
    ...graph.nodes.filter((node) => !movingIds.has(node.id)).map(rect),
    ...graph.edges.filter((edge) => !movingIds.has(edge.source)).flatMap(segments),
  ];

  const shifts = new Map<string, { dx: number; dy: number }>();

  for (const group of moving) {
    const ids = new Set(group.map((node) => node.id));

    const own = [
      ...group.map(rect),
      ...graph.edges.filter((e) => ids.has(e.source)).flatMap(segments),
    ];

    const box = boundsOf(own);
    const placed = boundsOf(obstacles);

    let best: { dx: number; dy: number; cost: number } | null = null;

    // A grid over the placed drawing and its margin, plus the spots flush with its edges.
    const span = (from: number, to: number, ...flush: number[]) => [
      ...Array.from(
        { length: Math.floor((to - from) / PACK_STEP) + 1 },
        (_, i) => from + i * PACK_STEP,
      ),
      ...flush,
    ];

    const xs = span(
      placed.x - box.w - COMPONENT_GAP,
      placed.x + placed.w + COMPONENT_GAP,
      placed.x,
      placed.x + placed.w - box.w,
    );

    const ys = span(
      placed.y - box.h - COMPONENT_GAP,
      placed.y + placed.h + COMPONENT_GAP,
      placed.y,
      placed.y + placed.h - box.h,
    );

    for (const y of ys) {
      for (const x of xs) {
        const dx = x - box.x;
        const dy = y - box.y;
        let blocked = false;
        let minX = placed.x;
        let minY = placed.y;
        let maxX = placed.x + placed.w;
        let maxY = placed.y + placed.h;

        for (const r of own) {
          const rx = r.x + dx;
          const ry = r.y + dy;

          for (const o of obstacles) {
            if (
              rx - COMPONENT_GAP < o.x + o.w &&
              rx + r.w + COMPONENT_GAP > o.x &&
              ry - COMPONENT_GAP < o.y + o.h &&
              ry + r.h + COMPONENT_GAP > o.y
            ) {
              blocked = true;
              break;
            }
          }

          if (blocked) break;
          minX = Math.min(minX, rx);
          minY = Math.min(minY, ry);
          maxX = Math.max(maxX, rx + r.w);
          maxY = Math.max(maxY, ry + r.h);
        }

        if (blocked) continue;
        const all = { w: maxX - minX, h: maxY - minY };
        // The side that limits the zoom, then the area, so ties pick the tightest spot.
        const cost = Math.max(all.w, all.h * VIEW_ASPECT) + (all.w * all.h) / 1e6;

        if (!best || cost < best.cost) best = { dx, dy, cost };
      }
    }

    if (!best) continue;

    for (const r of own) obstacles.push({ ...r, x: r.x + best.dx, y: r.y + best.dy });

    for (const id of ids) shifts.set(id, best);
  }

  // Moving a group can leave the old origin empty; start the drawing at ELK's padding again.
  const moved = graph.nodes.map((node) => {
    const { dx, dy } = shifts.get(node.id) ?? { dx: 0, dy: 0 };

    return { x: node.position.x + dx, y: node.position.y + dy };
  });

  const originX = Math.min(...moved.map((point) => point.x)) - LAYOUT_PADDING;
  const originY = Math.min(...moved.map((point) => point.y)) - LAYOUT_PADDING;

  const shift = (id: string) => {
    const { dx, dy } = shifts.get(id) ?? { dx: 0, dy: 0 };

    return { dx: dx - originX, dy: dy - originY };
  };

  return {
    nodes: graph.nodes.map((node) => ({
      ...node,
      position: { x: node.position.x + shift(node.id).dx, y: node.position.y + shift(node.id).dy },
    })),
    edges: graph.edges.map((edge) => ({
      ...edge,
      points: edge.points?.map((point) => ({
        x: point.x + shift(edge.source).dx,
        y: point.y + shift(edge.source).dy,
      })),
    })),
  };
}
