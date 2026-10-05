import { describe, expect, it } from "vitest";

import type { WorkspaceContentNode, WorkspaceFieldNode } from "../../api/types";
import { extractChildIdentity } from "./childIdentity";

function child(properties: Record<string, string>, identifierField?: string): WorkspaceContentNode {
  return {
    id: "node|story",
    kind: "embedded",
    ref: {
      notePath: "specs/demo.md",
      fragment: "^story",
      nodeId: "story",
      kind: "EMBEDDED",
    },
    notePath: "specs/demo.md",
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
    data: {
      title: "Story",
      binding: { typeName: "Story", properties, identifierField },
    },
  };
}

function field(owner: WorkspaceContentNode, name: string, value: string): WorkspaceFieldNode {
  return {
    id: `field|${owner.id}|${name}`,
    kind: "field",
    ref: owner.ref,
    notePath: owner.notePath,
    parentId: owner.id,
    status: owner.status,
    capabilities: owner.capabilities,
    field: {
      name,
      valueKind: "scalar",
      present: true,
      values: [value],
      range: { start: 0, end: value.length },
    },
  };
}

describe("extractChildIdentity", () => {
  it("uses the schema-selected identifier case-insensitively before generic id", () => {
    const preferred = child({ id: "legacy-id", StoryKey: "STORY-42" }, "storykey");
    const fallback = child({ id: "legacy-id" });
    const workspace = { nodes: [preferred] };

    expect(extractChildIdentity(preferred, workspace).id).toBe("STORY-42");
    expect(extractChildIdentity(fallback, workspace).id).toBe("legacy-id");
  });

  it("uses the schema-selected identifier from focused field nodes", () => {
    const preferred = child({}, "storyKey");

    const workspace = {
      nodes: [
        preferred,
        field(preferred, "id", "legacy-id"),
        field(preferred, "STORYKEY", "STORY-43"),
      ],
    };

    expect(extractChildIdentity(preferred, workspace).id).toBe("STORY-43");
  });
});
