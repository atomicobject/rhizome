import { describe, expect, it } from "vitest";
import { fileFocusGraph, mergeFileNotes } from "./fileFocusGraph";
import type { GraphResponse } from "../api/types";

const graph: GraphResponse = {
  nodes: [
    { id: "f", kind: "code", path: "src/main.go", label: "main.go" },
    ...Array.from({ length: 30 }, (_, i) => ({
      id: `c${i}`,
      kind: "code",
      path: `src/c${i}.go`,
      label: `c${i}.go`,
    })),
    { id: "t", kind: "code", path: "src/main_test.go", label: "main_test.go" },
  ],
  edges: [
    ...Array.from({ length: 30 }, (_, i) => ({ source: "f", target: `c${i}`, kind: "calls" })),
    { source: "t", target: "f", kind: "calls" },
  ],
};

describe("file neighbour groups", () => {
  it("separates calls and tests behind labelled trunks and counts every aggregated file", () => {
    const result = fileFocusGraph(graph, "src/main.go");
    expect(result.nodes.find((n) => n.id === "f")).toMatchObject({ x: 0, y: 0 });
    expect(result.nodes.find((n) => n.id === "file-group:Calls / imports")).toMatchObject({
      label: "Calls / imports",
      labelDetail: "30 files",
    });
    expect(result.nodes.find((n) => n.id === "file-more:Calls / imports")).toMatchObject({
      label: "Show 22 more",
      childCount: 22,
    });
    expect(result.edges.filter((edge) => edge.source === "f")).toHaveLength(2);
    expect(result.edges).toContainEqual({
      source: "file-group:Tests",
      target: "t",
      kind: "explorer-tests",
    });
    const expanded = fileFocusGraph(graph, "src/main.go", undefined, new Set(["Calls / imports"]));
    expect(expanded.nodes.find((node) => node.id === "file-group:Calls / imports")).toMatchObject({
      x: result.nodes.find((node) => node.id === "file-group:Calls / imports")?.x,
      y: result.nodes.find((node) => node.id === "file-group:Calls / imports")?.y,
    });
    expect(expanded.nodes.filter((node) => node.path?.startsWith("src/c"))).toHaveLength(22);
    expect(expanded.nodes.find((node) => node.id.startsWith("file-more:"))).toMatchObject({
      childCount: 8,
    });
    const columns = new Map<number | undefined, number[]>();

    for (const node of expanded.nodes.filter((node) => node.path?.startsWith("src/c"))) {
      const ys = columns.get(node.x) ?? [];
      ys.push(node.y ?? 0);
      columns.set(node.x, ys);
    }

    expect(columns.size).toBe(3);

    for (const ys of columns.values()) {
      ys.sort((a, b) => a - b);
      expect(ys.slice(1).every((y, i) => y - ys[i] >= 24)).toBe(true);
    }
  });

  it("keeps unrelated groups and their members fixed when calls are revealed", () => {
    const input = {
      nodes: [
        ...graph.nodes.slice(0, 13),
        { id: "other", kind: "code", path: "src/other.go", label: "other.go" },
        graph.nodes[31],
      ],
      edges: graph.edges.slice(0, 12),
    };

    const before = fileFocusGraph(input, "src/main.go");
    const after = fileFocusGraph(input, "src/main.go", undefined, new Set(["Calls / imports"]));

    for (const id of ["file-group:Other code", "other", "file-group:Tests", "t"]) {
      const anchor = before.nodes.find((node) => node.id === id);
      expect(after.nodes.find((node) => node.id === id)).toMatchObject({
        x: anchor?.x,
        y: anchor?.y,
      });
    }

    const positions = after.nodes
      .filter((node) => node.id !== "f")
      .map((node) => `${node.x}:${node.y}`);

    expect(new Set(positions).size).toBe(positions.length);
  });

  it("keeps eleven tests in one lane when their disclosure expands", () => {
    const tests = Array.from({ length: 11 }, (_, i) => ({
      id: `t${i}`,
      kind: "code",
      path: `src/case${i}_test.go`,
      label: `case${i}_test.go`,
    }));

    const result = fileFocusGraph(
      { nodes: [graph.nodes[0], ...tests], edges: [] },
      "src/main.go",
      undefined,
      new Set(["Tests"]),
    );

    expect(result.nodes.filter((node) => node.path?.endsWith("_test.go"))).toHaveLength(11);
    expect(
      new Set(result.nodes.filter((node) => node.path?.endsWith("_test.go")).map((node) => node.x))
        .size,
    ).toBe(1);
  });

  it("prepares 10,000 anchor neighbours with a bounded number of property reads", () => {
    let reads = 0;

    const counted = <T extends object>(value: T): T =>
      new Proxy(value, {
        get(target, key, receiver) {
          reads++;

          // Preserve normal property access while counting work on the input.
          // oxlint-disable-next-line anti-slop/no-reflect-get
          return Reflect.get(target, key, receiver);
        },
      });

    const relatedNotes = Array.from({ length: 10_000 }, (_, i) =>
      counted({
        path: `notes/${i}.md`,
        title: `Note ${i}`,
        reason: "codeanchor",
      }),
    );

    const nodes = [
      graph.nodes[0],
      ...relatedNotes.map((note, i) =>
        counted({
          id: `n${i}`,
          kind: "note",
          path: note.path,
          label: note.title,
        }),
      ),
    ];

    const edges = relatedNotes.map((_, i) =>
      counted({ source: `n${i}`, target: "f", kind: "codeanchor" }),
    );

    // Include neighbour links so representative lookup is covered too.
    edges.push(
      ...relatedNotes
        .slice(1)
        .map((_, i) => counted({ source: `n${i}`, target: `n${i + 1}`, kind: "link" })),
    );
    reads = 0;

    const result = fileFocusGraph({ nodes, edges }, "src/main.go", {
      path: "src/main.go",
      kind: "code",
      relatedNotes,
    });

    expect(result.nodes).toHaveLength(11);
    expect(reads).toBeLessThan(2_000_000);

    const merged = mergeFileNotes(
      { nodes: [graph.nodes[0]], edges: [] },
      { path: "src/main.go", kind: "code", relatedNotes },
    );

    expect(merged.nodes).toHaveLength(10_001);
    expect(merged.edges).toHaveLength(10_000);
    expect(reads).toBeLessThan(4_000_000);
  });

  it("reveals 10,000 notes in bounded batches with an exact clickable remainder", () => {
    const file = {
      path: "src/main.go",
      kind: "code",
      relatedNotes: Array.from({ length: 10_000 }, (_, i) => ({
        path: `notes/${i}.md`,
        title: `Note ${i}`,
        reason: "codeanchor",
      })),
    };

    const input = { nodes: [graph.nodes[0]], edges: [] };
    const expanded = new Set<string>();

    for (let batch = 0; batch < 4; batch++) {
      const result = fileFocusGraph(input, file.path, file, expanded);
      const shown = result.nodes.filter((node) => !node.aggregate && !node.center);
      expect(shown).toHaveLength(8 + batch * 14);
      const more = result.nodes.find((node) => node.id.startsWith("file-more:"));
      expect(more).toMatchObject({
        label: `Show ${10_000 - shown.length} more`,
        childCount: 10_000 - shown.length,
      });
      expect(
        result.edges.every((edge) => result.nodes.some((node) => node.id === edge.target)),
      ).toBe(true);
      expanded.add(more!.id.slice("file-more:".length));
    }
  });

  it("unions inspector anchor notes and references with graph notes, without duplicates", () => {
    const input = {
      ...graph,
      nodes: [...graph.nodes, { id: "n", kind: "note", path: "related.md", label: "Related" }],
    };

    const file = {
      path: "src/main.go",
      kind: "code",
      relatedNotes: [
        { path: "anchor.md", title: "Anchor", reason: "codeanchor" },
        { path: "anchor.md", title: "Anchor", reason: "codeanchor" },
        { path: "spec.md", title: "Spec", reason: "coderef" },
      ],
    };

    const merged = mergeFileNotes(input, file);
    expect(merged.nodes.filter((node) => node.kind === "note")).toHaveLength(3);
    const result = fileFocusGraph(input, file.path, file);
    expect(result.nodes.map((node) => node.label)).toEqual(
      expect.arrayContaining([
        "Mentioned in",
        "Documented by",
        "Related in graph",
        "Anchor",
        "Spec",
        "Related",
      ]),
    );
    expect(
      result.nodes
        .filter((n) => n.id.startsWith("file-group:") && n.kind === "note")
        .map((n) => n.label),
    ).toEqual(["Documented by", "Mentioned in", "Related in graph"]);
    expect(mergeFileNotes(merged, file)).toEqual(merged);
  });
});

