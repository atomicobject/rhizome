import { expect, it } from "vitest";

import type { OntologyEditOp } from "../../api/types";
import {
  operationKey,
  rebaseCommittedFieldWitnesses,
  remapCommittedEditOps,
} from "./editSessionState";

it("rebases a saved legacy empty relation while preserving other expected witnesses", () => {
  const saved: OntologyEditOp = {
    kind: "setLinkField",
    path: "a.md#item-100",
    nodeId: "a.md#item-100",
    structuralFingerprint: "verified",
    field: "assignees",
  };

  const newer: OntologyEditOp = {
    ...saved,
    id: "newer-relation",
    values: ["Drew"],
    expected: {
      field: { kind: "list", items: ["Previous"] },
      sourceHash: "verified-source",
      sourceContent: "verified content",
    },
  };

  expect(rebaseCommittedFieldWitnesses([newer], [saved])).toEqual([
    { ...newer, expected: { ...newer.expected, field: { kind: "list", items: [] } } },
  ]);
  expect(newer.expected?.field).toEqual({ kind: "list", items: ["Previous"] });
});

it("chains only exact committed strong refs while preserving operation and source witnesses", () => {
  const original = {
    notePath: "a.md",
    fragment: "item-100",
    nodeId: "a.md#item-100",
    kind: "EMBEDDED",
    structuralFingerprint: "first",
  };

  const middle = {
    ...original,
    fragment: "item-130",
    nodeId: "a.md#item-130",
    structuralFingerprint: "second",
  };

  const final = {
    ...middle,
    fragment: "item-150",
    nodeId: "a.md#item-150",
    structuralFingerprint: "third",
  };

  const op: OntologyEditOp = {
    id: "field-status",
    kind: "setField",
    path: "a.md#item-100",
    nodeId: original.nodeId,
    structuralFingerprint: original.structuralFingerprint,
    field: "status",
    value: "blocked",
    expected: { field: { kind: "scalar", scalar: "done" }, sourceHash: "verified-source" },
  };

  const lineage = [
    { original, preview: middle },
    { original: middle, preview: final },
  ];

  expect(remapCommittedEditOps([op], lineage)).toEqual([
    {
      ...op,
      path: "a.md#item-150",
      nodeId: final.nodeId,
      structuralFingerprint: final.structuralFingerprint,
    },
  ]);
  expect(op.nodeId).toBe(original.nodeId);

  for (const id of [undefined, ""]) {
    const anonymous = { ...op, id };
    expect(remapCommittedEditOps([anonymous], lineage)[0].id).toBe(operationKey(anonymous));
  }

  const unmapped = [
    { ...op, structuralFingerprint: undefined },
    { ...op, nodeId: "a.md#different" },
    { ...op, path: "b.md#item-100" },
    { ...op, kind: "setSource" },
  ];

  expect(remapCommittedEditOps(unmapped, lineage)).toEqual(unmapped);
  expect(
    remapCommittedEditOps([op], [{ original, preview: { ...middle, notePath: "other.md" } }]),
  ).toEqual([op]);
});
