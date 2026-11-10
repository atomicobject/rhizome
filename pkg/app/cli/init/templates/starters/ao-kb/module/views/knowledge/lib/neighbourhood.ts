/** The scoped neighbourhood (SPEC-0135 R4, US2): the KB overview's map cropped to a record's
 * scope, never a rerun layout, and exactly what that crop draws, so its caption can say so. */
import type { Layout, MapNode } from "./site.ts";

/** The view box: the overview's positions cropped to the in-scope nodes and their labels.
 * Null when nothing is in scope. */
export function cropBox(l: Layout, inScope: Set<string>, aspect = 2.6) {
  const xs: number[] = [], ys: number[] = [];
  for (const nd of l.nodes) {
    if (!inScope.has(nd.path)) continue;
    xs.push(nd.x - nd.r - 10, nd.x + nd.r + 10, nd.label.x, nd.label.x + nd.label.w);
    ys.push(nd.y - nd.r - 10, nd.y + nd.r + 10, nd.label.y, nd.label.y + nd.label.h);
  }
  if (!xs.length) return null;
  const x0 = Math.min(...xs) - 30, x1 = Math.max(...xs) + 30, y0 = Math.min(...ys) - 16, y1 = Math.max(...ys) + 16;
  let w = Math.min(l.width, Math.max(x1 - x0, 320)), h = Math.max(y1 - y0, w / aspect);
  if (h > (w / aspect) * 1.6) w = Math.min(l.width, (h * aspect) / 1.6);
  h = Math.max(h, w / aspect);
  return { x: (x0 + x1) / 2 - w / 2, y: (y0 + y1) / 2 - h / 2, w, h };
}

export type Neighbourhood = NonNullable<ReturnType<typeof neighbourhood>>;

/** What the crop draws: the in-scope nodes, the out-of-scope nodes whose centres fall inside
 * the box (drawn faint), and only the Related Concepts lines whose two ends are drawn.
 * `outside` counts the KB's concepts the crop leaves out. Null when nothing is in scope. */
export function neighbourhood(l: Layout, inScope: Set<string>) {
  const box = cropBox(l, inScope);
  if (!box) return null;
  const inBox = (n: MapNode) => n.x >= box.x && n.x <= box.x + box.w && n.y >= box.y && n.y <= box.y + box.h;
  const drawn = new Set(l.nodes.filter((n) => inScope.has(n.path) || inBox(n)).map((n) => n.path));
  return {
    box,
    drawn,
    faint: l.nodes.filter((n) => drawn.has(n.path) && !inScope.has(n.path)).map((n) => n.path),
    edges: l.edges.filter(([a, b]) => drawn.has(l.nodes[a].path) && drawn.has(l.nodes[b].path)),
    outside: l.nodes.length - drawn.size,
  };
}
