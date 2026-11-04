import { describe, expect, it } from "vitest";

import type { ViewTableRow } from "../api/types";
import { repeatedColumnFields } from "./useViewLayout";

const columns = ["title", "resolvedType", "status", "owner"].map((field) => ({
  field,
  label: field,
}));

function row(index: number, owner: string): ViewTableRow {
  return {
    ref: { notePath: `n${index}.md`, kind: "NOTE" },
    path: `n${index}.md`,
    title: `Note ${index}`,
    resolvedType: "EffortNote",
    fields: { status: "active", owner },
  };
}

function execution(rows: ViewTableRow[], kinds: number[], total = rows.length) {
  return {
    rows,
    stats: {
      total,
      issueCount: 0,
      staleCount: 0,
      kinds: kinds.map((count, index) => ({ value: `T${index}`, count })),
    },
  };
}

describe("repeatedColumnFields", () => {
  const rows = Array.from({ length: 20 }, (_, index) => row(index, index < 19 ? "Ann" : "Bo"));

  it("hides a column one value fills on more than 90% of rows, never the title or lifecycle", () => {
    expect(repeatedColumnFields(columns, execution(rows, [19, 1]), "status")).toEqual([
      "resolvedType",
      "owner",
    ]);
  });

  it("keeps columns with real variety, small sets, and rows not yet loaded", () => {
    expect(repeatedColumnFields(columns, execution(rows, [12, 8]), "status")).toEqual(["owner"]);
    expect(repeatedColumnFields(columns, execution(rows.slice(0, 5), [5]), "status")).toEqual([]);
    expect(repeatedColumnFields(columns, execution(rows, [10, 10], 40), "status")).toEqual([]);
  });
});
