import { describe, expect, it } from "vitest";
import type { GraphNode } from "../api/types";
import { buildFolderTree, initialOpenFolders } from "./folderTree";
import { visibleFolderGraph } from "./folderGraph";
import { layoutFolderGraph } from "./folderLayout";

const code = (path: string): GraphNode => ({ id: `code:${path}`, path, label: path, kind: "code" });

const note = (path: string): GraphNode => ({
  id: `note:${path}`,
  path,
  label: path,
  kind: "note",
  resolvedType: "Spec",
});

const color = () => "red";

const state = (paths: string[] = [], showAll: string[] = []) => ({
  open: new Set(["", ...paths]),
  explicit: new Set(paths),
  showAll: new Set(showAll),
});

describe("complete folder model", () => {
  it("counts all files and note types, excluding embedded items, and deduplicates file pairs", () => {
    const tree = buildFolderTree({
      nodes: [
        note("docs/a.md"),
        code("src/a.ts"),
        { id: "criterion", kind: "embedded", label: "criterion", notePath: "docs/a.md" },
      ],
      edges: [
        { source: "note:docs/a.md", target: "criterion", kind: "embeds" },
        { source: "criterion", target: "code:src/a.ts", kind: "mentions" },
        { source: "note:docs/a.md", target: "code:src/a.ts", kind: "mentions" },
      ],
    });

    expect(tree.root).toMatchObject({
      notes: 1,
      files: 1,
      untyped: 0,
      types: new Map([["Spec", 1]]),
    });
    expect(tree.files.size).toBe(2);
    expect(tree.links).toEqual([
      { source: "code:src/a.ts", target: "note:docs/a.md", noteCode: 1, other: 0 },
    ]);
    const graph = visibleFolderGraph(tree, state(), color);
    expect(graph.edges.find((e) => e.kind === "folder-crosslink")).toMatchObject({
      weight: 1,
      relationLabel: "1 note–code links",
    });
  });
  it("budgets children without losing any file and reveals every aggregate member", () => {
    const tree = buildFolderTree({
      nodes: Array.from({ length: 40 }, (_, i) => code(`src/part${i}/a.ts`)),
      edges: [],
      truncated: true,
    });

    const graph = visibleFolderGraph(tree, state(["src"]), color);
    const children = graph.children.get("src") ?? [];
    expect(children.length).toBeLessThanOrEqual(24);
    expect(children.reduce((sum, c) => sum + c.members.length, 0)).toBe(40);
    expect(children.find((c) => c.node.aggregate)?.node.id).toBe("more:src");
    const all = visibleFolderGraph(tree, state(["src"], ["src"]), color);
    expect(all.children.get("src")).toHaveLength(40);
    expect(all.truncated).toBe(true);
  });
  it("makes direct files reachable even in a folder containing subfolders", () => {
    const tree = buildFolderTree({
      nodes: [code("src/sub/a.ts"), ...Array.from({ length: 35 }, (_, i) => code(`src/f${i}.ts`))],
      edges: [],
    });

    const collapsed = visibleFolderGraph(tree, state(), color);
    expect(collapsed.nodes.map((n) => n.id)).toEqual(["folder:", "folder:src"]);
    const open = visibleFolderGraph(tree, state(["src"], ["src"]), color);
    expect(open.nodes.filter((n) => n.kind === "code")).toHaveLength(35);
    expect(open.children.get("src")?.reduce((sum, c) => sum + c.members.length, 0)).toBe(36);
  });
  it("folds root files into a labelled reachable aggregate beside folders", () => {
    const tree = buildFolderTree({
      nodes: [note("README.md"), ...Array.from({ length: 30 }, (_, i) => code(`part${i}/a.ts`))],
      edges: [],
    });

    const graph = visibleFolderGraph(tree, state(), color);
    expect(graph.nodes.find((node) => node.id === "more:")).toMatchObject({
      label: "+1 note",
      labelDetail: undefined,
      childCount: 1,
    });
    expect(graph.children.get("")?.reduce((sum, child) => sum + child.members.length, 0)).toBe(31);
  });
  it("keeps every top-level folder visible beside root files", () => {
    const tree = buildFolderTree({
      nodes: [note("README.md"), ...Array.from({ length: 30 }, (_, i) => code(`part${i}/a.ts`))],
      edges: [],
    });

    const graph = visibleFolderGraph(tree, state(), color);
    expect(graph.children.get("")?.filter((child) => child.folder)).toHaveLength(30);
  });
  it("auto-opens only dominant top-level folders", () => {
    const tree = buildFolderTree({
      nodes: [
        ...Array.from({ length: 40 }, (_, i) => note(`docs/specs/nested/a${i}.md`)),
        code("pkg/a.ts"),
      ],
      edges: [],
    });

    expect(initialOpenFolders(tree)).toEqual(new Set(["", "docs"]));
  });
});

