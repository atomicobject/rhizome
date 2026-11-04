import { describe, expect, it } from "vitest";

import type { GraphNode } from "../api/types";
import { aggregateByFolder } from "./folderGraph";

const note = (path: string, resolvedType?: string): GraphNode => ({
  id: `note:${path}`,
  label: path,
  kind: "note",
  path,
  resolvedType,
});

const code = (path: string): GraphNode => ({ id: `code:${path}`, label: path, kind: "code", path });

describe("aggregateByFolder", () => {
  it("preserves a capped source graph's truncation warning", () => {
    expect(
      aggregateByFolder({ nodes: [note("a.md")], edges: [], truncated: true }, () => "red")
        .truncated,
    ).toBe(true);
  });

  it("opens a dominant folder into subfolders, folds small ones back, and counts links between folders", () => {
    const nodes = [
      ...Array.from({ length: 40 }, (_, i) => note(`docs/specs/s${i}.md`, "Spec")),
      ...Array.from({ length: 30 }, (_, i) => code(`pkg/app/a${i}.go`)),
      ...Array.from({ length: 30 }, (_, i) => code(`pkg/store/s${i}.go`)),
      code("pkg/tiny/t.go"),
      note("README.md"),
    ];

    const edges = [
      { source: "note:docs/specs/s0.md", target: "code:pkg/app/a0.go", kind: "mentions" },
      { source: "note:docs/specs/s1.md", target: "code:pkg/app/a1.go", kind: "mentions" },
      { source: "code:pkg/app/a0.go", target: "code:pkg/app/a1.go", kind: "calls" },
    ];

    const graph = aggregateByFolder({ nodes, edges }, (type) => `var(--${type})`);
    const byPath = new Map(graph.nodes.map((node) => [node.path, node]));

    expect(graph.nodes.map((node) => node.id.slice("folder:".length)).sort()).toEqual([
      "",
      "docs",
      "docs/specs",
      "pkg",
      "pkg/(smaller folders)",
      "pkg/app",
      "pkg/store",
    ]);
    expect(byPath.get("docs/specs")?.fill).toBe("var(--Spec)");
    expect(byPath.get("pkg/app")?.fill).toBe("var(--graph-code)");
    expect(byPath.get("docs/specs")?.fillKey).toBe("Spec");
    expect(graph.nodes.find((node) => node.id === "folder:pkg/(smaller folders)")?.label).toBe(
      "1 smaller folders  1 file",
    );
    expect(byPath.get("")?.label).toBe("Vault  41 notes, 61 files");
    expect(
      graph.edges
        .filter((edge) => edge.kind === "folder-containment")
        .map(({ source, target }) => [source, target]),
    ).toEqual(
      expect.arrayContaining([
        ["folder:", "folder:docs"],
        ["folder:docs", "folder:docs/specs"],
        ["folder:", "folder:pkg"],
        ["folder:pkg", "folder:pkg/app"],
        ["folder:pkg", "folder:pkg/(smaller folders)"],
      ]),
    );
    expect(aggregateByFolder({ nodes, edges: [] }, () => "red").edges).toEqual(
      graph.edges.filter((edge) => edge.kind === "folder-containment"),
    );
    // Links inside one folder are not drawn; links between folders add up.
    expect(graph.edges.filter((edge) => edge.kind === "folder-minor")).toEqual([
      expect.objectContaining({ source: "folder:docs/specs", target: "folder:pkg/app", weight: 2 }),
    ]);
  });

  it("counts notes, not embedded items, and keeps weak links for hover", () => {
    const criterion = (path: string, i: number): GraphNode => ({
      id: `embedded:${path}#c${i}`,
      label: `c${i}`,
      kind: "embedded",
      notePath: path,
      resolvedType: "AcceptanceCriterion",
    });

    const nodes = [
      note("docs/specs/a.md", "TechnicalSpec"),
      note("docs/specs/b.md", "TechnicalSpec"),
      ...Array.from({ length: 30 }, (_, i) => criterion("docs/specs/a.md", i)),
      ...Array.from({ length: 4 }, (_, i) => code(`pkg/a${i}.go`)),
      ...Array.from({ length: 4 }, (_, i) => code(`cmd/c${i}.go`)),
    ];

    const link = (source: string, target: string) => ({ source, target, kind: "mentions" });

    const edges = [
      ...Array.from({ length: 20 }, () => link("note:docs/specs/a.md", "code:pkg/a0.go")),
      ...Array.from({ length: 10 }, () => link("code:pkg/a1.go", "code:cmd/c0.go")),
      link("note:docs/specs/b.md", "code:cmd/c1.go"),
    ];

    const graph = aggregateByFolder({ nodes, edges }, (type) => `var(--${type})`);
    const specs = graph.nodes.find((node) => node.path === "docs/specs");

    // Thirty embedded criteria neither size nor color the folder.
    expect(specs).toMatchObject({ label: "docs/specs  2 notes", fillKey: "TechnicalSpec" });
    expect(
      graph.edges
        .filter((edge) => edge.kind === "folder-minor")
        .map((edge) => [edge.source, edge.target, edge.kind]),
    ).toEqual([
      ["folder:cmd", "folder:docs/specs", "folder-minor"],
      ["folder:cmd", "folder:pkg", "folder-minor"],
      ["folder:docs/specs", "folder:pkg", "folder-minor"],
    ]);
  });
});
