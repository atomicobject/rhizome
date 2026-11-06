import type Sigma from "sigma";
import type { ExplorerNodeAttrs } from "./explorerGraph";
import type { ReducerState, SigmaEdgeAttrs } from "../lib/graphStyle";
import { resolveCssColor } from "../lib/themeColors";

import { FolderObstacles } from "./folderObstacles";

const obstacleCaches = new WeakMap<Sigma<ExplorerNodeAttrs, SigmaEdgeAttrs>, FolderObstacles>();

const observed = new WeakSet<Sigma<ExplorerNodeAttrs, SigmaEdgeAttrs>>();

export function folderLinkLength(child: boolean, active: boolean, distance: number) {
  return child && !active ? 0 : distance;
}

/** Paint bundled routes at rest and child routes on hover or selection. */
export function drawFolderLinks(
  sigma: Sigma<ExplorerNodeAttrs, SigmaEdgeAttrs>,
  context: CanvasRenderingContext2D,
  state: Pick<ReducerState, "hoverNodeID">,
) {
  const graph = sigma.getGraph();

  if (!observed.has(sigma)) {
    observed.add(sigma);
    sigma.on("afterProcess", () => obstacleCaches.delete(sigma));
  }

  let obstacles = obstacleCaches.get(sigma);

  if (!obstacles) {
    const positions: { id: string; x: number; y: number; size: number }[] = [];
    graph.forEachNode((id) => {
      const data = sigma.getNodeDisplayData(id);

      if (data && !data.hidden) positions.push({ id, x: data.x, y: data.y, size: data.size });
    });
    obstacles = new FolderObstacles(positions);
    obstacleCaches.set(sigma, obstacles);
  }

  const point = (id: string) => {
    const data = sigma.getNodeDisplayData(id);

    return data && !data.hidden
      ? { ...sigma.framedGraphToViewport(data), r: sigma.scaleSize(data.size) }
      : undefined;
  };

  const dimensions = sigma.getDimensions();
  const origin = sigma.viewportToFramedGraph({ x: 0, y: 0 });
  const unit = sigma.viewportToFramedGraph({ x: 1, y: 0 });
  const unitsPerPixel = Math.hypot(unit.x - origin.x, unit.y - origin.y);
  context.strokeStyle = resolveCssColor("var(--explorer-link)");
  graph.forEachEdge((_id, attrs, source, target) => {
    if (attrs.kind === "folder-containment") {
      const a = point(source),
        b = point(target);

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

    const a = point(from),
      b = point(to);

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

    // The quadratic control point bends at most 80px beyond the endpoint box.
    if (
      Math.max(a.x, b.x) < -82 ||
      Math.min(a.x, b.x) > dimensions.width + 82 ||
      Math.max(a.y, b.y) < -82 ||
      Math.min(a.y, b.y) > dimensions.height + 82
    )
      return;

    const end = {
      x: a.x + ((b.x - a.x) * length) / distance,
      y: a.y + ((b.y - a.y) * length) / distance,
    };

    context.lineWidth =
      attrs.kind === "folder-child-crosslink" ? 1.25 : attrs.weight >= 40 ? 2.5 : 1.75;
    context.beginPath();
    context.moveTo(a.x, a.y);

    const startGraph = sigma.viewportToFramedGraph(a);
    const endGraph = sigma.viewportToFramedGraph(end);
    const padding = (sigma.scaleSize(obstacles.maxSize) + 8) * unitsPerPixel;
    let crossesNode = false;

    for (const obstacle of obstacles.nearRoute(startGraph, endGraph, padding)) {
      if (obstacle.id === from || obstacle.id === to) continue;
      const p = { ...sigma.framedGraphToViewport(obstacle), r: sigma.scaleSize(obstacle.size) };

      const t = Math.max(
        0,
        Math.min(
          1,
          ((p.x - a.x) * (end.x - a.x) + (p.y - a.y) * (end.y - a.y)) / (length * length),
        ),
      );

      if (Math.hypot(p.x - a.x - (end.x - a.x) * t, p.y - a.y - (end.y - a.y) * t) < p.r + 8) {
        crossesNode = true;
        break;
      }
    }

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
