import type Sigma from "sigma";
import type { SigmaNodeAttrs, SigmaEdgeAttrs } from "../lib/graphStyle";

/** Fit visible positions in the fixed coordinate frame, reserving room for labels. */
export function fitFixedGraph(sigma: Sigma<SigmaNodeAttrs, SigmaEdgeAttrs>, instant: boolean) {
  const points: { x: number; y: number }[] = [];
  sigma.getGraph().forEachNode((id) => {
    const node = sigma.getNodeDisplayData(id);

    if (node && !node.hidden) points.push(node);
  });

  if (!points.length) return;
  const xs = points.map((p) => p.x);
  const ys = points.map((p) => p.y);

  const left = Math.min(...xs),
    right = Math.max(...xs);

  const bottom = Math.min(...ys),
    top = Math.max(...ys);

  const { width, height } = sigma.getDimensions();
  // Sigma scales a square graph frame into the smaller viewport dimension.
  const span = Math.max(1, Math.min(width, height) - 88);

  const state = {
    x: (left + right) / 2,
    y: (bottom + top) / 2,
    angle: 0,
    ratio: Math.max(
      0.05,
      Math.max(
        ((right - left) * span) / Math.max(1, width - 360),
        ((top - bottom) * span) / Math.max(1, height - 120),
      ),
    ),
  };

  const camera = sigma.getCamera();

  if (instant || window.matchMedia?.("(prefers-reduced-motion: reduce)").matches)
    camera.setState(state);
  else camera.animate(state, { duration: 300 });
}

export function zoomGraph(sigma: Sigma<SigmaNodeAttrs, SigmaEdgeAttrs> | null, factor: number) {
  if (!sigma) return;
  const camera = sigma.getCamera();
  camera.animate(
    { ratio: camera.getBoundedRatio(camera.getState().ratio * factor) },
    { duration: 200 },
  );
}
