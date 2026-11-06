import { act, renderHook } from "@testing-library/react";
import { expect, it } from "vitest";
import { useFolderExplorer } from "./useFolderExplorer";

const graph = (paths: string[]) => ({
  nodes: paths.map((path) => ({ id: path, path, label: path, kind: "code" })),
  edges: [],
  truncated: false,
});

it("retains root anchors across weight changes, additions and removals", () => {
  const { result, rerender } = renderHook(
    ({ paths }) => useFolderExplorer(graph(paths), new Map()),
    {
      initialProps: { paths: ["a/one.ts", "b/one.ts", "c/one.ts"] },
    },
  );

  const before = result.current.drawn.nodes.filter((n) => n.id.startsWith("folder:"));
  rerender({ paths: ["a/one.ts", "b/one.ts", "b/two.ts", "c/one.ts"] });

  for (const node of before)
    expect(result.current.drawn.nodes.find((n) => n.id === node.id)).toMatchObject({
      x: node.x,
      y: node.y,
    });
  const removed = before.find((n) => n.id === "folder:b")!;
  rerender({ paths: ["a/one.ts", "c/one.ts"] });
  rerender({ paths: ["a/one.ts", "c/one.ts", "d/one.ts"] });
  expect(result.current.drawn.nodes.find((n) => n.id === "folder:d")).toMatchObject({
    x: removed.x,
    y: removed.y,
  });
  act(() => result.current.expand("a"));

  for (const node of before.filter((n) => n.id !== "folder:b"))
    expect(result.current.drawn.nodes.find((n) => n.id === node.id)).toMatchObject({
      x: node.x,
      y: node.y,
    });
});
