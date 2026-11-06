import { expect, it } from "vitest";
import { folderSearchMatches } from "./folderSearchMatches";
import { visibleFolderGraph } from "./folderGraph";
import { buildFolderTree } from "../test/folderOverview";
import { buildSigmaGraph } from "./sigmaGraphData";

it("counts matches on visible folders, files and overflow representatives without moving them", () => {
  const graph = {
    nodes: Array.from({ length: 20 }, (_, i) => ({
      id: `note:${i}`,
      kind: "note",
      path: `docs/${i}.md`,
      label: `${i}`,
    })),
    edges: [],
    truncated: false,
  };

  const folders = visibleFolderGraph(
    buildFolderTree(graph),
    { open: new Set(["", "docs"]), explicit: new Set(["docs"]), revealed: new Map() },
    () => undefined,
  );

  const counts = folderSearchMatches(folders, graph, new Set(graph.nodes.map((n) => n.path)));
  expect(counts.get("folder:docs")).toBe(20);
  expect(counts.get("more:docs")).toBeGreaterThan(0);
  expect(counts.get("note:0")).toBe(1);

  const { graph: rendered } = buildSigmaGraph({
    nodes: folders.nodes.map((node, index) => ({
      x: index * 10,
      y: index * 20,
      ...node,
      searchMatchCount: counts.get(node.id) ?? 0,
    })),
    edges: folders.edges,
    scope: { mode: "global" },
    connectionCounts: new Map(),
    globalLayout: new Map(),
    communityColors: new Map(),
    problemCounts: new Map(),
    edgeFaint: "#ccc",
    fixed: true,
    explorer: true,
  });

  expect(rendered.getNodeAttributes("folder:docs")).toMatchObject({
    searchMatchCount: 20,
    labelDetail: "20 matches",
    muted: false,
  });
  expect(rendered.getNodeAttributes("folder:")).toMatchObject({ muted: true });

  folders.nodes.forEach((node, index) =>
    expect(rendered.getNodeAttributes(node.id)).toMatchObject({ x: index * 10, y: index * 20 }),
  );
});
