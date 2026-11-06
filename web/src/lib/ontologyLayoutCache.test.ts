import { describe, expect, it, vi } from "vitest";
import { buildOntologyGraph, type OntologyGraphModel } from "./ontologyGraph";
import { createOntologyLayoutCache } from "./ontologyLayoutCache";

const graph = (count = 1) =>
  buildOntologyGraph({
    schemaPresent: true,
    types: [
      {
        type: { name: "Thing", fields: [{ name: "parent", kind: "link", typeName: "Thing" }] },
        count,
      },
    ],
  });

const position = async (input: OntologyGraphModel) => ({
  nodes: input.nodes.map((node) => ({
    ...node,
    position: { x: 23, y: 42 },
    data: { ...node.data, selfRelations: ["parent"] },
  })),
  edges: input.edges.map((edge) => ({ ...edge, points: [{ x: 1, y: 2 }] })),
});

describe("ontology layout reuse", () => {
  it("shares in-flight layout and preserves current counts without exposing cached geometry", async () => {
    const compute = vi.fn(position);
    const layout = createOntologyLayoutCache(compute);
    const [first, second] = await Promise.all([layout(graph()), layout(graph(8))]);
    expect(compute).toHaveBeenCalledTimes(1);
    expect(second.nodes[0].data.count).toBe(8);
    expect(second.nodes[0].data.selfRelations).toEqual(["parent"]);
    first.nodes[0].position.x = 999;
    first.nodes[0].data.selfRelations!.push("unexpected");
    first.edges[0].points![0].x = 999;
    const again = await layout(graph(9));
    expect(again.nodes[0].position).toEqual({ x: 23, y: 42 });
    expect(again.nodes[0].data.selfRelations).toEqual(["parent"]);
    expect(again.edges[0].points).toEqual([{ x: 1, y: 2 }]);
    expect(again.nodes[0].data.count).toBe(9);
  });

  it("recomputes for changed dimensions or edges and bounds retained layouts", async () => {
    const compute = vi.fn(position);
    const layout = createOntologyLayoutCache(compute);
    await layout(graph());
    const resized = graph();
    resized.nodes[0].width += 10;
    await layout(resized);
    const changed = graph();
    changed.edges = [];
    await layout(changed);
    expect(compute).toHaveBeenCalledTimes(3);

    for (let n = 0; n < 4; n++) {
      const other = graph();
      other.nodes[0].width += 20 + n;
      await layout(other);
    }

    await layout(graph());
    expect(compute).toHaveBeenCalledTimes(8);
  });

  it("allows retry after a failed layout", async () => {
    const compute = vi.fn(position).mockRejectedValueOnce(new Error("failed"));
    const layout = createOntologyLayoutCache(compute);
    await expect(layout(graph())).rejects.toThrow("failed");
    await expect(layout(graph())).resolves.toHaveProperty("nodes");
    expect(compute).toHaveBeenCalledTimes(2);
  });
});