it("shows server omissions as a remainder that cannot be revealed", () => {
  const result = fileFocusGraph(
    {
      nodes: [{ id: "main", kind: "code", path: "src/main.go", label: "main.go" }],
      edges: [],
      omittedNodes: 101,
      omittedEdges: 123,
    },
    "src/main.go",
  );

  expect(result.nodes.find((node) => node.id === "file-omitted")).toMatchObject({
    collapsed: true,
    label: "+101 more not loaded",
    labelDetail: "123 links not loaded",
    aggregate: true,
  });
  expect(result.nodes.some((node) => node.id.startsWith("file-more:"))).toBe(false);
});

it("reconciles the omitted neighbour when file details load all 201 notes", () => {
  const relatedNotes = Array.from({ length: 201 }, (_, i) => ({
    path: `notes/${i}.md`,
    title: `Note ${i}`,
    reason: "coderef",
  }));

  const input = {
    nodes: [
      graph.nodes[0],
      ...relatedNotes.slice(0, 200).map((note) => ({
        id: `note:${note.path}`,
        kind: "note",
        path: note.path,
        label: note.title,
      })),
    ],
    edges: relatedNotes
      .slice(0, 200)
      .map((note) => ({ source: `note:${note.path}`, target: "f", kind: "coderef" })),
    omittedNodes: 1,
    omittedEdges: 1,
    omittedNodeIDs: ["note:notes/200.md"],
    omittedLinks: [{ source: "note:notes/200.md", target: "f", kind: "coderef" }],
  };

  const file = { path: "src/main.go", kind: "code", relatedNotes };
  const result = fileFocusGraph(input, file.path, file);
  expect(result.nodes.some((node) => node.id === "file-omitted")).toBe(false);
  expect(result.nodes.find((node) => node.id === "file-group:Documented by")?.labelDetail).toBe(
    "201 notes",
  );
  expect(mergeFileNotes(mergeFileNotes(input, file), file)).toEqual(mergeFileNotes(input, file));

  const unrelated = {
    ...file,
    relatedNotes: [{ path: "anchor-only.md", title: "Extra", reason: "codeanchor" }],
  };

  expect(
    fileFocusGraph(input, file.path, unrelated).nodes.find((node) => node.id === "file-omitted")
      ?.label,
  ).toBe("+1 more not loaded");
});
