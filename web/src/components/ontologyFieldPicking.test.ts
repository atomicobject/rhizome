import { describe, expect, it } from "vitest";

import type { WorkspaceFieldNode } from "../api/types";
import { buildFieldEditOp, pickIdentityFields } from "./ontologyFieldPicking";

function singularRelationField(): WorkspaceFieldNode {
  const ref = { notePath: "notes/tasks.md", kind: "NOTE" as const, typeName: "Task" };

  return {
    id: "field:owner",
    kind: "field",
    ref,
    notePath: ref.notePath,
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
      canEditCollections: false,
      canNavigateChildren: false,
      canSubscribe: true,
    },
    field: {
      name: "owner",
      valueKind: "scalar",
      present: true,
      values: ["[[notes/people/alice]]"],
      range: { start: 0, end: 0 },
      capability: {
        ownerRef: ref,
        ownerType: "Task",
        typeName: "Person",
        valueKind: "relation",
        targetType: "Person",
        list: false,
        required: false,
        enumValues: [],
        valueOrigin: "authored",
        identifier: false,
        preferredIdentifier: false,
        displayImportance: "NORMAL",
        writeOperation: "setLinkField",
      },
    },
  };
}

function enumField(
  name: string,
  displayImportance: "KEY" | "NORMAL" | "DETAIL",
): WorkspaceFieldNode {
  const field = singularRelationField();

  return {
    ...field,
    id: `field:${name}`,
    field: {
      ...field.field,
      name,
      values: ["active"],
      enumValues: ["active", "complete"],
      capability: {
        ...field.field.capability!,
        typeName: "Status",
        valueKind: "enum",
        targetType: undefined,
        enumValues: ["active", "complete"],
        displayImportance,
        writeOperation: "setField",
      },
    },
  };
}

describe("buildFieldEditOp", () => {
  it("uses list witnesses for singular relations", () => {
    const op = buildFieldEditOp("notes/tasks.md", undefined, singularRelationField(), {
      kind: "scalar",
      scalar: "[[notes/people/bob]]",
    });

    expect(op).toMatchObject({
      kind: "setLinkField",
      values: ["[[notes/people/bob]]"],
      expected: { field: { kind: "list", items: ["[[notes/people/alice]]"] } },
    });
  });
});

describe("pickIdentityFields", () => {
  it("prefers a key status enum among status fields", () => {
    const reviewStatus = enumField("reviewStatus", "NORMAL");
    const approvalStatus = enumField("approvalStatus", "KEY");
    const sourceKind = enumField("sourceKind", "KEY");
    const normal = enumField("kind", "NORMAL");

    const identity = pickIdentityFields([reviewStatus, sourceKind, approvalStatus, normal]);

    expect(identity.status).toBe(approvalStatus);
    expect(identity.others).toEqual([reviewStatus, sourceKind, normal]);
  });

  it("does not consume a key enum when no status-named enum exists", () => {
    const sourceKind = enumField("sourceKind", "KEY");

    const identity = pickIdentityFields([sourceKind]);

    expect(identity.status).toBeNull();
    expect(identity.others).toEqual([sourceKind]);
  });
});
