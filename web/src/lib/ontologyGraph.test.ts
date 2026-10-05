import { describe, expect, it } from "vitest";

import type { OntologyAtlasResponse } from "../api/types";
import {
  buildOntologyGraph,
  fieldRowMidpointY,
  IMPL_SOURCE_RIGHT_HANDLE,
  layoutOntologyGraph,
  relationSourceHandle,
  TYPE_TARGET_LEFT_HANDLE,
} from "./ontologyGraph";

const atlas: OntologyAtlasResponse = {
  schemaPresent: true,
  types: [
    {
      type: {
        name: "Spec",
        description: "Governing product or technical intent.",
        implements: ["SpecLike"],
        fields: [
          {
            name: "summary",
            kind: "scalar",
            typeName: "string",
            required: true,
          },
          { name: "plan", kind: "link", typeName: "Plan", list: false },
          {
            name: "stories",
            kind: "section",
            typeName: "StoriesSection",
            list: true,
          },
        ],
      },
      count: 3,
      issueCount: 2,
    },
    {
      type: {
        name: "ProductSpec",
        implements: ["SpecLike"],
        fields: [
          {
            name: "summary",
            kind: "scalar",
            typeName: "string",
            required: true,
          },
          {
            name: "stories",
            kind: "section",
            typeName: "StoriesSection",
            list: true,
          },
        ],
      },
      count: 1,
    },
    {
      type: {
        name: "Plan",
        fields: [{ name: "tasks", kind: "link", typeName: "Tasks", list: true }],
      },
      count: 1,
    },
    { type: { name: "Story", locator: "EMBEDDED", fields: [] }, count: 0 },
    { type: { name: "Tasks", fields: [] }, count: 0 },
  ],
  interfaces: [
    {
      name: "SpecLike",
      fields: [],
    },
  ],
  sections: [
    {
      name: "StoriesSection",
      role: "SECTION",
      fields: [{ name: "stories", kind: "section", typeName: "Story", list: true }],
    },
  ],
};

