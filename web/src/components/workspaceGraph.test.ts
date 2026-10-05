import { describe, expect, it } from "vitest";
import type { NodeWorkspace } from "../api/types";
import {
  selectWorkspaceRelationGroups,
  selectWorkspaceRenderedSections,
  selectWorkspaceStructuralView,
} from "./workspaceGraph";

function makeWorkspace(overrides: Partial<NodeWorkspace>): NodeWorkspace {
  return {
    requestedRef: "specs/demo/spec.md",
    node: {
      ref: { notePath: "specs/demo/spec.md", kind: "NOTE" },
      notePath: "specs/demo/spec.md",
      title: "Demo spec",
      locator: "FILE",
    },
    content: {
      path: "specs/demo/spec.md",
      title: "Demo spec",
    },
    loaded: {
      rendered: true,
      assessment: false,
      structure: false,
      relations: true,
    },
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
    ...overrides,
  };
}

describe("workspaceGraph selectors", () => {
  it("prefers the real note node when canonicalizing relation scopes", () => {
    const noteID = "note|specs/demo/spec.md|||NOTE|";
    const fieldID = "field|note|specs/demo/spec.md|||NOTE||summary";
    const rootID = "note|specs/root.md|||NOTE|";

    const workspace = makeWorkspace({
      focusedNodeId: noteID,
      nodes: [
        {
          id: fieldID,
          kind: "field",
          ref: { notePath: "specs/demo/spec.md", kind: "NOTE" },
          notePath: "specs/demo/spec.md",
          parentId: noteID,
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
          field: {
            name: "summary",
            valueKind: "scalar",
            present: true,
            range: { start: 0, end: 1 },
          },
        },
        {
          id: noteID,
          kind: "note",
          ref: { notePath: "specs/demo/spec.md", kind: "NOTE" },
          notePath: "specs/demo/spec.md",
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
            locator: "FILE",
          },
        },
      ],
      views: {
        relationGroups: [
          {
            scopeNodeId: noteID,
            groups: [{ key: "ambient", label: "Ambient relations", items: [] }],
          },
        ],
      },
    });

    // The fallback root must differ from the requested note, or a failed
    // canonical lookup can satisfy this assertion by accident.
    workspace.focusedNodeId = rootID;
    workspace.nodes!.push({
      ...workspace.nodes![1],
      id: rootID,
      ref: { notePath: "specs/root.md", kind: "NOTE" },
      notePath: "specs/root.md",
    });
    workspace.views?.relationGroups?.unshift({
      scopeNodeId: rootID,
      groups: [{ key: "root", label: "Root relations", items: [] }],
    });

    expect(selectWorkspaceRelationGroups(workspace, "specs/demo/spec.md")).toEqual([
      { key: "ambient", label: "Ambient relations", items: [] },
    ]);
  });

  it("preserves embedded section levels when rebuilding rendered sections", () => {
    const noteID = "note|specs/demo/spec.md|||NOTE|";

    const embeddedID =
      "embedded|specs/demo/spec.md|^validation|specs/demo/spec.md#^validation|EMBEDDED|";

    const workspace = makeWorkspace({
      focusedNodeId: noteID,
      nodes: [
        {
          id: noteID,
          kind: "note",
          ref: { notePath: "specs/demo/spec.md", kind: "NOTE" },
          notePath: "specs/demo/spec.md",
          childIds: [embeddedID],
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
            locator: "FILE",
          },
        },
        {
          id: embeddedID,
          kind: "embedded",
          ref: {
            notePath: "specs/demo/spec.md",
            fragment: "^validation",
            nodeId: "specs/demo/spec.md#^validation",
            kind: "EMBEDDED",
          },
          notePath: "specs/demo/spec.md",
          parentId: noteID,
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
            title: "Validation story",
            locator: "EMBEDDED",
            level: "H4",
            markdown: "Story body",
            binding: {
              typeName: "Story",
              properties: { storyKey: "STORY-42" },
              identifierField: "storyKey",
            },
          },
        },
      ],
      views: {
        renderedOutline: {
          rootIds: [embeddedID],
        },
        structuralOutline: {
          rootId: embeddedID,
          defaultView: "structural",
        },
      },
    });

    const sections = selectWorkspaceRenderedSections(workspace, null);
    expect(sections).toHaveLength(1);
    expect(sections[0].level).toBe("H4");
    expect(sections[0].identifierField).toBe("storyKey");
    expect(selectWorkspaceStructuralView(workspace)?.root.identifierField).toBe("storyKey");
  });

  it("prefers the dedicated structural tree over mixed graph child ids", () => {
    const noteID = "note|docs/specs/product/dependency-api-cards.md|||NOTE|";

    const h1ID =
      "section|docs/specs/product/dependency-api-cards.md|dependency-api-cards-v1|docs/specs/product/dependency-api-cards.md#dependency-api-cards-v1|SECTION|";

    const summaryID =
      "section|docs/specs/product/dependency-api-cards.md|summary-329|docs/specs/product/dependency-api-cards.md#summary-329|SECTION|";

    const workspace = makeWorkspace({
      requestedRef: "docs/specs/product/dependency-api-cards.md",
      focusedNodeId: noteID,
      content: {
        path: "docs/specs/product/dependency-api-cards.md",
        title: "Dependency API Cards V1",
        structural: {
          defaultView: "structural",
          root: {
            nodeId: "docs/specs/product/dependency-api-cards.md",
            locator: "FILE",
            title: "Dependency API Cards V1",
            notePath: "docs/specs/product/dependency-api-cards.md",
            children: [
              {
                nodeId: "docs/specs/product/dependency-api-cards.md#summary-329",
                locator: "SECTION",
                title: "Summary",
                notePath: "docs/specs/product/dependency-api-cards.md",
                children: [],
              },
            ],
          },
          tabs: [],
        },
      },
      nodes: [
        {
          id: noteID,
          kind: "note",
          ref: {
            notePath: "docs/specs/product/dependency-api-cards.md",
            kind: "NOTE",
          },
          notePath: "docs/specs/product/dependency-api-cards.md",
          childIds: [h1ID, summaryID],
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
            title: "Dependency API Cards V1",
            locator: "FILE",
          },
        },
        {
          id: h1ID,
          kind: "section",
          ref: {
            notePath: "docs/specs/product/dependency-api-cards.md",
            fragment: "dependency-api-cards-v1",
            nodeId: "docs/specs/product/dependency-api-cards.md#dependency-api-cards-v1",
            kind: "SECTION",
          },
          notePath: "docs/specs/product/dependency-api-cards.md",
          parentId: noteID,
          childIds: [summaryID],
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
            title: "Dependency API Cards V1",
            level: "H1",
            locator: "SECTION",
          },
        },
        {
          id: summaryID,
          kind: "section",
          ref: {
            notePath: "docs/specs/product/dependency-api-cards.md",
            fragment: "summary-329",
            nodeId: "docs/specs/product/dependency-api-cards.md#summary-329",
            kind: "SECTION",
          },
          notePath: "docs/specs/product/dependency-api-cards.md",
          parentId: noteID,
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
          },
        },
      ],
      views: {
        structuralOutline: {
          rootId: noteID,
          defaultView: "structural",
          tabs: [],
        },
      },
    });

    const structural = selectWorkspaceStructuralView(workspace);
    expect(structural?.root.children).toHaveLength(1);
    expect(structural?.root.children?.[0]?.title).toBe("Summary");
  });
});
