import { EditorView } from "@codemirror/view";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import { nodeWorkspaceFromPublicGraphQL } from "../api/nodeWorkspaceAdapter";
import type {
  PublicNodeDetailData,
  PublicNodeWorkspaceProjection,
} from "../api/publicGraphQLTypes";
import type { NodeKind, NodeRef, NodeWorkspace, RenderedSection } from "../api/types";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { OntologyNotePane } from "./OntologyNotePane";
import type { NoteTabContext } from "./noteTabContext";

const publicWorkspaceDefaults = {
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
  version: "test-v1",
  structure: [],
  loaded: {
    rendered: true,
    assessment: false,
    structure: false,
    relations: false,
  },
} satisfies Pick<
  PublicNodeWorkspaceProjection,
  "capabilities" | "status" | "version" | "structure" | "loaded"
>;

type WorkspaceFixtureInput = {
  requestedRef?: string;
  focusedNodeId?: string;
  node?: Partial<NodeWorkspace["node"]> & {
    ref?: Partial<NodeWorkspace["node"]["ref"]>;
  };
  content?: Partial<NodeWorkspace["content"]>;
  nodes?: NodeWorkspace["nodes"];
  edges?: NodeWorkspace["edges"];
  views?: NodeWorkspace["views"];
  fields?: NodeWorkspace["fields"];
  collections?: NodeWorkspace["collections"];
  loaded?: Partial<NodeWorkspace["loaded"]>;
  capabilities?: Partial<NodeWorkspace["capabilities"]>;
  status?: Partial<NodeWorkspace["status"]>;
  version?: string;
  path?: string;
  title?: string;
  resolvedType?: string;
  assessment?: NodeWorkspace["content"]["assessment"];
  typeDoc?: NodeWorkspace["content"]["typeDoc"];
  structural?: NodeWorkspace["content"]["structural"];
  relationGroups?: NodeWorkspace["relations"];
  relations?: NodeWorkspace["relations"];
  sectionRelationGroups?: NodeWorkspace["sectionRelationGroups"];
  nodeLocator?: NodeWorkspace["nodeLocator"];
  linkTarget?: NodeWorkspace["linkTarget"];
  linkFixOps?: NodeWorkspace["linkFixOps"];
};

type ContextRef = { current: NoteTabContext | null };

function asWorkspace(workspace: WorkspaceFixtureInput): NodeWorkspace {
  const requestedRef = workspace.requestedRef || workspace.content?.path || workspace.path || "";
  const hashIndex = requestedRef.indexOf("#");

  const notePath =
    workspace.node?.notePath ||
    (hashIndex === -1 ? requestedRef : requestedRef.slice(0, hashIndex));

  const fragment =
    workspace.node?.ref?.fragment ||
    (hashIndex === -1 ? undefined : requestedRef.slice(hashIndex + 1));

  const resolvedType =
    workspace.node?.resolvedType || workspace.content?.resolvedType || workspace.resolvedType;

  const title = workspace.node?.title || workspace.content?.title || workspace.title || "";
  const contentPath = workspace.content?.path || workspace.path || requestedRef;
  const kind: NodeKind = workspace.node?.ref?.kind || (fragment ? "SECTION" : "NOTE");

  return {
    requestedRef,
    focusedNodeId: workspace.focusedNodeId,
    node: {
      ref: {
        notePath,
        fragment,
        nodeId: workspace.node?.ref?.nodeId,
        structuralFingerprint: workspace.node?.ref?.structuralFingerprint,
        kind,
      },
      notePath,
      title,
      resolvedType,
      locator: workspace.node?.locator || (fragment ? "SECTION" : "FILE"),
      nodeLocator: workspace.node?.nodeLocator || workspace.nodeLocator,
      parentRef: workspace.node?.parentRef,
    },
    content: {
      path: contentPath,
      title,
      resolvedType,
      markdown: workspace.content?.markdown,
      format: workspace.content?.format,
      sourceCapabilities: workspace.content?.sourceCapabilities,
      rendered: workspace.content?.rendered,
      assessment: workspace.content?.assessment || workspace.assessment,
      typeDoc: workspace.content?.typeDoc || workspace.typeDoc,
      structural: workspace.content?.structural || workspace.structural || null,
    },
    nodes: workspace.nodes,
    edges: workspace.edges,
    views: workspace.views,
    fields: workspace.fields || [],
    collections: workspace.collections || [],
    relations: workspace.relations || workspace.relationGroups || [],
    sectionRelationGroups: workspace.sectionRelationGroups,
    loaded: {
      rendered: workspace.loaded?.rendered ?? Boolean(workspace.content?.rendered),
      assessment:
        workspace.loaded?.assessment ??
        Boolean(workspace.content?.assessment || workspace.assessment),
      structure:
        workspace.loaded?.structure ??
        Boolean(workspace.content?.structural || workspace.structural),
      relations:
        workspace.loaded?.relations ??
        Boolean(
          (workspace.relations && workspace.relations.length > 0) ||
          (workspace.relationGroups && workspace.relationGroups.length > 0) ||
          workspace.sectionRelationGroups,
        ),
    },
    capabilities: {
      canEdit: workspace.capabilities?.canEdit ?? true,
      canEditFields: workspace.capabilities?.canEditFields ?? true,
      canEditCollections: workspace.capabilities?.canEditCollections ?? true,
      canNavigateChildren: workspace.capabilities?.canNavigateChildren ?? true,
      canSubscribe: workspace.capabilities?.canSubscribe ?? true,
    },
    status: {
      dirty: workspace.status?.dirty ?? false,
      validation: workspace.status?.validation || { issueCount: 0 },
      freshness: workspace.status?.freshness || {},
      session: workspace.status?.session || {},
      hasWarnings: workspace.status?.hasWarnings ?? false,
    },
    version: workspace.version || requestedRef || "fixture",
    nodeLocator: workspace.nodeLocator,
    linkTarget: workspace.linkTarget,
    linkFixOps: workspace.linkFixOps,
  };
}

function outlineFromPane(sections: RenderedSection[], notePath: string, title: string) {
  const contextRef: ContextRef = { current: null };

  const rendered = {
    path: notePath,
    title,
    rendered: `# ${title}`,
    links: [],
    embeds: [],
    sections,
  };

  render(
    <OntologyNotePane
      rendered={rendered}
      workspace={asWorkspace({
        requestedRef: notePath,
        content: { path: notePath, title, rendered },
      })}
      onOpen={vi.fn()}
      onContextChange={(next) => {
        contextRef.current = next;
      }}
    />,
  );

  if (!contextRef.current) throw new Error("OntologyNotePane did not publish outline context");

  return contextRef.current.outline.sections;
}

