import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useFolderExplorer } from "./useFolderExplorer";

it("reveals the last of 10,000 siblings with bounded sorts and exact overflow", () => {
  const graph = {
    nodes: Array.from({ length: 10000 }, (_, i) => {
      const path = `dir${String(i).padStart(5, "0")}/file.go`;

      return { id: path, path, label: path, kind: "code" };
    }),
    edges: [],
    truncated: false,
  };

  const colors = new Map<string, string>();
  const { result } = renderHook(() => useFolderExplorer(graph, colors));
  const before = result.current.drawn.nodes;
  const sort = Array.prototype.sort;
  let largeSorts = 0;

  const spy = vi
    .spyOn(Array.prototype, "sort")
    .mockImplementation(function (this: unknown[], compare) {
      if (this.length === 10000) {
        largeSorts++;

        if (largeSorts > 2) throw new Error("Selection repeatedly sorts the whole directory");
      }

      return sort.call(this, compare);
    });

  try {
    act(() => result.current.expand("dir09999"));
    expect(largeSorts).toBeLessThanOrEqual(2);
    const children = result.current.drawn.children.get("")!;

    expect(children.length).toBeLessThanOrEqual(11);
    expect(children.some((child) => child.node.id === "folder:dir09999")).toBe(true);
    expect(children.find((child) => child.node.aggregate)).toMatchObject({
      node: { label: "+9990 folders", childCount: 9990 },
      members: expect.any(Array),
    });
    expect(children.reduce((total, child) => total + child.totals.files, 0)).toBe(10000);
    expect(new Set(children.flatMap((child) => child.members)).size).toBe(10000);

    for (const node of before)
      expect(result.current.drawn.nodes.find((next) => next.id === node.id)).toMatchObject({
        x: node.x,
        y: node.y,
      });
  } finally {
    spy.mockRestore();
  }
});
