import { describe, expect, it } from "vitest";
import { fileFocusGraph, mergeFileNotes } from "./fileFocusGraph";
import type { GraphResponse } from "../api/types";

const graph: GraphResponse = {
  nodes: [
    { id: "f", kind: "code", path: "src/main.go", label: "main.go" },
    ...Array.from({ length: 30 }, (_, i) => ({
      id: `c${i}`,
      kind: "code",
      path: `src/c${i}.go`,
      label: `c${i}.go`,
    })),
    { id: "t", kind: "code", path: "src/main_test.go", label: "main_test.go" },
  ],
  edges: [
    ...Array.from({ length: 30 }, (_, i) => ({ source: "f", target: `c${i}`, kind: "calls" })),
    { source: "t", target: "f", kind: "calls" },
  ],
};

describe("file neighbour groups", () => {
  it("separates calls and tests behind labelled trunks and counts every aggregated file", () => {
    const result = fileFocusGraph(graph, "src/main.go");
    expect(result.nodes.find((n) => n.id === "f")).toMatchObject({ x: 0, y: 0 });
    expect(result.nodes.find((n) => n.id === "file-group:Calls / imports")).toMatchObject({
      label: "Calls / imports",
      labelDetail: "30 files",
    });
    expect(result.nodes.find((n) => n.id === "file-more:Calls / imports")).toMatchObject({
      label: "Show 22 more",
      childCount: 22,
    });
    expect(result.edges.filter((edge) => edge.source === "f")).toHaveLength(2);
    expect(result.edges).toContainEqual({
      source: "file-group:Tests",
      target: "t",
      kind: "explorer-tests",
    });
    const expanded = fileFocusGraph(graph, "src/main.go", undefined, new Set(["Calls / imports"]));
    expect(expanded.nodes.find((node) => node.id === "file-group:Calls / imports")).toMatchObject({
      x: result.nodes.find((node) => node.id === "file-group:Calls / imports")?.x,
      y: result.nodes.find((node) => node.id === "file-group:Calls / imports")?.y,
    });
    expect(expanded.nodes.filter((node) => node.path?.startsWith("src/c"))).toHaveLength(30);
    expect(expanded.nodes.some((node) => node.id.startsWith("file-more:"))).toBe(false);
    const columns = new Map<number | undefined, number[]>();

    for (const node of expanded.nodes.filter((node) => node.path?.startsWith("src/c"))) {
      const ys = columns.get(node.x) ?? [];
      ys.push(node.y ?? 0);
      columns.set(node.x, ys);
    }

    expect(columns.size).toBe(2);

    for (const ys of columns.values()) {
      ys.sort((a, b) => a - b);
      expect(ys.slice(1).every((y, i) => y - ys[i] >= 24)).toBe(true);
    }
  });

  it("unions inspector anchor notes and references with graph notes, without duplicates", () => {
    const input = {
      ...graph,
      nodes: [...graph.nodes, { id: "n", kind: "note", path: "related.md", label: "Related" }],
    };

    const file = {
      path: "src/main.go",
      kind: "code",
      relatedNotes: [
        { path: "anchor.md", title: "Anchor", reason: "codeanchor" },
        { path: "anchor.md", title: "Anchor", reason: "codeanchor" },
        { path: "spec.md", title: "Spec", reason: "coderef" },
      ],
    };

    const merged = mergeFileNotes(input, file);
    expect(merged.nodes.filter((node) => node.kind === "note")).toHaveLength(3);
    const result = fileFocusGraph(input, file.path, file);
    expect(result.nodes.map((node) => node.label)).toEqual(
      expect.arrayContaining([
        "Mentioned in",
        "Documented by",
        "Related in graph",
        "Anchor",
        "Spec",
        "Related",
      ]),
    );
    expect(
      result.nodes
        .filter((n) => n.id.startsWith("file-group:") && n.kind === "note")
        .map((n) => n.label),
    ).toEqual(["Documented by", "Mentioned in", "Related in graph"]);
    expect(mergeFileNotes(merged, file)).toEqual(merged);
  });
});
