import { describe, expect, it } from "vitest";

import {
  NATIVE_EXPANSION_KEYS,
  withExpansionChoice,
  type ExpansionChoices,
} from "./useRememberedExpansion";

// The server's limits (pkg/app/userstate): 64 KiB per value, 128 KiB per instance.
const MAX_VALUE_BYTES = 64 * 1024;

const MAX_INSTANCE_BYTES = 128 * 1024;

const bytes = (value: ExpansionChoices) => new TextEncoder().encode(JSON.stringify(value)).length;

describe("remembered expansion", () => {
  it("keeps every renderer's choices storable when group keys are long multibyte text", () => {
    // Group keys can carry whole field values; each of these is about 600 UTF-8 bytes.
    const longKey = (index: number) => `${"界".repeat(200)}-${index}`;
    const values: Record<string, ExpansionChoices> = {};

    for (const key of NATIVE_EXPANSION_KEYS) {
      let choices: ExpansionChoices = {};

      for (let grouping = 0; grouping < 8; grouping++)
        for (let index = 0; index < 400; index++)
          choices = withExpansionChoice(choices, `status:${grouping}`, longKey(index), false);

      values[key] = withExpansionChoice(choices, "status:0", "newest", true);
      expect(bytes(values[key])).toBeLessThanOrEqual(MAX_VALUE_BYTES);
      expect(values[key]["status:0"]?.newest).toBe(true);
      expect(values[key]["status:7"]?.[longKey(399)]).toBe(false);
      expect(values[key]["status:1"]).toBeUndefined();
    }

    const stored = Object.entries(values).reduce(
      (total, [key, value]) => total + key.length + bytes(value),
      0,
    );

    // Leaves room for the instance's columns, widths, filters, and sorting.
    expect(stored).toBeLessThanOrEqual(MAX_INSTANCE_BYTES / 2);
  });

  it("drops a single key too long to store instead of every other choice", () => {
    const choices = withExpansionChoice({}, "status", "active", false);
    const next = withExpansionChoice(choices, "status", "界".repeat(20_000), true);

    expect(next).toEqual({ status: { active: false } });
  });
});
