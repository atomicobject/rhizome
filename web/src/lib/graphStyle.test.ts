import { describe, expect, it } from "vitest";
import {
  computeEdgeStyle,
  computeNodeStyle,
  lodFromRatio,
  type ReducerState,
  type SigmaEdgeAttrs,
  type SigmaNodeAttrs,
} from "./graphStyle";

function reducerState(overrides: Partial<ReducerState> = {}): ReducerState {
  return {
    graphMatches: new Set<string>(),
    semanticMatches: new Set<string>(),
    highlightTypes: new Set<string>(),
    graphFilter: false,
    searchActive: false,
    searchOnlyMatches: false,
    activeCommunity: null,
    scaleByAuthority: true,
    showEdgeLabels: true,
    hoverCommunity: null,
    hoverNodeID: null,
    cameraRatio: 1,
    maxScore: 10,
    ...overrides,
  };
}

function nodeAttrs(overrides: Partial<SigmaNodeAttrs> = {}): SigmaNodeAttrs {
  return {
    label: "Task Flow",
    kind: "note",
    baseSize: 6,
    baseColor: "#3b82f6",
    size: 6,
    color: "#3b82f6",
    type: "circle",
    x: 0,
    y: 0,
    score: 8,
    image: "data:image/svg+xml,icon",
    ...overrides,
  };
}

function edgeAttrs(overrides: Partial<SigmaEdgeAttrs> = {}): SigmaEdgeAttrs {
  return {
    size: 2,
    weight: 1,
    color: "#e8e5e0",
    baseColor: "#e8e5e0",
    kind: "wikilink",
    label: "Link",
    ...overrides,
  };
}

describe("graph style reducers", () => {
  it("hides non-matching nodes when search-only filter is active", () => {
    const result = computeNodeStyle({
      nodeId: "note:task-flow",
      attrs: nodeAttrs(),
      state: reducerState({
        searchActive: true,
        searchOnlyMatches: true,
        semanticMatches: new Set(["other-node"]),
      }),
      lod: lodFromRatio(1),
    });

    expect(result.hidden).toBe(true);
  });

  it("keeps focused nodes visible with labels", () => {
    const result = computeNodeStyle({
      nodeId: "note:task-flow",
      attrs: nodeAttrs(),
      state: reducerState({
        semanticMatches: new Set(["note:task-flow"]),
        searchActive: true,
      }),
      lod: lodFromRatio(1),
    });

    expect(result.hidden).toBe(false);
    expect(result.label).toBe("Task Flow");
  });

  it("dims nodes outside the active type highlight without hiding them", () => {
    const highlightTypes = new Set(["Spec"]);

    const matched = computeNodeStyle({
      nodeId: "spec",
      attrs: nodeAttrs({ resolvedType: "Spec", baseSize: 20 }),
      state: reducerState({ highlightTypes }),
      lod: lodFromRatio(1),
    });

    const unmatched = computeNodeStyle({
      nodeId: "untyped",
      attrs: nodeAttrs({ resolvedType: undefined, baseSize: 20 }),
      state: reducerState({ highlightTypes }),
      lod: lodFromRatio(1),
    });

    expect(matched.hidden).toBe(false);
    expect(unmatched.hidden).toBe(false);
    expect(unmatched.color).toBe("rgba(36, 39, 44, 0.24)");
    expect(unmatched.label).toBe("");
    expect(unmatched.type).toBe("circle");
    expect(unmatched.image).toBeUndefined();
    expect(matched.type).toBe("image");
    expect(matched.image).toBe(nodeAttrs({ baseSize: 20 }).image);
    expect(unmatched.color).not.toBe(matched.color);
  });

  it("restores the image program for a hovered type-dimmed node", () => {
    const result = computeNodeStyle({
      nodeId: "untyped",
      attrs: nodeAttrs({ resolvedType: undefined, baseSize: 20 }),
      state: reducerState({
        highlightTypes: new Set(["Spec"]),
        hoverNodeID: "untyped",
      }),
      lod: lodFromRatio(1),
    });

    expect(result.type).toBe("image");
    expect(result.image).toBe(nodeAttrs({ baseSize: 20 }).image);
    expect(result.color).toBe("rgba(59, 130, 246, 1)");
  });

  it("keeps hover-only nodes on the same glyph program to avoid pick flicker", () => {
    const attrs = nodeAttrs({
      score: 0,
      image: "data:image/svg+xml,icon",
    });

    const normal = computeNodeStyle({
      nodeId: "note:task-flow",
      attrs,
      state: reducerState(),
      lod: lodFromRatio(4),
    });

    const hovered = computeNodeStyle({
      nodeId: "note:task-flow",
      attrs,
      state: reducerState({
        hoverNodeID: "note:task-flow",
      }),
      lod: lodFromRatio(4),
    });

    expect(normal.type).toBe("circle");
    expect(hovered.type).toBe(normal.type);
    expect(hovered.size).toBe(normal.size);
    expect(hovered.label).toBe("Task Flow");
  });

  it("hides edges when one endpoint is filtered out", () => {
    const result = computeEdgeStyle({
      source: "a",
      target: "b",
      attrs: edgeAttrs(),
      sourceAttrs: nodeAttrs({ community: "platform" }),
      targetAttrs: nodeAttrs({ community: "product" }),
      state: reducerState({
        activeCommunity: "platform",
      }),
      lod: lodFromRatio(1),
    });

    expect(result.hidden).toBe(true);
  });

  it("dims cross-highlight edges while keeping them rendered", () => {
    const result = computeEdgeStyle({
      source: "spec",
      target: "untyped",
      attrs: edgeAttrs(),
      sourceAttrs: nodeAttrs({ resolvedType: "Spec" }),
      targetAttrs: nodeAttrs({ resolvedType: undefined }),
      state: reducerState({ highlightTypes: new Set(["Spec"]) }),
      lod: lodFromRatio(1),
    });

    expect(result.hidden).toBe(false);
    expect(result.color).toBe("rgba(65, 64, 63, 0.28)");
    expect(result.label).toBe("");
  });
});
