import { expect, it, vi } from "vitest";
import { buildFolderTree } from "./folderTree";
import { visibleFolderGraph } from "./folderGraph";
import { layoutFolderGraph } from "./folderLayout";

it("budgets 10,000 root folders with counted overflow and sorts the overview once", () => {
  const tree = buildFolderTree({
    nodes: Array.from({ length: 10000 }, (_, i) => ({
      id: `f${i}`,
      path: `folder${i}/a.ts`,
      label: "a.ts",
      kind: "code",
    })),
    edges: [],
  });

  const graph = visibleFolderGraph(
    tree,
    { open: new Set([""]), explicit: new Set(), revealed: new Map() },
    () => undefined,
  );

  expect(graph.children.get("")).toHaveLength(10);
  expect(graph.nodes.find((n) => n.aggregate)).toMatchObject({
    label: "+9991 folders",
    childCount: 9991,
  });
  expect(graph.children.get("")?.flatMap((c) => c.members)).toHaveLength(10000);
  const compare = vi.spyOn(String.prototype, "localeCompare");

  try {
    const before = layoutFolderGraph(tree, graph);

    const revealed = {
      open: new Set([""]),
      explicit: new Set<string>(),
      revealed: new Map([["", 1]]),
    };

    const next = visibleFolderGraph(tree, revealed, () => undefined);

    const added = next.children
      .get("")
      ?.find((child) => child.folder && !graph.nodes.some((node) => node.id === child.node.id));

    expect(added?.folder).toBeDefined();
    revealed.open.add(added!.folder!.path);
    revealed.explicit.add(added!.folder!.path);

    const after = layoutFolderGraph(
      tree,
      visibleFolderGraph(tree, revealed, () => undefined),
    );

    expect(after.nodes.every((node) => Number.isFinite(node.x) && Number.isFinite(node.y))).toBe(
      true,
    );

    for (const node of before.nodes)
      expect(after.nodes.find((n) => n.id === node.id)).toMatchObject({ x: node.x, y: node.y });
    expect(compare.mock.calls.length).toBeLessThan(300000);
  } finally {
    compare.mockRestore();
  }
});
