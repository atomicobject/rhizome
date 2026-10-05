import { describe, expect, it } from "vitest";

import type { ViewTableRow } from "../api/types";
import {
  type OrderKey,
  type PlacementRequest,
  placementOps,
  placementValues,
  shortestBetween,
  sortRowsByKeys,
} from "./viewOrdering";

function row(name: string, fields: ViewTableRow["fields"] = {}): ViewTableRow {
  return { ref: { kind: "NOTE", notePath: `${name}.md` }, path: `${name}.md`, title: name, fields };
}

const rank = (kind: "real" | "int" = "real", descending = false): OrderKey => ({
  field: "rank",
  editField: "rank",
  kind,
  descending,
  indexed: true,
});

const priority: OrderKey = {
  field: "priority",
  editField: "priority",
  kind: "enum",
  descending: false,
  indexed: true,
};

/** `name -> field -> value` for compact assertions. */
function place(request: PlacementRequest) {
  const out: Record<string, Record<string, string | null>> = {};

  for (const [key, values] of placementValues(request)) {
    out[key.replace(/\.md:$/, "")] = Object.fromEntries(values);
  }

  return out;
}

describe("placementValues", () => {
  const a = row("a", { rank: 1 });
  const b = row("b", { rank: 2 });
  const c = row("c", { rank: 3 });

  it("takes the midpoint between real neighbors", () => {
    expect(
      place({
        keys: [rank()],
        moving: c,
        groupRows: [a, b, c],
        target: b,
        side: "before",
      }),
    ).toEqual({
      c: { rank: "1.5" },
    });
  });

  it("steps past a single neighbor at either end", () => {
    expect(
      place({
        keys: [rank()],
        moving: c,
        groupRows: [a, b, c],
        target: a,
        side: "before",
      }),
    ).toEqual({
      c: { rank: "0" },
    });
    expect(
      place({
        keys: [rank()],
        moving: a,
        groupRows: [a, b, c],
        target: null,
        side: "after",
      }),
    ).toEqual({
      a: { rank: "4" },
    });
  });

  it("stages nothing for a drop on the item itself", () => {
    for (const side of ["before", "after"] as const) {
      expect(place({ keys: [rank()], moving: a, groupRows: [a, b, c], target: a, side })).toEqual(
        {},
      );
    }
  });

  it("renumbers integer ranks too large to place between exactly", () => {
    const big = row("big", { rank: "9007199254740993" });
    const bigger = row("bigger", { rank: "9007199254740995" });
    const moving = row("m", { rank: 1 });

    expect(
      place({
        keys: [rank("int")],
        moving,
        groupRows: [moving, big, bigger],
        target: bigger,
        side: "before",
      }),
    ).toEqual({ big: { rank: "1" }, m: { rank: "2" }, bigger: { rank: "3" } });
    expect(
      place({
        keys: [rank("int")],
        moving,
        groupRows: [moving, big, bigger],
        target: bigger,
        side: "after",
      }),
    ).toEqual({ big: { rank: "1" }, bigger: { rank: "2" }, m: { rank: "3" } });
  });

  it("renumbers when a real step past a huge neighbor would not change it", () => {
    const huge = row("huge", { rank: 1e300 });
    const moving = row("m", { rank: 1 });

    expect(
      place({ keys: [rank()], moving, groupRows: [moving, huge], target: huge, side: "after" }),
    ).toEqual({ huge: { rank: "1" }, m: { rank: "2" } });
  });

  it("stages nothing for a drop at the item's own position", () => {
    expect(
      place({
        keys: [rank()],
        moving: b,
        groupRows: [a, b, c],
        target: a,
        side: "after",
      }),
    ).toEqual({});
    expect(
      place({
        keys: [rank()],
        moving: b,
        groupRows: [a, b, c],
        target: c,
        side: "before",
      }),
    ).toEqual({});
  });

  it("follows descending sorts", () => {
    const high = row("high", { rank: 9 });
    const low = row("low", { rank: 5 });
    const moving = row("m", { rank: 1 });

    expect(
      place({
        keys: [rank("real", true)],
        moving,
        groupRows: [high, low, moving],
        target: high,
        side: "before",
      }),
    ).toEqual({ m: { rank: "10" } });
    expect(
      place({
        keys: [rank("real", true)],
        moving,
        groupRows: [high, low, moving],
        target: low,
        side: "before",
      }),
    ).toEqual({ m: { rank: "7" } });
  });

  it("uses the integer midpoint and renumbers adjacent integers", () => {
    const x = row("x", { rank: 10 });
    const y = row("y", { rank: 20 });
    const moving = row("m", { rank: 30 });

    expect(
      place({
        keys: [rank("int")],
        moving,
        groupRows: [x, y, moving],
        target: y,
        side: "before",
      }),
    ).toEqual({ m: { rank: "15" } });
    expect(
      place({
        keys: [rank("int")],
        moving: c,
        groupRows: [a, b, c],
        target: b,
        side: "before",
      }),
    ).toEqual({
      b: { rank: "3" },
      c: { rank: "2" },
    });
  });

  it("renumbers when the item lands among unnumbered rows", () => {
    const p = row("p");
    const q = row("q");
    const r = row("r");

    expect(
      place({
        keys: [rank()],
        moving: r,
        groupRows: [p, q, r],
        target: q,
        side: "before",
      }),
    ).toEqual({
      p: { rank: "1" },
      r: { rank: "2" },
    });
    expect(
      place({
        keys: [rank()],
        moving: r,
        groupRows: [p, q, r],
        target: p,
        side: "before",
      }),
    ).toEqual({
      r: { rank: "1" },
    });
  });

  it("keeps a value that already fits", () => {
    const moving = row("m", { rank: 2.5 });

    expect(
      place({
        keys: [rank()],
        moving,
        groupRows: [a, b, moving, c],
        target: c,
        side: "after",
      }),
    ).toEqual({
      m: { rank: "4" },
    });
    expect(
      place({
        keys: [rank()],
        moving,
        groupRows: [a, moving, b, c],
        target: c,
        side: "before",
      }),
    ).toEqual({});
  });

  it("takes the anchor's enum value and places within that run", () => {
    const h1 = row("h1", { priority: "high", rank: 1 });
    const h2 = row("h2", { priority: "high", rank: 2 });
    const m1 = row("m1", { priority: "medium", rank: 1 });
    const moving = row("x", { priority: "low", rank: 1 });
    const groupRows = [h1, h2, m1, moving];

    expect(
      place({
        keys: [priority, rank()],
        moving,
        groupRows,
        target: h2,
        side: "after",
      }),
    ).toEqual({
      x: { priority: "high", rank: "3" },
    });
    expect(
      place({
        keys: [priority, rank()],
        moving,
        groupRows,
        target: m1,
        side: "before",
      }),
    ).toEqual({
      x: { priority: "medium", rank: "0" },
    });
    expect(
      place({
        keys: [priority, rank()],
        moving,
        groupRows,
        target: h1,
        side: "after",
      }),
    ).toEqual({
      x: { priority: "high", rank: "1.5" },
    });
  });

  it("treats enum values the server sorts as equal as one run", () => {
    const first = row("first", { priority: "high" });
    const second = row("second", { priority: " High " });
    const moving = row("x", { priority: "[[HIGH]]" });

    expect(
      place({
        keys: [priority],
        moving,
        groupRows: [first, second, moving],
        target: second,
        side: "before",
      }),
    ).toEqual({});
  });

  it("renumbers across enum values the server sorts as equal", () => {
    const first = row("first", { priority: "high", rank: 1 });
    const second = row("second", { priority: " High ", rank: 2 });
    const moving = row("x", { priority: "high", rank: 5 });

    expect(
      place({
        keys: [priority, rank("int")],
        moving,
        groupRows: [first, second, moving],
        target: second,
        side: "before",
      }),
    ).toEqual({ x: { rank: "2" }, second: { rank: "3" } });
  });

  it("treats a boolean key like an enum", () => {
    const done = row("done", { done: "false" });
    const moving = row("x", { done: "true" });

    const key: OrderKey = {
      field: "done",
      editField: "done",
      kind: "bool",
      descending: false,
      indexed: false,
    };

    expect(
      place({
        keys: [key],
        moving,
        groupRows: [done, moving],
        target: done,
        side: "before",
      }),
    ).toEqual({
      x: { done: "false" },
    });
  });
});

