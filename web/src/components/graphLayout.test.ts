import { describe, expect, it, vi } from "vitest";
import { runLayout } from "./graphLayout";

describe("runLayout", () => {
  it("lays out a small graph inline and returns a moved position for every node", () => {
    const done = vi.fn();
    const nodes = ["a", "b", "c"].map((id, index) => ({ id, x: index * 10, y: 0 }));

    runLayout(
      {
        nodes,
        edges: [
          { source: "a", target: "b", weight: 1 },
          { source: "b", target: "c", weight: 2 },
        ],
        iterations: 20,
      },
      done,
    );

    expect(done).toHaveBeenCalledTimes(1);
    const positions: Map<string, { x: number; y: number }> = done.mock.calls[0]?.[0];
    expect([...positions.keys()]).toEqual(["a", "b", "c"]);
    expect(positions.get("a")).not.toEqual({ x: 0, y: 0 });
  });
});
