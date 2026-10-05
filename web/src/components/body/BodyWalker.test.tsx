import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  NodeBodyBlock,
  NodeRef,
  NodeWorkspace,
  WorkspaceContentNode,
  WorkspaceFieldNode,
} from "../../api/types";
import { BodyWalker } from "./BodyWalker";
import type { BodyRenderContext } from "./registry";

beforeEach(() => window.localStorage.clear());

/**
 * Fixture: a UserStory-like embedded node whose body has two inline fields,
 * a narrative chunk, and an INLINE child section that recurses. The child
 * has one narrative block. Covers every renderer kind except collection.
 */
function makeWorkspace(): NodeWorkspace {
  const notePath = "docs/spec.md";

  const storyRef: NodeRef = {
    notePath,
    kind: "EMBEDDED",
    nodeId: `${notePath}#^story-a`,
    fragment: "^story-a",
  };

  const acRef: NodeRef = {
    notePath,
    kind: "SECTION",
    nodeId: `${notePath}#acceptance-200`,
    fragment: "acceptance-200",
  };

  const storyBody: NodeBodyBlock[] = [
    {
      kind: "inline_field",
      range: { start: 10, end: 16 },
      fieldName: "id",
      rawKey: "id",
    },
    {
      kind: "inline_field",
      range: { start: 17, end: 31 },
      fieldName: "status",
      rawKey: "status",
    },
    {
      kind: "narrative",
      range: { start: 32, end: 60 },
      markdown: "A short narrative block.",
    },
    {
      kind: "child_section",
      range: { start: 61, end: 120 },
      fieldName: "acceptanceCriteria",
      childRef: acRef,
      sectionDisplay: "INLINE",
    },
    {
      kind: "inline_field",
      range: { start: 121, end: 129 },
      fieldName: "locator",
      rawKey: "locator",
    },
  ];

  const acBody: NodeBodyBlock[] = [
    {
      kind: "inline_field",
      range: { start: 62, end: 69 },
      fieldName: "status",
      rawKey: "status",
    },
    {
      kind: "narrative",
      range: { start: 70, end: 115 },
      markdown: "- must do the thing",
    },
  ];

  const statusField: WorkspaceFieldNode = {
    id: "field|focused|status",
    kind: "field",
    ref: storyRef,
    notePath,
    parentId: "node|story",
    status: {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    },
    capabilities: emptyCaps(),
    field: {
      name: "status",
      valueKind: "scalar",
      typeName: "UserStoryStatus",
      enumValues: ["future", "ready", "complete"],
      present: true,
      values: ["ready"],
      range: { start: 17, end: 31 },
    },
  };

  const idField: WorkspaceFieldNode = {
    id: "field|focused|id",
    kind: "field",
    ref: storyRef,
    notePath,
    parentId: "node|story",
    status: {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    },
    capabilities: emptyCaps(),
    field: {
      name: "id",
      valueKind: "scalar",
      typeName: "ID",
      present: true,
      values: ["SPEC-0014.US2"],
      range: { start: 10, end: 16 },
    },
  };

  const locatorField: WorkspaceFieldNode = {
    id: "field|focused|locator",
    kind: "field",
    ref: storyRef,
    notePath,
    parentId: "node|story",
    status: {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    },
    capabilities: emptyCaps(),
    field: {
      name: "locator",
      valueKind: "scalar",
      typeName: "String",
      present: true,
      values: ["story-a"],
      range: { start: 121, end: 129 },
    },
  };

  const storyNode: WorkspaceContentNode = {
    id: "node|story",
    kind: "embedded",
    ref: storyRef,
    notePath,
    status: {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    },
    capabilities: emptyCaps(),
    body: storyBody,
    data: {
      title: "As a user exploring a note",
      locator: "EMBEDDED",
      fragment: "^story-a",
      binding: { typeName: "UserStory" },
    },
  };

  const acNode: WorkspaceContentNode = {
    id: "node|ac",
    kind: "note",
    ref: acRef,
    notePath,
    status: {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    },
    capabilities: emptyCaps(),
    body: acBody,
    data: {
      title: "Acceptance Criteria",
      resolvedType: "NarrativeSection",
    },
  };

  const childStatusField: WorkspaceFieldNode = {
    id: "field|ac|status",
    kind: "field",
    ref: acRef,
    notePath,
    parentId: "node|ac",
    status: {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    },
    capabilities: emptyCaps(),
    field: {
      name: "status",
      valueKind: "scalar",
      typeName: "AcceptanceStatus",
      enumValues: ["draft", "complete"],
      present: true,
      values: ["complete"],
      range: { start: 62, end: 69 },
    },
  };

  // acNode is tagged `kind: "note"` in this fixture — kind is informational
  // for callers that care, but the walker reads solely from `data`.
  return {
    requestedRef: `${notePath}#^story-a`,
    focusedNodeId: "node|story",
    node: {
      ref: storyRef,
      resolvedType: "UserStory",
      notePath,
      title: "As a user exploring a note",
      locator: "EMBEDDED",
    },
    content: {
      path: notePath,
      title: "As a user exploring a note",
      resolvedType: "UserStory",
    },
    nodes: [storyNode, acNode, idField, statusField, locatorField, childStatusField],
    loaded: {
      rendered: true,
      assessment: true,
      structure: true,
      relations: true,
    },
    capabilities: emptyCaps(),
    status: {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    },
    version: "v1",
    sourceRevision: {
      notePath,
      contentFingerprint: "source-fingerprint",
      content: "source markdown",
    },
  };
}

