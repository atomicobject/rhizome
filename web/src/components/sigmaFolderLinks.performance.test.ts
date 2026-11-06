import Sigma from "sigma";
import { expect, it, vi } from "vitest";
import { buildSigmaGraph } from "./sigmaGraphData";
import { drawFolderLinks } from "./sigmaFolderLinks";
import { installFakeWebGL } from "../test/fakeWebGL";

it("reuses obstacle positions while panning a 10,000-node layout", () => {
  const restore = installFakeWebGL();
  const container = document.createElement("div");
  document.body.append(container);

  const { graph } = buildSigmaGraph({
    nodes: Array.from({ length: 10000 }, (_, i) => ({
      id: String(i),
      kind: "module",
      label: String(i),
      x: i % 100,
      y: Math.floor(i / 100),
    })),
    edges: [{ source: "0", target: "1", kind: "folder-crosslink", weight: 1 }],
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
    const context = document.createElement("canvas").getContext("2d")!;
    const state = { hoverNodeID: null, selectedNodeID: null, hoveredNeighbors: new Set<string>() };
    const reads = vi.spyOn(sigma, "getNodeDisplayData");
    drawFolderLinks(sigma, context, state);
    reads.mockClear();
    sigma.getCamera().setState({ x: 0.51 });
    drawFolderLinks(sigma, context, state);
    expect(reads.mock.calls.length).toBeLessThan(10);
    reads.mockRestore();
  } finally {
    sigma.kill();
    container.remove();
    restore();
  }
});