describe("buildOntologyGraph", () => {
  it("shows exact reverse fields as typed relation edges", () => {
    const graph = buildOntologyGraph({
      schemaPresent: true,
      types: [
        {
          count: 1,
          type: {
            name: "Opportunity",
            fields: [{ name: "evidence", kind: "reverse", typeName: "Evidence", list: true }],
          },
        },
        {
          count: 1,
          type: {
            name: "Evidence",
            fields: [{ name: "opportunities", kind: "link", typeName: "Opportunity", list: true }],
          },
        },
      ],
    });

    const opportunity = graph.nodes.find((node) => node.id === "Opportunity");

    expect(opportunity?.data.relationCount).toBe(1);
    expect(opportunity?.data.fields.find((field) => field.name === "evidence")?.isSchemaEdge).toBe(
      true,
    );

    const reverse = graph.edges.find(
      (edge) => edge.source === "Opportunity" && edge.kind === "reverse",
    );

    expect(reverse?.target).toBe("Evidence");
    expect(reverse?.sourceHandle).toBe(relationSourceHandle("evidence", "right"));
  });

  it("emits a node for every type and interface", () => {
    const { nodes } = buildOntologyGraph(atlas);
    const ids = nodes.map((n) => n.id).sort();
    expect(ids).toEqual(["Plan", "ProductSpec", "Spec", "SpecLike", "Story", "Tasks"]);
    const specLike = nodes.find((n) => n.id === "SpecLike");
    expect(specLike?.data.isInterface).toBe(true);
    const spec = nodes.find((n) => n.id === "Spec");
    expect(spec?.data.fields.map((field) => field.name)).toEqual(["summary", "plan"]);
    expect(spec?.data.description).toBe("Governing product or technical intent.");
    expect(spec?.data.issueCount).toBe(2);
    expect(spec?.data.scalarFieldCount).toBe(1);
    expect(spec?.data.relationCount).toBe(1);
    expect(spec?.data.sectionCount).toBe(0);
    expect(specLike?.data.fields.map((field) => field.name)).toEqual(["stories.stories"]);
    expect(specLike?.data.sectionCount).toBe(1);
  });

  it("anchors relation edges to the source field handle", () => {
    const { edges } = buildOntologyGraph(atlas);
    const planEdge = edges.find((e) => e.kind === "link" && e.source === "Spec");
    expect(planEdge?.target).toBe("Plan");
    expect(planEdge?.sourceHandle).toBe(relationSourceHandle("plan", "right"));
    expect(planEdge?.label).toBe("plan");
    expect(planEdge?.data?.list).toBe(false);

    const storiesEdge = edges.find((e) => e.kind === "section" && e.source === "SpecLike");
    expect(storiesEdge?.target).toBe("Story");
    expect(storiesEdge?.sourceHandle).toBe(relationSourceHandle("stories.stories", "right"));
    expect(storiesEdge?.label).toBe("stories.stories*");
    expect(storiesEdge?.data?.list).toBe(true);
  });

  it("emits implements edges separately from relation edges", () => {
    const { edges } = buildOntologyGraph(atlas);
    const impl = edges.find((e) => e.type === "implements");
    expect(impl?.source).toBe("SpecLike");
    expect(impl?.target).toBe("Spec");
    expect(impl?.sourceHandle).toBe(IMPL_SOURCE_RIGHT_HANDLE);
    expect(impl?.targetHandle).toBe(TYPE_TARGET_LEFT_HANDLE);
  });

  it("drops relations whose target is unknown", () => {
    const { nodes, edges } = buildOntologyGraph({
      schemaPresent: true,
      types: [
        {
          type: {
            name: "Spec",
            fields: [{ name: "ghost", kind: "link", typeName: "Missing" }],
          },
          count: 1,
        },
      ],
    });

    expect(nodes.find((n) => n.id === "Missing")).toBeUndefined();
    expect(edges.find((e) => e.target === "Missing")).toBeUndefined();
  });

  it("places interfaces to the left of implementers", async () => {
    const graph = buildOntologyGraph(atlas);
    const { nodes, edges } = await layoutOntologyGraph(graph);
    const spec = nodes.find((n) => n.id === "Spec");
    const specLike = nodes.find((n) => n.id === "SpecLike");

    if (!spec || !specLike) {
      throw new Error("expected Spec and SpecLike nodes to exist");
    }

    expect(specLike.position.x).toBeLessThan(spec.position.x);

    const impl = edges.find((e) => e.type === "implements");
    expect(impl?.sourceHandle).toBe(IMPL_SOURCE_RIGHT_HANDLE);
    expect(impl?.targetHandle).toBe(TYPE_TARGET_LEFT_HANDLE);
  });

  it("preserves ELK-routed edge points for rendering", async () => {
    const graph = buildOntologyGraph(atlas);
    const { nodes, edges } = await layoutOntologyGraph(graph);

    for (const [source, kind, field] of [
      ["Spec", "link", "plan"],
      ["SpecLike", "section", "stories.stories"],
    ] as const) {
      const routedEdge = edges.find(
        (edge) => edge.type === "schema" && edge.source === source && edge.kind === kind,
      );

      const sourceNode = nodes.find((node) => node.id === source);
      expect(routedEdge?.sourceHandle).toBe(relationSourceHandle(field, "right"));
      expect(routedEdge?.points?.length).toBeGreaterThanOrEqual(2);
      expect(routedEdge?.points?.[0]).toMatchObject({
        x: expect.any(Number),
        y: expect.any(Number),
      });
      expect(routedEdge?.points?.[0]?.y).toBeCloseTo(
        (sourceNode?.position.y ?? 0) + fieldRowMidpointY(0),
        3,
      );
    }
  });

  it("keeps cyclic and self relations orthogonal at the visible field rows", async () => {
    const { nodes, edges } = await layoutOntologyGraph(
      buildOntologyGraph({
        schemaPresent: true,
        types: [
          {
            type: {
              name: "Alpha",
              fields: [
                { name: "summary", kind: "scalar" },
                { name: "next", kind: "link", typeName: "Beta" },
              ],
            },
            count: 1,
          },
          {
            type: {
              name: "Beta",
              fields: [
                { name: "back", kind: "link", typeName: "Alpha" },
                { name: "self", kind: "link", typeName: "Beta" },
              ],
            },
            count: 1,
          },
        ],
      }),
    );

    expect(edges).toHaveLength(3);

    for (const edge of edges) {
      const source = nodes.find((node) => node.id === edge.source)!;
      const target = nodes.find((node) => node.id === edge.target)!;

      const rowIndex = source.data.fields
        .filter((field) => field.isSchemaEdge)
        .findIndex((field) => field.name === edge.data?.fieldName);

      const points = edge.points!;
      expect(points[0]).toEqual({
        x: source.position.x + source.width,
        y: source.position.y + fieldRowMidpointY(rowIndex),
      });
      expect(points.at(-1)).toEqual({
        x: target.position.x,
        y: target.position.y + target.height / 2,
      });
      expect(edge.sourceHandle).toBe(relationSourceHandle(edge.data!.fieldName, "right"));
      expect(edge.targetHandle).toBe(TYPE_TARGET_LEFT_HANDLE);

      for (let i = 1; i < points.length; i += 1) {
        const start = points[i - 1];
        const end = points[i];
        expect(start.x === end.x || start.y === end.y).toBe(true);

        for (const node of nodes) {
          const crossesInterior =
            start.x === end.x
              ? start.x > node.position.x &&
                start.x < node.position.x + node.width &&
                Math.max(start.y, end.y) > node.position.y &&
                Math.min(start.y, end.y) < node.position.y + node.height
              : start.y > node.position.y &&
                start.y < node.position.y + node.height &&
                Math.max(start.x, end.x) > node.position.x &&
                Math.min(start.x, end.x) < node.position.x + node.width;

          expect(crossesInterior, `${edge.id} crosses ${node.id}`).toBe(false);
        }
      }
    }
  });

  it("packs disconnected types into a compact landscape without overlapping cards", async () => {
    const { nodes } = await layoutOntologyGraph(
      buildOntologyGraph({
        schemaPresent: true,
        types: Array.from({ length: 12 }, (_, index) => ({
          type: { name: `Type${index}`, fields: [] },
          count: 0,
        })),
      }),
    );

    const width = Math.max(...nodes.map((node) => node.position.x + node.width));
    const height = Math.max(...nodes.map((node) => node.position.y + node.height));
    expect(width / height).toBeGreaterThan(1.2);
    expect(width / height).toBeLessThan(2.2);
    expect(
      nodes.reduce((area, node) => area + node.width * node.height, 0) / (width * height),
    ).toBeGreaterThan(0.65);

    for (let i = 0; i < nodes.length; i += 1) {
      const a = nodes[i];

      for (const b of nodes.slice(i + 1)) {
        expect(
          a.position.x + a.width <= b.position.x ||
            b.position.x + b.width <= a.position.x ||
            a.position.y + a.height <= b.position.y ||
            b.position.y + b.height <= a.position.y,
        ).toBe(true);
      }
    }
  });

  it("keeps a detached pair from widening a larger branching component", async () => {
    const branches = Array.from({ length: 7 }, (_, index) => ({
      type: { name: `Branch${index}`, fields: [{ name: "next", kind: "link", typeName: "Tail" }] },
      count: 0,
    }));

    const types = [
      ...branches,
      {
        type: {
          name: "Root",
          fields: branches.map(({ type }, index) => ({
            name: `branch${index}`,
            kind: "link",
            typeName: type.name,
          })),
        },
        count: 0,
      },
      {
        type: { name: "Tail", fields: [{ name: "next", kind: "link", typeName: "Last" }] },
        count: 0,
      },
      { type: { name: "Last", fields: [] }, count: 0 },
    ];

    const main = await layoutOntologyGraph(buildOntologyGraph({ schemaPresent: true, types }));

    const mixed = await layoutOntologyGraph(
      buildOntologyGraph({
        schemaPresent: true,
        types: [
          {
            type: {
              name: "SeparateA",
              fields: [{ name: "next", kind: "link", typeName: "SeparateB" }],
            },
            count: 0,
          },
          { type: { name: "SeparateB", fields: [] }, count: 0 },
          ...types,
        ],
      }),
    );

    const width = (nodes: typeof main.nodes) =>
      Math.max(...nodes.map((node) => node.position.x + node.width));

    expect(width(mixed.nodes)).toBeLessThanOrEqual(width(main.nodes));
    const pair = mixed.nodes.filter((node) => node.id.startsWith("Separate"));
    const mainNodes = mixed.nodes.filter((node) => !node.id.startsWith("Separate"));
    const pairBottom = Math.max(...pair.map((node) => node.position.y + node.height));
    const mainTop = Math.min(...mainNodes.map((node) => node.position.y));
    expect(pairBottom).toBeLessThan(mainTop);
  });

  it("renders shared section relationships on the interface instead of each implementer", () => {
    const graph = buildOntologyGraph(atlas);

    const wrapperEdge = graph.edges.find(
      (e) => e.type === "schema" && e.source === "SpecLike" && e.target === "Story",
    );

    expect(wrapperEdge?.kind).toBe("section");
    expect(wrapperEdge?.label).toBe("stories.stories*");
    expect(
      graph.edges.find((e) => e.type === "schema" && e.source === "Spec" && e.target === "Story"),
    ).toBeUndefined();
    expect(
      graph.edges.find(
        (e) => e.type === "schema" && e.source === "ProductSpec" && e.target === "Story",
      ),
    ).toBeUndefined();
  });

  it("hoists shared note-implementer fields even when an embedded implementer differs", () => {
    const graph = buildOntologyGraph({
      schemaPresent: true,
      types: [
        {
          type: {
            name: "ProcessSpec",
            implements: ["SpecLike"],
            fields: [
              {
                name: "userStories",
                kind: "section",
                typeName: "UserStoriesSection",
                list: false,
              },
            ],
            locator: "FILE",
          },
          count: 1,
        },
        {
          type: {
            name: "ProductSpec",
            implements: ["SpecLike"],
            fields: [
              {
                name: "userStories",
                kind: "section",
                typeName: "UserStoriesSection",
                list: false,
              },
            ],
            locator: "FILE",
          },
          count: 1,
        },
        {
          type: {
            name: "SpecMetric",
            implements: ["SpecLike"],
            fields: [],
            locator: "EMBEDDED",
          },
          count: 0,
        },
        {
          type: { name: "UserStory", locator: "EMBEDDED", fields: [] },
          count: 0,
        },
      ],
      interfaces: [{ name: "SpecLike", fields: [] }],
      sections: [
        {
          name: "UserStoriesSection",
          role: "SECTION",
          fields: [
            {
              name: "stories",
              kind: "section",
              typeName: "UserStory",
              list: true,
            },
          ],
        },
      ],
    });

    const specLike = graph.nodes.find((node) => node.id === "SpecLike");
    expect(specLike?.data.fields.map((field) => field.name)).toContain("userStories.stories");

    expect(
      graph.edges.find(
        (edge) =>
          edge.type === "schema" &&
          edge.source === "SpecLike" &&
          edge.target === "UserStory" &&
          edge.label === "userStories.stories*",
      ),
    ).toBeDefined();

    expect(
      graph.edges.find(
        (edge) =>
          edge.type === "schema" && edge.source === "ProcessSpec" && edge.target === "UserStory",
      ),
    ).toBeUndefined();
  });
});
