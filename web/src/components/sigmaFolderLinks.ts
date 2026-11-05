import type Sigma from "sigma";
import type { ExplorerNodeAttrs } from "./explorerGraph";
import type { ReducerState, SigmaEdgeAttrs } from "../lib/graphStyle";
import { resolveCssColor } from "../lib/themeColors";

export function folderLinkLength(child: boolean, active: boolean, distance: number) {
  return child && !active ? 0 : distance;
}

/** Paint bundled routes at rest and child routes on hover or selection. */
export function drawFolderLinks(
  sigma: Sigma<ExplorerNodeAttrs, SigmaEdgeAttrs>,
  context: CanvasRenderingContext2D,
  state: ReducerState,
) {
  const graph = sigma.getGraph();
  const points = new Map<string, { x: number; y: number; r: number }>();
  graph.forEachNode((id) => {
    const data = sigma.getNodeDisplayData(id);

    if (data && !data.hidden)
      points.set(id, { ...sigma.framedGraphToViewport(data), r: sigma.scaleSize(data.size) });
  });
  context.strokeStyle = resolveCssColor("var(--explorer-link)");
  graph.forEachEdge((_id, attrs, source, target) => {
    if (attrs.kind === "folder-containment") {
      const a = points.get(source),
        b = points.get(target);

      if (!a || !b) return;
      const distance = Math.hypot(b.x - a.x, b.y - a.y);

      if (distance <= a.r + b.r) return;
      context.strokeStyle = resolveCssColor("var(--type-8)");
      context.lineWidth = 1.5;
      context.beginPath();
      context.moveTo(a.x + ((b.x - a.x) * a.r) / distance, a.y + ((b.y - a.y) * a.r) / distance);
      context.lineTo(b.x - ((b.x - a.x) * b.r) / distance, b.y - ((b.y - a.y) * b.r) / distance);
      context.stroke();

      return;
    }

    if (attrs.kind !== "folder-crosslink" && attrs.kind !== "folder-child-crosslink") return;
    context.strokeStyle = resolveCssColor("var(--explorer-link)");

    let from = source,
      to = target;

    if (attrs.kind === "folder-child-crosslink" && !graph.getNodeAttribute(from, "branch"))
      [from, to] = [to, from];

    const a = points.get(from),
      b = points.get(to);

    if (!a || !b) return;
    const distance = Math.hypot(b.x - a.x, b.y - a.y);

    if (!distance) return;

    const active =
      state.hoverNodeID === source ||
      state.hoverNodeID === target ||
      Boolean(
        graph.getNodeAttribute(source, "selected") || graph.getNodeAttribute(target, "selected"),
      );

    const length = folderLinkLength(attrs.kind === "folder-child-crosslink", active, distance);

    if (!length) return;

    const end = {
      x: a.x + ((b.x - a.x) * length) / distance,
      y: a.y + ((b.y - a.y) * length) / distance,
    };

    context.lineWidth =
      attrs.kind === "folder-child-crosslink" ? 1.25 : attrs.weight >= 40 ? 2.5 : 1.75;
    context.beginPath();
    context.moveTo(a.x, a.y);

    const crossesNode = [...points].some(([id, p]) => {
      if (id === from || id === to) return false;

      const t = Math.max(
        0,
        Math.min(
          1,
          ((p.x - a.x) * (end.x - a.x) + (p.y - a.y) * (end.y - a.y)) / (length * length),
        ),
      );

      return Math.hypot(p.x - a.x - (end.x - a.x) * t, p.y - a.y - (end.y - a.y) * t) < p.r + 8;
    });

    if (crossesNode) {
      const cx = (a.x + end.x) / 2 - ((end.y - a.y) / length) * 80;
      const cy = (a.y + end.y) / 2 + ((end.x - a.x) / length) * 80;

      for (let step = 1; step <= 24; step++) {
        const t = step / 24,
          u = 1 - t;

        context.lineTo(
          u * u * a.x + 2 * u * t * cx + t * t * end.x,
          u * u * a.y + 2 * u * t * cy + t * t * end.y,
        );
      }
    } else context.lineTo(end.x, end.y);
    context.stroke();
  });
}
