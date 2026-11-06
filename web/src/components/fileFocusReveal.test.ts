import { expect, it } from "vitest";
import { fileFocusGraph } from "./fileFocusGraph";

it("preserves existing neighbour positions across two reveals of the last group", () => {
  const focused = { id: "f", kind: "code", path: "main.go", label: "main.go" };

  const neighbours = Array.from({ length: 40 }, (_, i) => ({
    id: `c${i}`,
    kind: "code",
    path: `c${String(i).padStart(2, "0")}.go`,
    label: `c${i}.go`,
  }));

  const graph = {
    nodes: [focused, ...neighbours],
    edges: neighbours.map((node) => ({ source: "f", target: node.id, kind: "calls" })),
  };

  const expanded = new Set<string>();
  let before = fileFocusGraph(graph, focused.path, undefined, expanded);

  for (let reveal = 0; reveal < 2; reveal++) {
    const more = before.nodes.find((node) => node.id.startsWith("file-more:"));
    expect(more).toBeDefined();
    expanded.add(more!.id.slice("file-more:".length));
    const after = fileFocusGraph(graph, focused.path, undefined, expanded);

    for (const node of before.nodes.filter((node) => !node.aggregate)) {
      expect(after.nodes.find((other) => other.id === node.id)).toMatchObject({
        x: node.x,
        y: node.y,
      });
    }

    before = after;
  }
});
