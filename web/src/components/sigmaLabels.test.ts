import { createHash } from "node:crypto";
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

it("places colliding folder names, counts and overflow clear of unrelated nodes", () => {
  const restore = installFakeWebGL();
  const container = document.createElement("div");
  document.body.append(container);

  const { graph } = buildSigmaGraph({
    nodes: [
      {
        id: "overflow:pkg",
        kind: "module",
        label: "+12 notes · 1 file in Vault",
        aggregate: true,
        x: 300,
        y: 220,
      },
      {
        id: "folder:embedindex",
        kind: "module",
        label: "embedindex",
        labelDetail: "3 notes · 20 files",
        x: 300,
        y: 240,
      },
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
      x: id === "blocker" ? 120 : id === "folder:pkg" ? 400 : 300,
      y: id === "overflow:pkg" ? 220 : id === "folder:embedindex" ? 240 : 200,
      size: id === "blocker" ? 60 : 10,
      color: "blue",
      label:
        id === "folder:pkg/app"
          ? "app"
          : id === "overflow:pkg"
            ? "+12 notes · 1 file in Vault"
            : id === "folder:embedindex"
              ? "embedindex"
              : null,
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

    for (const [key, label, y] of [
      ["overflow:pkg", "+12 notes · 1 file in Vault", 220],
      ["folder:embedindex", "embedindex", 240],
    ] as const) {
      painter.draw(
        context,
        { key, label, color: "blue", x: 300, y, size: 10 },
        sigma.getSettings(),
      );
    }

    expect(fillText.mock.calls.map(([text]) => text)).toEqual(
      expect.arrayContaining([
        "app",
        "111 notes · 1,184 files",
        "embedindex",
        "3 notes · 20 files",
        "+12 notes · 1 file in Vault",
      ]),
    );
    const boxes = painter.occupied();
    expect(boxes).toHaveLength(3);

    for (let i = 0; i < boxes.length; i++) {
      for (const other of boxes.slice(i + 1)) {
        const box = boxes[i];
        expect(
          box[0] < other[2] && box[2] > other[0] && box[1] < other[3] && box[3] > other[1],
        ).toBe(false);
      }
    }
  } finally {
    sigma.kill();
    container.remove();
    vi.restoreAllMocks();
    restore();
  }
});

it("bounds placement work for 300 folders and preserves the PR-head positions", () => {
  const restore = installFakeWebGL();
  const container = document.createElement("div");
  document.body.append(container);

  const { graph } = buildSigmaGraph({
    nodes: Array.from({ length: 300 }, (_, i) => ({
      id: `folder:${i}`,
      kind: "module",
      label: "f",
      labelDetail: "1",
      x: 64 + (i % 20) * 90,
      y: 40 + Math.floor(i / 20) * 68,
    })),
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
    vi.spyOn(sigma, "getDimensions").mockReturnValue({ width: 1920, height: 1080 });
    vi.spyOn(sigma, "framedGraphToViewport").mockImplementation((point) => point);
    vi.spyOn(sigma, "scaleSize").mockImplementation((size = 2) => size);
    vi.spyOn(sigma, "getNodeDisplayData").mockImplementation((id) => {
      const i = Number(String(id).slice(7));

      return {
        x: 64 + (i % 20) * 90,
        y: 40 + Math.floor(i / 20) * 68,
        size: 2,
        color: "blue",
        label: "f",
        forceLabel: true,
        hidden: false,
        highlighted: false,
        zIndex: 0,
        type: "circle",
      };
    });
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

    const painter = createLabelPainter(
      () => sigma,
      () => ({ label: "black", canvas: "white" }),
    );

    const maps = vi.spyOn(Array.prototype, "map");
    painter.draw(
      context,
      { key: "folder:0", label: "f", color: "blue", x: 64, y: 40, size: 2 },
      sigma.getSettings(),
    );

    const transformed = maps.mock.results.reduce<number>(
      (count, result) =>
        count + (Array.isArray(result.value) && result.value[0]?.box ? result.value.length : 0),
      0,
    );

    maps.mockRestore();
    expect(painter.occupied()).toHaveLength(300);
    expect(createHash("sha256").update(JSON.stringify(painter.occupied())).digest("hex")).toBe(
      "e6c7a72eb52e1087b561f4daa93fa1adfe68abf579acd5ad50c532ddd2c55a68",
    );
    expect(transformed).toBeLessThanOrEqual(900);
  } finally {
    sigma.kill();
    container.remove();
    vi.restoreAllMocks();
    restore();
  }
});