afterEach(() => {
  vi.useRealTimers();
});

function emptyCaps() {
  return {
    canEdit: false,
    canEditFields: false,
    canEditCollections: false,
    canNavigateChildren: false,
    canSubscribe: false,
  };
}

describe("BodyWalker", () => {
  it("keeps identical collection items mounted by their distinct workspace identities", () => {
    const workspace = makeWorkspace();

    const original = workspace.nodes?.find(
      (node): node is WorkspaceContentNode => node.kind === "note",
    );

    if (!original) throw new Error("missing fixture content node");

    const children = [1, 2].map((position) => ({
      ...original,
      id: `item-${position}`,
      ref: {
        ...original.ref,
        nodeId: `item-${position}`,
        fragment: undefined,
        structuralFingerprint: "identical",
      },
      data: { ...original.data, title: "Identical item" },
    }));

    const parent = {
      ...original,
      body: [
        {
          kind: "collection" as const,
          range: { start: 0, end: 20 },
          childRefs: children.map((child) => child.ref),
        },
      ],
    };

    workspace.nodes = [parent, ...children];

    const context: BodyRenderContext = {
      workspace,
      mode: "view",
      lookupNode: (id) => workspace.nodes?.find((node) => node.id === id),
    };

    const rendered = render(<BodyWalker node={parent} context={context} />);
    const second = screen.getAllByRole("button", { name: /Identical item/ })[1];

    const filteredParent = {
      ...parent,
      body: [{ ...parent.body[0], childRefs: [children[1].ref] }],
    };

    rendered.rerender(<BodyWalker node={filteredParent} context={context} />);
    expect(screen.getByRole("button", { name: /Identical item/ })).toBe(second);
  });

  it("renders inline fields, narrative, and recurses into an INLINE child section", () => {
    const workspace = makeWorkspace();

    const context: BodyRenderContext = {
      workspace,
      mode: "view",
      lookupNode: (id) => workspace.nodes?.find((node) => node.id === id),
    };

    const nodes = workspace.nodes ?? [];
    render(<BodyWalker node={nodes[0]} context={context} />);

    // Inline property source blocks are shown in the properties panel, not
    // repeated in the body flow.
    expect(screen.queryByText("id")).not.toBeInTheDocument();
    expect(screen.queryByText("SPEC-0014.US2")).not.toBeInTheDocument();
    expect(screen.queryByText("status")).not.toBeInTheDocument();
    expect(screen.queryByText("locator")).not.toBeInTheDocument();

    // Narrative renders
    expect(screen.getByText("A short narrative block.")).toBeInTheDocument();

    // INLINE child section recurses
    expect(screen.getByText("Acceptance Criteria")).toBeInTheDocument();
    // remark-gfm turns "- must do..." into a bullet list; the li text is
    // just the content without the bullet marker.
    expect(screen.getByText("must do the thing")).toBeInTheDocument();
  });

  it("renders wikilinks as copyable web links while routing normal clicks through onOpen", () => {
    const workspace = makeWorkspace();
    const onOpen = vi.fn();
    const nodes = workspace.nodes ?? [];

    const node = {
      ...nodes[0],
      body: [
        {
          kind: "narrative" as const,
          range: { start: 0, end: 100 },
          markdown: "[SPEC-0014.US10.AC1](rhizome://note/docs%2Fspec.md%23%5ESPEC-0014-US10-AC1)",
        },
      ],
    };

    window.history.replaceState({}, "", "/notes/effort-note?note=docs/effort.md");

    const context: BodyRenderContext = {
      workspace,
      mode: "view",
      rendered: {
        path: "docs/effort.md",
        title: "Effort",
        rendered: "",
        links: [
          {
            target: "docs/specs/technical/action-items-starter-template.md",
            text: "action-items-starter-template",
            kind: "wikilink",
          },
          {
            target: "docs/spec.md#^SPEC-0014-US10-AC1",
            text: "docs/spec.md#^SPEC-0014-US10-AC1",
            kind: "wikilink",
          },
        ],
        embeds: [],
        sections: [],
      },
      onOpen,
      lookupNode: (id) => workspace.nodes?.find((entry) => entry.id === id),
    };

    render(<BodyWalker node={node} context={context} />);

    const link = screen.getByRole("link", { name: "SPEC-0014.US10.AC1" });
    expect(link).toHaveAttribute(
      "href",
      "/notes/effort-note?note=docs%2Fspec.md#%5ESPEC-0014-US10-AC1",
    );
    fireEvent.click(link);
    expect(onOpen).toHaveBeenCalledWith("docs/spec.md#^SPEC-0014-US10-AC1", "stack");
    fireEvent.click(link, { metaKey: true });
    expect(onOpen).toHaveBeenLastCalledWith("docs/spec.md#^SPEC-0014-US10-AC1", "beside");
    fireEvent.click(link, { ctrlKey: true });
    expect(onOpen).toHaveBeenLastCalledWith("docs/spec.md#^SPEC-0014-US10-AC1", "beside");
  });

  it("renders and opens an authored note link containing a malformed percent escape", () => {
    const workspace = makeWorkspace();
    const onOpen = vi.fn();

    const node = {
      ...workspace.nodes![0],
      body: [
        {
          kind: "narrative" as const,
          range: { start: 0, end: 100 },
          markdown: "[Progress](rhizome://note/docs/%E0%A4.md#progress)",
        },
      ],
    };

    window.history.replaceState({}, "", "/notes");
    render(
      <BodyWalker
        node={node}
        context={{
          workspace,
          mode: "view",
          onOpen,
          lookupNode: () => undefined,
        }}
      />,
    );

    const link = screen.getByRole("link", { name: "Progress" });
    expect(link).toHaveAttribute("href", "/notes?note=docs%2F%25E0%25A4.md#progress");
    fireEvent.click(link);
    expect(onOpen).toHaveBeenCalledWith("docs/%E0%A4.md#progress", "stack");
  });

  it("uses rendered link metadata for basename wikilink web urls", () => {
    const workspace = makeWorkspace();
    const onOpen = vi.fn();
    const nodes = workspace.nodes ?? [];

    const node = {
      ...nodes[0],
      body: [
        {
          kind: "narrative" as const,
          range: { start: 0, end: 100 },
          markdown:
            "[SPEC-0060 Action items starter template](rhizome://note/action-items-starter-template)",
        },
      ],
    };

    window.history.replaceState({}, "", "/notes/effort-note?note=docs/effort.md");

    const context: BodyRenderContext = {
      workspace,
      mode: "view",
      rendered: {
        path: "docs/effort.md",
        title: "Effort",
        rendered: "",
        links: [
          {
            target: "docs/specs/technical/action-items-starter-template.md",
            text: "action-items-starter-template",
            kind: "wikilink",
          },
        ],
        embeds: [],
        sections: [],
      },
      onOpen,
      lookupNode: (id) => workspace.nodes?.find((entry) => entry.id === id),
    };

    render(<BodyWalker node={node} context={context} />);

    const link = screen.getByRole("link", {
      name: "SPEC-0060 Action items starter template",
    });

    expect(link).toHaveAttribute(
      "href",
      "/notes/effort-note?note=docs%2Fspecs%2Ftechnical%2Faction-items-starter-template.md",
    );
    fireEvent.click(link);
    expect(onOpen).toHaveBeenCalledWith(
      "docs/specs/technical/action-items-starter-template.md",
      "stack",
    );
  });

  it("registers inline child section targets for outline scrolling", () => {
    const workspace = makeWorkspace();
    const registerSectionTarget = vi.fn();

    const context: BodyRenderContext = {
      workspace,
      mode: "view",
      registerSectionTarget,
      lookupNode: (id) => workspace.nodes?.find((node) => node.id === id),
    };

    const nodes = workspace.nodes ?? [];
    const { unmount } = render(<BodyWalker node={nodes[0]} context={context} />);

    expect(registerSectionTarget).toHaveBeenCalledWith(
      "docs/spec.md#acceptance-200",
      expect.any(HTMLElement),
    );

    unmount();

    expect(registerSectionTarget).toHaveBeenCalledWith("docs/spec.md#acceptance-200", null);
  });
});