describe("OntologyNotePane", () => {
  beforeAll(() => {
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
    window.HTMLElement.prototype.scrollIntoView = vi.fn();
  });

  it("focuses a UTF-8 byte range on the correct CRLF source line", async () => {
    render(
      <OntologyNotePane
        workspace={asWorkspace({
          requestedRef: "notes/source.md",
          content: {
            markdown: "é\r\nsecond\nthird",
            rendered: {
              path: "notes/source.md",
              title: "Source",
              rendered: "é\r\nsecond\nthird",
              links: [],
              embeds: [],
              sections: [],
            },
          },
        })}
        onOpen={vi.fn()}
        viewMode="source"
        issueTarget={{ unit: "utf8_bytes", start: 4, end: 10 }}
      />,
    );

    const contentDOM = await screen.findByRole<HTMLElement>("textbox", { name: "Note source" });
    const view = EditorView.findFromDOM(contentDOM);

    if (!view) throw new Error("Expected a CodeMirror source editor");
    await waitFor(() => expect(view.state.selection.main.head).toBe(view.state.doc.line(2).from));
    expect(contentDOM).toHaveFocus();
  });

  it("keeps raw source line spans for HTML notes", () => {
    render(
      <OntologyNotePane
        workspace={asWorkspace({
          requestedRef: "notes/source.html",
          content: {
            format: "html",
            markdown: "<main>\n  <h1>Title</h1>\n</main>",
            rendered: {
              path: "notes/source.html",
              title: "Source",
              rendered: "<main>\n  <h1>Title</h1>\n</main>",
              links: [],
              embeds: [],
              sections: [],
            },
          },
        })}
        onOpen={vi.fn()}
        viewMode="source"
      />,
    );

    const source = screen.getByLabelText("Note source");
    expect(screen.getByRole("button", { name: "Preview" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Source" })).toHaveAttribute("aria-pressed", "true");
    expect(source).toHaveTextContent("<h1>Title</h1>");
    expect(source.querySelector('[data-source-line="2"]')).toBeInTheDocument();
  });

  it("projects the current note wrapper while preserving embedded outline identities", () => {
    const notePath = "docs/specs/technical/linkable-embedded-node-identifiers.md";
    const identityID = `${notePath}#Linkable embedded node identifiers`;
    const userStoriesID = `${notePath}#User Stories`;
    const storyID = `${notePath}#^SPEC-0023-US1`;
    const acceptanceID = `${notePath}#^SPEC-0023-US1-AC1`;
    const overviewID = `${notePath}#Overview`;
    const duplicateID = `${notePath}#^duplicate-title`;

    const sections: RenderedSection[] = [
      {
        id: `note:${notePath}`,
        title: "Linkable embedded node identifiers",
        level: "H1",
        locator: "FILE",
        notePath,
        children: [
          {
            id: overviewID,
            title: "Overview",
            level: "H1",
            notePath,
          },
          {
            id: identityID,
            title: "Linkable embedded node identifiers",
            level: "H1",
            notePath,
            children: [
              {
                id: userStoriesID,
                title: "User Stories",
                level: "H2",
                notePath,
                children: [
                  {
                    id: storyID,
                    title: "As a user, I can link to an embedded node",
                    level: "H3",
                    locator: "EMBEDDED",
                    notePath,
                    children: [
                      {
                        id: acceptanceID,
                        title: "Acceptance criteria",
                        level: "H4",
                        locator: "EMBEDDED",
                        notePath,
                      },
                    ],
                  },
                ],
              },
            ],
          },
          {
            id: duplicateID,
            title: "Linkable embedded node identifiers",
            level: "H1",
            notePath,
          },
        ],
      },
    ];

    const projected = outlineFromPane(sections, notePath, "Linkable embedded node identifiers");

    expect(projected.map((section) => section.id)).toEqual([
      overviewID,
      userStoriesID,
      duplicateID,
    ]);
    expect(projected[1]?.title).toBe("User Stories");
    expect(projected[1]?.children?.[0]?.id).toBe(storyID);
    expect(projected[1]?.children?.[0]?.children?.[0]?.id).toBe(acceptanceID);
    expect(projected[2]?.id).toBe(duplicateID);
  });

  it("projects a structural NOTE root used as the current note identity", () => {
    const notePath = "notes/specs/search-rewrite.md";

    const requirements = {
      id: `${notePath}#requirements-section`,
      title: "Requirements",
      level: "H2",
      locator: "SECTION",
      notePath,
    } satisfies RenderedSection;

    const sections: RenderedSection[] = [
      {
        id: `note:${notePath}`,
        title: "Search Rewrite",
        level: "H2",
        locator: "NOTE",
        notePath,
        children: [requirements],
      },
    ];

    expect(outlineFromPane(sections, notePath, "Search Rewrite")).toEqual([requirements]);
  });

  it("promotes an identity H1 whose Markdown renders as the plain note title", () => {
    const notePath = "Log/sync.md";

    const agenda = {
      id: `${notePath}#agenda`,
      title: "Agenda",
      level: "H2",
      notePath,
    } satisfies RenderedSection;

    const sections: RenderedSection[] = [
      {
        id: `${notePath}#tech-lead-sync-2022-05-12`,
        title: "[[Tech Lead Sync]] 2022-05-12",
        level: "H1",
        notePath,
        children: [agenda],
      },
    ];

    expect(outlineFromPane(sections, notePath, "Tech Lead Sync 2022-05-12")).toEqual([agenda]);
  });

  it("renders a public GraphQL body projection through BodyWalker and its property panel", () => {
    // SAFETY: this fixture intentionally supplies only the public node projection
    // consumed by nodeWorkspaceFromPublicGraphQL; every supplied field follows that schema.
    const publicData = {
      node: {
        ref: {
          ref: "specs/public.md",
          kind: "NOTE",
          notePath: "specs/public.md",
          path: "specs/public.md",
          nodeId: "note:specs/public.md",
          typeName: "Spec",
        },
        nodeId: "note:specs/public.md",
        nodeKind: "NOTE",
        path: "specs/public.md",
        title: "Public spec",
        resolvedType: "Spec",
        content: "# Public spec\n\nRaw markdown fallback.",
        locator: {
          sourceLocator: "specs/public.md",
          status: "linkable",
          exists: true,
          requiresFix: false,
        },
        localGraph: { nodes: [], edges: [], truncated: false },
        workspace: {
          ...publicWorkspaceDefaults,
          fields: [],
          collections: [],
          relationGroups: [],
          bodies: [
            {
              ref: {
                ref: "specs/public.md",
                kind: "NOTE",
                notePath: "specs/public.md",
                path: "specs/public.md",
                nodeId: "note:specs/public.md",
                typeName: "Spec",
              },
              title: "Public spec",
              resolvedType: "Spec",
              locator: "FILE",
              markdown: "BodyWalker narrative.",
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
                  values: ["Public summary"],
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
                  range: { start: 22, end: 40 },
                  items: [
                    {
                      ref: {
                        ref: "specs/public.md#^story-1",
                        kind: "EMBEDDED",
                        notePath: "specs/public.md",
                        fragment: "^story-1",
                        nodeId: "story-1",
                        typeName: "Story",
                      },
                      range: { start: 22, end: 40 },
                    },
                  ],
                },
              ],
              blocks: [
                {
                  kind: "narrative",
                  range: { start: 0, end: 21 },
                  markdown: "BodyWalker narrative.",
                },
                {
                  kind: "collection",
                  range: { start: 22, end: 40 },
                  fieldName: "stories",
                  childRefs: [
                    {
                      ref: "specs/public.md#^story-1",
                      kind: "EMBEDDED",
                      notePath: "specs/public.md",
                      fragment: "^story-1",
                      nodeId: "story-1",
                      typeName: "Story",
                    },
                  ],
                },
              ],
            },
            {
              ref: {
                ref: "specs/public.md#^story-1",
                kind: "EMBEDDED",
                notePath: "specs/public.md",
                fragment: "^story-1",
                nodeId: "story-1",
                typeName: "Story",
              },
              parentRef: {
                ref: "specs/public.md",
                kind: "NOTE",
                notePath: "specs/public.md",
                path: "specs/public.md",
                nodeId: "note:specs/public.md",
              },
              title: "Public story",
              resolvedType: "Story",
              locator: "EMBEDDED",
              markdown: "",
              binding: {
                typeName: "Story",
                fieldName: "stories",
                fieldPath: "stories",
                fieldList: true,
                properties: {
                  id: "STORY-1",
                  status: "planned",
                  summary: "Collection identity survives.",
                },
              },
              fields: [],
              collections: [],
              blocks: [],
            },
          ],
        },
      },
    } as PublicNodeDetailData;

    render(
      <OntologyNotePane
        workspace={nodeWorkspaceFromPublicGraphQL(publicData, "specs/public.md")}
        onOpen={vi.fn()}
      />,
    );

    expect(document.querySelector(".body-narrative")).not.toBeNull();
    expect(screen.getByText("BodyWalker narrative.")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Properties" })).toBeInTheDocument();
    expect(screen.getAllByText("Public summary")).not.toHaveLength(0);
    expect(screen.getByText("Public story")).toBeInTheDocument();
    expect(screen.getByText("STORY-1")).toBeInTheDocument();
    expect(screen.getByText("planned")).toBeInTheDocument();
    expect(screen.getByText("Collection identity survives.")).toBeInTheDocument();
    expect(screen.queryByText("Raw markdown fallback.")).not.toBeInTheDocument();
  });

  it("stages public locator fixes and restores the focused node parent without legacy linkFixOps", async () => {
    const onOpen = vi.fn();
    const onStageOps = vi.fn().mockResolvedValue(undefined);

    // SAFETY: this fixture intentionally supplies only the public node projection
    // consumed by nodeWorkspaceFromPublicGraphQL; every supplied field follows that schema.
    const publicData = {
      node: {
        ref: {
          ref: "specs/api.md#Story A",
          kind: "SECTION",
          notePath: "specs/api.md",
          path: "specs/api.md",
          fragment: "Story A",
          nodeId: "story-a",
          structural: "story-fingerprint",
        },
        nodeId: "story-a",
        nodeKind: "SECTION",
        path: "specs/api.md",
        title: "Story A",
        content: "## Story A",
        locator: {
          ref: {
            ref: "specs/api.md#Story A",
            kind: "SECTION",
            notePath: "specs/api.md",
            fragment: "Story A",
            nodeId: "story-a",
            structural: "story-fingerprint",
          },
          kind: "SECTION",
          sourceLocator: "specs/api.md#Story A",
          status: "requires_fix",
          exists: false,
          requiresFix: true,
          linkTarget: {
            ref: {
              ref: "specs/api.md#Story A",
              kind: "SECTION",
              notePath: "specs/api.md",
              fragment: "Story A",
              nodeId: "story-a",
              structural: "story-fingerprint",
            },
            exists: false,
            requiresFix: true,
            blockId: "story-a",
          },
          diagnostics: [
            {
              code: "missing_block_id",
              message: "A block ID is required.",
            },
          ],
          fixActions: [
            {
              ref: {
                ref: "specs/api.md#Story A",
                kind: "SECTION",
                notePath: "specs/api.md",
                fragment: "Story A",
                nodeId: "story-a",
                structural: "story-fingerprint",
              },
              blockId: "story-a",
            },
          ],
        },
        workspace: {
          ...publicWorkspaceDefaults,
          parentRef: {
            ref: "specs/api.md",
            kind: "NOTE",
            notePath: "specs/api.md",
            nodeId: "note:specs/api.md",
          },
          bodies: [],
          fields: [],
          collections: [],
          relationGroups: [],
        },
      },
    } as PublicNodeDetailData;

    const workspace = nodeWorkspaceFromPublicGraphQL(publicData, "specs/api.md#Story A");

    expect(workspace.linkFixOps).toBeUndefined();
    renderWithQueryClient(
      <OntologyNotePane workspace={workspace} onOpen={onOpen} onStageOps={onStageOps} />,
    );

    expect(screen.getByRole("heading", { name: "Properties" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "specs/api.md" }));
    expect(onOpen).toHaveBeenCalledWith("specs/api.md", "stack");

    fireEvent.click(screen.getByRole("button", { name: "Make linkable" }));
    expect(onStageOps).toHaveBeenCalledWith([
      {
        kind: "ensureBlockID",
        path: "specs/api.md",
        nodeId: "story-a",
        structuralFingerprint: "story-fingerprint",
        blockId: "story-a",
      },
    ]);
    expect(await screen.findByText("Block ID fix staged.")).toBeInTheDocument();
  });

  it("renders and opens bounded public GraphQL inline note previews", () => {
    const onOpen = vi.fn();

    // SAFETY: this fixture intentionally supplies only the public node projection
    // consumed by nodeWorkspaceFromPublicGraphQL; every supplied field follows that schema.
    const publicData = {
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
        workspace: {
          ...publicWorkspaceDefaults,
          bodies: [],
          fields: [],
          collections: [],
          relationGroups: [],
          sourceLinks: [
            {
              target: "notes/embed.md",
              authoredTarget: "Embedded note",
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
              preview: "A bounded preview.",
            },
          ],
        },
      },
    } as PublicNodeDetailData;

    render(
      <OntologyNotePane
        workspace={nodeWorkspaceFromPublicGraphQL(publicData, "specs/api.md")}
        onOpen={onOpen}
      />,
    );

    expect(screen.getByRole("heading", { name: "Inline previews" })).toBeInTheDocument();
    expect(screen.getByText("Embedded note")).toBeInTheDocument();
    expect(screen.getByText("A bounded preview.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open" }));
    expect(onOpen).toHaveBeenCalledWith("notes/embed.md", "stack");
  });

  it("routes aliased wikilinks and labeled Markdown links from public source links through BodyWalker", () => {
    const onOpen = vi.fn();

    // SAFETY: this fixture intentionally supplies only the public node projection
    // consumed by nodeWorkspaceFromPublicGraphQL; every supplied field follows that schema.
    const publicData = {
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
        title: "API",
        resolvedType: "Spec",
        content: "# API",
        locator: {
          sourceLocator: "specs/api.md",
          status: "linkable",
          exists: true,
          requiresFix: false,
        },
        workspace: {
          ...publicWorkspaceDefaults,
          fields: [],
          collections: [],
          relationGroups: [],
          sourceLinks: [
            {
              target: "docs/aliased-target.md",
              authoredTarget: "API Alias",
              text: "Aliased API",
              kind: "wikilink",
              embed: false,
              targetKind: "note",
              resolved: true,
              resolvedRef: {
                ref: "docs/aliased-target.md",
                kind: "NOTE",
                notePath: "docs/aliased-target.md",
              },
            },
            {
              target: "docs/markdown-target.md",
              authoredTarget: "../docs/markdown-target.md",
              text: "Markdown label",
              kind: "mdlink",
              embed: false,
              targetKind: "note",
              resolved: true,
              resolvedRef: {
                ref: "docs/markdown-target.md",
                kind: "NOTE",
                notePath: "docs/markdown-target.md",
              },
            },
          ],
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
              title: "API",
              resolvedType: "Spec",
              locator: "FILE",
              fields: [],
              collections: [],
              blocks: [
                {
                  kind: "narrative",
                  range: { start: 0, end: 64 },
                  markdown:
                    "[[API Alias|Aliased API]]\n\n[Markdown label](../docs/markdown-target.md)",
                  childRefs: [],
                },
              ],
            },
          ],
        },
      },
    } as PublicNodeDetailData;

    render(
      <OntologyNotePane
        workspace={nodeWorkspaceFromPublicGraphQL(publicData, "specs/api.md")}
        onOpen={onOpen}
      />,
    );

    const alias = screen.getByRole("link", { name: "Aliased API" });
    expect(alias).toHaveAttribute("href", "/notes?note=docs%2Faliased-target.md");
    fireEvent.click(alias);
    expect(onOpen).toHaveBeenCalledWith("docs/aliased-target.md", "stack");

    const markdown = screen.getByRole("link", { name: "Markdown label" });
    expect(markdown).toHaveAttribute("href", "/notes?note=docs%2Fmarkdown-target.md");
    fireEvent.click(markdown);
    expect(onOpen).toHaveBeenCalledWith("docs/markdown-target.md", "stack");
  });

  it("copies the focused node wikilink when the locator is linkable", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });

    const workspace = asWorkspace({
      requestedRef: "specs/spec.md#^story-a",
      title: "Story A",
      content: {
        path: "specs/spec.md#^story-a",
        title: "Story A",
        rendered: {
          path: "specs/spec.md",
          title: "Spec",
          rendered: "### Story A\n^story-a",
          links: [],
          embeds: [],
          sections: [],
        },
      },
      nodeLocator: {
        ref: {
          notePath: "specs/spec.md",
          fragment: "^story-a",
          kind: "EMBEDDED",
        },
        kind: "EMBEDDED",
        sourceLocator: "specs/spec.md#^story-a",
        status: "linkable",
        linkTarget: {
          ref: {
            notePath: "specs/spec.md",
            fragment: "^story-a",
            kind: "EMBEDDED",
          },
          wikilink: "[[spec#^story-a]]",
          markdown: "specs/spec.md#^story-a",
          exists: true,
          requiresFix: false,
        },
      },
    });

    render(<OntologyNotePane workspace={workspace} onOpen={vi.fn()} />);

    fireEvent.click(screen.getByRole("button", { name: "Copy link" }));

    expect(writeText).toHaveBeenCalledWith("[[spec#^story-a]]");
    expect(await screen.findByText("Copied link.")).toBeInTheDocument();
  });

  it("stages the block-id fix instead of copying a fragile locator", async () => {
    const onStageOps = vi.fn().mockResolvedValue(undefined);
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });

    const workspace = asWorkspace({
      requestedRef: "specs/spec.md#Story A",
      title: "Story A",
      content: {
        path: "specs/spec.md#Story A",
        title: "Story A",
        rendered: {
          path: "specs/spec.md",
          title: "Spec",
          rendered: "### Story A",
          links: [],
          embeds: [],
          sections: [],
        },
      },
      nodeLocator: {
        ref: {
          notePath: "specs/spec.md",
          fragment: "Story A",
          nodeId: "specs/spec.md#Story A",
          kind: "EMBEDDED",
        },
        kind: "EMBEDDED",
        sourceLocator: "specs/spec.md#Story A",
        status: "requires_fix",
        linkTarget: {
          ref: {
            notePath: "specs/spec.md",
            fragment: "Story A",
            nodeId: "specs/spec.md#Story A",
            kind: "EMBEDDED",
          },
          wikilink: "[[spec#^story-a]]",
          markdown: "specs/spec.md#^story-a",
          exists: false,
          requiresFix: true,
          blockId: "story-a",
        },
        fixActions: [
          {
            ref: {
              notePath: "specs/spec.md",
              fragment: "Story A",
              nodeId: "specs/spec.md#Story A",
              kind: "EMBEDDED",
            },
            blockId: "story-a",
          },
        ],
      },
      linkFixOps: [
        {
          kind: "ensureBlockID",
          path: "specs/spec.md",
          nodeId: "specs/spec.md#Story A",
          blockId: "story-a",
        },
      ],
    });

    render(<OntologyNotePane workspace={workspace} onOpen={vi.fn()} onStageOps={onStageOps} />);

    fireEvent.click(screen.getByRole("button", { name: "Make linkable" }));

    expect(onStageOps).toHaveBeenCalledWith([
      {
        kind: "ensureBlockID",
        path: "specs/spec.md",
        nodeId: "specs/spec.md#Story A",
        blockId: "story-a",
      },
    ]);
    expect(writeText).not.toHaveBeenCalled();
    expect(await screen.findByText("Block ID fix staged.")).toBeInTheDocument();
  });

  it("preserves preface markdown when a later section is typed", () => {
    render(
      <OntologyNotePane
        rendered={{
          path: "notes/mixed.md",
          title: "Mixed note",
          rendered: "Preface paragraph.\n\n## Untyped\n\nBody.\n\n## Typed\n\nTyped body.",
          links: [],
          embeds: [],
          sections: [
            {
              id: "sec-1",
              title: "Untyped",
              level: "H2",
              content: "## Untyped\n\nBody.",
              children: [],
            },
            {
              id: "sec-2",
              title: "Typed",
              level: "H2",
              content: "## Typed\n\nTyped body.",
              typeName: "Story",
              previewTemplate: "Typed",
              children: [],
            },
          ],
        }}
        workspace={null}
        onOpen={vi.fn()}
      />,
    );

    expect(screen.getByText("Preface paragraph.")).toBeInTheDocument();
    expect(screen.getByText("Typed body.")).toBeInTheDocument();
  });

  it("routes preview-row note links through onOpen", () => {
    const onOpen = vi.fn();
    render(
      <OntologyNotePane
        rendered={{
          path: "notes/typed.md",
          title: "Typed note",
          rendered: "## Typed\n\nBody.",
          links: [
            {
              target: "notes/linked-note.md",
              text: "Linked Note",
              kind: "wikilink",
            },
          ],
          embeds: [],
          sections: [
            {
              id: "sec-1",
              title: "Typed",
              level: "H2",
              content: "## Typed\n\nBody.",
              typeName: "Story",
              previewTemplate: "[[Linked Note]]",
              children: [],
            },
          ],
        }}
        workspace={null}
        onOpen={onOpen}
      />,
    );

    const link = screen.getByRole("link", { name: "Linked Note" });
    expect(link).toHaveAttribute("href", "/notes?note=notes%2Flinked-note.md");
    fireEvent.click(link);
    expect(onOpen).toHaveBeenCalledWith("notes/linked-note.md", "stack");
  });

  it("opens markdown note links in a new pane by default", () => {
    const onOpen = vi.fn();
    render(
      <OntologyNotePane
        rendered={{
          path: "notes/linked.md",
          title: "Linked note",
          rendered: "[[Target note]]",
          links: [
            {
              target: "notes/target.md",
              text: "Target note",
              kind: "wikilink",
            },
          ],
          embeds: [],
          sections: [],
        }}
        workspace={null}
        onOpen={onOpen}
      />,
    );

    const link = screen.getByRole("link", { name: "Target note" });
    expect(link).toHaveAttribute("href", "/notes?note=notes%2Ftarget.md");
    fireEvent.click(link);
    expect(onOpen).toHaveBeenCalledWith("notes/target.md", "stack");
  });

  it("keeps server field-node values canonical while preserving dirty indicators", () => {
    render(
      <OntologyNotePane
        rendered={{
          path: "notes/spec.md",
          title: "Spec",
          rendered: "# Spec\n\nBody.",
          links: [],
          embeds: [],
          sections: [],
        }}
        workspace={asWorkspace({
          requestedRef: "notes/spec.md",
          focusedNodeId: "node:spec",
          node: {
            ref: {
              notePath: "notes/spec.md",
              nodeId: "node:spec",
              kind: "NOTE",
            },
            notePath: "notes/spec.md",
            title: "Spec",
            resolvedType: "Spec",
            locator: "NOTE",
          },
          content: {
            path: "notes/spec.md",
            title: "Spec",
            resolvedType: "Spec",
            rendered: {
              path: "notes/spec.md",
              title: "Spec",
              rendered: "# Spec\n\nBody.",
              links: [],
              embeds: [],
              sections: [],
            },
          },
          nodes: [
            {
              id: "node:spec",
              kind: "note",
              ref: {
                notePath: "notes/spec.md",
                nodeId: "node:spec",
                kind: "NOTE",
              },
              notePath: "notes/spec.md",
              data: {
                title: "Spec",
                resolvedType: "Spec",
              },
              status: {
                dirty: false,
                validation: { issueCount: 0 },
                freshness: {},
                session: {},
                hasWarnings: false,
              },
              capabilities: {
                canEdit: true,
                canEditFields: true,
                canEditCollections: true,
                canNavigateChildren: true,
                canSubscribe: true,
              },
            },
            {
              id: "field:status",
              kind: "field",
              parentId: "node:spec",
              ref: {
                notePath: "notes/spec.md",
                nodeId: "node:spec",
                kind: "NOTE",
              },
              notePath: "notes/spec.md",
              field: {
                name: "specStatus",
                valueKind: "enum",
                typeName: "SpecStatus",
                enumValues: ["server-ready", "staged-done"],
                present: true,
                values: ["server-ready"],
                range: { start: 0, end: 10 },
              },
              status: {
                dirty: false,
                validation: { issueCount: 0 },
                freshness: {},
                session: {},
                hasWarnings: false,
              },
              capabilities: {
                canEdit: true,
                canEditFields: true,
                canEditCollections: true,
                canNavigateChildren: true,
                canSubscribe: true,
              },
            },
          ],
        })}
        editSession={{
          sessionId: "sess-1",
          status: "dirty",
          hasUncommittedChanges: true,
          createdAt: "2026-05-06T00:00:00Z",
          updatedAt: "2026-05-06T00:00:01Z",
          ops: [
            {
              kind: "setField",
              path: "notes/spec.md",
              nodeId: "node:spec",
              field: "specStatus",
              value: "staged-done",
            },
          ],
          changedFieldsByNodeRef: {
            "notes/spec.md||node:spec|NOTE|": ["specStatus"],
          },
        }}
        onOpen={vi.fn()}
      />,
    );

    expect(screen.getAllByText("server-ready").length).toBeGreaterThan(0);
    expect(screen.queryByText("staged-done")).toBeNull();
    expect(screen.getByText("Staged")).toBeInTheDocument();
  });

  it("keys duplicate field names by field-node identity", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});

    const status = {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    };

    const capabilities = {
      canEdit: true,
      canEditFields: true,
      canEditCollections: true,
      canNavigateChildren: true,
      canSubscribe: true,
    };

    try {
      render(
        <OntologyNotePane
          rendered={{
            path: "notes/spec.md",
            title: "Spec",
            rendered: "# Spec\n\nBody.",
            links: [],
            embeds: [],
            sections: [],
          }}
          workspace={asWorkspace({
            requestedRef: "notes/spec.md",
            focusedNodeId: "node:spec",
            node: {
              ref: {
                notePath: "notes/spec.md",
                nodeId: "node:spec",
                kind: "NOTE",
              },
              notePath: "notes/spec.md",
              title: "Spec",
              resolvedType: "Spec",
              locator: "NOTE",
            },
            content: {
              path: "notes/spec.md",
              title: "Spec",
              resolvedType: "Spec",
              rendered: {
                path: "notes/spec.md",
                title: "Spec",
                rendered: "# Spec\n\nBody.",
                links: [],
                embeds: [],
                sections: [],
              },
            },
            nodes: [
              {
                id: "node:spec",
                kind: "note",
                ref: {
                  notePath: "notes/spec.md",
                  nodeId: "node:spec",
                  kind: "NOTE",
                },
                notePath: "notes/spec.md",
                data: {
                  title: "Spec",
                  resolvedType: "Spec",
                },
                status,
                capabilities,
              },
              {
                id: "field:locator:frontmatter",
                kind: "field",
                parentId: "node:spec",
                ref: {
                  notePath: "notes/spec.md",
                  nodeId: "node:spec",
                  kind: "NOTE",
                },
                notePath: "notes/spec.md",
                field: {
                  name: "locator",
                  valueKind: "scalar",
                  present: true,
                  values: ["frontmatter"],
                  range: { start: 1, end: 2 },
                },
                status,
                capabilities,
              },
              {
                id: "field:locator:embedded",
                kind: "field",
                parentId: "node:spec",
                ref: {
                  notePath: "notes/spec.md",
                  nodeId: "node:spec",
                  kind: "NOTE",
                },
                notePath: "notes/spec.md",
                field: {
                  name: "locator",
                  valueKind: "scalar",
                  present: true,
                  values: ["embedded"],
                  range: { start: 3, end: 4 },
                },
                status,
                capabilities,
              },
            ],
          })}
          onOpen={vi.fn()}
        />,
      );

      expect(screen.getAllByText("Locator").length).toBeGreaterThanOrEqual(2);
      expect(screen.getByText("^frontmatter")).toBeVisible();
      expect(screen.getByText("^embedded")).toBeVisible();
      expect(
        consoleError.mock.calls.some((call) =>
          call.join(" ").includes("Encountered two children with the same key"),
        ),
      ).toBe(false);
    } finally {
      consoleError.mockRestore();
    }
  });

  it("does not show dirty session status after a clean save with touched metadata", () => {
    const pane = (status: "dirty" | "clean") => (
      <OntologyNotePane
        rendered={{
          path: "notes/spec.md",
          title: "Spec",
          rendered: "# Spec\n\nBody.",
          links: [],
          embeds: [],
          sections: [],
        }}
        workspace={asWorkspace({
          requestedRef: "notes/spec.md",
          node: {
            ref: { notePath: "notes/spec.md", kind: "NOTE" },
            notePath: "notes/spec.md",
            title: "Spec",
            resolvedType: "Spec",
            locator: "NOTE",
          },
          content: {
            path: "notes/spec.md",
            title: "Spec",
            resolvedType: "Spec",
            rendered: {
              path: "notes/spec.md",
              title: "Spec",
              rendered: "# Spec\n\nBody.",
              links: [],
              embeds: [],
              sections: [],
            },
          },
          fields: [],
          collections: [],
          relations: [],
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
          version: "v1",
        })}
        editSession={{
          sessionId: "session-1",
          status,
          touchedPaths: ["notes/spec.md"],
          touchedNodes: ["notes/spec.md"],
          touchedNodeRefs: [{ notePath: "notes/spec.md", kind: "NOTE" }],
          hasUncommittedChanges: status === "dirty",
          createdAt: "2026-04-13T00:00:00Z",
          updatedAt: "2026-04-13T00:00:01Z",
        }}
        onOpen={vi.fn()}
      />
    );

    const { rerender } = render(pane("dirty"));
    expect(screen.getByText("Staged", { selector: ".ontology-identity__chip" })).toBeVisible();
    rerender(pane("clean"));
    expect(
      screen.queryByText("Staged", { selector: ".ontology-identity__chip" }),
    ).not.toBeInTheDocument();
  });

  // Graph-only omits body blocks so selectWorkspaceStructuralView renders through
  // StructuralNodeView; the BodyWalker case is a separate path.
  it.each([
    { path: "dedicated structural tree", useDedicatedTree: true, useBody: false },
    { path: "graph-only structural view", useDedicatedTree: false, useBody: false },
    { path: "BodyWalker body blocks", useDedicatedTree: false, useBody: true },
  ])(
    "does not duplicate section bodies in graph-backed structural mode ($path)",
    ({ path, useDedicatedTree, useBody }) => {
      const focusedNodeId = "note|specs/demo/spec.md|||NOTE|";

      const summaryNodeId =
        "section|specs/demo/spec.md|Summary|specs/demo/spec.md#Summary|SECTION|";

      render(
        <OntologyNotePane
          onOpen={() => {}}
          workspace={asWorkspace({
            requestedRef: "specs/demo/spec.md",
            focusedNodeId,
            content: {
              path: "specs/demo/spec.md",
              title: "Demo spec",
              resolvedType: "Spec",
              rendered: {
                path: "specs/demo/spec.md",
                title: "Demo spec",
                resolvedType: "Spec",
                rendered: "# Demo spec\n\nLoose intro.\n\n## Summary\n\nBody.",
                links: [],
                embeds: [],
                sections: [],
              },
              structural: useDedicatedTree
                ? {
                    defaultView: "structural",
                    root: {
                      nodeId: "specs/demo/spec.md",
                      locator: "FILE",
                      title: "Demo spec",
                      notePath: "specs/demo/spec.md",
                      content: "Loose intro.",
                      children: [
                        {
                          nodeId: "specs/demo/spec.md#Summary",
                          locator: "SECTION",
                          title: "Summary",
                          notePath: "specs/demo/spec.md",
                          content: "Body.",
                          children: [],
                        },
                      ],
                    },
                    tabs: [],
                  }
                : undefined,
            },
            nodes: [
              {
                id: focusedNodeId,
                kind: "note",
                ref: { notePath: "specs/demo/spec.md", kind: "NOTE" },
                notePath: "specs/demo/spec.md",
                childIds: [summaryNodeId],
                body: !useBody
                  ? undefined
                  : [
                      { kind: "narrative", range: { start: 0, end: 12 }, markdown: "Loose intro." },
                      {
                        kind: "child_section",
                        range: { start: 13, end: 30 },
                        childRef: {
                          notePath: "specs/demo/spec.md",
                          fragment: "Summary",
                          nodeId: "specs/demo/spec.md#Summary",
                          kind: "SECTION",
                        },
                        sectionDisplay: "INLINE",
                      },
                    ],
                status: {
                  dirty: false,
                  validation: { issueCount: 0 },
                  freshness: {},
                  session: {},
                  hasWarnings: false,
                },
                capabilities: {
                  canEdit: true,
                  canEditFields: true,
                  canEditCollections: true,
                  canNavigateChildren: true,
                  canSubscribe: true,
                },
                data: {
                  title: "Demo spec",
                  resolvedType: "Spec",
                  locator: "FILE",
                  markdown: "# Demo spec\n\nLoose intro.\n\n## Summary\n\nBody.",
                },
              },
              {
                id: summaryNodeId,
                kind: "section",
                ref: {
                  notePath: "specs/demo/spec.md",
                  fragment: "Summary",
                  nodeId: "specs/demo/spec.md#Summary",
                  kind: "SECTION",
                },
                notePath: "specs/demo/spec.md",
                parentId: focusedNodeId,
                childIds: [],
                body: !useBody
                  ? undefined
                  : [{ kind: "narrative", range: { start: 13, end: 18 }, markdown: "Body." }],
                status: {
                  dirty: false,
                  validation: { issueCount: 0 },
                  freshness: {},
                  session: {},
                  hasWarnings: false,
                },
                capabilities: {
                  canEdit: false,
                  canEditFields: false,
                  canEditCollections: false,
                  canNavigateChildren: true,
                  canSubscribe: false,
                },
                data: {
                  title: "Summary",
                  level: "H2",
                  locator: "SECTION",
                  fragment: "Summary",
                  markdown: "Body.",
                },
              },
            ],
            views: {
              renderedOutline: { rootIds: [summaryNodeId] },
              structuralOutline: {
                rootId: focusedNodeId,
                defaultView: "structural",
                tabs: [],
              },
              relationGroups: [],
            },
          })}
        />,
      );

      expect(screen.getAllByText("Body.")).toHaveLength(1);

      // Graph-only structural roots intentionally drop FILE content when children exist;
      // whether the intro should render there is an open product question.
      if (path !== "graph-only structural view") {
        expect(screen.getAllByText("Loose intro.")).toHaveLength(1);
      }
    },
  );

  it("renders undeclared markdown sections inline beside declared section links", () => {
    render(
      <OntologyNotePane
        rendered={{
          path: "docs/specs/experience/ontology-browser-navigation-model.md",
          title: "Ontology browser navigation model",
          rendered:
            "# Ontology browser navigation model\n\ntesting 1 2 3\n\n## Summary\n\nDeclared summary.\n\n## Scratchpad\n\nLoose heading body.",
          links: [],
          embeds: [],
          sections: [
            {
              id: "summary",
              title: "Summary",
              level: "H2",
              content: "Declared summary.",
              fieldName: "summarySection",
              fieldPath: "summarySection",
              sectionDisplay: "INLINE",
              children: [],
            },
            {
              id: "scratchpad",
              title: "Scratchpad",
              level: "H2",
              content: "Loose heading body.",
              children: [],
            },
          ],
        }}
        workspace={asWorkspace({
          path: "docs/specs/experience/ontology-browser-navigation-model.md",
          title: "Ontology browser navigation model",
          resolvedType: "ExperienceSpec",
          structural: {
            defaultView: "structural",
            root: {
              nodeId: "root",
              title: "Ontology browser navigation model",
              locator: "FILE",
              notePath: "docs/specs/experience/ontology-browser-navigation-model.md",
              content: "testing 1 2 3",
              children: [
                {
                  nodeId: "summary",
                  title: "Summary",
                  locator: "SECTION",
                  notePath: "docs/specs/experience/ontology-browser-navigation-model.md",
                  fieldName: "summarySection",
                  fieldPath: "summarySection",
                  sectionDisplay: "INLINE",
                  content: "Declared summary.",
                  children: [],
                },
                {
                  nodeId: "scratchpad",
                  title: "Scratchpad",
                  locator: "SECTION",
                  notePath: "docs/specs/experience/ontology-browser-navigation-model.md",
                  content: "Loose heading body.",
                  children: [],
                },
              ],
            },
            tabs: [],
          },
          relationGroups: [],
        })}
        onOpen={vi.fn()}
        onOpenNode={vi.fn()}
      />,
    );

    expect(screen.getByText("testing 1 2 3")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Summary" })).toBeInTheDocument();
    expect(screen.getByText("Declared summary.")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Scratchpad" })).toBeInTheDocument();
    expect(screen.getByText("Loose heading body.")).toBeInTheDocument();
  });

  it("renders note content from canonical workspace graph selectors", () => {
    const focusedNodeId = "note|specs/demo/spec.md|||NOTE|";
    const summaryNodeId = "section|specs/demo/spec.md|Summary|specs/demo/spec.md#Summary|SECTION|";
    const fieldNodeId = `field|${focusedNodeId}|summary`;
    const collectionNodeId = `collection|${focusedNodeId}|stories`;

    render(
      <OntologyNotePane
        onOpen={() => {}}
        workspace={asWorkspace({
          requestedRef: "specs/demo/spec.md",
          focusedNodeId,
          content: {
            path: "specs/demo/spec.md",
            title: "Demo spec",
            resolvedType: "Spec",
            rendered: {
              path: "specs/demo/spec.md",
              title: "Demo spec",
              resolvedType: "Spec",
              rendered: "# Demo spec\n\n## Summary\n\nBody.",
              links: [],
              embeds: [],
              sections: [],
            },
          },
          nodes: [
            {
              id: focusedNodeId,
              kind: "note",
              ref: { notePath: "specs/demo/spec.md", kind: "NOTE" },
              notePath: "specs/demo/spec.md",
              childIds: [summaryNodeId, fieldNodeId, collectionNodeId],
              status: {
                dirty: false,
                validation: { issueCount: 0 },
                freshness: {},
                session: {},
                hasWarnings: false,
              },
              capabilities: {
                canEdit: true,
                canEditFields: true,
                canEditCollections: true,
                canNavigateChildren: true,
                canSubscribe: true,
              },
              version: "note-v1",
              data: {
                title: "Demo spec",
                resolvedType: "Spec",
                markdown: "# Demo spec",
                locator: "FILE",
              },
            },
            {
              id: summaryNodeId,
              kind: "section",
              ref: {
                notePath: "specs/demo/spec.md",
                fragment: "Summary",
                nodeId: "specs/demo/spec.md#Summary",
                kind: "SECTION",
              },
              notePath: "specs/demo/spec.md",
              parentId: focusedNodeId,
              childIds: [],
              status: {
                dirty: false,
                validation: { issueCount: 0 },
                freshness: {},
                session: {},
                hasWarnings: false,
              },
              capabilities: {
                canEdit: false,
                canEditFields: false,
                canEditCollections: false,
                canNavigateChildren: true,
                canSubscribe: false,
              },
              version: "section-v1",
              data: {
                title: "Summary",
                level: "H2",
                locator: "SECTION",
                fragment: "Summary",
                markdown: "Body.",
                binding: {
                  typeName: "SummarySection",
                  fieldName: "summary",
                  fieldPath: "summary",
                },
              },
            },
            {
              id: fieldNodeId,
              kind: "field",
              ref: { notePath: "specs/demo/spec.md", kind: "NOTE" },
              notePath: "specs/demo/spec.md",
              parentId: focusedNodeId,
              childIds: [],
              status: {
                dirty: false,
                validation: { issueCount: 0 },
                freshness: {},
                session: {},
                hasWarnings: false,
              },
              capabilities: {
                canEdit: false,
                canEditFields: false,
                canEditCollections: false,
                canNavigateChildren: false,
                canSubscribe: false,
              },
              version: "field-v1",
              field: {
                name: "summary",
                valueKind: "scalar",
                present: true,
                values: ["Body."],
                range: { start: 1, end: 10 },
              },
            },
            {
              id: collectionNodeId,
              kind: "collection",
              ref: { notePath: "specs/demo/spec.md", kind: "NOTE" },
              notePath: "specs/demo/spec.md",
              parentId: focusedNodeId,
              childIds: [],
              status: {
                dirty: false,
                validation: { issueCount: 0 },
                freshness: {},
                session: {},
                hasWarnings: false,
              },
              capabilities: {
                canEdit: false,
                canEditFields: false,
                canEditCollections: false,
                canNavigateChildren: false,
                canSubscribe: false,
              },
              version: "collection-v1",
              collection: {
                name: "stories",
                itemRefs: [],
                range: { start: 11, end: 12 },
              },
            },
          ],
          edges: [
            { kind: "contains", fromId: focusedNodeId, toId: summaryNodeId },
            {
              kind: "binds_field",
              fromId: focusedNodeId,
              toId: fieldNodeId,
              fieldName: "summary",
            },
          ],
          views: {
            renderedOutline: {
              rootIds: [summaryNodeId],
            },
            relationGroups: [
              {
                scopeNodeId: focusedNodeId,
                groups: [
                  {
                    key: "ambient",
                    label: "Ambient relations",
                    items: [
                      {
                        path: "specs/demo/plan.md",
                        title: "Demo plan",
                        kind: "note",
                      },
                    ],
                  },
                ],
              },
            ],
          },
        })}
      />,
    );

    expect(screen.getByRole("heading", { name: "Summary" })).toBeInTheDocument();
    // "Body." appears in the rendered markdown body and the canonical field value.
    expect(screen.getAllByText("Body.")).toHaveLength(2);
    // The field's display name is rendered in the property panel above the body.
    expect(screen.getAllByText("Summary").length).toBeGreaterThanOrEqual(2);
  });

  it("renders collection rows from content nodes when owner field nodes share the same ref", () => {
    const focusedNodeId = "node|notes/spec.md|user-stories|notes/spec.md#user-stories";
    const storyNodeId = "node|notes/spec.md|^story-one|notes/spec.md#^story-one";
    const fieldNodeId = `field|${storyNodeId}|locator`;

    const storyRef: NodeRef = {
      notePath: "notes/spec.md",
      fragment: "^story-one",
      nodeId: "notes/spec.md#^story-one",
      kind: "EMBEDDED",
    };

    const cleanStatus = {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    };

    const readCaps = {
      canEdit: false,
      canEditFields: false,
      canEditCollections: false,
      canNavigateChildren: true,
      canSubscribe: false,
    };

    render(
      <OntologyNotePane
        onOpen={() => {}}
        workspace={asWorkspace({
          requestedRef: "notes/spec.md#user-stories",
          focusedNodeId,
          content: {
            path: "notes/spec.md#user-stories",
            title: "User Stories",
            resolvedType: "UserStoriesSection",
            rendered: {
              path: "notes/spec.md",
              title: "Spec",
              resolvedType: "Spec",
              rendered: "# Spec",
              links: [],
              embeds: [],
              sections: [],
            },
          },
          nodes: [
            {
              id: focusedNodeId,
              kind: "section",
              ref: {
                notePath: "notes/spec.md",
                fragment: "user-stories",
                nodeId: "notes/spec.md#user-stories",
                kind: "SECTION",
              },
              notePath: "notes/spec.md",
              childIds: [storyNodeId],
              status: cleanStatus,
              capabilities: readCaps,
              data: {
                title: "User Stories",
                resolvedType: "UserStoriesSection",
                locator: "SECTION",
                fragment: "user-stories",
              },
              body: [
                {
                  kind: "collection",
                  range: { start: 1, end: 2 },
                  fieldName: "stories",
                  childRefs: [storyRef],
                  sectionDisplay: "PANE",
                },
              ],
            },
            {
              id: fieldNodeId,
              kind: "field",
              ref: storyRef,
              notePath: "notes/spec.md",
              parentId: storyNodeId,
              status: cleanStatus,
              capabilities: readCaps,
              field: {
                name: "locator",
                valueKind: "scalar",
                present: true,
                values: ["story-one"],
                range: { start: 1, end: 2 },
              },
            },
            {
              id: storyNodeId,
              kind: "embedded",
              ref: storyRef,
              notePath: "notes/spec.md",
              parentId: focusedNodeId,
              status: cleanStatus,
              capabilities: readCaps,
              data: {
                title: "As a user seeing story rows",
                resolvedType: "UserStory",
                locator: "EMBEDDED",
                fragment: "^story-one",
                binding: {
                  typeName: "UserStory",
                  fieldName: "stories",
                  fieldPath: "userStories.stories",
                  fieldList: true,
                  properties: {
                    id: "SPEC-0001.US1",
                    status: "ready",
                    summary: "Story rows use embedded-node identity.",
                  },
                },
              },
            },
          ],
        })}
      />,
    );

    expect(screen.getByRole("button", { name: /As a user seeing story rows/ })).toBeInTheDocument();
    expect(screen.queryByText("Untitled")).not.toBeInTheDocument();
  });

  it("scrolls to inline outline targets in BodyWalker structural mode", () => {
    const originalScrollIntoView = window.HTMLElement.prototype.scrollIntoView;
    const scrollIntoView = vi.fn();
    const onOpen = vi.fn();
    window.HTMLElement.prototype.scrollIntoView = scrollIntoView;

    try {
      const focusedNodeId = "node|specs/demo/spec.md||";
      const summaryNodeId = "node|specs/demo/spec.md|Summary|specs/demo/spec.md#Summary";
      const detailsNodeId = "node|specs/demo/spec.md|Details|specs/demo/spec.md#Details";
      const contextRef: ContextRef = { current: null };

      render(
        <OntologyNotePane
          onOpen={onOpen}
          onContextChange={(next) => {
            contextRef.current = next;
          }}
          workspace={asWorkspace({
            requestedRef: "specs/demo/spec.md",
            focusedNodeId,
            content: {
              path: "specs/demo/spec.md",
              title: "Demo spec",
              resolvedType: "Spec",
              rendered: {
                path: "specs/demo/spec.md",
                title: "Demo spec",
                resolvedType: "Spec",
                rendered: "# Demo spec\n\n## Summary\n\nBody.\n\n## Details\n\nMore.",
                links: [],
                embeds: [],
                sections: [
                  {
                    id: "specs/demo/spec.md#Summary",
                    title: "Summary",
                    level: "H2",
                    content: "Body.",
                    children: [],
                  },
                  {
                    id: "specs/demo/spec.md#Details",
                    title: "Details",
                    level: "H2",
                    content: "More.",
                    children: [],
                  },
                ],
              },
            },
            nodes: [
              {
                id: focusedNodeId,
                kind: "note",
                ref: { notePath: "specs/demo/spec.md", kind: "NOTE" },
                notePath: "specs/demo/spec.md",
                childIds: [summaryNodeId, detailsNodeId],
                status: {
                  dirty: false,
                  validation: { issueCount: 0 },
                  freshness: {},
                  session: {},
                  hasWarnings: false,
                },
                capabilities: {
                  canEdit: true,
                  canEditFields: true,
                  canEditCollections: true,
                  canNavigateChildren: true,
                  canSubscribe: true,
                },
                version: "note-v1",
                body: [
                  {
                    kind: "child_section",
                    range: { start: 0, end: 10 },
                    fieldName: "summary",
                    childRef: {
                      notePath: "specs/demo/spec.md",
                      fragment: "Summary",
                      nodeId: "specs/demo/spec.md#Summary",
                      kind: "SECTION",
                    },
                    sectionDisplay: "INLINE",
                  },
                  {
                    kind: "child_section",
                    range: { start: 17, end: 28 },
                    fieldName: "details",
                    childRef: {
                      notePath: "specs/demo/spec.md",
                      fragment: "Details",
                      nodeId: "specs/demo/spec.md#Details",
                      kind: "SECTION",
                    },
                    sectionDisplay: "INLINE",
                  },
                ],
                data: {
                  title: "Demo spec",
                  resolvedType: "Spec",
                  locator: "FILE",
                },
              },
              {
                id: summaryNodeId,
                kind: "section",
                ref: {
                  notePath: "specs/demo/spec.md",
                  fragment: "Summary",
                  nodeId: "specs/demo/spec.md#Summary",
                  kind: "SECTION",
                },
                notePath: "specs/demo/spec.md",
                parentId: focusedNodeId,
                childIds: [],
                status: {
                  dirty: false,
                  validation: { issueCount: 0 },
                  freshness: {},
                  session: {},
                  hasWarnings: false,
                },
                capabilities: {
                  canEdit: false,
                  canEditFields: false,
                  canEditCollections: false,
                  canNavigateChildren: true,
                  canSubscribe: false,
                },
                version: "section-v1",
                body: [
                  {
                    kind: "narrative",
                    range: { start: 11, end: 16 },
                    markdown: "Body.",
                  },
                ],
                data: {
                  title: "Summary",
                  level: "H2",
                  locator: "SECTION",
                  fragment: "Summary",
                  binding: {
                    typeName: "SummarySection",
                    fieldName: "summary",
                    fieldPath: "summary",
                    sectionDisplay: "INLINE",
                  },
                },
              },
              {
                id: detailsNodeId,
                kind: "section",
                ref: {
                  notePath: "specs/demo/spec.md",
                  fragment: "Details",
                  nodeId: "specs/demo/spec.md#Details",
                  kind: "SECTION",
                },
                notePath: "specs/demo/spec.md",
                parentId: focusedNodeId,
                childIds: [],
                status: {
                  dirty: false,
                  validation: { issueCount: 0 },
                  freshness: {},
                  session: {},
                  hasWarnings: false,
                },
                capabilities: {
                  canEdit: false,
                  canEditFields: false,
                  canEditCollections: false,
                  canNavigateChildren: true,
                  canSubscribe: false,
                },
                version: "section-v2",
                body: [
                  {
                    kind: "narrative",
                    range: { start: 29, end: 34 },
                    markdown: "More.",
                  },
                ],
                data: {
                  title: "Details",
                  level: "H2",
                  locator: "SECTION",
                  fragment: "Details",
                  binding: {
                    typeName: "DetailsSection",
                    fieldName: "details",
                    fieldPath: "details",
                    sectionDisplay: "INLINE",
                  },
                },
              },
            ],
            views: {
              structuralOutline: {
                rootId: focusedNodeId,
                defaultView: "structural",
                tabs: [],
              },
            },
          })}
        />,
      );

      expect(contextRef.current).not.toBeNull();
      const currentContext = contextRef.current;

      if (!currentContext) throw new Error("OntologyNotePane did not publish its tab context");

      const summary = currentContext.outline.sections.find(
        (section) => section.title === "Summary",
      );

      const details = currentContext.outline.sections.find(
        (section) => section.title === "Details",
      );

      if (!summary || !details) {
        throw new Error("OntologyNotePane did not publish both outline targets");
      }

      // Navigate to both registered sections so neither the first nor the last
      // registered ref can stand in for the requested one.
      currentContext.outline.navigate(summary);
      currentContext.outline.navigate(details);

      const summarySection = screen.getByRole("heading", { name: "Summary" }).closest("section");
      const detailsSection = screen.getByRole("heading", { name: "Details" }).closest("section");
      expect(summarySection).not.toBeNull();
      expect(detailsSection).not.toBe(summarySection);
      expect(scrollIntoView).toHaveBeenCalledTimes(2);
      expect(scrollIntoView.mock.contexts[0]).toBe(summarySection);
      expect(scrollIntoView.mock.contexts[1]).toBe(detailsSection);
      expect(onOpen).not.toHaveBeenCalled();
    } finally {
      window.HTMLElement.prototype.scrollIntoView = originalScrollIntoView;
    }
  });

  it("reads plain headings inline and keeps outline navigation within the note", () => {
    const originalScrollIntoView = window.HTMLElement.prototype.scrollIntoView;
    const scrollIntoView = vi.fn();
    window.HTMLElement.prototype.scrollIntoView = scrollIntoView;
    const onOpenNode = vi.fn();
    const contextRef: ContextRef = { current: null };
    const notePath = "notes/plain.md";
    const focusedNodeId = `node|${notePath}||`;
    const sectionRef = { notePath, fragment: "plans-10", nodeId: `${notePath}#plans-10` };
    const sectionNodeId = `node|${notePath}|plans-10|${sectionRef.nodeId}`;
    const laterRef = { notePath, fragment: "later-30", nodeId: `${notePath}#later-30` };
    const laterNodeId = `node|${notePath}|later-30|${laterRef.nodeId}`;

    const status = {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    };

    const capabilities = {
      canEdit: false,
      canEditFields: false,
      canEditCollections: false,
      canNavigateChildren: true,
      canSubscribe: false,
    };

    try {
      render(
        <OntologyNotePane
          onOpen={() => {}}
          onOpenNode={onOpenNode}
          onContextChange={(next) => {
            contextRef.current = next;
          }}
          workspace={asWorkspace({
            requestedRef: notePath,
            focusedNodeId,
            content: {
              path: notePath,
              title: "Plain",
              rendered: {
                path: notePath,
                title: "Plain",
                rendered: "## Plans\n\nShip it.",
                links: [],
                embeds: [],
                sections: [],
              },
            },
            nodes: [
              {
                id: focusedNodeId,
                kind: "note",
                ref: { notePath, kind: "NOTE" },
                notePath,
                childIds: [sectionNodeId],
                status,
                capabilities,
                body: [
                  {
                    kind: "child_section",
                    range: { start: 0, end: 18 },
                    childRef: { ...sectionRef, kind: "SECTION" },
                  },
                ],
                data: { title: "Plain", locator: "FILE" },
              },
              {
                id: sectionNodeId,
                kind: "section",
                ref: { ...sectionRef, kind: "SECTION" },
                notePath,
                parentId: focusedNodeId,
                childIds: [],
                status,
                capabilities,
                body: [],
                data: {
                  title: "Plans",
                  level: "H2",
                  locator: "SECTION",
                  fragment: "plans-10",
                  markdown: "## Plans\n\nShip it.\n\n### Later\n\nAfter.",
                },
              },
              // The server parents nested sections to the note, not their heading.
              {
                id: laterNodeId,
                kind: "section",
                ref: { ...laterRef, kind: "SECTION" },
                notePath,
                parentId: focusedNodeId,
                childIds: [],
                status,
                capabilities,
                body: [],
                data: {
                  title: "Later",
                  level: "H3",
                  locator: "SECTION",
                  fragment: "later-30",
                  markdown: "### Later\n\nAfter.",
                },
              },
            ],
            structural: {
              defaultView: "structural",
              root: {
                nodeId: notePath,
                title: "Plain",
                locator: "FILE",
                notePath,
                children: [
                  {
                    nodeId: sectionRef.nodeId,
                    title: "Plans",
                    level: "H2",
                    locator: "SECTION",
                    notePath,
                    children: [
                      {
                        nodeId: laterRef.nodeId,
                        title: "Later",
                        level: "H3",
                        locator: "SECTION",
                        notePath,
                        children: [],
                      },
                    ],
                  },
                ],
              },
              tabs: [],
            },
          })}
        />,
      );

      expect(screen.getByRole("heading", { level: 2, name: /Plans/ })).toBeInTheDocument();
      expect(screen.getByText("Ship it.")).toBeInTheDocument();
      const later = screen.getByRole("heading", { level: 3, name: /Later/ });
      expect(later.closest("section")).toHaveTextContent("After.");
      expect(later.closest("section")).not.toHaveTextContent("Ship it.");

      const plans = contextRef.current?.outline.sections.find(
        (section) => section.title === "Plans",
      );

      const laterTarget = plans?.children?.find((section) => section.title === "Later");

      if (!laterTarget)
        throw new Error("OntologyNotePane did not publish the Later outline target");
      contextRef.current?.outline.navigate(laterTarget);

      expect(scrollIntoView).toHaveBeenCalledTimes(1);
      expect(scrollIntoView.mock.contexts[0]).toBe(later.closest("section"));
      expect(onOpenNode).not.toHaveBeenCalled();
    } finally {
      window.HTMLElement.prototype.scrollIntoView = originalScrollIntoView;
    }
  });

  it("uses structural outline indexes when structural order differs from markdown sections", () => {
    const originalScrollIntoView = window.HTMLElement.prototype.scrollIntoView;
    const scrollIntoView = vi.fn();
    window.HTMLElement.prototype.scrollIntoView = scrollIntoView;
    const contextRef: ContextRef = { current: null };

    try {
      render(
        <OntologyNotePane
          onOpen={() => {}}
          onContextChange={(next) => {
            contextRef.current = next;
          }}
          workspace={asWorkspace({
            path: "notes/typed.md",
            title: "Typed note",
            resolvedType: "Spec",
            content: {
              path: "notes/typed.md",
              title: "Typed note",
              resolvedType: "Spec",
              rendered: {
                path: "notes/typed.md",
                title: "Typed note",
                resolvedType: "Spec",
                rendered: "# Typed note\n\n## Beta\n\nSecond.\n\n## Alpha\n\nFirst.",
                links: [],
                embeds: [],
                sections: [
                  {
                    id: "beta",
                    title: "Beta",
                    level: "H2",
                    content: "Second.",
                    children: [],
                  },
                  {
                    id: "alpha",
                    title: "Alpha",
                    level: "H2",
                    content: "First.",
                    children: [],
                  },
                ],
              },
            },
            structural: {
              defaultView: "structural",
              root: {
                nodeId: "root",
                title: "Typed note",
                locator: "FILE",
                notePath: "notes/typed.md",
                children: [
                  {
                    nodeId: "alpha",
                    title: "Alpha",
                    locator: "SECTION",
                    notePath: "notes/typed.md",
                    content: "First.",
                    children: [],
                  },
                  {
                    nodeId: "beta",
                    title: "Beta",
                    locator: "SECTION",
                    notePath: "notes/typed.md",
                    content: "Second.",
                    children: [],
                  },
                ],
              },
              tabs: [],
            },
            relationGroups: [],
          })}
        />,
      );

      expect(screen.getByRole("button", { name: "Structure" })).toHaveAttribute(
        "aria-pressed",
        "true",
      );
      expect(screen.getByRole("button", { name: "Markdown" })).toBeInTheDocument();
      expect(contextRef.current).not.toBeNull();
      const structuralContext = contextRef.current;

      if (!structuralContext)
        throw new Error("OntologyNotePane did not publish structural context");
      expect(structuralContext.outline.sections.map((section) => section.title)).toEqual([
        "Alpha",
        "Beta",
      ]);
      const [alpha, beta] = structuralContext.outline.sections;
      // Navigate to both so neither the first nor the last registered ref can
      // stand in for the requested section.
      structuralContext.outline.navigate(alpha);
      structuralContext.outline.navigate(beta);

      const alphaSection = screen.getByText("First.").closest("section");
      const betaSection = screen.getByText("Second.").closest("section");
      expect(alphaSection).not.toBeNull();
      expect(betaSection).not.toBe(alphaSection);
      expect(scrollIntoView).toHaveBeenCalledTimes(2);
      expect(scrollIntoView.mock.contexts[0]).toBe(alphaSection);
      expect(scrollIntoView.mock.contexts[1]).toBe(betaSection);
    } finally {
      window.HTMLElement.prototype.scrollIntoView = originalScrollIntoView;
    }
  });

  it("reveals source headings through projected outline navigation", async () => {
    const contextRef: ContextRef = { current: null };
    const notePath = "notes/source.md";
    const summaryID = `${notePath}#Summary`;

    render(
      <OntologyNotePane
        viewMode="source"
        onOpen={() => {}}
        onContextChange={(next) => {
          contextRef.current = next;
        }}
        workspace={asWorkspace({
          requestedRef: notePath,
          content: {
            path: notePath,
            title: "Source note",
            markdown: "# Source note\n\n## Summary\n\nBody.",
            rendered: {
              path: notePath,
              title: "Source note",
              resolvedType: "Spec",
              rendered: "# Source note\n\n## Summary\n\nBody.",
              links: [],
              embeds: [],
              sections: [
                {
                  id: `${notePath}#Source note`,
                  title: "Source note",
                  level: "H1",
                  notePath,
                  children: [
                    {
                      id: summaryID,
                      title: "Summary",
                      level: "H2",
                      notePath,
                      children: [],
                    },
                  ],
                },
              ],
            },
          },
        })}
      />,
    );

    const contentDOM = await screen.findByRole<HTMLElement>("textbox", { name: "Note source" });
    const view = EditorView.findFromDOM(contentDOM);

    if (!view) throw new Error("Expected a CodeMirror source editor");
    const context = contextRef.current;

    if (!context) throw new Error("OntologyNotePane did not publish source context");
    const summary = context.outline.sections.find((section) => section.id === summaryID);

    if (!summary) throw new Error("OntologyNotePane did not publish the Summary outline target");
    context.outline.navigate(summary);

    await waitFor(() => expect(view.state.selection.main.head).toBe(view.state.doc.line(3).from));
    expect(contentDOM).toHaveFocus();
  });

  it("renders embedded node panes with identity and body from the public workspace", () => {
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

    const cleanStatus = {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    };

    const publicData: PublicNodeDetailData = {
      node: {
        ref: storyRef,
        nodeId: `section:${notePath}#${notePath}#^SPEC-0023-US1`,
        nodeKind: "EMBEDDED",
        path: `${notePath}#^SPEC-0023-US1`,
        title: "US1 - Link target for any surfaced node",
        resolvedType: "UserStory",
        content: "- status:: ready\n\n#### Acceptance Criteria\n\n- Tools return a link target.",
        locator: {
          sourceLocator: `${notePath}#^SPEC-0023-US1`,
          status: "linkable",
          exists: true,
          requiresFix: false,
        },
        localGraph: { nodes: [], edges: [], truncated: false },
        workspace: {
          ...publicWorkspaceDefaults,
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
              markdown: "Story narrative.",
              fields: [
                {
                  name: "status",
                  kind: "INLINE_FIELD",
                  present: true,
                  status: cleanStatus,
                  range: { start: 0, end: 16 },
                  valueRanges: [{ start: 10, end: 16 }],
                  values: ["ready"],
                  sectionNodes: [],
                },
              ],
              collections: [],
              blocks: [
                {
                  kind: "narrative",
                  range: { start: 0, end: 16 },
                  markdown: "Story narrative.",
                  childRefs: [],
                },
                {
                  kind: "child_section",
                  range: { start: 17, end: 70 },
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
              markdown: "Tools return a link target.",
              fields: [],
              collections: [],
              blocks: [
                {
                  kind: "narrative",
                  range: { start: 17, end: 70 },
                  markdown: "Tools return a link target.",
                  childRefs: [],
                },
              ],
            },
          ],
        },
      },
    };

    renderWithQueryClient(
      <OntologyNotePane
        workspace={nodeWorkspaceFromPublicGraphQL(publicData, `${notePath}#^SPEC-0023-US1`)}
        onOpen={vi.fn()}
        onOpenNode={vi.fn()}
      />,
    );

    expect(screen.queryByText("Choose a note")).not.toBeInTheDocument();
    expect(
      screen.queryByText("Pick a note to open the ontology workspace."),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("heading", {
        name: "US1 - Link target for any surfaced node",
      }),
    ).toBeInTheDocument();
    expect(screen.getByText("Story narrative.")).toBeInTheDocument();
    expect(screen.getByText("Tools return a link target.")).toBeInTheDocument();
    expect(screen.getByText("Properties", { exact: true })).toBeInTheDocument();
  });
});