describe("stable radial containment", () => {
  it("opening a folder preserves every coordinate outside its subtree and its own anchor", () => {
    const tree = buildFolderTree({
      nodes: [code("a/one.ts"), code("a/sub/two.ts"), note("b/readme.md"), code("c/main.ts")],
      edges: [],
    });

    const before = layoutFolderGraph(tree, visibleFolderGraph(tree, state(), color));
    const after = layoutFolderGraph(tree, visibleFolderGraph(tree, state(["a"]), color));

    for (const node of before.nodes)
      expect(after.nodes.find((n) => n.id === node.id)).toMatchObject({ x: node.x, y: node.y });
    expect(after.layoutExtent).toBe(before.layoutExtent);
    expect(after.nodes.every((n) => Math.hypot(n.x ?? 0, n.y ?? 0) <= after.layoutExtent)).toBe(
      true,
    );
    expect(after.nodes.every((n) => Number.isFinite(n.x) && Number.isFinite(n.y))).toBe(true);
    expect(after.edges.filter((e) => e.kind === "folder-containment")).toEqual(
      expect.arrayContaining([expect.objectContaining({ source: "folder:", target: "folder:b" })]),
    );
  });
  it("keeps both dominant fan anchors fixed when a descendant opens", () => {
    const tree = buildFolderTree({
      nodes: [
        ...Array.from({ length: 40 }, (_, i) => code(`pkg/app/p${i}/a.ts`)),
        ...Array.from({ length: 40 }, (_, i) => note(`docs/topic${i}/a.md`)),
      ],
      edges: [],
    });

    const open = initialOpenFolders(tree);

    const before = layoutFolderGraph(
      tree,
      visibleFolderGraph(tree, { open, explicit: new Set(), showAll: new Set() }, color),
    );

    const after = layoutFolderGraph(
      tree,
      visibleFolderGraph(
        tree,
        { open: new Set([...open, "pkg/app"]), explicit: new Set(["pkg/app"]), showAll: new Set() },
        color,
      ),
    );

    for (const node of before.nodes)
      expect(after.nodes.find((n) => n.id === node.id)).toMatchObject({ x: node.x, y: node.y });
    expect(before.children.get("docs")).toHaveLength(10);
    expect(before.children.get("docs")?.reduce((sum, child) => sum + child.members.length, 0)).toBe(
      40,
    );
    const pkg = before.nodes.find((n) => n.id === "folder:pkg")!;
    const docs = before.nodes.find((n) => n.id === "folder:docs")!;
    expect(pkg.x! * docs.x!).toBeLessThan(0);
  });
  it("deep expansions add local radii instead of amplifying narrow root wedges", () => {
    const nodes = Array.from({ length: 20 }, (_, i) => code(`large/p${i}/a.ts`));

    const paths = Array.from(
      { length: 12 },
      (_, i) => "tiny/" + Array.from({ length: i }, () => "sub").join("/"),
    );

    nodes.push(code("tiny/" + "sub/".repeat(11) + "a.ts"));
    const tree = buildFolderTree({ nodes, edges: [] });
    const before = layoutFolderGraph(tree, visibleFolderGraph(tree, state(), color));
    const after = layoutFolderGraph(tree, visibleFolderGraph(tree, state(paths), color));
    expect(after.layoutExtent).toBe(before.layoutExtent);
    expect(after.layoutExtent).toBeLessThan(10000);

    for (const edge of after.edges.filter(
      (e) => e.kind === "folder-containment" && e.source !== "folder:",
    )) {
      const parent = after.nodes.find((n) => n.id === edge.source)!;
      const child = after.nodes.find((n) => n.id === edge.target)!;
      expect(Math.hypot(child.x! - parent.x!, child.y! - parent.y!)).toBeLessThanOrEqual(1000);
    }
  });
  it("places linked top-level siblings next to each other", () => {
    const tree = buildFolderTree({
      nodes: [note("docs/a.md"), code("web/a.ts"), code("pkg/a.go")],
      edges: [{ source: "note:docs/a.md", target: "code:web/a.ts", kind: "mentions" }],
    });

    const graph = layoutFolderGraph(tree, visibleFolderGraph(tree, state(), color));

    const angles = graph.nodes
      .filter((n) => n.id !== "folder:")
      .sort((a, b) => Math.atan2(a.y ?? 0, a.x ?? 0) - Math.atan2(b.y ?? 0, b.x ?? 0))
      .map((n) => n.id);

    expect(angles.slice(0, 2)).toEqual(["folder:docs", "folder:web"]);
  });
});

