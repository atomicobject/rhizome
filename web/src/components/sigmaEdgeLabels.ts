import type Sigma from "sigma";
import type { ExplorerNodeAttrs } from "./explorerGraph";
import type { SigmaEdgeAttrs } from "../lib/graphStyle";

/** Counts occupy free space near the endpoint, never a node or its planned label. */
export function drawFolderEdgeCounts(
  sigma: Sigma<ExplorerNodeAttrs, SigmaEdgeAttrs>,
  context: CanvasRenderingContext2D,
  occupied: readonly number[][],
  colors: { canvas: string; edgeLabel: string },
) {
  const boxes = [...occupied];
  const graph = sigma.getGraph();
  const { width, height } = sigma.getDimensions();
  graph.forEachNode((id) => {
    const node = sigma.getNodeDisplayData(id);

    if (!node || node.hidden) return;

    const p = sigma.framedGraphToViewport(node),
      r = sigma.scaleSize(node.size) + 6;

    boxes.push([p.x - r, p.y - r, p.x + r, p.y + r]);
  });
  context.font = "500 13px Inter, system-ui, sans-serif";
  graph.forEachEdge((id, attrs, source, target) => {
    const edge = sigma.getEdgeDisplayData(id);

    if (attrs.kind !== "folder-crosslink" || !edge?.label || edge.hidden) return;

    const a = sigma.getNodeDisplayData(source),
      b = sigma.getNodeDisplayData(target);

    if (!a || !b) return;

    const from = sigma.framedGraphToViewport(a),
      to = sigma.framedGraphToViewport(b);

    const text = String(attrs.weight),
      w = context.measureText(text).width + 8;

    for (const t of [0.8, 0.72, 0.64]) {
      const x = from.x + (to.x - from.x) * t,
        y = from.y + (to.y - from.y) * t;

      const box = [x - w / 2, y - 10, x + w / 2, y + 9];

      if (
        box[0] < 24 ||
        box[1] < 24 ||
        box[2] > width - 24 ||
        box[3] > height - 24 ||
        boxes.some(
          (b) => b[0] < box[2] + 4 && b[2] + 4 > box[0] && b[1] < box[3] + 4 && b[3] + 4 > box[1],
        )
      )
        continue;
      context.fillStyle = colors.canvas;
      context.fillRect(box[0], box[1], w, 19);
      context.fillStyle = colors.edgeLabel;
      context.fillText(text, box[0] + 4, y + 4);
      boxes.push(box);
      break;
    }
  });
}