describe("placementOps", () => {
  it("builds one setField op per changed value with a witness", () => {
    const a = row("a", { rank: 1 });
    const b = row("b", { rank: 2 });

    const ops = placementOps({
      keys: [rank()],
      moving: b,
      groupRows: [a, b],
      target: a,
      side: "before",
    });

    expect(ops).toEqual([
      expect.objectContaining({
        kind: "setField",
        path: "b.md",
        field: "rank",
        value: "0",
        expected: { field: { kind: "scalar", scalar: "2" } },
      }),
    ]);
  });
});

describe("shortestBetween", () => {
  it("prefers whole numbers and short decimals near the midpoint", () => {
    expect(shortestBetween(1, 5)).toBe(3);
    expect(shortestBetween(1, 2)).toBe(1.5);
    expect(shortestBetween(1, 1.5)).toBe(1.3);
    expect(shortestBetween(2, 2)).toBeUndefined();
  });
});

describe("sortRowsByKeys", () => {
  it("orders indexed enums by normalized text, as the server does, with missing values last", () => {
    const rows = [
      row("none"),
      row("medium", { priority: "medium", rank: 1 }),
      row("low", { priority: " Low ", rank: 1 }),
      row("high2", { priority: "high", rank: 2 }),
      row("high1", { priority: "[[High|top]]", rank: 1 }),
    ];

    expect(sortRowsByKeys(rows, [priority, rank()]).map((r) => r.title)).toEqual([
      "high1",
      "high2",
      "low",
      "medium",
      "none",
    ]);
    expect(
      sortRowsByKeys(rows, [{ ...priority, descending: true }, rank()]).map((r) => r.title),
    ).toEqual(["medium", "low", "high1", "high2", "none"]);
  });

  it("sorts missing booleans first ascending and last descending", () => {
    const done: OrderKey = {
      field: "done",
      editField: "done",
      kind: "bool",
      descending: false,
      indexed: false,
    };

    const rows = [row("yes", { done: true }), row("none"), row("no", { done: false })];

    expect(sortRowsByKeys(rows, [done]).map((r) => r.title)).toEqual(["none", "no", "yes"]);
    expect(sortRowsByKeys(rows, [{ ...done, descending: true }]).map((r) => r.title)).toEqual([
      "yes",
      "no",
      "none",
    ]);
  });
});
