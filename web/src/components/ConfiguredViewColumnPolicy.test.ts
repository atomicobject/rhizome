import { describe, expect, it } from "vitest";

import { allocateColumns, type ColumnKind, columnKind } from "./ConfiguredViewColumnPolicy";

// The Effort notes table: Title, ID, Status, Governing spec, Created.
const effortColumns: { field: string; kind: ColumnKind }[] = [
  { field: "title", kind: "title" },
  { field: "id", kind: "id" },
  { field: "status", kind: "enum" },
  { field: "governingSpec", kind: "relation" },
  { field: "createdAt", kind: "date" },
];

// Table container widths for each window width, with a 260px sidebar and
// the shell's insets; below 1200px the right pane starts collapsed.
const AT_1100 = 820;

const AT_1440_WITH_PANE = 790;

const AT_1440 = 1150;

const AT_1920_WITH_PANE = 1270;

describe("allocateColumns", () => {
  it("keeps every column at 1920 and leaves the slack to the title", () => {
    const allocation = allocateColumns(effortColumns, AT_1920_WITH_PANE, false, {});

    expect(allocation.shown).toEqual(effortColumns.map((column) => column.field));
    expect(allocation.moved).toEqual([]);
    // 30 check + 240 title + 176 + 136 + 180 + 104.
    expect(allocation.minWidth).toBe(866);
    expect(AT_1920_WITH_PANE - allocation.minWidth + 240).toBeGreaterThan(600);
  });

  it("keeps every column at 1440 without the pane", () => {
    expect(allocateColumns(effortColumns, AT_1440, false, {}).moved).toEqual([]);
  });

  it("moves the relation column first when the table is too narrow", () => {
    const allocation = allocateColumns(effortColumns, AT_1100, false, {});

    expect(allocation.moved).toEqual(["governingSpec"]);
    expect(allocation.minWidth).toBeLessThanOrEqual(AT_1100);
  });

  it("then moves the ID column, keeping the title minimum", () => {
    const allocation = allocateColumns(effortColumns, 600, false, {});

    expect(allocation.moved).toEqual(["id", "governingSpec"]);
    expect(allocation.shown).toEqual(["title", "status", "createdAt"]);
    expect(allocation.minWidth).toBe(30 + 240 + 136 + 104);
  });

  it("moves dates last and never the title or status", () => {
    const allocation = allocateColumns(effortColumns, 300, false, {});

    expect(allocation.shown).toEqual(["title", "status"]);
    expect(allocation.minWidth).toBeGreaterThan(300);
  });

  it("raises the title minimum for two-line rows", () => {
    expect(allocateColumns(effortColumns, AT_1440_WITH_PANE, false, {}).moved).toEqual([
      "governingSpec",
    ]);

    const twoLine = allocateColumns(effortColumns, AT_1440_WITH_PANE, true, {});

    expect(twoLine.moved).toEqual(["governingSpec"]);
    expect(twoLine.minWidth).toBe(30 + 320 + 176 + 136 + 104);
  });

  it("drops the rightmost column of a kind first", () => {
    const columns: { field: string; kind: ColumnKind }[] = [
      { field: "title", kind: "title" },
      { field: "owner", kind: "relation" },
      { field: "spec", kind: "relation" },
    ];

    expect(allocateColumns(columns, 30 + 240 + 180 + 10, false, {}).moved).toEqual(["spec"]);
  });

  it("uses a user-resized width in place of the fixed width", () => {
    const allocation = allocateColumns(effortColumns, AT_1920_WITH_PANE, false, {
      status: 220,
      title: 400,
    });

    expect(allocation.minWidth).toBe(30 + 400 + 176 + 220 + 180 + 104);
  });
});

describe("columnKind", () => {
  it("reads the kind from cell classes and sizes plain text like an enum", () => {
    const cell = (...kinds: string[]) =>
      ["configured-view__cell", ...kinds.map((kind) => `configured-view__cell--${kind}`)].join(" ");

    expect(columnKind(cell("identifier", "id"), false)).toBe("id");
    expect(columnKind(cell("identifier"), false)).toBe("id");
    expect(columnKind(cell("date"), false)).toBe("date");
    expect(columnKind(cell("numeric", "flag"), false)).toBe("flag");
    expect(columnKind(cell("numeric"), false)).toBe("number");
    expect(columnKind(cell("relation"), false)).toBe("relation");
    expect(columnKind(cell(), false)).toBe("enum");
    expect(columnKind(cell("relation"), true)).toBe("title");
  });
});
