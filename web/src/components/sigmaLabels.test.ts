import Sigma from "sigma";
import { buildSigmaGraph } from "./sigmaGraphData";
import { installFakeWebGL } from "../test/fakeWebGL";
import { expect, it, vi } from "vitest";
import { createLabelPainter, outwardSpots } from "./sigmaLabels";

it("keeps labels on the outward side with labels within 8px", () => {
  for (const [dx, dy] of [
    [1, 0],
    [-1, 0],
    [0, -1],
    [0, 1],
  ]) {
    for (const { box } of outwardSpots(100, 100, 10, 80, 14, dx, dy)) {
      const x = Math.max(box[0], Math.min(100, box[2]));
      const y = Math.max(box[1], Math.min(100, box[3]));
      expect(Math.hypot(x - 100, y - 100) - 10).toBeLessThanOrEqual(8);

      if (dx > 0) expect(box[0]).toBeGreaterThan(110);

      if (dx < 0) expect(box[2]).toBeLessThan(90);

      if (dy < 0) expect(box[3]).toBeLessThan(90);

      if (dy > 0) expect(box[1]).toBeGreaterThan(110);
    }
  }
});

it("places fixed Explorer detail clear of unrelated nodes", () => {
  const restore = installFakeWebGL();
  const container = document.createElement("div");
  document.body.append(container);

  const { graph } = buildSigmaGraph({
    nodes: [
      { id: "folder:pkg", kind: "module", label: "pkg", x: 400, y: 200 },
      { id: "blocker", kind: "module", label: "blocker", x: 120, y: 200 },
      {
        id: "folder:pkg/app",
        kind: "module",
        path: "pkg/app",
        label: "app",
        labelDetail: "111 notes · 1,184 files",
        x: 300,
        y: 200,
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
  const context = document.createElement("canvas").getContext("2d")!;

  try {
    vi.spyOn(sigma, "getDimensions").mockReturnValue({ width: 400, height: 400 });
    vi.spyOn(sigma, "framedGraphToViewport").mockImplementation((point) => point);
    vi.spyOn(sigma, "scaleSize").mockImplementation((size = 10) => size);
    vi.spyOn(sigma, "getNodeDisplayData").mockImplementation((id) => ({
      x: id === "folder:pkg/app" ? 300 : id === "blocker" ? 120 : 400,
      y: 200,
      size: id === "blocker" ? 60 : 10,
      color: "blue",
      label: id === "folder:pkg/app" ? "app" : null,
      forceLabel: true,
      hidden: false,
      highlighted: false,
      zIndex: 0,
      type: "circle",
    }));
    vi.spyOn(context, "measureText").mockImplementation((text) => ({
      width: text.length * 8,
      actualBoundingBoxAscent: 0,
      actualBoundingBoxDescent: 0,
      actualBoundingBoxLeft: 0,
      actualBoundingBoxRight: 0,
      fontBoundingBoxAscent: 0,
      fontBoundingBoxDescent: 0,
      alphabeticBaseline: 0,
      hangingBaseline: 0,
      ideographicBaseline: 0,
      emHeightAscent: 0,
      emHeightDescent: 0,
    }));
    const fillText = vi.spyOn(context, "fillText");

    const painter = createLabelPainter(
      () => sigma,
      () => ({ label: "black", canvas: "white" }),
    );

    painter.draw(
      context,
      {
        key: "folder:pkg/app",
        label: "app",
        color: "blue",
        x: 300,
        y: 200,
        size: 10,
      },
      sigma.getSettings(),
    );
    expect(fillText.mock.calls.map(([text]) => text)).toContain("app");
  } finally {
    sigma.kill();
    container.remove();
    vi.restoreAllMocks();
    restore();
  }
});
