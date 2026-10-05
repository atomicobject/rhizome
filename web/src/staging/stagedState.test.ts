import { describe, expect, it } from "vitest";

import type { OntologyEditSessionResponse } from "../api/types";
import {
  changedCollections,
  changedFields,
  changeSummary,
  isTouched,
  stagedFieldValue,
} from "./stagedState";

const note = { ref: { notePath: "docs/a.md", kind: "NOTE" as const }, paths: ["docs/a.md"] };

const section = {
  ref: { notePath: "docs/a.md", fragment: "^item-1", kind: "EMBEDDED" as const },
};

function session(
  overrides: Partial<OntologyEditSessionResponse> = {},
): OntologyEditSessionResponse {
  return {
    sessionId: "s1",
    status: "dirty",
    hasUncommittedChanges: true,
    createdAt: "2026-09-23T00:00:00Z",
    updatedAt: "2026-09-23T00:00:01Z",
    ops: [],
    ...overrides,
  };
}

describe("staged state", () => {
  it("marks a field as soon as its edit is staged, before the server reports it", () => {
    const pending = session({ ops: [{ kind: "setField", path: "docs/a.md", field: "status" }] });

    expect(changedFields(pending, note)).toEqual(new Set(["status"]));
    expect(isTouched(pending, note)).toBe(true);
  });

  it.each([
    [
      "local operations",
      {
        ops: [
          { kind: "setField", path: "docs/a.md", field: "summary" },
          { kind: "setField", path: "docs/a.md#^item-1", field: "status" },
        ],
      },
    ],
    [
      "server map",
      { changedFieldsByNode: { "docs/a.md": ["summary"], "docs/a.md#^item-1": ["status"] } },
    ],
  ])("keeps %s field changes on their addressed node", (_name, changes) => {
    const staged = session(changes);

    expect(changedFields(staged, note)).toEqual(new Set(["summary"]));
    expect(changedFields(staged, section)).toEqual(new Set(["status"]));
  });

  it("reads the latest staged value, with unset as undefined", () => {
    const staged = session({
      ops: [
        {
          kind: "setField",
          path: "docs/a.md",
          field: "status",
          fieldValue: { kind: "scalar", scalar: "draft" },
        },
        { kind: "setField", path: "docs/a.md", field: "status", fieldValue: { kind: "unset" } },
      ],
    });

    expect(stagedFieldValue(staged, note, "status")).toEqual({ value: undefined });
    expect(stagedFieldValue(staged, note, "title")).toBeNull();
  });

  it("gives a note the collection changes addressed inside it", () => {
    const staged = session({
      collectionChanges: [{ path: "docs/a.md#Stories", collection: "stories" }],
    });

    expect(changedCollections(staged, note)).toEqual(new Set(["stories"]));
    expect(changedCollections(staged, section)).toEqual(new Set());
  });

  it("summarizes the session once for every badge", () => {
    const staged = session({
      touchedNodes: ["docs/a.md"],
      touchedPaths: ["docs/a.md"],
      changedFieldsByNode: { "docs/a.md": ["status", "summary"] },
      ops: [
        { kind: "setField", path: "docs/a.md", field: "status" },
        { kind: "setSource", path: "docs/a.md" },
      ],
    });

    expect(changeSummary(staged)).toEqual({
      nodeCount: 1,
      fieldCount: 2,
      touchedPaths: new Set(["docs/a.md"]),
      opCount: 2,
      sourceFileCount: 1,
    });
  });

  it("counts notes edited locally before the server acknowledges them", () => {
    const local = session({
      ops: [{ kind: "setField", path: "docs/b.md#^item-2", field: "status" }],
    });

    expect(changeSummary(local).touchedPaths).toEqual(new Set(["docs/b.md"]));
    expect(changeSummary(local).nodeCount).toBe(1);
    expect(changeSummary(local).fieldCount).toBe(1);
  });
});

it("shows a newer canonical edit on the retained row whose byte-offset identity shifted at Save", () => {
  const original = {
    notePath: "docs/a.md",
    fragment: "item-100",
    nodeId: "docs/a.md#item-100",
    kind: "EMBEDDED",
    structuralFingerprint: "old-item",
  };

  const preview = {
    ...original,
    fragment: "item-130",
    nodeId: "docs/a.md#item-130",
    structuralFingerprint: "saved-item",
  };

  const staged = session({
    refLineage: [{ original, preview }],
    ops: [
      {
        kind: "setField",
        path: "docs/a.md#item-130",
        nodeId: preview.nodeId,
        structuralFingerprint: preview.structuralFingerprint,
        field: "status",
        value: "blocked",
      },
    ],
  });

  expect(
    stagedFieldValue(staged, { ref: original, paths: ["docs/a.md#item-100"] }, "status"),
  ).toEqual({ value: "blocked" });
  expect(changedFields(staged, { ref: original })).toEqual(new Set(["status"]));
  // A different strong witness at the old offset never inherits the saved mapping.
  expect(
    stagedFieldValue(
      staged,
      { ref: { ...original, structuralFingerprint: "different-item" } },
      "status",
    ),
  ).toBeNull();
});
