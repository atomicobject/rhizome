import { noteLabelLines, selectedNoteSpot } from "./sigmaLabelText";
import type { ExplorerNodeAttrs as SigmaNodeAttrs } from "./explorerGraph";
import type Sigma from "sigma";
import { outwardSpots } from "./sigmaLabels";
import { CAMERA } from "./graphViewShared";
import type { SigmaEdgeAttrs } from "../lib/graphStyle";

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
    ratio: Math.min(
      CAMERA.maxRatio,
      Math.max(
        CAMERA.minRatio,
        Math.max(
          ((right - left) * span) / Math.max(1, width - 80),
          ((top - bottom) * span) / Math.max(1, height - 80),
        ),
      ),
    ),
  };

  const camera = sigma.getCamera();

  const original = camera.getState();
  const context = document.createElement("canvas").getContext("2d");

  if (context) {
    const size = sigma.getSetting("labelSize");

    for (let pass = 0; pass < 12; pass++) {
      const boxes: number[][] = [];
      sigma.getGraph().forEachNode((id, attrs) => {
        const node = sigma.getNodeDisplayData(id);

        if (!node || node.hidden) return;
        const point = sigma.framedGraphToViewport(node, { cameraState: state });

        const parent = sigma.getNodeDisplayData(
          `folder:${attrs.aggregate ? attrs.path || "" : (attrs.path || "").split("/").slice(0, -1).join("/")}`,
        );

        const origin = parent ? sigma.framedGraphToViewport(parent, { cameraState: state }) : point;

        context.font = `${attrs.selected || id.startsWith("file-group:") ? "600" : sigma.getSetting("labelWeight")} ${size}px ${sigma.getSetting("labelFont")}`;

        const lines =
          attrs.kind === "note" || attrs.kind === "embedded"
            ? noteLabelLines(
                attrs.label,
                (text) => context.measureText(text).width,
                attrs.selected ? 140 : 220,
              )
            : [attrs.label];

        const labelWidth = Math.max(...lines.map((text) => context.measureText(text).width));

        const detail = attrs.muted && !attrs.aggregate ? "" : attrs.labelDetail;
        const detailWidth = detail ? context.measureText("  " + detail).width : 0;

        const stacked = attrs.fixed && attrs.kind === "module";
        const textWidth = stacked ? Math.max(labelWidth, detailWidth) : labelWidth + detailWidth;

        const spot =
          attrs.selected && attrs.kind === "note"
            ? selectedNoteSpot(point.x, point.y, sigma.scaleSize(node.size), textWidth, size)
            : outwardSpots(
                point.x,
                point.y,
                sigma.scaleSize(node.size),
                textWidth,
                size,
                (attrs.selected && attrs.path) || attrs.kind !== "module"
                  ? attrs.kind === "note" || attrs.kind === "embedded"
                    ? -1
                    : 1
                  : id === "folder:"
                    ? 0
                    : point.x - origin.x,
                (attrs.selected && attrs.path) || attrs.kind !== "module"
                  ? 0
                  : id === "folder:"
                    ? 1
                    : point.y - origin.y,
              )[0];

        if (stacked && detail && spot.box[3] < point.y) spot.box[1] -= size + 3;
        else spot.box[3] += (lines.length - 1 + (stacked && detail ? 1 : 0)) * (size + 3);
        boxes.push(spot.box, [point.x - 16, point.y - 16, point.x + 16, point.y + 16]);
      });

      if (!boxes.length) break;
      const l = Math.min(...boxes.map((b) => b[0])) - 24;
      const r = Math.max(...boxes.map((b) => b[2])) + 24;
      const t = Math.min(...boxes.map((b) => b[1])) - 24;
      const b = Math.max(...boxes.map((b) => b[3])) + 24;

      const center = sigma.viewportToFramedGraph(
        { x: (l + r) / 2, y: (t + b) / 2 },
        { cameraState: state },
      );

      Object.assign(state, center, {
        ratio: Math.max(
          CAMERA.minRatio,
          Math.min(CAMERA.maxRatio, state.ratio * Math.max((r - l) / width, (b - t) / height)),
        ),
      });
    }
  }

  if (instant || window.matchMedia?.("(prefers-reduced-motion: reduce)").matches)
    camera.setState(state);
  else {
    camera.setState(original);
    camera.animate(state, { duration: 200 });
  }
}

export function zoomGraph(sigma: Sigma<SigmaNodeAttrs, SigmaEdgeAttrs> | null, factor: number) {
  if (!sigma) return;
  const camera = sigma.getCamera();
  camera.animate(
    { ratio: camera.getBoundedRatio(camera.getState().ratio * factor) },
    { duration: 200 },
  );
}