it("bundles underlying links while keeping focused children separate", () => {
  const tree = buildFolderTree({
    nodes: [note("docs/a.md"), note("docs/b.md"), code("pkg/a/a.ts"), code("pkg/b/b.ts")],
    edges: [
      { source: "note:docs/a.md", target: "code:pkg/a/a.ts", kind: "mentions" },
      { source: "note:docs/b.md", target: "code:pkg/a/a.ts", kind: "mentions" },
      { source: "note:docs/b.md", target: "code:pkg/b/b.ts", kind: "mentions" },
    ],
  });

  const open = state(["pkg", "docs"]);
  expect(
    visibleFolderGraph(tree, open, color).edges.filter((e) => e.kind === "folder-crosslink"),
  ).toEqual([
    {
      source: "folder:docs",
      target: "folder:pkg",
      kind: "folder-crosslink",
      weight: 3,
      relationLabel: "3 note–code links",
    },
  ]);
  const focused = visibleFolderGraph(tree, open, color, "pkg");
  expect(focused.edges.filter((e) => e.kind === "folder-crosslink")).toEqual([
    {
      source: "folder:pkg",
      target: "folder:docs",
      kind: "folder-crosslink",
      weight: 3,
      relationLabel: "3 note–code links",
    },
  ]);
  expect(focused.edges.filter((e) => e.kind === "folder-child-crosslink")).toHaveLength(2);
  expect(focused.nodes.find((n) => n.id === "folder:pkg")?.labelDetail).toContain(
    "3 links to docs",
  );
  expect(focused.nodes.find((n) => n.id === "folder:pkg/a")?.labelDetail).toContain(
    "2 links to docs",
  );
  expect(focused.nodes.find((n) => n.id === "folder:pkg/b")?.labelDetail).toContain(
    "1 link to docs",
  );
});

it("overflow gives direct counts once and labels descendant totals explicitly", () => {
  const tree = buildFolderTree({
    nodes: [...Array.from({ length: 12 }, (_, i) => note(`n${i}.md`)), code("a.ts")],
    edges: [],
  });

  expect(visibleFolderGraph(tree, state(), color).nodes.find((n) => n.aggregate)).toMatchObject({
    label: "+12 notes · 1 file",
    labelDetail: undefined,
  });

  const nested = buildFolderTree({
    nodes: Array.from({ length: 20 }, (_, i) => note(`docs/f${i}/n.md`)),
    edges: [],
  });

  expect(
    visibleFolderGraph(nested, state(["docs"]), color).nodes.find((n) => n.aggregate),
  ).toMatchObject({ label: "+7 folders", labelDetail: "total 7 notes" });
});

it("keeps repeated folder names and parent prefixes on one line, with counts separate", () => {
  const tree = buildFolderTree({ nodes: [code("web/a.ts"), code("pkg/web/b.ts")], edges: [] });
  const graph = visibleFolderGraph(tree, state(["pkg"]), color);
  expect(graph.nodes.find((node) => node.id === "folder:web")).toMatchObject({
    label: "Vault/ web",
    labelDetail: "1 file",
  });
  expect(graph.nodes.find((node) => node.id === "folder:pkg/web")).toMatchObject({
    label: "pkg/ web",
    labelDetail: "1 file",
  });
});

it("computes only root affinity and preserves the linked root ordering", () => {
  const tree = buildFolderTree({
    nodes: ["a/x/one.ts", "b/x/one.ts", "c/x/one.ts"].map(code),
    edges: [{ source: "code:a/x/one.ts", target: "code:c/x/one.ts", kind: "calls" }],
  });

  expect([...tree.affinity.keys()]).toEqual([""]);
  expect(tree.affinity.get("")).toEqual(new Map([['["folder:a","folder:c"]', 1]]));
  const graph = layoutFolderGraph(tree, visibleFolderGraph(tree, state(), color));

  const order = graph.nodes
    .filter((n) => ["folder:a", "folder:b", "folder:c"].includes(n.id))
    .sort((a, b) => Math.atan2(a.y ?? 0, a.x ?? 0) - Math.atan2(b.y ?? 0, b.x ?? 0))
    .map((n) => n.id);

  expect(order).toEqual(["folder:a", "folder:c", "folder:b"]);
});
