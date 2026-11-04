import { describe, expect, it, vi } from "vitest";
import { radialSeed, runLayout } from "./graphLayout";

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

describe("radialSeed", () => {
  it("keeps the vault at the centre even when a child has more branches", () => {
    const edges = [
      ["folder:", "docs"],
      ["docs", "specs"],
      ["docs", "guides"],
      ["specs", "technical"],
    ].map(([source, target]) => ({ source, target, weight: 1 }));

    const done = vi.fn();

    runLayout(
      {
        nodes: ["folder:", "docs", "specs", "guides", "technical"].map((id) => ({
          id,
          x: 0,
          y: 0,
        })),
        edges,
        iterations: 100,
        radial: true,
      },
      done,
    );
    const positions: Map<string, { x: number; y: number }> = done.mock.calls[0][0];
    expect(
      ["folder:", "docs", "specs", "technical"].map((id) =>
        Math.round(Math.hypot(positions.get(id)!.x, positions.get(id)!.y)),
      ),
    ).toEqual([0, 100, 200, 300]);
  });
  it("rings a hub's neighbors around it and puts their own neighbors one ring out", () => {
    const edges = [
      { source: "hub", target: "a", weight: 9 },
      { source: "hub", target: "b", weight: 8 },
      { source: "a", target: "a1", weight: 3 },
      { source: "b", target: "a1", weight: 1 },
    ];

    const seed = radialSeed(["a", "a1", "b", "hub", "alone"], edges, 100);
    const ring = (id: string) => Math.round(Math.hypot(seed.get(id)!.x, seed.get(id)!.y));

    expect(ring("hub")).toBe(0);
    // a1 hangs off a, its heaviest link; a node with no links hangs off the hub.
    expect(["a", "b", "alone", "a1"].map(ring)).toEqual([100, 100, 100, 200]);
  });
});
