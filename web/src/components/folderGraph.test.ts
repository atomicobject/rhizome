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

    expect([...byPath.keys()].sort()).toEqual(["", "docs/specs", "pkg", "pkg/app", "pkg/store"]);
    expect(byPath.get("docs/specs")?.fill).toBe("var(--Spec)");
    expect(byPath.get("pkg/app")?.fill).toBe("var(--graph-code)");
    expect(byPath.get("pkg")?.label).toBe("pkg · 1 smaller folders  1");
    expect(byPath.get("")?.label).toBe("Top-level files  1");
    // Links inside one folder are not drawn; links between folders add up.
    expect(graph.edges).toEqual([
      expect.objectContaining({ source: "folder:docs/specs", target: "folder:pkg/app", weight: 2 }),
    ]);
  });
});
