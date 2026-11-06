import { expect, it } from "vitest";
import { buildFolderTree } from "./folderTree";
import { visibleFolderGraph } from "./folderGraph";
import { layoutFolderGraph } from "./folderLayout";

it("root file reveals preserve every folder and descendant coordinate", () => {
  for (const count of [3, 40]) {
    const tree = buildFolderTree({
      nodes: [
        ...Array.from({ length: 30 }, (_, i) => ({
          id: `root${i}`,
          path: `root${i}.ts`,
          label: `root${i}`,
          kind: "code",
        })),
        ...["a", "b", "c"].flatMap((folder) =>
          Array.from({ length: count }, (_, i) => ({
            id: `${folder}/${i}`,
            path: `${folder}/f${i}.ts`,
            label: `f${i}`,
            kind: "code",
          })),
        ),
      ],
      edges: [],
    });

    const draw = (batches: number) =>
      layoutFolderGraph(
        tree,
        visibleFolderGraph(
          tree,
          {
            open: new Set(["", "a", "b"]),
            explicit: new Set(["a", "b"]),
            revealed: new Map([["", batches]]),
          },
          () => undefined,
        ),
      );

    const before = draw(0);

    for (const batches of [1, 2, 3]) {
      const after = draw(batches);

      for (const node of before.nodes.filter((n) => !n.aggregate)) {
        expect(after.nodes.find((n) => n.id === node.id)).toMatchObject({ x: node.x, y: node.y });
      }

      expect(after.nodes.every((n) => Number.isFinite(n.x) && Number.isFinite(n.y))).toBe(true);
    }
  }
});
