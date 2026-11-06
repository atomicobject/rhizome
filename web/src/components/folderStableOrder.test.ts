import { expect, it } from "vitest";
import { buildFolderTree } from "./folderTree";
import { visibleFolderGraph } from "./folderGraph";
import { layoutFolderGraph } from "./folderLayout";

it("keeps an open child fan fixed when another branch disambiguates its labels", () => {
  const tree = buildFolderTree({
    nodes: ["a/alpha/file.ts", "a/foo/file.ts", "b/foo/file.ts"].map((path) => ({
      id: path,
      path,
      label: path,
      kind: "code",
    })),
    edges: [],
  });

  const draw = (open: string[]) =>
    layoutFolderGraph(
      tree,
      visibleFolderGraph(
        tree,
        { open: new Set(["", ...open]), explicit: new Set(open), revealed: new Map() },
        () => undefined,
      ),
    );

  const before = draw(["a"]);
  const after = draw(["a", "b"]);
  expect(before.nodes.find((n) => n.id === "folder:a/foo")?.label).toBe("foo");
  expect(after.nodes.find((n) => n.id === "folder:a/foo")?.label).toBe("a/ foo");

  for (const node of before.nodes)
    expect(after.nodes.find((n) => n.id === node.id)).toMatchObject({ x: node.x, y: node.y });
});