it.each(["_FallbackSection", "_FallbackNote"])(
  "hides %s badges while preserving child navigation",
  (typeName) => {
    const workspace = makeWorkspace();
    const parent = workspace.nodes!.find((node) => node.id === "node|story")!;
    const child = workspace.nodes!.find((node) => node.id === "node|ac")!;

    if (!child.data) throw new Error("Expected content node");
    child.data.resolvedType = typeName;
    child.ref.typeName = typeName;
    parent.body = parent.body?.map((block) =>
      block.kind === "child_section" ? { ...block, sectionDisplay: "PANE" } : block,
    );
    const onOpenNode = vi.fn();
    const onOpen = vi.fn();

    const { container } = render(
      <BodyWalker
        node={parent}
        context={{
          workspace,
          mode: "view",
          onOpenNode,
          onOpen,
          lookupNode: (id) => workspace.nodes?.find((node) => node.id === id),
        }}
      />,
    );

    const link = screen.getByRole("button", { name: /Acceptance Criteria/ });
    expect(link).not.toHaveTextContent(/fallback/i);
    expect(container.querySelector(".body-child-link__type")).toBeNull();
    fireEvent.click(link);
    expect(onOpenNode).toHaveBeenCalledWith(
      expect.objectContaining({ nodeId: child.ref.nodeId, typeName }),
    );
    fireEvent.click(link, { ctrlKey: true });
    expect(onOpen).toHaveBeenCalledWith("docs/spec.md#acceptance-200", "beside");
    expect(onOpenNode).toHaveBeenCalledTimes(1);
  },
);
