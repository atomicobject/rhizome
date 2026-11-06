import Sigma from "sigma";
import { expect, it, vi } from "vitest";
import { buildFolderTree } from "./folderTree";
import { visibleFolderGraph } from "./folderGraph";
import { layoutFolderGraph } from "./folderLayout";
import { buildSigmaGraph } from "./sigmaGraphData";
import { createLabelPainter } from "./sigmaLabels";
import { installFakeWebGL } from "../test/fakeWebGL";

it.each([0, 1])(
  "places the app fan outside the arc without overlapping names or counts (reveal %i)",
  (reveal) => {
    const restore = installFakeWebGL();
    const container = document.createElement("div");
    document.body.append(container);

    const names = [
      "validationproduct",
      "views",
      ...Array.from({ length: 38 }, (_, i) => `service${i}`),
    ];

    const tree = buildFolderTree({
      nodes: [
        ...Array.from({ length: 10 }, (_, i) => ({
          id: `doc${i}`,
          kind: "code",
          path: `docs/sub/f${i}.ts`,
          label: `doc${i}`,
        })),
        ...["validationproduct", "views"].map((name) => ({
          id: `${name}extra`,
          kind: "code",
          path: `pkg/app/${name}/extra.ts`,
          label: name,
        })),
        ...names.map((name) => ({
          id: name,
          kind: "code",
          path: `pkg/app/${name}/file.ts`,
          label: name,
        })),
      ],
      edges: [],
    });

    const layout = layoutFolderGraph(
      tree,
      visibleFolderGraph(
        tree,
        {
          open: new Set(["", "pkg", "pkg/app"]),
          explicit: new Set(["pkg/app"]),
          revealed: new Map([["pkg/app", reveal]]),
        },
        () => undefined,
        "pkg/app",
      ),
    );

    const fan = layout.nodes.filter(
      (n) => (n.path === "pkg/app" && n.aggregate) || n.path?.startsWith("pkg/app/"),
    );

    expect(fan.length).toBeGreaterThanOrEqual(reveal ? 20 : 14);
    expect(fan.some((n) => n.aggregate)).toBe(true);
    const parent = layout.nodes.find((n) => n.id === "folder:pkg/app")!;
    const ys = fan.map((n) => n.y ?? 0);
    const scale = (reveal ? 800 : 600) / (Math.max(...ys) - Math.min(...ys));

    const nodes = [parent, ...fan].map((n) => ({
      ...n,
      center: n.id === parent.id,
      labelDetail: "2 notes · 62 files 11 links to docs",
      x: (reveal ? 1300 : 568) + ((n.x ?? 0) - (parent.x ?? 0)) * scale,
      y: (reveal ? 60 : 40) + ((n.y ?? 0) - Math.min(...ys)) * scale,
    }));

    const { graph } = buildSigmaGraph({
      nodes,
      edges: [],
      scope: { mode: "global" },
      fixed: true,
      explorer: true,
      connectionCounts: new Map(),
      globalLayout: new Map(),
      communityColors: new Map(),
      problemCounts: new Map(),
      edgeFaint: "#ccc",
    });

    const sigma = new Sigma(graph, container);
    const context = document.createElement("canvas").getContext("2d")!;

    try {
      vi.spyOn(sigma, "getDimensions").mockReturnValue(
        reveal ? { width: 1800, height: 1000 } : { width: 1170, height: 698 },
      );
      vi.spyOn(sigma, "framedGraphToViewport").mockImplementation((p) => p);
      vi.spyOn(sigma, "scaleSize").mockReturnValue(6);
      vi.spyOn(sigma, "getNodeDisplayData").mockImplementation((id) =>
        graph.hasNode(id)
          ? {
              ...graph.getNodeAttributes(id),
              forceLabel: true,
              hidden: false,
              highlighted: false,
              zIndex: 0,
              type: "circle",
            }
          : undefined,
      );
      const measure = context.measureText.bind(context);
      vi.spyOn(context, "measureText").mockImplementation((text) => ({
        ...measure(text),
        width: text.length * 7,
      }));
      const drawn = vi.spyOn(context, "fillText");

      const painter = createLabelPainter(
        () => sigma,
        () => ({ label: "black", canvas: "white" }),
      );

      for (const node of nodes)
        painter.draw(
          context,
          { key: node.id, label: node.label, x: node.x, y: node.y, size: 6, color: "blue" },
          sigma.getSettings(),
        );
      const boxes = painter.occupied();
      expect(
        nodes.flatMap((n) => (drawn.mock.calls.some(([text]) => text === n.label) ? [] : [n.id])),
      ).toEqual([]);
      expect(boxes).toHaveLength(nodes.length);

      for (const node of nodes)
        if (node.labelDetail)
          expect(drawn.mock.calls.some(([text]) => text === node.labelDetail)).toBe(true);

      for (let i = 0; i < boxes.length; i++) {
        for (const b of boxes.slice(i + 1)) {
          const a = boxes[i];
          expect(a[0] < b[2] && a[2] > b[0] && a[1] < b[3] && a[3] > b[1]).toBe(false);
        }
      }

      // The selected parent is placed first; every child is on its left-facing arc.
      const children = nodes.slice(1).sort((a, b) => a.y - b.y || a.id.localeCompare(b.id));
      children.forEach((node, i) => expect(boxes[i + 1][2]).toBeLessThan(node.x));
    } finally {
      sigma.kill();
      container.remove();
      vi.restoreAllMocks();
      restore();
    }
  },
);
