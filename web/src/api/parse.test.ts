import { describe, expect, it } from "vitest";

import {
  decodeJson,
  isBoolean,
  isCallable,
  isErrorResponse,
  isFiniteNumber,
  isJsonObject,
  isJsonValue,
  isNodeEvent,
  isNodeRef,
  isString,
} from "./parse";

const ref = { notePath: "notes/a.md", kind: "NOTE" };

type GuardCase = [
  name: string,
  guard: (v: unknown) => v is unknown,
  valid: unknown[],
  invalid: unknown[],
];

const guardCases: GuardCase[] = [
  ["isString", isString, ["", "x"], [1, null, undefined, {}, []]],
  ["isFiniteNumber", isFiniteNumber, [0, -1.5], [NaN, Infinity, "1", null]],
  ["isBoolean", isBoolean, [true, false], ["true", 0, null]],
  ["isCallable", isCallable, [() => 1, Math.max, class {}], [{}, "fn", null]],
  [
    "isJsonValue",
    isJsonValue,
    [null, "s", 1, true, [], [1, "a", null], {}, { a: { b: [1, { c: null }] } }],
    [undefined, () => 1, new Date(), [undefined], { a: () => 1 }, Symbol("s")],
  ],
  [
    "isJsonObject",
    isJsonObject,
    [{}, { a: 1 }, Object.create(null), { nested: { deep: [true] } }],
    [null, [], "x", new Date(), { a: undefined }, { a: new Map() }],
  ],
  [
    "isNodeRef",
    isNodeRef,
    [ref, { ...ref, fragment: "h", nodeId: "n", startByte: 0, endByte: 10 }],
    [
      "notes/a.md",
      null,
      {},
      { notePath: "x" },
      { kind: "NOTE" },
      { ...ref, fragment: 1 },
      { ...ref, startByte: "0" },
    ],
  ],
  [
    "isNodeEvent",
    isNodeEvent,
    [
      { id: "1", kind: "changed", ref },
      { id: "1", kind: "changed", ref, version: "v", cause: "edit", changed: ["title"] },
    ],
    [
      {},
      { id: "1", kind: "changed" },
      { id: 1, kind: "changed", ref },
      { id: "1", kind: "changed", ref: "notes/a.md" },
      { id: "1", kind: "changed", ref, changed: [1] },
    ],
  ],
  [
    "isErrorResponse",
    isErrorResponse,
    [{ error: "boom" }, { error: "boom", code: "index_initializing", details: { retry: 1 } }],
    [{}, { code: "x" }, { error: 1 }, { error: "boom", code: 1 }, { error: "boom", details: "d" }],
  ],
];

describe.each(guardCases)("%s", (_name, guard, valid, invalid) => {
  it.each(valid.map((v) => [v]))("accepts %j", (v) => {
    expect(guard(v)).toBe(true);
  });
  it.each(invalid.map((v) => [v]))("rejects %j", (v) => {
    expect(guard(v)).toBe(false);
  });
});

describe("decodeJson", () => {
  it.each([
    ["object text", '{"a":1}', { a: 1 }],
    ["array text", "[1,2]", [1, 2]],
    ["null text", "null", null],
  ])("decodes %s", (_name, raw, expected) => {
    expect(decodeJson(raw, isJsonValue)).toEqual(expected);
  });

  it.each([
    ["empty string", ""],
    ["null input", null],
    ["undefined input", undefined],
    ["invalid JSON", "{not json"],
  ])("returns null for %s", (_name, raw) => {
    expect(decodeJson(raw, isJsonValue)).toBeNull();
  });

  it("returns null when the guard rejects the decoded value", () => {
    expect(decodeJson('"a string"', isJsonObject)).toBeNull();
    expect(decodeJson('{"notePath":"x"}', isNodeRef)).toBeNull();
  });

  it("returns the narrowed value when the guard accepts it", () => {
    const event = decodeJson(JSON.stringify({ id: "1", kind: "changed", ref }), isNodeEvent);
    expect(event?.ref.notePath).toBe("notes/a.md");
  });
});
