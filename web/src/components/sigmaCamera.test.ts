import Sigma from "sigma";
import { expect, it } from "vitest";
import { buildSigmaGraph } from "./sigmaGraphData";
import { fitFixedGraph } from "./sigmaCamera";
import { outwardSpots } from "./sigmaLabels";
import { installFakeWebGL } from "../test/fakeWebGL";

it("fits every visible node and its label, including context outside the opened folder", () => {
  const restore = installFakeWebGL();
  const container = document.createElement("div");
  document.body.append(container);

  const { graph } = buildSigmaGraph({
    nodes: [
      { id: "folder:", kind: "module", label: "Vault", x: 0, y: 0 },
      { id: "folder:pkg", kind: "module", path: "pkg", label: "pkg", center: true, x: -180, y: 80 },
      {
        id: "folder:docs",
        kind: "module",
        path: "docs",
        label: "documentation with a long name",
        labelDetail: "655 notes",
        x: 260,
        y: -200,
      },
    ],
    edges: [],
    scope: { mode: "global" },
    fixed: true,
    explorer: true,
    connectionCounts: new Map(),
    globalLayout: new Map(),
    communityColors: new Map(),
    problemCounts: new Map(),
    edgeFaint: "#ccc",
  });

  const sigma = new Sigma(graph, container);

  try {
    sigma.refresh();
    fitFixedGraph(sigma, true);
    const { width, height } = sigma.getDimensions();
    const context = document.createElement("canvas").getContext("2d")!;
    context.font = `${sigma.getSetting("labelWeight")} ${sigma.getSetting("labelSize")}px ${sigma.getSetting("labelFont")}`;
    graph.forEachNode((id, attrs) => {
      const node = sigma.getNodeDisplayData(id)!;
      const point = sigma.framedGraphToViewport(node);
      const origin = sigma.framedGraphToViewport(sigma.getNodeDisplayData("folder:")!);

      const labelWidth = Math.max(
        context.measureText(attrs.label).width,
        context.measureText(attrs.labelDetail || "").width,
      );

      const { box } = outwardSpots(
        point.x,
        point.y,
        sigma.scaleSize(node.size),
        labelWidth,
        sigma.getSetting("labelSize"),
        id === "folder:" ? 0 : point.x - origin.x,
        id === "folder:" ? 1 : point.y - origin.y,
      )[0];

      if (attrs.labelDetail && box[3] < point.y) box[1] -= sigma.getSetting("labelSize") + 3;
      else if (attrs.labelDetail) box[3] += sigma.getSetting("labelSize") + 3;
      expect(box[0]).toBeGreaterThanOrEqual(23.9);
      expect(box[1]).toBeGreaterThanOrEqual(23.9);
      expect(box[2]).toBeLessThanOrEqual(width - 23.9);
      expect(box[3]).toBeLessThanOrEqual(height - 23.9);
    });
  } finally {
    sigma.kill();
    container.remove();
    restore();
  }
});
