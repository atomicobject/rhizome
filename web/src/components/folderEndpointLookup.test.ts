import { expect, it, vi } from "vitest";
import { buildFolderTree } from "./folderTree";
import { visibleFolderGraph } from "./folderGraph";

it("resolves 10,000 file pairs without scanning 10,000 visible folders per endpoint", () => {
  const tree = buildFolderTree({
    nodes: Array.from({ length: 10000 }, (_, i) => ({
      id: `f${i}`,
      path: `folder${i}/a.ts`,
      label: "a.ts",
      kind: "code",
    })),
    edges: Array.from({ length: 10000 }, (_, i) => ({
      source: `f${i}`,
      target: `f${(i + 1) % 10000}`,
      kind: "calls",
    })),
  });

  const scan = vi.spyOn(Array.prototype, "some");

  try {
    const graph = visibleFolderGraph(
      tree,
      { open: new Set([""]), explicit: new Set(), revealed: new Map([["", 1000]]) },
      () => undefined,
    );

    expect(graph.nodes).toHaveLength(10001);
    expect(graph.edges.filter((e) => e.kind === "folder-minor")).toHaveLength(10000);
    expect(
      scan.mock.contexts.reduce<number>(
        (sum, array) => sum + (Array.isArray(array) ? array.length : 0),
        0,
      ),
    ).toBeLessThan(100000);
  } finally {
    scan.mockRestore();
  }
});
