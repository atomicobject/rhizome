import { describe, expect, it } from "vitest";
import { buildSigmaGraph } from "./sigmaGraphData";
import { computeEdgeStyle, computeNodeStyle, type ReducerState } from "../lib/graphStyle";

const options = {
  scope: { mode: "global" as const },
  connectionCounts: new Map<string, number>(),
  globalLayout: new Map(),
  communityColors: new Map(),
  problemCounts: new Map(),
  edgeFaint: "#ccc",
  fixed: true,
  explorer: true,
};

describe("Explorer renderer data", () => {
  it("preserves crosslinks alongside hover-only links between the same endpoints", () => {
    const { graph } = buildSigmaGraph({
      ...options,
      nodes: [
        { id: "a", label: "A", kind: "module", x: 0, y: 0 },
        { id: "b", label: "B", kind: "code", x: 100, y: 0 },
      ],
      edges: [
        { source: "a", target: "b", kind: "folder-containment", weight: 1 },
        {
          source: "a",
          target: "b",
          kind: "folder-crosslink",
          weight: 34,
          relationLabel: "34 note–code links",
        },
        { source: "a", target: "b", kind: "folder-minor", weight: 90 },
      ],
    });

    expect(graph.size).toBe(3);

    const state: ReducerState = {
      graphMatches: new Set(),
      semanticMatches: new Set(),
      highlightTypes: new Set(),
      graphFilter: false,
      searchActive: false,
      searchOnlyMatches: false,
      activeCommunity: null,
      scaleByAuthority: true,
      showEdgeLabels: true,
      hoverCommunity: null,
      hoverNodeID: null,
      cameraRatio: 1,
      maxScore: 0,
      colors: { neutral: "#888", edgeFaint: "#ccc", edgeLight: "#aaa", edgeStrong: "#777" },
    };

    const style = (id: string) =>
      computeEdgeStyle({
        source: "a",
        target: "b",
        attrs: graph.getEdgeAttributes(id),
        sourceAttrs: graph.getNodeAttributes("a"),
        targetAttrs: graph.getNodeAttributes("b"),
        state,
        lod: 1,
      });

    expect(style("edge-1")).toMatchObject({
      hidden: false,
      label: "",
      size: expect.any(Number),
    });
    expect(style("edge-0").size).toBe(1);
    expect(style("edge-1").size).toBeGreaterThan(1);
    expect(style("edge-1").size).toBeLessThan(2.5);
    expect(style("edge-2").hidden).toBe(true);
    state.hoverNodeID = "unrelated";
    state.hoverNeighbors = new Set();
    expect(
      computeNodeStyle({ nodeId: "a", attrs: graph.getNodeAttributes("a"), state, lod: 1 }).label,
    ).toBe("A");
    state.hoverNodeID = "a";
    expect(style("edge-1").label).toBe("34 note–code links");
    expect(style("edge-2").hidden).toBe(false);
  });
  it("marks the focused file and shortens local names without losing paths", () => {
    const { graph } = buildSigmaGraph({
      ...options,
      scope: { mode: "local", label: "pkg/search/planner.go" },
      nodes: [
        { id: "a", label: "pkg/search/planner.go", path: "pkg/search/planner.go", kind: "code" },
      ],
      edges: [],
    });

    expect(graph.getNodeAttributes("a")).toMatchObject({
      center: true,
      label: "planner.go",
      path: "pkg/search/planner.go",
      landmark: true,
    });
  });
});
