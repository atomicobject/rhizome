import Graph from "graphology";
import { describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { drawTestEdges, fileFocusGraph, focusFolderNodes, onBranch } from "./explorerGraph";
import { useFolderExplorer } from "./useFolderExplorer";

describe("Explorer focus", () => {
  it("retains the Vault totals at root and hides them in an expanded branch", () => {
    const root = {
      id: "folder:",
      kind: "module",
      label: "Vault",
      path: "",
      labelDetail: "655 notes · 4,572 files",
    };

    expect(focusFolderNodes([root], "")[0].labelDetail).toBe(root.labelDetail);
    expect(focusFolderNodes([root], "pkg/app")[0].labelDetail).toBeUndefined();
  });
  it("keeps positions while selecting a branch and retaining child counts", () => {
    const nodes = [
      {
        id: "folder:pkg",
        kind: "module",
        label: "pkg",
        path: "pkg",
        labelDetail: "3 files",
        x: 10,
        y: 20,
      },
      {
        id: "folder:pkg/app",
        kind: "module",
        label: "app",
        path: "pkg/app",
        labelDetail: "2 files",
        x: 40,
        y: 50,
      },
      {
        id: "folder:docs",
        kind: "module",
        label: "docs",
        path: "docs",
        labelDetail: "1 note",
        x: -20,
        y: 0,
      },
    ];

    expect(focusFolderNodes(nodes, "")).toEqual(nodes.map((node) => ({ ...node, center: false })));
    expect(focusFolderNodes(nodes, "pkg")).toEqual([
      { ...nodes[0], center: true },
      { ...nodes[1], center: false },
      { ...nodes[2], center: false, labelDetail: undefined },
    ]);
    expect(
      ["", "pkg", "pkg/app", "docs", "pkg-other"].map((path) => onBranch(path, "pkg/app")),
    ).toEqual([true, true, true, false, false]);
  });
  it("groups file neighbours deterministically and retains every edge and label", () => {
    const graph = {
      nodes: [
        { id: "f", kind: "code", path: "pkg/a.go", label: "a.go" },
        { id: "n", kind: "note", label: "Documenting note" },
        { id: "c", kind: "code", path: "pkg/b.go", label: "b.go" },
        { id: "t", kind: "code", path: "pkg/a_test.go", label: "a_test.go" },
      ],
      edges: [
        { source: "n", target: "f", kind: "mentions" },
        { source: "f", target: "t", kind: "calls" },
      ],
      truncated: false,
    };

    const focused = fileFocusGraph(graph, "pkg/a.go");
    expect(focused).toEqual(fileFocusGraph(graph, "pkg/a.go"));
    expect(focused.nodes.find((node) => node.id === "f")?.center).toBe(true);
    expect(focused.nodes.every((node) => Boolean(node.label) && Number.isFinite(node.x))).toBe(
      true,
    );
    expect(focused.nodes.map((node) => node.label)).toEqual(
      expect.arrayContaining(["Tests", "Other code"]),
    );
    expect(focused.edges.some((edge) => edge.kind === "explorer-tests")).toBe(true);
  });
  it("closes off-path automatic fans and restores them at the vault root", () => {
    const graph = {
      nodes: Array.from({ length: 20 }, (_, i) => [
        { id: `n${i}`, kind: "note", path: `docs/topic/a${i}.md`, label: `a${i}` },
        { id: `c${i}`, kind: "code", path: `pkg/app/cli/a${i}.go`, label: `a${i}` },
      ]).flat(),
      edges: [],
      truncated: false,
    };

    const { result } = renderHook(() => useFolderExplorer(graph, new Map()));
    expect(result.current.open.has("docs")).toBe(true);
    act(() => result.current.expand("pkg/app/cli"));
    expect([...result.current.open]).toEqual(["", "pkg", "pkg/app", "pkg/app/cli"]);
    act(() => result.current.reset());
    expect(result.current.open.has("docs")).toBe(true);
  });
});

it("draws one bracket when Sigma canonicalizes code edges toward the group", () => {
  const graph = new Graph();
  graph.addNode("focus", { selected: true });
  graph.addNode("file-group:Tests");
  graph.addNode("code:a");
  graph.addNode("code:b");
  graph.addEdge("file-group:Tests", "focus", { kind: "explorer-tests" });
  graph.addEdge("code:a", "file-group:Tests", { kind: "explorer-tests" });
  graph.addEdge("code:b", "file-group:Tests", { kind: "explorer-tests" });

  const points = new Map([
    ["focus", { x: 0, y: 0 }],
    ["file-group:Tests", { x: 100, y: 0 }],
    ["code:a", { x: 130, y: 40 }],
    ["code:b", { x: 130, y: 80 }],
  ]);

  const context = {
    strokeStyle: "",
    lineWidth: 1,
    moveTo: vi.fn(),
    lineTo: vi.fn(),
    beginPath: vi.fn(),
    stroke: vi.fn(),
    setLineDash: vi.fn(),
  };

  const sigma = {
    getGraph: () => graph,
    getNodeDisplayData: (id: string) => points.get(id),
    framedGraphToViewport: (point: { x: number; y: number }) => point,
  };

  drawTestEdges(sigma, context, "blue");
  expect(context.moveTo.mock.calls).toEqual([
    [0, 0],
    [100, 0],
    [118, 0],
    [118, 40],
    [118, 80],
  ]);
  expect(context.lineTo.mock.calls).toEqual([
    [100, 0],
    [118, 0],
    [118, 80],
    [130, 40],
    [130, 80],
  ]);
});
