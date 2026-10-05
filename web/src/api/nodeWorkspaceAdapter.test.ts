import { describe, expect, it } from "vitest";
import { resolveRenderedLinkTarget } from "../lib/content";
import { nodeWorkspaceFromPublicGraphQL } from "./nodeWorkspaceAdapter";
import type { PublicNodeDetailData, PublicNodeWorkspaceProjection } from "./publicGraphQLTypes";

/** Fills the projection fields a fixture does not exercise. */
function publicWorkspace(
  overrides: Partial<PublicNodeWorkspaceProjection>,
): PublicNodeWorkspaceProjection {
  return {
    fields: [],
    collections: [],
    bodies: [],
    relationGroups: [],
    structure: [],
    capabilities: {
      canEdit: true,
      canEditFields: true,
      canEditCollections: true,
      canNavigateChildren: true,
      canSubscribe: true,
    },
    status: {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    },
    version: "",
    loaded: { rendered: true, assessment: false, structure: false, relations: false },
    ...overrides,
  };
}

describe("nodeWorkspaceFromPublicGraphQL", () => {
  it("maps public GraphQL node detail into the workspace view model", () => {
    const data: PublicNodeDetailData = {
      node: {
        ref: {
          ref: "specs/api.md",
          kind: "NOTE",
          notePath: "specs/api.md",
          path: "specs/api.md",
          typeName: "Spec",
        },
        nodeId: "note:specs/api.md",
        nodeKind: "NOTE",
        path: "specs/api.md",
        title: "Public API",
        resolvedType: "Spec",
        content: "# Public API\n",
        locator: {
          sourceLocator: "specs/api.md",
          status: "linkable",
          wikilink: "[[api]]",
          markdown: "specs/api.md",
          exists: true,
          requiresFix: false,
          linkTarget: {
            markdown: "specs/api.md",
            wikilink: "[[api]]",
            displayLabel: "api",
            exists: true,
            requiresFix: false,
          },
        },
        localGraph: {
          truncated: false,
          nodes: [
            {
              id: "note:specs/api.md",
              ref: {
                ref: "specs/api.md",
                kind: "NOTE",
                notePath: "specs/api.md",
                path: "specs/api.md",
                typeName: "Spec",
              },
              nodeKind: "NOTE",
              path: "specs/api.md",
              notePath: "specs/api.md",
              title: "Public API",
              typeName: "Spec",
              sourceLocator: "specs/api.md",
            },
            {
              id: "note:specs/plan.md",
              ref: {
                ref: "specs/plan.md",
                kind: "NOTE",
                notePath: "specs/plan.md",
                path: "specs/plan.md",
                typeName: "Plan",
              },
              nodeKind: "NOTE",
              path: "specs/plan.md",
              notePath: "specs/plan.md",
              title: "Plan",
              typeName: "Plan",
              sourceLocator: "specs/plan.md",
            },
          ],
          edges: [
            {
              source: "note:specs/api.md",
              target: "note:specs/plan.md",
              kind: "ontology",
              relation: "plans",
              relationLabel: "plans",
              provenance: "field",
              structural: true,
              weight: 1,
            },
          ],
        },
        workspace: {
          bodies: [],
          fields: [
            {
              name: "summary",
              kind: "FRONTMATTER_FIELD",
              present: true,
              status: {
                dirty: false,
                validation: { issueCount: 1 },
                freshness: {},
                session: {},
                hasWarnings: true,
              },
              range: { start: 0, end: 20 },
              valueRanges: [{ start: 10, end: 20 }],
              values: ["Composable API"],
              sectionNodes: [],
            },
          ],
          collections: [],
          capabilities: {
            canEdit: true,
            canEditFields: true,
            canEditCollections: false,
            canNavigateChildren: true,
            canSubscribe: true,
          },
          status: {
            dirty: false,
            validation: { issueCount: 1 },
            freshness: {},
            session: {},
            hasWarnings: true,
          },
          version: "workspace-v1",
          structure: [
            {
              ref: {
                ref: "specs/api.md#design-10",
                kind: "SECTION",
                notePath: "specs/api.md",
                fragment: "design-10",
                nodeId: "design-10",
              },
              title: "Design",
              level: "H2",
              content: "Typed projections",
            },
          ],
          relationGroups: [
            {
              key: "structural",
              label: "Structural relations",
              items: [
                {
                  ref: {
                    ref: "specs/plan.md",
                    kind: "NOTE",
                    notePath: "specs/plan.md",
                  },
                  title: "Plan",
                  relationName: "plans",
                  provenance: "field",
                  structural: true,
                },
              ],
            },
          ],
          loaded: {
            rendered: true,
            assessment: true,
            structure: true,
            relations: true,
          },
        },
      },
    };

    const workspace = nodeWorkspaceFromPublicGraphQL(data, "specs/api.md");

    expect(workspace.requestedRef).toBe("specs/api.md");
    expect(workspace.focusedNodeId).toBe("note:specs/api.md");
    expect(workspace.node.title).toBe("Public API");
    expect(workspace.content.markdown).toBe("# Public API\n");
    expect(workspace.nodes).toHaveLength(2);
    expect(workspace.edges).toEqual([
      expect.objectContaining({
        fromId: "note:specs/api.md",
        toId: "note:specs/plan.md",
        kind: "links_to",
        relationKey: "plans",
      }),
    ]);
    expect(workspace.views?.localGraph?.centerNodeIds).toEqual(["note:specs/api.md"]);
    expect(workspace.fields?.[0]).toEqual(
      expect.objectContaining({ name: "summary", values: ["Composable API"] }),
    );
    expect(workspace.fields?.[0].capability?.displayImportance).toBe("NORMAL");
    expect(workspace.status.validation.issueCount).toBe(1);
    expect(workspace.capabilities.canEdit).toBe(true);
    expect(workspace.content.structural?.root.children?.[0]?.title).toBe("Design");
    expect(workspace.relations?.[0]?.items?.[0]).toEqual(
      expect.objectContaining({
        path: "specs/plan.md",
        relationName: "plans",
      }),
    );
    expect(workspace.loaded).toEqual({
      rendered: true,
      assessment: true,
      structure: true,
      relations: true,
    });
  });

  it("defaults missing rendered outline levels without flattening the hierarchy", () => {
    const data: PublicNodeDetailData = {
      node: {
        ref: { ref: "specs/api.md", kind: "NOTE", notePath: "specs/api.md" },
        nodeId: "note:specs/api.md",
        nodeKind: "NOTE",
        path: "specs/api.md",
        title: "Public API",
        content: "# Public API\n",
        locator: {
          sourceLocator: "specs/api.md",
          status: "linkable",
          exists: true,
          requiresFix: false,
        },
        workspace: publicWorkspace({
          structure: [
            {
              ref: {
                ref: "specs/api.md#overview",
                kind: "SECTION",
                notePath: "specs/api.md",
                nodeId: "overview",
              },
              title: "Overview",
            },
            {
              ref: {
                ref: "specs/api.md#details",
                kind: "SECTION",
                notePath: "specs/api.md",
                nodeId: "details",
              },
              parentRef: {
                ref: "specs/api.md#overview",
                kind: "SECTION",
                notePath: "specs/api.md",
                nodeId: "overview",
              },
              title: "Details",
              level: "H3",
            },
          ],
        }),
      },
    };

    const rendered = nodeWorkspaceFromPublicGraphQL(data, "specs/api.md").content.rendered;

    expect(rendered?.sections).toEqual([
      {
        id: "overview",
        title: "Overview",
        level: "H2",
        content: "",
        notePath: "specs/api.md",
        locator: "SECTION",
        children: [
          {
            id: "details",
            title: "Details",
            level: "H3",
            content: "",
            notePath: "specs/api.md",
            locator: "SECTION",
            children: [],
          },
        ],
      },
    ]);
  });

  it("keeps embedded focus when the note root shares the same path", () => {
    const data: PublicNodeDetailData = {
      node: {
        ref: {
          ref: "specs/api.md#^story-a",
          kind: "EMBEDDED",
          notePath: "specs/api.md",
          path: "specs/api.md",
          fragment: "^story-a",
          nodeId: "story-a",
          typeName: "Story",
        },
        nodeId: "story-a",
        nodeKind: "EMBEDDED",
        path: "specs/api.md",
        title: "Story A",
        resolvedType: "Story",
        content: "Story content",
        locator: {
          sourceLocator: "specs/api.md#^story-a",
          status: "linkable",
          wikilink: "[[api#^story-a]]",
          exists: true,
          requiresFix: false,
        },
        localGraph: {
          truncated: false,
          nodes: [
            {
              id: "note:specs/api.md",
              ref: {
                ref: "specs/api.md",
                kind: "NOTE",
                notePath: "specs/api.md",
                path: "specs/api.md",
                typeName: "Spec",
              },
              nodeKind: "NOTE",
              path: "specs/api.md",
              notePath: "specs/api.md",
              title: "Public API",
              typeName: "Spec",
              sourceLocator: "specs/api.md",
            },
            {
              id: "embedded:story-a",
              ref: {
                ref: "specs/api.md#^story-a",
                kind: "EMBEDDED",
                notePath: "specs/api.md",
                path: "specs/api.md",
                fragment: "^story-a",
                nodeId: "story-a",
                typeName: "Story",
              },
              nodeKind: "EMBEDDED",
              path: "specs/api.md",
              notePath: "specs/api.md",
              title: "Story A",
              nodeId: "story-a",
              typeName: "Story",
              sourceLocator: "specs/api.md#^story-a",
            },
          ],
          edges: [],
        },
      },
    };

    const workspace = nodeWorkspaceFromPublicGraphQL(data, "specs/api.md#^story-a");

    expect(workspace.focusedNodeId).toBe("embedded:story-a");
    expect(workspace.views?.localGraph?.centerNodeIds).toEqual(["embedded:story-a"]);
  });

  it("does not center a parent-fallback graph on an absent embedded node", () => {
    const data: PublicNodeDetailData = {
      node: {
        ref: {
          ref: "specs/api.md#^story-a",
          kind: "EMBEDDED",
          notePath: "specs/api.md",
          fragment: "^story-a",
          nodeId: "story-a",
        },
        nodeId: "story-a",
        nodeKind: "EMBEDDED",
        path: "specs/api.md",
        title: "Story A",
        locator: {
          sourceLocator: "specs/api.md#^story-a",
          status: "linkable",
          exists: true,
          requiresFix: false,
        },
        localGraph: {
          truncated: false,
          nodes: [
            {
              id: "note:specs/api.md",
              nodeKind: "NOTE",
              path: "specs/api.md",
              notePath: "specs/api.md",
              title: "Public API",
            },
          ],
          edges: [],
        },
      },
    };

    const workspace = nodeWorkspaceFromPublicGraphQL(data, "specs/api.md#^story-a");

    expect(workspace.focusedNodeId).toBe("story-a");
    expect(workspace.views?.localGraph?.nodeIds).toEqual(["note:specs/api.md"]);
    expect(workspace.views?.localGraph?.centerNodeIds).toEqual([]);
  });

  it("preserves relation target kinds", () => {
    const data: PublicNodeDetailData = {
      node: {
        ref: { ref: "specs/api.md", kind: "NOTE", notePath: "specs/api.md" },
        nodeId: "note:specs/api.md",
        nodeKind: "NOTE",
        path: "specs/api.md",
        title: "API",
        locator: {
          sourceLocator: "specs/api.md",
          status: "linkable",
          exists: true,
          requiresFix: false,
        },
        workspace: publicWorkspace({
          fields: [],
          collections: [],
          relationGroups: [
            {
              key: "structural",
              label: "Structural relations",
              items: [
                {
                  ref: {
                    ref: "specs/api.md#^story-a",
                    kind: "EMBEDDED",
                    notePath: "specs/api.md",
                    fragment: "^story-a",
                    nodeId: "story-a",
                  },
                  title: "Story A",
                  structural: true,
                },
              ],
            },
          ],
        }),
      },
    };

    const workspace = nodeWorkspaceFromPublicGraphQL(data, "specs/api.md");

    expect(workspace.relations?.[0]?.items?.[0]).toEqual(
      expect.objectContaining({
        path: "specs/api.md",
        anchor: "^story-a",
        kind: "embedded",
      }),
    );
  });

  it("uses only the selected embedded subtree for structural content", () => {
    const data: PublicNodeDetailData = {
      node: {
        ref: {
          ref: "specs/api.md#^story-a",
          kind: "EMBEDDED",
          notePath: "specs/api.md",
          fragment: "^story-a",
          nodeId: "story-a",
        },
        nodeId: "story-a",
        nodeKind: "EMBEDDED",
        path: "specs/api.md",
        title: "Story A",
        content: "Story content",
        locator: {
          sourceLocator: "specs/api.md#^story-a",
          status: "linkable",
          exists: true,
          requiresFix: false,
        },
        workspace: publicWorkspace({
          fields: [],
          collections: [],
          relationGroups: [],
          structure: [
            {
              ref: {
                ref: "specs/api.md#stories",
                kind: "SECTION",
                notePath: "specs/api.md",
                nodeId: "stories",
              },
              title: "Stories",
            },
            {
              ref: {
                ref: "specs/api.md#^story-a",
                kind: "EMBEDDED",
                notePath: "specs/api.md",
                fragment: "^story-a",
                nodeId: "story-a",
              },
              parentRef: {
                ref: "specs/api.md#stories",
                kind: "SECTION",
                notePath: "specs/api.md",
                nodeId: "stories",
              },
              title: "Story A",
              content: "Story content",
            },
            {
              ref: {
                ref: "specs/api.md#acceptance",
                kind: "SECTION",
                notePath: "specs/api.md",
                nodeId: "acceptance",
              },
              parentRef: {
                ref: "specs/api.md#^story-a",
                kind: "EMBEDDED",
                notePath: "specs/api.md",
                nodeId: "story-a",
              },
              title: "Acceptance",
            },
            {
              ref: {
                ref: "specs/api.md#^story-b",
                kind: "EMBEDDED",
                notePath: "specs/api.md",
                nodeId: "story-b",
              },
              parentRef: {
                ref: "specs/api.md#stories",
                kind: "SECTION",
                notePath: "specs/api.md",
                nodeId: "stories",
              },
              title: "Story B",
            },
          ],
        }),
      },
    };

    const workspace = nodeWorkspaceFromPublicGraphQL(data, "specs/api.md#^story-a");

    expect(workspace.content.structural?.root.nodeId).toBe("story-a");
    expect(workspace.content.structural?.root.children?.map((child) => child.title)).toEqual([
      "Acceptance",
    ]);
  });

  it("builds a canonical BodyWalker graph from the public workspace bodies projection", () => {
    const data: PublicNodeDetailData = {
      node: {
        ref: {
          ref: "specs/api.md",
          kind: "NOTE",
          notePath: "specs/api.md",
          path: "specs/api.md",
          nodeId: "note:specs/api.md",
          typeName: "Spec",
        },
        nodeId: "note:specs/api.md",
        nodeKind: "NOTE",
        path: "specs/api.md",
        title: "Public API",
        resolvedType: "Spec",
        content: "# Public API\n\nRaw fallback content.",
        locator: {
          sourceLocator: "specs/api.md",
          status: "linkable",
          exists: true,
          requiresFix: false,
        },
        localGraph: { nodes: [], edges: [], truncated: false },
        workspace: publicWorkspace({
          fields: [],
          collections: [],
          relationGroups: [],
          bodies: [
            {
              ref: {
                ref: "specs/api.md",
                kind: "NOTE",
                notePath: "specs/api.md",
                path: "specs/api.md",
                nodeId: "note:specs/api.md",
                typeName: "Spec",
              },
              title: "Public API",
              resolvedType: "Spec",
              locator: "FILE",
              markdown: "# Public API\n\nBodyWalker content.",
              fields: [
                {
                  name: "summary",
                  kind: "FRONTMATTER_FIELD",
                  present: true,
                  status: {
                    dirty: false,
                    validation: { issueCount: 0 },
                    freshness: {},
                    session: {},
                    hasWarnings: false,
                  },
                  range: { start: 0, end: 22 },
                  valueRanges: [{ start: 9, end: 22 }],
                  values: ["Composable API"],
                  sectionNodes: [],
                },
                {
                  name: "successor",
                  kind: "FRONTMATTER_FIELD",
                  present: false,
                  status: {
                    dirty: false,
                    validation: { issueCount: 0 },
                    freshness: {},
                    session: {},
                    hasWarnings: false,
                  },
                  range: { start: 0, end: 0 },
                  valueRanges: [],
                  values: null,
                  sectionNodes: [],
                },
              ],
              collections: [
                {
                  name: "stories",
                  kind: "COLLECTION",
                  status: {
                    dirty: false,
                    validation: { issueCount: 0 },
                    freshness: {},
                    session: {},
                    hasWarnings: false,
                  },
                  range: { start: 26, end: 40 },
                  items: [
                    {
                      ref: {
                        ref: "specs/api.md#^story-a",
                        kind: "EMBEDDED",
                        notePath: "specs/api.md",
                        fragment: "^story-a",
                        nodeId: "story-a",
                        typeName: "Story",
                      },
                      range: { start: 26, end: 40 },
                    },
                  ],
                },
              ],
              blocks: [
                {
                  kind: "narrative",
                  range: { start: 0, end: 25 },
                  markdown: "BodyWalker content.",
                  childRefs: [],
                },
                {
                  kind: "child_section",
                  range: { start: 26, end: 40 },
                  fieldName: "story",
                  sectionDisplay: "INLINE",
                  childRef: {
                    ref: "specs/api.md#^story-a",
                    kind: "EMBEDDED",
                    notePath: "specs/api.md",
                    fragment: "^story-a",
                    nodeId: "story-a",
                    typeName: "Story",
                  },
                  childRefs: [],
                },
                {
                  kind: "collection",
                  range: { start: 41, end: 50 },
                  fieldName: "stories",
                  childRefs: [
                    {
                      ref: "specs/api.md#^story-a",
                      kind: "EMBEDDED",
                      notePath: "specs/api.md",
                      fragment: "^story-a",
                      nodeId: "story-a",
                      typeName: "Story",
                    },
                  ],
                },
              ],
            },
            {
              ref: {
                ref: "specs/api.md#^story-a",
                kind: "EMBEDDED",
                notePath: "specs/api.md",
                fragment: "^story-a",
                nodeId: "story-a",
                typeName: "Story",
              },
              parentRef: {
                ref: "specs/api.md",
                kind: "NOTE",
                notePath: "specs/api.md",
                path: "specs/api.md",
                nodeId: "note:specs/api.md",
              },
              title: "Story A",
              resolvedType: "Story",
              locator: "EMBEDDED",
              level: "H3",
              blockId: "story-a",
              markdown: "Nested BodyWalker content.",
              binding: {
                typeName: "Story",
                fieldName: "stories",
                fieldPath: "userStories.stories",
                fieldList: true,
                sectionDisplay: "PANE",
                properties: {
                  id: "STORY-A",
                  status: "planned",
                  summary: "Build the workflow",
                },
                identifierField: "id",
              },
              fields: [],
              collections: [],
              blocks: [
                {
                  kind: "narrative",
                  range: { start: 26, end: 40 },
                  markdown: "Nested BodyWalker content.",
                  childRefs: [],
                },
              ],
            },
          ],
        }),
      },
    };

    const workspace = nodeWorkspaceFromPublicGraphQL(data, "specs/api.md");
    const focused = workspace.nodes?.find((node) => node.id === workspace.focusedNodeId);
    const field = workspace.nodes?.find((node) => node.kind === "field");

    const missingField = workspace.nodes?.find(
      (node) => node.kind === "field" && node.field.name === "successor",
    );

    const collection = workspace.nodes?.find((node) => node.kind === "collection");
    const child = workspace.nodes?.find((node) => node.ref.nodeId === "story-a");

    expect(focused?.ref.nodeId).toBe("note:specs/api.md");
    expect(focused?.id).toBe(workspace.focusedNodeId);
    expect(child?.id).not.toBe(focused?.id);
    expect(workspace.views?.localGraph?.centerNodeIds).toEqual([workspace.focusedNodeId]);
    expect(focused).toEqual(expect.objectContaining({ kind: "note" }));
    expect(focused?.body).toContainEqual(
      expect.objectContaining({
        kind: "narrative",
        markdown: "BodyWalker content.",
      }),
    );
    expect(field).toEqual(
      expect.objectContaining({
        parentId: workspace.focusedNodeId,
        field: expect.objectContaining({
          name: "summary",
          values: ["Composable API"],
        }),
      }),
    );
    expect(missingField).toEqual(
      expect.objectContaining({
        field: expect.objectContaining({ values: [] }),
      }),
    );
    expect(collection).toEqual(
      expect.objectContaining({
        collection: expect.objectContaining({ name: "stories" }),
        childIds: [child?.id],
      }),
    );
    expect(child).toEqual(
      expect.objectContaining({
        kind: "embedded",
        body: [expect.objectContaining({ markdown: "Nested BodyWalker content." })],
        data: expect.objectContaining({
          blockId: "story-a",
          level: "H3",
          binding: expect.objectContaining({
            identifierField: "id",
            properties: expect.objectContaining({ id: "STORY-A" }),
          }),
        }),
      }),
    );
  });

  it("maps relation field links with refs and titles, keeping unresolved values", () => {
    const data: PublicNodeDetailData = {
      node: {
        ref: { ref: "ideas/a.md", kind: "NOTE", notePath: "ideas/a.md", nodeId: "note:ideas/a.md" },
        nodeId: "note:ideas/a.md",
        nodeKind: "NOTE",
        path: "ideas/a.md",
        title: "Idea A",
        locator: {
          ref: {
            ref: "ideas/a.md",
            kind: "NOTE",
            notePath: "ideas/a.md",
            nodeId: "note:ideas/a.md",
          },
          kind: "NOTE",
          sourceLocator: "ideas/a.md",
          status: "ok",
          exists: true,
          requiresFix: false,
          diagnostics: [],
          fixActions: [],
        },
        workspace: publicWorkspace({
          fields: [
            {
              name: "opportunities",
              kind: "FRONTMATTER_FIELD",
              present: true,
              status: {
                dirty: false,
                validation: { issueCount: 0 },
                freshness: {},
                session: {},
                hasWarnings: false,
              },
              range: { start: 0, end: 0 },
              values: ["[[AI]]", "[[ghost|Ghost]]"],
              links: [
                {
                  value: "[[AI]]",
                  resolved: true,
                  ref: {
                    ref: "opportunities/AI.md",
                    kind: "NOTE",
                    notePath: "opportunities/AI.md",
                    nodeId: "note:opportunities/AI.md",
                  },
                  title: "AI in products",
                },
                { value: "[[ghost|Ghost]]", resolved: false, ref: null, title: null },
              ],
              valueRanges: [],
              sectionNodes: [],
            },
          ],
        }),
      },
    };

    const workspace = nodeWorkspaceFromPublicGraphQL(data, "ideas/a.md");

    expect(workspace.fields?.[0].links).toEqual([
      {
        value: "[[AI]]",
        ref: expect.objectContaining({
          notePath: "opportunities/AI.md",
          nodeId: "note:opportunities/AI.md",
        }),
        title: "AI in products",
      },
      { value: "[[ghost|Ghost]]", ref: undefined, title: undefined },
    ]);
  });

  it("normalizes public locator actions and the focused workspace parent", () => {
    const data: PublicNodeDetailData = {
      node: {
        ref: {
          ref: "specs/api.md#Story A",
          kind: "EMBEDDED",
          notePath: "specs/api.md",
          fragment: "Story A",
          nodeId: "story-a",
          structural: "story-fingerprint",
        },
        nodeId: "story-a",
        nodeKind: "EMBEDDED",
        path: "specs/api.md",
        title: "Story A",
        locator: {
          ref: {
            ref: "specs/api.md#Story A",
            kind: "EMBEDDED",
            notePath: "specs/api.md",
            fragment: "Story A",
            nodeId: "story-a",
            structural: "story-fingerprint",
          },
          kind: "EMBEDDED",
          sourceLocator: "specs/api.md#Story A",
          status: "requires_fix",
          exists: false,
          requiresFix: true,
          linkTarget: {
            ref: {
              ref: "specs/api.md#Story A",
              kind: "EMBEDDED",
              notePath: "specs/api.md",
              fragment: "Story A",
              nodeId: "story-a",
              structural: "story-fingerprint",
            },
            wikilink: "[[api#^story-a]]",
            markdown: "specs/api.md#^story-a",
            exists: false,
            requiresFix: true,
            blockId: "story-a",
          },
          diagnostics: [
            {
              code: "missing_block_id",
              message: "A block ID is required.",
              ref: {
                ref: "specs/api.md#Story A",
                kind: "EMBEDDED",
                notePath: "specs/api.md",
                fragment: "Story A",
                nodeId: "story-a",
                structural: "story-fingerprint",
              },
            },
          ],
          fixActions: [
            {
              ref: {
                ref: "specs/api.md#Story A",
                kind: "EMBEDDED",
                notePath: "specs/api.md",
                fragment: "Story A",
                nodeId: "story-a",
                structural: "story-fingerprint",
              },
              blockId: "story-a",
            },
          ],
        },
        workspace: publicWorkspace({
          parentRef: {
            ref: "specs/api.md",
            kind: "NOTE",
            notePath: "specs/api.md",
            nodeId: "note:specs/api.md",
            structural: "note-fingerprint",
          },
          parentTitle: "API spec",
          bodies: [],
          fields: [],
          collections: [],
          relationGroups: [],
          structure: [
            {
              ref: {
                ref: "specs/api.md#Story A",
                kind: "EMBEDDED",
                notePath: "specs/api.md",
                fragment: "Story A",
                nodeId: "story-a",
                structural: "story-fingerprint",
              },
              title: "Story A",
              content: "Story content",
            },
          ],
        }),
      },
    };

    const workspace = nodeWorkspaceFromPublicGraphQL(data, "specs/api.md#Story A");

    expect(workspace.node.parentRef).toEqual(
      expect.objectContaining({
        notePath: "specs/api.md",
        nodeId: "note:specs/api.md",
        structuralFingerprint: "note-fingerprint",
      }),
    );
    expect(workspace.node.parentTitle).toBe("API spec");
    expect(workspace.content.structural?.root.structuralFingerprint).toBe("story-fingerprint");
    expect(workspace.nodeLocator).toEqual(
      expect.objectContaining({
        kind: "EMBEDDED",
        ref: expect.objectContaining({
          nodeId: "story-a",
          structuralFingerprint: "story-fingerprint",
        }),
        linkTarget: expect.objectContaining({
          ref: expect.objectContaining({
            structuralFingerprint: "story-fingerprint",
          }),
        }),
        diagnostics: [
          expect.objectContaining({
            message: "A block ID is required.",
            ref: expect.objectContaining({
              structuralFingerprint: "story-fingerprint",
            }),
          }),
        ],
        fixActions: [
          expect.objectContaining({
            blockId: "story-a",
            ref: expect.objectContaining({
              structuralFingerprint: "story-fingerprint",
            }),
          }),
        ],
      }),
    );
  });

  it("maps bounded public source links into rendered links and note embeds", () => {
    const data: PublicNodeDetailData = {
      node: {
        ref: {
          ref: "specs/api.md",
          kind: "NOTE",
          notePath: "specs/api.md",
        },
        nodeId: "note:specs/api.md",
        nodeKind: "NOTE",
        path: "specs/api.md",
        title: "API",
        content: "# API",
        locator: {
          sourceLocator: "specs/api.md",
          status: "linkable",
          exists: true,
          requiresFix: false,
        },
        workspace: publicWorkspace({
          bodies: [],
          fields: [],
          collections: [],
          relationGroups: [],
          sourceLinks: [
            {
              target: "notes/linked.md",
              authoredTarget: "Linked Note",
              text: "Read the linked note",
              kind: "wikilink",
              anchor: "context",
              embed: false,
              targetKind: "note",
              resolved: true,
              resolvedRef: {
                ref: "notes/linked.md",
                kind: "NOTE",
                notePath: "notes/linked.md",
              },
              title: "Linked note",
            },
            {
              target: "notes/linked.md",
              authoredTarget: "Linked Note",
              text: "Read it again",
              kind: "wikilink",
              embed: false,
              targetKind: "note",
              resolved: true,
              resolvedRef: {
                ref: "notes/linked.md",
                kind: "NOTE",
                notePath: "notes/linked.md",
              },
              title: "Linked note",
            },
            {
              target: "notes/embed.md",
              authoredTarget: "Embedded Note",
              kind: "wikilink",
              embed: true,
              targetKind: "note",
              resolved: true,
              resolvedRef: {
                ref: "notes/embed.md",
                kind: "NOTE",
                notePath: "notes/embed.md",
              },
              title: "Embedded note",
              preview: "A bounded note preview.",
            },
            {
              target: "notes/embed.md",
              authoredTarget: "Embedded Note Again",
              text: "Second embed label",
              kind: "wikilink",
              embed: true,
              targetKind: "note",
              resolved: true,
              resolvedRef: {
                ref: "notes/embed.md",
                kind: "NOTE",
                notePath: "notes/embed.md",
              },
              title: "Repeated embed title",
              preview: "This duplicate should not create another card.",
            },
          ],
        }),
      },
    };

    const rendered = nodeWorkspaceFromPublicGraphQL(data, "specs/api.md").content.rendered;

    expect(rendered?.links).toEqual([
      expect.objectContaining({
        target: "notes/linked.md",
        text: "Linked Note",
        kind: "wikilink",
        anchor: "context",
      }),
      expect.objectContaining({
        target: "notes/linked.md",
        text: "Linked Note",
        kind: "wikilink",
      }),
      expect.objectContaining({
        target: "notes/embed.md",
        text: "Embedded Note",
        kind: "wikilink",
      }),
      expect.objectContaining({
        target: "notes/embed.md",
        text: "Embedded Note Again",
        kind: "wikilink",
      }),
    ]);
    expect(rendered?.embeds).toEqual([
      expect.objectContaining({
        target: "notes/embed.md",
        title: "Embedded note",
        kind: "note",
        resolved: true,
        preview: "A bounded note preview.",
      }),
    ]);
  });

  it("preserves authored targets for aliased and labeled rendered-link navigation", () => {
    const data: PublicNodeDetailData = {
      node: {
        ref: {
          ref: "notes/source.md",
          kind: "NOTE",
          notePath: "notes/source.md",
        },
        nodeId: "note:notes/source.md",
        nodeKind: "NOTE",
        path: "notes/source.md",
        title: "Source",
        content: "[[target-a|First]] then [Second](../target-b.md#Heading)",
        locator: {
          sourceLocator: "notes/source.md",
          status: "linkable",
          exists: true,
          requiresFix: false,
        },
        workspace: publicWorkspace({
          bodies: [],
          fields: [],
          collections: [],
          relationGroups: [],
          sourceLinks: [
            {
              target: "notes/target-a.md",
              authoredTarget: "target-a",
              text: "First",
              kind: "wikilink",
              embed: false,
              targetKind: "note",
              resolved: true,
              resolvedRef: {
                ref: "notes/target-a.md",
                kind: "NOTE",
                notePath: "notes/target-a.md",
              },
            },
            {
              target: "notes/target-b.md#Heading",
              authoredTarget: "../target-b.md#Heading",
              text: "Second",
              kind: "mdlink",
              anchor: "Heading",
              embed: false,
              targetKind: "note",
              resolved: true,
              resolvedRef: {
                ref: "notes/target-b.md#Heading",
                kind: "NOTE",
                notePath: "notes/target-b.md",
                fragment: "Heading",
              },
            },
          ],
        }),
      },
    };

    const rendered = nodeWorkspaceFromPublicGraphQL(data, "notes/source.md").content.rendered;

    expect(rendered?.links).toEqual([
      expect.objectContaining({
        target: "notes/target-a.md",
        text: "target-a",
      }),
      expect.objectContaining({
        target: "notes/target-b.md#Heading",
        text: "../target-b.md#Heading",
      }),
    ]);
    expect(resolveRenderedLinkTarget("rhizome://note/target-a", rendered || null)).toBe(
      "notes/target-a.md",
    );
    expect(resolveRenderedLinkTarget("../target-b.md#Heading", rendered || null)).toBe(
      "notes/target-b.md#Heading",
    );
  });

  it("renders note-backed EMBEDDED nodes with their own content and identity", () => {
    const notePath = "docs/specs/technical/linkable-embedded-node-identifiers.md";

    const storyRef = {
      ref: `${notePath}#^SPEC-0023-US1`,
      kind: "EMBEDDED",
      notePath,
      path: notePath,
      fragment: "^SPEC-0023-US1",
      nodeId: `${notePath}#^SPEC-0023-US1`,
      typeName: "UserStory",
    };

    const parentRef = {
      ref: `${notePath}#User Stories`,
      kind: "SECTION",
      notePath,
      path: notePath,
      nodeId: "user-stories",
      typeName: "StoriesSection",
    };

    const criterionRef = {
      ref: `${notePath}#^SPEC-0023-US1-AC1`,
      kind: "EMBEDDED",
      notePath,
      path: notePath,
      fragment: "^SPEC-0023-US1-AC1",
      nodeId: `${notePath}#^SPEC-0023-US1-AC1`,
      typeName: "AcceptanceCriterion",
    };

    const storyMarkdown =
      "- id:: ^SPEC-0023-US1\n- summary:: Callers can ask for the link target of any surfaced node.\n- status:: ready\n\n#### Acceptance Criteria\n\n- Tools return a link target.";

    const cleanStatus = {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    };

    const data: PublicNodeDetailData = {
      node: {
        ref: storyRef,
        nodeId: `section:${notePath}#${notePath}#^SPEC-0023-US1`,
        nodeKind: "EMBEDDED",
        path: `${notePath}#^SPEC-0023-US1`,
        title: "US1 - Link target for any surfaced node",
        resolvedType: "UserStory",
        content: storyMarkdown,
        locator: {
          sourceLocator: `${notePath}#^SPEC-0023-US1`,
          status: "linkable",
          wikilink: "[[linkable-embedded-node-identifiers#^SPEC-0023-US1]]",
          exists: true,
          requiresFix: false,
        },
        localGraph: { nodes: [], edges: [], truncated: false },
        workspace: {
          parentRef,
          sourceLinks: [],
          fields: [],
          collections: [],
          relationGroups: [],
          bodies: [
            {
              ref: storyRef,
              parentRef,
              title: "US1 - Link target for any surfaced node",
              resolvedType: "UserStory",
              locator: "EMBEDDED",
              level: "H3",
              blockId: "SPEC-0023-US1",
              markdown: storyMarkdown,
              fields: [
                {
                  name: "status",
                  kind: "INLINE_FIELD",
                  present: true,
                  status: cleanStatus,
                  range: { start: 0, end: 18 },
                  valueRanges: [{ start: 10, end: 18 }],
                  values: ["ready"],
                  sectionNodes: [],
                  capability: {
                    ownerRef: storyRef,
                    ownerType: "UserStory",
                    typeName: "UserStoryStatus",
                    valueKind: "enum",
                    list: false,
                    required: true,
                    enumValues: ["draft", "ready"],
                    valueOrigin: "authored",
                    sourceKind: "INLINE",
                    identifier: false,
                    preferredIdentifier: false,
                    displayImportance: "KEY",
                    writeOperation: "setField",
                  },
                  issues: [],
                },
              ],
              collections: [],
              blocks: [
                {
                  kind: "inline_field",
                  childRefs: [],
                  range: { start: 0, end: 22 },
                  fieldName: "id",
                },
                {
                  kind: "inline_field",
                  childRefs: [],
                  range: { start: 23, end: 90 },
                  fieldName: "summary",
                },
                {
                  kind: "inline_field",
                  childRefs: [],
                  range: { start: 91, end: 110 },
                  fieldName: "status",
                },
                {
                  kind: "child_section",
                  range: { start: 111, end: 160 },
                  fieldName: "acceptanceCriteria",
                  sectionDisplay: "INLINE",
                  childRef: criterionRef,
                  childRefs: [],
                },
              ],
            },
            {
              ref: criterionRef,
              parentRef: storyRef,
              title: "Acceptance Criteria",
              resolvedType: "AcceptanceCriterion",
              locator: "EMBEDDED",
              level: "H4",
              blockId: "SPEC-0023-US1-AC1",
              markdown: "- Tools return a link target.",
              fields: [],
              collections: [],
              blocks: [
                {
                  kind: "narrative",
                  range: { start: 111, end: 160 },
                  markdown: "- Tools return a link target.",
                  childRefs: [],
                },
              ],
            },
          ],
          structure: [
            { ref: parentRef, title: "User Stories", level: "H2" },
            {
              ref: storyRef,
              parentRef,
              title: "US1 - Link target for any surfaced node",
              level: "H3",
              content: storyMarkdown,
            },
          ],
          loaded: {
            rendered: true,
            assessment: true,
            structure: true,
            relations: true,
          },
          capabilities: {
            canEdit: true,
            canEditFields: true,
            canEditCollections: true,
            canNavigateChildren: true,
            canSubscribe: true,
          },
          status: cleanStatus,
          version: "v1",
        },
      },
    };

    const workspace = nodeWorkspaceFromPublicGraphQL(data, `${notePath}#^SPEC-0023-US1`);

    expect(workspace.content.rendered).toBeDefined();
    expect(workspace.content.rendered?.title).toBe("US1 - Link target for any surfaced node");
    expect(workspace.content.rendered?.path).toBe(`${notePath}#^SPEC-0023-US1`);
    expect(workspace.content.rendered?.resolvedType).toBe("UserStory");
    expect(workspace.content.rendered?.rendered).toContain("#### Acceptance Criteria");
    expect(workspace.content.rendered?.links).toEqual([]);
    expect(workspace.nodes?.find((node) => node.id === workspace.focusedNodeId)?.ref).toMatchObject(
      {
        notePath,
        nodeId: `${notePath}#^SPEC-0023-US1`,
      },
    );
    expect(workspace.node.parentRef?.nodeId).toBe("user-stories");
    expect(workspace.nodes?.find((node) => node.id === workspace.focusedNodeId)?.body).toHaveLength(
      4,
    );

    const statusField = workspace.nodes?.find(
      (node) => node.kind === "field" && node.field?.name === "status",
    );

    expect(statusField?.field?.capability).toMatchObject({
      ownerType: "UserStory",
      typeName: "UserStoryStatus",
      valueKind: "enum",
      enumValues: ["draft", "ready"],
      displayImportance: "KEY",
    });
  });

  it.each(["CODE_FILE", "CODE_SYMBOL", "MODULE"])(
    "does not synthesize rendered content for %s nodes",
    (nodeKind) => {
      const data: PublicNodeDetailData = {
        node: {
          ref: {
            ref: "app/search.py",
            kind: nodeKind,
            notePath: "app/search.py",
            path: "app/search.py",
            nodeId: `${nodeKind.toLowerCase()}:app/search.py`,
          },
          nodeId: `${nodeKind.toLowerCase()}:app/search.py`,
          nodeKind,
          path: "app/search.py",
          title: "search.py",
          content: "def main():\n    return None\n",
          locator: {
            sourceLocator: "app/search.py",
            status: "linkable",
            exists: true,
            requiresFix: false,
          },
          localGraph: { nodes: [], edges: [], truncated: false },
          workspace: publicWorkspace({}),
        },
      };

      const workspace = nodeWorkspaceFromPublicGraphQL(data, "app/search.py");

      expect(workspace.content.rendered).toBeUndefined();
    },
  );

  it("throws when the public node query does not resolve", () => {
    expect(() => nodeWorkspaceFromPublicGraphQL({ node: null }, "missing.md")).toThrow(
      "Public GraphQL node did not resolve: missing.md",
    );
  });
});
